package keys

const (
	Aux                   = "aux"
	AuxPower              = "auxPower"
	Circuits              = "circuits"
	Consumers             = "consumers"
	Currency              = "currency"
	Ext                   = "ext"
	GreenShareHome        = "greenShareHome"
	GreenShareLoadpoints  = "greenShareLoadpoints"
	GridConfigured        = "gridConfigured"
	Grid                  = "grid"
	HistoryUpdated        = "historyUpdated"
	HomePower             = "homePower"
	PrioritySoc           = "prioritySoc"
	Pv                    = "pv"
	PvEnergy              = "pvEnergy"
	PvPower               = "pvPower"
	ResidualPower         = "residualPower"
	SiteTitle             = "siteTitle"
	SmartCostType         = "smartCostType"
	Statistics            = "statistics"
	Forecast              = "forecast"
	TariffCo2             = "tariffCo2"
	TariffCo2Home         = "tariffCo2Home"
	TariffCo2Loadpoints   = "tariffCo2Loadpoints"
	TariffFeedIn          = "tariffFeedIn"
	TariffGrid            = "tariffGrid"
	TariffGridEnergy      = "tariffGridEnergy"
	TariffCharges         = "tariffCharges"
	TariffTax             = "tariffTax"
	TariffFormula         = "tariffFormula"
	TariffPriceHome       = "tariffPriceHome"
	TariffPriceLoadpoints = "tariffPriceLoadpoints"
	TariffSolar           = "tariffSolar"
	Vehicles              = "vehicles"

	// meters
	GridMeter      = "gridMeter"
	PvMeters       = "pvMeters"
	BatteryMeters  = "batteryMeters"
	ExtMeters      = "extMeters"
	AuxMeters      = "auxMeters"
	ConsumerMeters = "consumerMeters"

	// battery settings
	BatteryDischargeControl         = "batteryDischargeControl"
	BatteryGridChargeLimit          = "batteryGridChargeLimit"
	BatteryGridChargeLimitEnergy    = "batteryGridChargeLimitEnergy"
	BatteryGridChargeActive         = "batteryGridChargeActive"
	BufferSoc                       = "bufferSoc"
	BufferStartSoc                  = "bufferStartSoc"
	GridThreshold                   = "gridThreshold"
	PeakShaveReserveSoc             = "peakShaveReserveSoc"
	PeakShaveMinSoc                 = "peakShaveMinSoc"
	PeakShaveMaintainSocChargePower = "peakShaveMaintainSocChargePower"
	PeakShaveLoadShedDelay          = "peakShaveLoadShedDelay"
	PeakShaveAverage                = "peakShaveAverage"
	PeakShaveState                  = "peakShaveState"
	BatteryCycleCost                = "batteryCycleCost"
	BatteryTrade                    = "batteryTrade"
	BatteryPlan                     = "batteryPlan"

	// optimizer
	OptimizerChargingStrategy   = "optimizerChargingStrategy"
	OptimizerChargingStrategies = "optimizerChargingStrategies"

	// battery status
	Battery     = "battery"
	BatteryMode = "batteryMode"

	// external battery control
	BatteryModeExternal = "batteryModeExternal"

	// smart charging
	SmartCostAvailable           = "smartCostAvailable"           // smart cost available
	SmartFeedInPriorityAvailable = "smartFeedInPriorityAvailable" // smart feed-in priority available
)
