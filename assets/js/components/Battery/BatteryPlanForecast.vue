<template>
	<div class="battery-plan-forecast mb-4">
		<p class="fw-bold mb-1">{{ $t("peakShave.plan.forecast") }}</p>
		<p v-if="liveOverride" class="text-muted small mb-2">
			{{ $t("peakShave.plan.liveOverride") }}
		</p>
		<p v-else-if="!hasPrices" class="text-muted small mb-2">
			{{ $t("peakShave.plan.noPrices") }}
		</p>

		<div v-if="parsedSlots.length" class="legend small mb-2">
			<span v-for="key in actionKeys" :key="key" class="legend-item">
				<span class="legend-swatch" :class="`action-${key}`"></span>
				{{ $t("peakShave.plan.action." + key) }}
			</span>
		</div>

		<div
			v-if="parsedSlots.length"
			class="schedule"
			role="list"
			:aria-label="$t('peakShave.plan.forecast')"
		>
			<button
				v-for="(slot, index) in parsedSlots"
				:key="slot.start.getTime()"
				type="button"
				class="schedule-slot"
				:class="[
					`action-${slot.action || 'normal'}`,
					{
						now: index === 0,
						active: activeIndex === index,
						peak: slot.peak,
					},
				]"
				role="listitem"
				:aria-label="slotLabel(slot, index)"
				:aria-pressed="activeIndex === index"
				@mouseenter="activeIndex = index"
				@mouseleave="activeIndex = null"
				@focus="activeIndex = index"
				@click="activeIndex = index"
			></button>
		</div>
		<p v-else class="text-muted small mb-0">{{ $t("peakShave.plan.noForecast") }}</p>

		<div v-if="parsedSlots.length" ref="chartEl" class="forecast-chart mt-3"></div>

		<div v-if="activeSlot" class="small mt-3">
			<p class="mb-1 fw-bold">
				<span v-if="activeIndex === 0 || activeIndex === null">{{
					$t("peakShave.plan.now")
				}}</span>
				<span v-else>{{ slotRange(activeSlot) }}</span>
			</p>
			<p class="mb-0">
				{{ $t("peakShave.plan.action." + (activeSlot.action || "normal")) }}
				<span v-if="activeSlot.reason" class="text-muted">
					· {{ $t("peakShave.plan.reason." + activeSlot.reason) }}
				</span>
			</p>
			<p class="text-muted mb-0">
				{{ $t("peakShave.plan.house") }}
				{{ fmtW(activeSlot.homeW, POWER_UNIT.KW, true, 1) }}
				· {{ $t("peakShave.plan.solar") }}
				{{ fmtW(activeSlot.solarW, POWER_UNIT.KW, true, 1) }}
				<template v-if="(activeSlot.loadW || 0) > 50">
					· {{ $t("peakShave.plan.charging") }}
					{{ fmtW(activeSlot.loadW, POWER_UNIT.KW, true, 1) }}
				</template>
				· SoC {{ Math.round(activeSlot.soc || 0) }}%
			</p>
			<p v-if="activeSlot.price" class="text-muted mb-0">
				{{ fmtPricePerKWh(activeSlot.price, currency) }}
			</p>
			<p v-if="activeSlot.peak" class="text-warning mb-0">
				{{ $t("peakShave.plan.overLimit") }}
			</p>
		</div>
	</div>
</template>

<script lang="ts">
import { defineComponent, markRaw, type PropType } from "vue";
import {
	echarts,
	FONT_FAMILY,
	tooltipStyle,
	tooltipTable,
	type TooltipRow,
} from "../Forecast/echarts";
import colors from "@/colors";
import formatter, { POWER_UNIT } from "@/mixins/formatter";
import { CURRENCY, type BatteryPlanSlot, type PeakShaveState } from "@/types/evcc";

type EChartsType = ReturnType<typeof echarts.init>;

interface ParsedSlot {
	start: Date;
	end: Date;
	action: string;
	reason?: string;
	chargeW?: number;
	dischargeW?: number;
	homeW?: number;
	solarW?: number;
	loadW?: number;
	residualW?: number;
	price?: number;
	feedIn?: number;
	soc?: number;
	peak?: boolean;
}

const ACTION_KEYS = ["charge", "hold", "discharge", "normal"] as const;

