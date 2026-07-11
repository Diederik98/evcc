package site

import (
	"github.com/evcc-io/evcc/api"
	"github.com/evcc-io/evcc/core/loadpoint"
)

// publisher gives access to the site's publish function
type Publisher interface {
	Publish(key string, val any)
}

// API is the external site API
type API interface {
	Publisher

	Loadpoints() []loadpoint.API
	Vehicles() Vehicles
	Optimize() error

	// Meta
	GetTitle() string
	SetTitle(string)

	// Config
	GetGridMeterRef() string
	SetGridMeterRef(string)
	GetPVMeterRefs() []string
	SetPVMeterRefs([]string)
	GetBatteryMeterRefs() []string
	SetBatteryMeterRefs([]string)
	GetAuxMeterRefs() []string
	SetAuxMeterRefs([]string)
	GetExtMeterRefs() []string
	SetExtMeterRefs([]string)
	GetConsumerMeterRefs() []string
	SetConsumerMeterRefs([]string)

	// circuits
	GetCircuit() api.Circuit

	//
	// battery
	//

	GetBatterySoc() float64
	GetPrioritySoc() float64
	SetPrioritySoc(float64) error
	GetBufferSoc() float64
	SetBufferSoc(float64) error
	GetBufferStartSoc() float64
	SetBufferStartSoc(float64) error

	// GetBatteryGridChargeLimit get the grid charge limit
	GetBatteryGridChargeLimit() *float64
	// SetBatteryGridChargeLimit sets the grid charge limit
	SetBatteryGridChargeLimit(limit *float64) error

	// GetOptimizerChargingStrategy gets the optimizer grid charging strategy
	GetOptimizerChargingStrategy() string
	// SetOptimizerChargingStrategy sets the optimizer grid charging strategy
	SetOptimizerChargingStrategy(strategy string) error

	//
	// power and energy
	//

	GetGridPower() float64
	GetResidualPower() float64
	SetResidualPower(float64) error

	//
	// tariffs and costs
	//

	// GetTariff returns the respective tariff
	GetTariff(api.TariffUsage) api.Tariff

	//
	// battery control
	//

	GetBatteryDischargeControl() bool
	SetBatteryDischargeControl(bool) error

	//
	// battery control external
	//

	// GetBatteryModeExternal returns the external battery mode
	GetBatteryModeExternal() api.BatteryMode
	// SetBatteryModeExternal sets the external battery mode
	SetBatteryModeExternal(api.BatteryMode) error

	//
	// peak shaving
	//

	// GetGridThreshold returns the grid threshold in kW
	GetGridThreshold() float64
	// SetGridThreshold sets the grid threshold in kW
	SetGridThreshold(float64) error
	// GetPeakShaveReserveSoc returns the peak shave reserve SoC in %
	GetPeakShaveReserveSoc() float64
	// SetPeakShaveReserveSoc sets the peak shave reserve SoC in %
	SetPeakShaveReserveSoc(float64) error
	// GetPeakShaveMinSoc returns the peak shave min SoC in %
	GetPeakShaveMinSoc() float64
	// SetPeakShaveMinSoc sets the peak shave min SoC in %
	SetPeakShaveMinSoc(float64) error
	// GetPeakShaveMaintainSocChargePower returns the charging power limit for recovery in W
	GetPeakShaveMaintainSocChargePower() float64
	// SetPeakShaveMaintainSocChargePower sets the charging power limit for recovery in W
	SetPeakShaveMaintainSocChargePower(float64) error
	// GetPeakShaveLoadShedDelay returns the load shed grace period in seconds
	GetPeakShaveLoadShedDelay() float64
	// SetPeakShaveLoadShedDelay sets the load shed grace period in seconds
	SetPeakShaveLoadShedDelay(float64) error
	// GetPeakShaveState returns the current peak shaving state string
	GetPeakShaveState() string
}
