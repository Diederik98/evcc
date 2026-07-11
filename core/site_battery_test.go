package core

import (
	"errors"
	"testing"
	"time"

	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/types"
	"github.com/evcc-io/evcc/util"
	"github.com/evcc-io/evcc/util/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

// TestBatterySocRetainOnReadError guards that a failed soc read keeps the last
// known soc instead of reporting the pack as empty (discussion #26560).
func TestBatterySocRetainOnReadError(t *testing.T) {
	ctrl := gomock.NewController(t)

	meter := api.NewMockMeter(ctrl)
	meter.EXPECT().CurrentPower().Return(0.0, nil).AnyTimes()

	battery := api.NewMockBattery(ctrl)
	battery.EXPECT().Soc().Return(0.0, errors.New("read failed")).AnyTimes()

	var bat api.Meter = &struct {
		api.Meter
		api.Battery
	}{
		Meter:   meter,
		Battery: battery,
	}

	site := &Site{
		log:           util.NewLogger("foo"),
		batteryMeters: []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
	}
	site.battery.Soc = 84

	site.updateBatteryMeters()

	assert.Equal(t, 84.0, site.battery.Soc, "soc retained when the read fails")
}

func TestApplyBatteryMode(t *testing.T) {
	for _, tc := range []struct {
		internal, expected api.BatteryMode
	}{
		{api.BatteryUnknown, api.BatteryUnknown}, // no change required
		{api.BatteryNormal, api.BatteryUnknown},  // no change required
		{api.BatteryHold, api.BatteryNormal},
		{api.BatteryCharge, api.BatteryNormal},
	} {
		t.Logf("%+v", tc)

		ctrl := gomock.NewController(t)

		var bat api.Meter
		batCon := api.NewMockBatteryController(ctrl)

		bat = &struct {
			api.Meter
			api.BatteryController
		}{
			BatteryController: batCon,
		}

		site := &Site{
			log:           util.NewLogger("foo"),
			batteryMeters: []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
			batteryMode:   tc.internal,
		}

		// verify mode applied to battery
		if tc.expected != api.BatteryUnknown {
			batCon.EXPECT().SetBatteryMode(tc.expected).Times(1)
		}
		site.updateBatteryMode(false, api.Rate{})

		if tc.internal != api.BatteryNormal {
			assert.Equal(t, tc.expected, site.batteryMode)
		}

		ctrl.Finish()
	}
}

func TestRequiredExternalBatteryMode(t *testing.T) {
	for _, tc := range []struct {
		internal, external, new api.BatteryMode
	}{
		{api.BatteryUnknown, api.BatteryUnknown, api.BatteryUnknown},
		{api.BatteryUnknown, api.BatteryNormal, api.BatteryNormal},
		{api.BatteryUnknown, api.BatteryCharge, api.BatteryCharge},

		{api.BatteryNormal, api.BatteryUnknown, api.BatteryUnknown},
		{api.BatteryNormal, api.BatteryNormal, api.BatteryUnknown}, // no change required
		{api.BatteryNormal, api.BatteryCharge, api.BatteryCharge},

		{api.BatteryCharge, api.BatteryUnknown, api.BatteryNormal},
		{api.BatteryCharge, api.BatteryNormal, api.BatteryNormal},
		{api.BatteryCharge, api.BatteryCharge, api.BatteryUnknown}, // no change required
	} {
		t.Logf("%+v", tc)

		site := &Site{
			log:           util.NewLogger("foo"),
			batteryMeters: []config.Device[api.Meter]{nil},
		}

		site.batteryMode = tc.internal
		site.batteryModeExternal = tc.external

		mode := site.requiredBatteryMode(false, api.Rate{})
		assert.Equal(t, tc.new.String(), mode.String(), "internal mode expected %s got %s", tc.new, mode)
	}
}

func TestExternalBatteryModeChange(t *testing.T) {
	for _, tc := range []struct {
		internal, external, expected api.BatteryMode
	}{
		{api.BatteryUnknown, api.BatteryUnknown, api.BatteryUnknown},
		{api.BatteryUnknown, api.BatteryNormal, api.BatteryNormal},
		{api.BatteryUnknown, api.BatteryCharge, api.BatteryCharge},

		{api.BatteryNormal, api.BatteryUnknown, api.BatteryUnknown},
		{api.BatteryNormal, api.BatteryNormal, api.BatteryUnknown},
		{api.BatteryNormal, api.BatteryCharge, api.BatteryCharge},

		{api.BatteryHold, api.BatteryUnknown, api.BatteryNormal}, // return to normal
		{api.BatteryHold, api.BatteryNormal, api.BatteryNormal},
		{api.BatteryHold, api.BatteryHold, api.BatteryUnknown},

		{api.BatteryCharge, api.BatteryUnknown, api.BatteryNormal}, // return to normal
		{api.BatteryCharge, api.BatteryNormal, api.BatteryNormal},
		{api.BatteryCharge, api.BatteryCharge, api.BatteryUnknown},
	} {
		t.Logf("%+v", tc)

		ctrl := gomock.NewController(t)

		var bat api.Meter
		batCon := api.NewMockBatteryController(ctrl)

		bat = &struct {
			api.Meter
			api.BatteryController
		}{
			BatteryController: batCon,
		}

		site := &Site{
			log:           util.NewLogger("foo"),
			batteryMeters: []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
			batteryMode:   tc.internal,
		}

		// 1. set required external mode
		site.SetBatteryModeExternal(tc.external)
		assert.Equal(t, site.batteryModeExternal, tc.external, "external mode expected %s got %s", tc.external, site.batteryModeExternal)
		assert.Equal(t, site.batteryMode, tc.internal, "internal mode expected unchanged %s got %s", tc.internal, site.batteryMode)

		// 2. verify external mode applied to battery
		if tc.expected != api.BatteryUnknown {
			batCon.EXPECT().SetBatteryMode(tc.expected).Times(1)
		}
		site.updateBatteryMode(false, api.Rate{})
		if !ctrl.Satisfied() {
			ctrl.Finish()
		}

		// 3. verify required external mode only applied once
		site.updateBatteryMode(false, api.Rate{})
		if !ctrl.Satisfied() {
			ctrl.Finish()
		}

		// 4. verify timer expiry
		site.batteryModeExternalTimer = site.batteryModeExternalTimer.Add(-time.Hour)
		site.batteryModeWatchdogExpired()

		// mode reverted to unknown, timer still active
		assert.Equal(t, site.batteryModeExternal, api.BatteryUnknown)
		assert.False(t, site.batteryModeExternalTimer.IsZero())

		// battery switched back to normal mode
		batCon.EXPECT().SetBatteryMode(api.BatteryNormal).Times(1)
		site.updateBatteryMode(false, api.Rate{})

		// timer disabled
		assert.True(t, site.batteryModeExternalTimer.IsZero())

		ctrl.Finish()
	}
}

func TestForcedBatteryChargeLimits(t *testing.T) {
	limit := 80.0

	for _, tc := range []struct {
		internal, expected api.BatteryMode
		soc                float64
	}{
		{api.BatteryUnknown, api.BatteryCharge, 50},
		{api.BatteryUnknown, api.BatteryHold, 90},

		{api.BatteryNormal, api.BatteryCharge, 50},
		{api.BatteryNormal, api.BatteryHold, 90},

		{api.BatteryHold, api.BatteryCharge, 50},
		{api.BatteryHold, api.BatteryHold, 90}, // TODO make this api.BatteryUnknown

		{api.BatteryCharge, api.BatteryUnknown, 50},
		{api.BatteryCharge, api.BatteryHold, 90},
	} {
		t.Logf("%+v", tc)

		ctrl := gomock.NewController(t)

		var bat api.Meter
		batSoc := api.NewMockBattery(ctrl)
		batCon := api.NewMockBatteryController(ctrl)
		batSocLimit := api.NewMockBatterySocLimiter(ctrl)

		bat = &struct {
			api.Meter
			api.Battery
			api.BatteryController
			api.BatterySocLimiter
		}{
			Meter:             bat,
			Battery:           batSoc,
			BatteryController: batCon,
			BatterySocLimiter: batSocLimit,
		}

		site := &Site{
			log:           util.NewLogger("foo"),
			batteryMeters: []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
			batteryMode:   tc.internal,
		}

		batSoc.EXPECT().Soc().Return(tc.soc, nil).Times(1)
		batSocLimit.EXPECT().GetSocLimits().Return(0.0, limit).Times(1)

		if tc.expected != api.BatteryUnknown {
			batCon.EXPECT().SetBatteryMode(tc.expected).Times(1)
		}

		site.updateBatteryMode(true, api.Rate{})

		ctrl.Finish()
	}
}

func newLimitBat(ctrl *gomock.Controller, limitCtrl api.BatteryLimitController) api.Meter {
	meter := api.NewMockMeter(ctrl)
	meter.EXPECT().CurrentPower().Return(0.0, nil).AnyTimes()
	type fullBat struct {
		api.Meter
		api.BatteryLimitController
	}
	return &fullBat{Meter: meter, BatteryLimitController: limitCtrl}
}

func TestManageGridLimitsIdle(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		gridPower:                       5000.0,
		battery:                         types.BatteryState{Soc: 60.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveIdle, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsShaving(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(0).Return(nil).Times(1)
	limitCtrl.EXPECT().SetDischargeLimit(5000).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		PeakShaveLoadShedDelay:          30.0,
		gridPower:                       15000.0,
		battery:                         types.BatteryState{Soc: 80.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveActive, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsSheddingAfterDelay(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(0).Return(nil).Times(1)
	limitCtrl.EXPECT().SetDischargeLimit(5000).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		PeakShaveLoadShedDelay:          30.0,
		peakShaveOverloadSince:          time.Now().Add(-31 * time.Second),
		gridPower:                       15000.0,
		battery:                         types.BatteryState{Soc: 80.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveShedding, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsImmediateShedding(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(0).Return(nil).Times(1)
	limitCtrl.EXPECT().SetDischargeLimit(5000).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		PeakShaveLoadShedDelay:          0,
		gridPower:                       15000.0,
		battery:                         types.BatteryState{Soc: 80.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveShedding, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsCritical(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(0).Return(nil).Times(1)
	limitCtrl.EXPECT().SetDischargeLimit(5000).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		PeakShaveLoadShedDelay:          30.0,
		gridPower:                       15000.0,
		battery:                         types.BatteryState{Soc: 30.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveCritical, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsHardLockout(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetDischargeLimit(0).Return(nil).Times(1)
	limitCtrl.EXPECT().SetChargeLimit(0).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		gridPower:                       15000.0,
		battery:                         types.BatteryState{Soc: 15.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveLockout, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsRecovery(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(1000).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		gridPower:                       3000.0,
		battery:                         types.BatteryState{Soc: 25.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveRecovery, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsRecoveryHeadroomCap(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(500).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		gridPower:                       9500.0,
		battery:                         types.BatteryState{Soc: 25.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveRecovery, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsGridChargeHeadroom(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(2000).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   10.0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		gridPower:                       8000.0,
		battery:                         types.BatteryState{Soc: 60.0},
	}

	site.ManageGridLimits(true)
	assert.Equal(t, PeakShaveIdle, site.peakShaveState)
	assert.True(t, site.peakShaveBatteryLimited)
	ctrl.Finish()
}

func TestManageGridLimitsDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)

	limitCtrl := api.NewMockBatteryLimitController(ctrl)
	limitCtrl.EXPECT().SetChargeLimit(0).Return(nil).Times(1)
	limitCtrl.EXPECT().SetDischargeLimit(0).Return(nil).Times(1)
	bat := newLimitBat(ctrl, limitCtrl)

	site := &Site{
		log:                             util.NewLogger("ps"),
		batteryMeters:                   []config.Device[api.Meter]{config.NewStaticDevice(config.Named{}, bat)},
		GridThreshold:                   0,
		PeakShaveReserveSoc:             40.0,
		PeakShaveMinSoc:                 20.0,
		PeakShaveMaintainSocChargePower: 1000.0,
		peakShaveBatteryLimited:         true,
		gridPower:                       15000.0,
		battery:                         types.BatteryState{Soc: 80.0},
	}

	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveIdle, site.peakShaveState)
	ctrl.Finish()
}

func TestManageGridLimitsNoBatteries(t *testing.T) {
	site := &Site{
		log:           util.NewLogger("ps"),
		GridThreshold: 10.0,
		gridPower:     20000.0,
	}
	site.ManageGridLimits(false)
	assert.Equal(t, PeakShaveIdle, site.peakShaveState)
}

func TestDischargeControlBypassDuringPeakShave(t *testing.T) {
	site := &Site{
		log:                     util.NewLogger("ps"),
		batteryDischargeControl: true,
		peakShaveState:          PeakShaveActive,
	}

	assert.False(t, site.dischargeControlActive(api.Rate{}))
}

func TestApplyBatteryModeBypassDuringLockout(t *testing.T) {
	site := &Site{
		log:            util.NewLogger("ps"),
		peakShaveState: PeakShaveLockout,
	}

	err := site.applyBatteryMode(api.BatteryNormal)
	assert.NoError(t, err)
}

