<template>
	<div class="battery-plan-forecast mb-4">
		<p class="fw-bold mb-1">{{ forecastTitle }}</p>
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
			<template v-if="hasChargerPlan">
				<span class="legend-item">
					<span class="legend-swatch action-charger"></span>
					{{ $t("peakShave.plan.chargerAction.charging") }}
				</span>
				<span class="legend-item">
					<span class="legend-swatch action-charger-idle"></span>
					{{ $t("peakShave.plan.chargerAction.idle") }}
				</span>
			</template>
			<span v-if="hasMeasured" class="legend-item">
				<span class="legend-swatch series-measured"></span>
				{{ $t("peakShave.plan.measured") }}
			</span>
		</div>
		<div v-if="parsedSlots.length" class="legend small mb-2">
			<span class="legend-item">
				<span class="legend-swatch series-house"></span>
				{{ $t("peakShave.plan.house") }}
			</span>
			<span class="legend-item">
				<span class="legend-swatch series-solar"></span>
				{{ $t("peakShave.plan.solar") }}
			</span>
			<span class="legend-item">
				<span class="legend-swatch series-soc"></span>
				{{ $t("peakShave.plan.soc") }}
			</span>
			<span v-if="hasPrices" class="legend-item">
				<span class="legend-swatch series-price"></span>
				{{ $t("peakShave.plan.price") }}
			</span>
		</div>

		<p v-if="parsedSlots.length" class="text-muted small mb-1">
			{{ $t("peakShave.plan.batteryStrip") }}
		</p>
		<div v-if="parsedSlots.length" class="schedule-wrap">
			<div
				class="schedule"
				role="list"
				:aria-label="$t('peakShave.plan.batteryStrip')"
			>
				<button
					v-for="(slot, index) in parsedSlots"
					:key="'b-' + slot.start.getTime()"
					type="button"
					class="schedule-slot"
					:class="[
						`action-${slot.action || 'normal'}`,
						{
							now: isNowSlot(slot),
							active: activeIndex === index,
							peak: slot.peak,
							measured: slot.measured,
						},
					]"
					:style="slotStyle(slot)"
					role="listitem"
					:aria-label="slotLabel(slot, index)"
					:aria-pressed="activeIndex === index"
					@mouseenter="activeIndex = index"
					@mouseleave="activeIndex = null"
					@focus="activeIndex = index"
					@click="activeIndex = index"
				></button>
			</div>
			<div
				v-if="nowInWindow"
				class="schedule-now"
				:style="{ left: nowPosition + '%' }"
				:aria-label="$t('peakShave.plan.now')"
			></div>
		</div>
		<p v-if="hasChargerPlan" class="text-muted small mb-1 mt-2">
			{{ $t("peakShave.plan.chargerStrip") }}
		</p>
		<div v-if="hasChargerPlan" class="schedule-wrap">
			<div
				class="schedule"
				role="list"
				:aria-label="$t('peakShave.plan.chargerStrip')"
			>
				<button
					v-for="(slot, index) in parsedSlots"
					:key="'c-' + slot.start.getTime()"
					type="button"
					class="schedule-slot"
					:class="{
						'action-charger': chargerActive(slot),
						'action-charger-idle': !chargerActive(slot),
						now: isNowSlot(slot),
						active: activeIndex === index,
						measured: slot.measured,
					}"
					:style="slotStyle(slot)"
					role="listitem"
					:aria-label="chargerLabel(slot, index)"
					:aria-pressed="activeIndex === index"
					@mouseenter="activeIndex = index"
					@mouseleave="activeIndex = null"
					@focus="activeIndex = index"
					@click="activeIndex = index"
				></button>
			</div>
			<div
				v-if="nowInWindow"
				class="schedule-now"
				:style="{ left: nowPosition + '%' }"
				:aria-label="$t('peakShave.plan.now')"
			></div>
		</div>
		<p v-else-if="!parsedSlots.length" class="text-muted small mb-0">
			{{ $t("peakShave.plan.noForecast") }}
		</p>

		<div v-if="parsedSlots.length" ref="chartEl" class="forecast-chart mt-3"></div>

		<div v-if="activeSlot" class="small mt-3">
			<p class="mb-1 fw-bold">
				<span v-if="isNowSlot(activeSlot) || activeIndex === null">{{
					$t("peakShave.plan.now")
				}}</span>
				<span v-else>{{ slotRange(activeSlot) }}</span>
			</p>
			<p class="mb-0">
				<template v-if="activeSlot.measured">
					{{ $t("peakShave.plan.measured") }}
				</template>
				<template v-else>
					{{ $t("peakShave.plan.action." + (activeSlot.action || "normal")) }}
					<span v-if="activeSlot.reason" class="text-muted">
						· {{ $t("peakShave.plan.reason." + activeSlot.reason) }}
					</span>
				</template>
			</p>
			<p class="text-muted mb-0">
				{{ $t("peakShave.plan.house") }}
				{{ fmtW(activeSlot.homeW || 0, POWER_UNIT.KW, true, 1) }}
				· {{ $t("peakShave.plan.solar") }}
				{{ fmtW(activeSlot.solarW || 0, POWER_UNIT.KW, true, 1) }}
				<template v-if="(activeSlot.loadW || 0) > 50">
					· {{ $t("peakShave.plan.charging") }}
					{{ fmtW(activeSlot.loadW, POWER_UNIT.KW, true, 1) }}
				</template>
				<template v-if="activeSlot.soc"> · SoC {{ Math.round(activeSlot.soc) }}% </template>
			</p>
			<p v-for="(load, i) in activeSlotLoads(activeSlot)" :key="i" class="text-muted mb-0">
				{{ load.title }}: {{ fmtW(load.loadW || 0, POWER_UNIT.KW, true, 1) }}
			</p>
			<p v-if="activeSlot.hasPrice && activeSlot.price" class="text-muted mb-0">
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
import {
	CURRENCY,
	type BatteryPlanLoad,
	type BatteryPlanSlot,
	type BatteryPlanSlotLoad,
	type PeakShaveState,
} from "@/types/evcc";

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
	loads?: BatteryPlanSlotLoad[];
	residualW?: number;
	price?: number;
	hasPrice?: boolean;
	feedIn?: number;
	soc?: number;
	peak?: boolean;
	measured?: boolean;
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
		hasPricesHint: { type: Boolean, default: undefined },
		planLoads: { type: Array as PropType<BatteryPlanLoad[]>, default: () => [] },
	},
	data() {
		return {
			POWER_UNIT,
			actionKeys: ACTION_KEYS,
			activeIndex: null as number | null,
			chart: null as EChartsType | null,
			nowMs: Date.now(),
			nowTimer: undefined as number | undefined,
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
					hasPrice: s.hasPrice ?? ((s.price || 0) > 0),
				}))
				.filter((s) => !Number.isNaN(s.start.getTime()));
		},
		hasPrices(): boolean {
			if (this.hasPricesHint !== undefined) {
				return this.hasPricesHint;
			}
			return this.parsedSlots.some((s) => s.hasPrice);
		},
		hasMeasured(): boolean {
			return this.parsedSlots.some((s) => s.measured);
		},
		liveOverride(): boolean {
			return ["shaving", "critical", "shedding", "lockout"].includes(this.peakShaveState);
		},
		hasChargerPlan(): boolean {
			return this.parsedSlots.some((s) => this.chargerActive(s));
		},
		windowStartMs(): number {
			return this.parsedSlots[0]?.start.getTime() || 0;
		},
		windowEndMs(): number {
			const slots = this.parsedSlots;
			return slots.length ? slots[slots.length - 1].end.getTime() : 0;
		},
		windowSpanMs(): number {
			return Math.max(1, this.windowEndMs - this.windowStartMs);
		},
		nowInWindow(): boolean {
			return this.nowMs >= this.windowStartMs && this.nowMs <= this.windowEndMs;
		},
		nowPosition(): number {
			return ((this.nowMs - this.windowStartMs) / this.windowSpanMs) * 100;
		},
		forecastHours(): number {
			return Math.max(1, Math.round(this.windowSpanMs / 3600000));
		},
		forecastTitle(): string {
			if (!this.forecastHours) {
				return this.$t("peakShave.plan.forecast");
			}
			return this.$t("peakShave.plan.forecastHours", { hours: this.forecastHours });
		},
		activeSlot(): ParsedSlot | null {
			if (!this.parsedSlots.length) {
				return null;
			}
			const i = this.activeIndex ?? this.nowIndex;
			return this.parsedSlots[i] || this.parsedSlots[0];
		},
		nowIndex(): number {
			for (let i = 0; i < this.parsedSlots.length; i++) {
				if (this.isNowSlot(this.parsedSlots[i])) {
					return i;
				}
			}
			return 0;
		},
		chartOption(): Record<string, unknown> {
			const slots = this.parsedSlots;
			if (!slots.length) {
				return {};
			}
			const houseForecast: [number, number][] = [];
			const houseMeasured: [number, number][] = [];
			const load: [number, number][] = [];
			const solarForecast: [number, number][] = [];
			const solarMeasured: [number, number][] = [];
			const socForecast: [number, number][] = [];
			const socMeasured: [number, number][] = [];
			const price: [number, number | null][] = [];
			for (const s of slots) {
				const t = s.start.getTime();
				const mid = t + (s.end.getTime() - t) / 2;
				if (s.measured) {
					houseMeasured.push([mid, (s.homeW || 0) / 1000]);
					solarMeasured.push([mid, (s.solarW || 0) / 1000]);
					if (s.soc) {
						socMeasured.push([mid, s.soc]);
					}
				} else {
					houseForecast.push([t, (s.homeW || 0) / 1000]);
					load.push([t, (s.loadW || 0) / 1000]);
					solarForecast.push([t, (s.solarW || 0) / 1000]);
					socForecast.push([t, s.soc || 0]);
				}
				price.push([t, s.hasPrice && s.price ? s.price * 100 : null]);
			}
			const threshold = this.gridThreshold > 0 ? this.gridThreshold : undefined;
			const solarColor = colors.selfPalette?.[1] || colors.price || "#FFBD2F";
			const priceColor = colors.price || "#ff912f";
			const socColor = colors.batteryPalette[0] || "#0BA631";
			const gridColor = colors.grid || "#FD6158";
			const muted = colors.muted || "#9ca3af";
			const houseColor = "#64748B";
			const yAxes: Record<string, unknown>[] = [
				{
					type: "value",
					name: "kW",
					nameTextStyle: { color: muted, fontSize: 10, fontFamily: FONT_FAMILY },
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
					name: "SoC %",
					nameTextStyle: { color: socColor, fontSize: 10, fontFamily: FONT_FAMILY },
					min: 0,
					max: 100,
					splitLine: { show: false },
					axisLabel: {
						color: socColor,
						fontSize: 11,
						fontFamily: FONT_FAMILY,
						formatter: (v: number) => `${v}`,
					},
				},
			];
			if (this.hasPrices) {
				yAxes.push({
					type: "value",
					name: "ct",
					nameTextStyle: { color: priceColor, fontSize: 10, fontFamily: FONT_FAMILY },
					min: 0,
					offset: 45,
					splitLine: { show: false },
					axisLabel: {
						color: priceColor,
						fontSize: 11,
						fontFamily: FONT_FAMILY,
						formatter: (v: number) => this.fmtNumber(v, 0),
					},
				});
			}
			const series: Record<string, unknown>[] = [
				{
					name: this.$t("peakShave.plan.house"),
					type: "bar",
					stack: "load",
					barWidth: "70%",
					itemStyle: { color: houseColor },
					data: houseForecast,
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
					smooth: 0.2,
					lineStyle: { width: 2, color: solarColor },
					itemStyle: { color: solarColor },
					areaStyle: { color: solarColor, opacity: 0.18 },
					data: solarForecast,
				},
				{
					name: "SoC",
					type: "line",
					yAxisIndex: 1,
					showSymbol: false,
					lineStyle: { width: 2.5, color: socColor },
					itemStyle: { color: socColor },
					data: socForecast,
				},
			];
			if (this.hasMeasured) {
				series.push(
					{
						name: this.$t("peakShave.plan.measuredHouse"),
						type: "line",
						showSymbol: false,
						lineStyle: { width: 1.5, type: "dotted", color: houseColor },
						itemStyle: { color: houseColor },
						data: houseMeasured,
					},
					{
						name: this.$t("peakShave.plan.measuredSolar"),
						type: "line",
						showSymbol: false,
						lineStyle: { width: 1.5, type: "dotted", color: solarColor },
						itemStyle: { color: solarColor },
						data: solarMeasured,
					}
				);
				if (socMeasured.length) {
					series.push({
						name: this.$t("peakShave.plan.measuredSoc"),
						type: "line",
						yAxisIndex: 1,
						showSymbol: false,
						lineStyle: { width: 1.5, type: "dotted", color: socColor },
						itemStyle: { color: socColor },
						data: socMeasured,
					});
				}
			}
			if (this.hasPrices) {
				series.push({
					name: this.$t("peakShave.plan.price"),
					type: "line",
					yAxisIndex: 2,
					showSymbol: false,
					connectNulls: false,
					lineStyle: { width: 1.5, type: "dashed", color: priceColor },
					itemStyle: { color: priceColor },
					data: price,
				});
			}
			if (this.nowInWindow) {
				series.push({
					id: "now-line",
					type: "line",
					data: [
						[this.nowMs, 0],
						[this.nowMs, 0],
					],
					showSymbol: false,
					silent: true,
					z: 10,
					markLine: {
						symbol: "none",
						label: { show: false },
						lineStyle: { color: muted, width: 1, type: "solid" },
						data: [{ xAxis: this.nowMs }],
					},
				});
			}
			if (threshold) {
				series.push({
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
				});
			}
			return {
				animation: false,
				grid: {
					top: 28,
					right: this.hasPrices ? 72 : 36,
					bottom: 28,
					left: 40,
					borderWidth: 0,
				},
				tooltip: {
					...tooltipStyle(colors.text || "#111", () => this.chart),
					trigger: "axis",
					formatter: (params: { dataIndex: number }[]) => this.tooltipHtml(params),
				},
				xAxis: {
					type: "time",
					min: this.windowStartMs,
					max: this.windowEndMs,
					axisLine: { show: false },
					axisTick: { show: false },
					splitLine: { show: false },
					axisLabel: {
						color: muted,
						fontSize: 11,
						fontFamily: FONT_FAMILY,
						formatter: (value: number) => this.axisLabel(value),
					},
				},
				yAxis: yAxes,
				series,
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
		this.nowTimer = window.setInterval(() => {
			this.nowMs = Date.now();
		}, 60000);
		this.$nextTick(() => this.ensureChart());
	},
	beforeUnmount() {
		window.removeEventListener("resize", this.resize);
		if (this.nowTimer) {
			window.clearInterval(this.nowTimer);
		}
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
		slotStyle(slot: ParsedSlot) {
			const span = Math.max(1, slot.end.getTime() - slot.start.getTime());
			return { flex: `${span} 0 0` };
		},
		isNowSlot(slot: ParsedSlot) {
			return this.nowMs >= slot.start.getTime() && this.nowMs < slot.end.getTime();
		},
		chargerActive(slot: ParsedSlot) {
			return (slot.loadW || 0) > 50;
		},
		activeSlotLoads(slot: ParsedSlot): BatteryPlanSlotLoad[] {
			if (slot.loads?.length) {
				return slot.loads;
			}
			return (this.planLoads || [])
				.filter((load) => {
					if (!load.start || !load.end) {
						return false;
					}
					const start = new Date(load.start).getTime();
					const end = new Date(load.end).getTime();
					return slot.start.getTime() < end && slot.end.getTime() > start;
				})
				.map((load) => ({
					title: load.title,
					loadW: load.powerW,
				}));
		},
		axisLabel(value: number) {
			const d = new Date(value);
			if (d.getMinutes() !== 0) {
				return "";
			}
			if (this.forecastHours > 26) {
				if (d.getHours() === 0) {
					return d.toLocaleDateString(undefined, { weekday: "short" });
				}
				if (d.getHours() % 6 !== 0) {
					return "";
				}
				return String(d.getHours());
			}
			if (d.getHours() % 4 !== 0) {
				return "";
			}
			return String(d.getHours());
		},
		slotRange(slot: ParsedSlot) {
			return this.fmtTimeSlot(slot.start, slot.end.getTime() - slot.start.getTime());
		},
		slotLabel(slot: ParsedSlot, index: number) {
			const action = slot.measured
				? this.$t("peakShave.plan.measured")
				: this.$t("peakShave.plan.action." + (slot.action || "normal"));
			const now = this.isNowSlot(slot) ? `${this.$t("peakShave.plan.now")}: ` : "";
			return `${now}${this.slotRange(slot)}: ${action}`;
		},
		chargerLabel(slot: ParsedSlot, index: number) {
			const loads = this.activeSlotLoads(slot);
			const key = this.chargerActive(slot)
				? "peakShave.plan.chargerAction.charging"
				: "peakShave.plan.chargerAction.idle";
			const now = this.isNowSlot(slot) ? `${this.$t("peakShave.plan.now")}: ` : "";
			const power = this.chargerActive(slot)
				? ` ${this.fmtW(slot.loadW || 0, POWER_UNIT.KW, true, 1)}`
				: "";
			const names =
				loads.length > 0 ? ` (${loads.map((l) => l.title).filter(Boolean).join(", ")})` : "";
			return `${now}${this.slotRange(slot)}: ${this.$t(key)}${power}${names}`;
		},
		tooltipHtml(params: { dataIndex: number }[]) {
			const idx = params?.[0]?.dataIndex ?? 0;
			const slot = this.parsedSlots[idx];
			if (!slot) {
				return "";
			}
			const rows: TooltipRow[] = [];
			if (slot.measured) {
				rows.push({
					name: this.$t("peakShave.plan.measured"),
					values: [this.$t("peakShave.plan.measuredHelp")],
				});
			} else {
				rows.push({
					name: this.$t("peakShave.plan.action." + (slot.action || "normal")),
					values: [this.$t("peakShave.plan.reason." + (slot.reason || "idle"))],
				});
			}
			rows.push(
				{
					name: this.$t("peakShave.plan.house"),
					values: [this.fmtW(slot.homeW || 0, POWER_UNIT.KW, true, 1)],
				},
				{
					name: this.$t("peakShave.plan.solar"),
					values: [this.fmtW(slot.solarW || 0, POWER_UNIT.KW, true, 1)],
				}
			);
			if ((slot.loadW || 0) > 50) {
				rows.push({
					name: this.$t("peakShave.plan.charging"),
					values: [this.fmtW(slot.loadW || 0, POWER_UNIT.KW, true, 1)],
				});
			}
			for (const load of this.activeSlotLoads(slot)) {
				if (load.title) {
					rows.push({
						name: load.title,
						values: [this.fmtW(load.loadW || 0, POWER_UNIT.KW, true, 1)],
					});
				}
			}
			if (slot.soc) {
				rows.push({ name: "SoC", values: [`${Math.round(slot.soc)}%`] });
			}
			if (slot.hasPrice && slot.price) {
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
.schedule-wrap {
	position: relative;
}
.schedule {
	display: flex;
	gap: 1px;
	height: 18px;
	overflow: hidden;
}
.schedule-now {
	position: absolute;
	top: 0;
	bottom: 0;
	width: 2px;
	margin-left: -1px;
	background: currentcolor;
	opacity: 0.85;
	pointer-events: none;
	z-index: 2;
}
.schedule-slot {
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
.schedule-slot.measured {
	opacity: 0.55;
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
.action-charger,
.schedule-slot.action-charger {
	background: #7c3aed;
}
.action-charger-idle,
.schedule-slot.action-charger-idle {
	background: #e2e8f0;
}
.series-house {
	background: #64748b;
}
.series-solar {
	background: #ffbd2f;
}
.series-soc {
	background: #0ba631;
}
.series-price {
	background: var(--evcc-price, #ff912f);
}
.series-measured {
	background: #64748b;
	opacity: 0.5;
}
.forecast-chart {
	height: 260px;
	width: 100%;
}
</style>