export default defineComponent({
	name: "BatteryPlanForecast",
	mixins: [formatter],
	props: {
		slots: { type: Array as PropType<BatteryPlanSlot[]>, default: () => [] },
		gridThreshold: { type: Number, default: 0 },
		currency: { type: String as PropType<CURRENCY>, default: CURRENCY.EUR },
		peakShaveState: { type: String as PropType<PeakShaveState>, default: "idle" },
	},
	data() {
		return {
			POWER_UNIT,
			actionKeys: ACTION_KEYS,
			activeIndex: null as number | null,
			chart: null as EChartsType | null,
		};
	},
	computed: {
		parsedSlots(): ParsedSlot[] {
			return (this.slots || [])
				.map((s) => ({
					...s,
					start: new Date(s.start),
					end: new Date(s.end),
					action: s.action || "normal",
				}))
				.filter((s) => !Number.isNaN(s.start.getTime()));
		},
		hasPrices(): boolean {
			return this.parsedSlots.some((s) => (s.price || 0) > 0);
		},
		liveOverride(): boolean {
			return ["shaving", "critical", "shedding", "lockout"].includes(this.peakShaveState);
		},
		activeSlot(): ParsedSlot | null {
			if (!this.parsedSlots.length) {
				return null;
			}
			const i = this.activeIndex ?? 0;
			return this.parsedSlots[i] || this.parsedSlots[0];
		},
		chartOption(): Record<string, unknown> {
			const slots = this.parsedSlots;
			if (!slots.length) {
				return {};
			}
			const house: [number, number][] = [];
			const load: [number, number][] = [];
			const solar: [number, number][] = [];
			const soc: [number, number][] = [];
			for (const s of slots) {
				const t = s.start.getTime();
				house.push([t, (s.homeW || 0) / 1000]);
				load.push([t, (s.loadW || 0) / 1000]);
				solar.push([t, (s.solarW || 0) / 1000]);
				soc.push([t, s.soc || 0]);
			}
			const threshold = this.gridThreshold > 0 ? this.gridThreshold : undefined;
			const selfColor = colors.self || "#0FDE41";
			const gridColor = colors.grid || "#FD6158";
			const muted = colors.muted || "#9ca3af";
			return {
				animation: false,
				grid: { top: 28, right: 36, bottom: 28, left: 40, borderWidth: 0 },
				tooltip: {
					...tooltipStyle(colors.text || "#111", () => this.chart),
					trigger: "axis",
					formatter: (params: { dataIndex: number }[]) => this.tooltipHtml(params),
				},
				xAxis: {
					type: "time",
					axisLine: { show: false },
					axisTick: { show: false },
					splitLine: { show: false },
					axisLabel: {
						color: muted,
						fontSize: 11,
						fontFamily: FONT_FAMILY,
						formatter: (value: number) => {
							const d = new Date(value);
							if (d.getMinutes() !== 0 || d.getHours() % 4 !== 0) {
								return "";
							}
							return String(d.getHours());
						},
					},
				},
				yAxis: [
					{
						type: "value",
						min: 0,
						splitLine: { lineStyle: { color: colors.border || "#e5e7eb" } },
						axisLabel: {
							color: muted,
							fontSize: 11,
							fontFamily: FONT_FAMILY,
							formatter: (v: number) => this.fmtNumber(v, 0),
						},
					},
					{
						type: "value",
						min: 0,
						max: 100,
						splitLine: { show: false },
						axisLabel: {
							color: muted,
							fontSize: 11,
							fontFamily: FONT_FAMILY,
							formatter: (v: number) => `${v}`,
						},
					},
				],
				series: [
					{
						name: this.$t("peakShave.plan.house"),
						type: "bar",
						stack: "load",
						barWidth: "70%",
						itemStyle: { color: muted },
						data: house,
					},
					{
						name: this.$t("peakShave.plan.charging"),
						type: "bar",
						stack: "load",
						barWidth: "70%",
						itemStyle: { color: colors.palette[0] },
						data: load,
					},
					{
						name: this.$t("peakShave.plan.solar"),
						type: "line",
						showSymbol: false,
						lineStyle: { width: 2, color: selfColor },
						itemStyle: { color: selfColor },
						data: solar,
					},
					{
						name: "SoC",
						type: "line",
						yAxisIndex: 1,
						showSymbol: false,
						lineStyle: { width: 2, color: colors.batteryPalette[0] },
						itemStyle: { color: colors.batteryPalette[0] },
						data: soc,
					},
					...(threshold
						? [
								{
									type: "line",
									markLine: {
										silent: true,
										symbol: "none",
										label: {
											formatter: this.$t("peakShave.plan.threshold"),
											color: gridColor,
											fontFamily: FONT_FAMILY,
											fontSize: 11,
										},
										lineStyle: { type: "dashed", color: gridColor },
										data: [{ yAxis: threshold }],
									},
									data: [],
								},
							]
						: []),
				],
			};
		},
	},
	watch: {
		parsedSlots: {
			handler() {
				this.$nextTick(() => this.ensureChart());
			},
			deep: true,
		},
	},
	mounted() {
		window.addEventListener("resize", this.resize);
		this.$nextTick(() => this.ensureChart());
	},
	beforeUnmount() {
		window.removeEventListener("resize", this.resize);
		this.disposeChart();
	},
	methods: {
		resize() {
			this.chart?.resize();
		},
		disposeChart() {
			this.chart?.dispose();
			this.chart = null;
		},
		ensureChart() {
			const el = this.$refs.chartEl as HTMLElement | undefined;
			if (!el || !this.parsedSlots.length) {
				this.disposeChart();
				return;
			}
			if (!this.chart) {
				this.chart = markRaw(echarts.init(el));
			}
			this.chart.setOption(this.chartOption, { notMerge: true });
		},
		slotRange(slot: ParsedSlot) {
			return this.fmtTimeSlot(slot.start, slot.end.getTime() - slot.start.getTime());
		},
		slotLabel(slot: ParsedSlot, index: number) {
			const action = this.$t("peakShave.plan.action." + (slot.action || "normal"));
			const now = index === 0 ? `${this.$t("peakShave.plan.now")}: ` : "";
			return `${now}${this.slotRange(slot)}: ${action}`;
		},
		tooltipHtml(params: { dataIndex: number }[]) {
			const idx = params?.[0]?.dataIndex ?? 0;
			const slot = this.parsedSlots[idx];
			if (!slot) {
				return "";
			}
			const rows: TooltipRow[] = [
				{
					name: this.$t("peakShave.plan.action." + (slot.action || "normal")),
					values: [this.$t("peakShave.plan.reason." + (slot.reason || "idle"))],
				},
				{
					name: this.$t("peakShave.plan.house"),
					values: [this.fmtW(slot.homeW || 0, POWER_UNIT.KW, true, 1)],
				},
				{
					name: this.$t("peakShave.plan.solar"),
					values: [this.fmtW(slot.solarW || 0, POWER_UNIT.KW, true, 1)],
				},
			];
			if ((slot.loadW || 0) > 50) {
				rows.push({
					name: this.$t("peakShave.plan.charging"),
					values: [this.fmtW(slot.loadW || 0, POWER_UNIT.KW, true, 1)],
				});
			}
			rows.push({ name: "SoC", values: [`${Math.round(slot.soc || 0)}%`] });
			if (slot.price) {
				rows.push({
					name: this.$t("peakShave.plan.price"),
					values: [this.fmtPricePerKWh(slot.price, this.currency)],
				});
			}
			return tooltipTable(this.slotRange(slot), rows);
		},
	},
});
</script>

<style scoped>
.legend {
	display: flex;
	flex-wrap: wrap;
	gap: 0.75rem 1rem;
}
.legend-item {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
}
.legend-swatch {
	width: 10px;
	height: 10px;
	border-radius: 2px;
	flex-shrink: 0;
	background: #94a3b8;
}
.schedule {
	display: flex;
	gap: 1px;
	height: 18px;
	overflow: hidden;
}
.schedule-slot {
	flex: 1 1 0;
	min-width: 0;
	border: 0;
	padding: 0;
	margin: 0;
	height: 100%;
	cursor: pointer;
	background: #94a3b8;
}
.schedule-slot.now {
	outline: 2px solid currentcolor;
	outline-offset: -2px;
	z-index: 1;
}
.schedule-slot.active {
	filter: brightness(1.15);
}
.action-charge,
.schedule-slot.action-charge {
	background: #2563eb;
}
.action-discharge,
.schedule-slot.action-discharge {
	background: var(--evcc-darker-green);
}
.action-hold,
.schedule-slot.action-hold {
	background: var(--evcc-orange);
}
.action-normal,
.schedule-slot.action-normal {
	background: #94a3b8;
}
.schedule-slot.peak:not(.action-charge) {
	box-shadow: inset 0 3px 0 var(--evcc-red);
}
.forecast-chart {
	height: 220px;
	width: 100%;
}
</style>
