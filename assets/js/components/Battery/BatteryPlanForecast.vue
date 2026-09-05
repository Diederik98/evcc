<template>
	<div class="battery-plan-forecast mb-4">
		<div class="d-flex align-items-center justify-content-between gap-2 mb-1">
			<p class="fw-bold mb-0">{{ forecastTitle }}</p>
			<button
				type="button"
				class="btn btn-sm btn-outline-secondary"
				:disabled="replanning"
				:aria-label="$t('peakShave.plan.replan')"
				@click="replan"
			>
				{{ $t("peakShave.plan.replan") }}
			</button>
		</div>
		<p class="text-muted small mb-2">{{ $t("peakShave.plan.replanHelp") }}</p>
		<p v-if="liveOverride" class="text-muted small mb-2">
			{{ $t("peakShave.plan.liveOverride") }}
		</p>
		<p v-else-if="!hasPrices" class="text-muted small mb-2">
			{{ $t("peakShave.plan.noPrices") }}
		</p>
		<p v-else-if="!parsedSlots.length" class="text-muted small mb-0">
			{{ $t("peakShave.plan.noForecast") }}
		</p>

		<div v-if="parsedSlots.length" class="legend small mb-2">
			<span
				v-for="item in legendItems"
				:key="item.key"
				class="legend-item"
				:title="$t('peakShave.plan.help.' + item.key)"
			>
				<span class="legend-swatch" :class="item.swatch"></span>
				{{ $t(item.label) }}
			</span>
		</div>
		<p v-if="parsedSlots.length" class="text-muted small mb-2">
			{{ $t("peakShave.plan.legendIntro") }}
		</p>
		<dl v-if="showLegendHelp && parsedSlots.length" class="legend-defs small mb-3">
			<template v-for="item in legendItems" :key="'help-' + item.key">
				<dt>{{ $t(item.label) }}</dt>
				<dd>{{ $t("peakShave.plan.help." + item.key) }}</dd>
			</template>
		</dl>

		<div
			v-if="parsedSlots.length"
			ref="chartEl"
			class="forecast-chart"
			:class="{ 'has-charger': hasChargerPlan }"
			role="img"
			:aria-label="forecastTitle"
		></div>

		<div v-if="activeSlot" class="small mt-3 slot-summary" aria-live="polite">
			<p class="mb-1 fw-bold">
				<span v-if="isNowSlot(activeSlot)">{{ $t("peakShave.plan.now") }} · </span>
				{{ slotRange(activeSlot) }}
				<span v-if="activeSlot.measured" class="text-muted fw-normal">
					· {{ $t("peakShave.plan.measured") }}
				</span>
			</p>
			<p class="mb-0">
				{{ $t("peakShave.plan.action." + (activeSlot.action || "normal")) }}
				<span v-if="activeSlot.reason && !activeSlot.measured" class="text-muted">
					· {{ $t("peakShave.plan.reason." + activeSlot.reason) }}
				</span>
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
				<template v-if="!activeSlot.measured && (activeSlot.coverSoc || 0) > 0">
					· {{ $t("peakShave.plan.cover") }} {{ Math.round(activeSlot.coverSoc || 0) }}%
				</template>
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
import api from "@/api";
import colors from "@/colors";
import formatter, { POWER_UNIT } from "@/mixins/formatter";
import {
	CURRENCY,
	type BatteryPlanLoad,
	type BatteryPlanSlot,
	type BatteryPlanSlotLoad,
	type PeakShaveState,
} from "@/types/evcc";
import { displayGridPrice } from "@/utils/tariffPrice";

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
	energy?: number;
	hasPrice?: boolean;
	feedIn?: number;
	soc?: number;
	coverSoc?: number;
	peak?: boolean;
	measured?: boolean;
}

const ACTION_KEYS = ["charge", "hold", "discharge", "normal"] as const;

const SERIES_COLORS = {
	house: "#475569",
	solar: "#ca8a04",
	charging: "#7c3aed",
	soc: "#16a34a",
	cover: "#c026d3",
	price: "#0891b2",
	charge: "#1d4ed8",
	hold: "#ea580c",
	discharge: "#0d9488",
	export: "#be185d",
	normal: "#94a3b8",
} as const;

const ACTION_COLORS_SOLID: Record<string, string> = {
	charge: SERIES_COLORS.charge,
	hold: SERIES_COLORS.hold,
	discharge: SERIES_COLORS.discharge,
	normal: SERIES_COLORS.normal,
};
const EXPORT_COLOR = SERIES_COLORS.export;
const COVER_COLOR = SERIES_COLORS.cover;

interface LegendItem {
	key: string;
	label: string;
	swatch: string;
}

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
		tariffCharges: { type: Number, default: 0 },
		tariffTax: { type: Number, default: 0 },
		tariffFormula: Boolean,
		showLegendHelp: Boolean,
	},
	data() {
		return {
			POWER_UNIT,
			activeIndex: null as number | null,
			chart: null as EChartsType | null,
			nowMs: Date.now(),
			nowTimer: undefined as number | undefined,
			replanning: false,
		};
	},
	computed: {
		parsedSlots(): ParsedSlot[] {
			const now = this.nowMs;
			return (this.slots || [])
				.map((s) => {
					const start = new Date(s.start);
					const end = new Date(s.end);
					// Recorded history must end at or before now. Never paint measured into the future.
					const measured = !!s.measured && end.getTime() <= now;
					return {
						...s,
						start,
						end,
						action: measured ? "normal" : s.action || "normal",
						hasPrice: s.hasPrice ?? (s.price || 0) > 0,
						price: s.price != null ? displayGridPrice(s.price, s.energy) : s.price,
						measured,
					};
				})
				.filter((s) => !Number.isNaN(s.start.getTime()) && s.end > s.start)
				.sort((a, b) => a.start.getTime() - b.start.getTime());
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
			return this.parsedSlots.some((s) => !s.measured && (s.loadW || 0) > 50);
		},
		hasCover(): boolean {
			return this.parsedSlots.some((s) => !s.measured && (s.coverSoc || 0) > 0);
		},
		hasExport(): boolean {
			return this.parsedSlots.some((s) => !s.measured && s.reason === "export");
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
		forecastHours(): number {
			const futureMs = Math.max(
				0,
				this.windowEndMs - Math.max(this.nowMs, this.windowStartMs)
			);
			const pastMs = Math.max(0, Math.min(this.nowMs, this.windowEndMs) - this.windowStartMs);
			const span = futureMs > 0 ? pastMs + futureMs : this.windowSpanMs;
			return Math.max(1, Math.round(span / 3600000));
		},
		forecastTitle(): string {
			if (!this.parsedSlots.length) {
				return this.$t("peakShave.plan.forecast");
			}
			return this.$t("peakShave.plan.forecastHours", { hours: this.forecastHours });
		},
		legendItems(): LegendItem[] {
			const items: LegendItem[] = [
				{ key: "house", label: "peakShave.plan.house", swatch: "series-house" },
				{ key: "solar", label: "peakShave.plan.solar", swatch: "series-solar" },
				{ key: "soc", label: "peakShave.plan.soc", swatch: "series-soc" },
			];
			if (this.hasChargerPlan) {
				items.push({
					key: "charging",
					label: "peakShave.plan.charging",
					swatch: "action-charger",
				});
			}
			if (this.hasCover) {
				items.push({
					key: "cover",
					label: "peakShave.plan.cover",
					swatch: "series-cover",
				});
			}
			if (this.hasPrices) {
				items.push({
					key: "price",
					label: "peakShave.plan.price",
					swatch: "series-price",
				});
			}
			for (const key of ACTION_KEYS) {
				items.push({
					key,
					label: "peakShave.plan.action." + key,
					swatch: `action-${key}`,
				});
			}
			if (this.hasExport) {
				items.push({
					key: "export",
					label: "peakShave.plan.reason.export",
					swatch: "action-export",
				});
			}
			if (this.hasMeasured) {
				items.push({
					key: "measured",
					label: "peakShave.plan.measured",
					swatch: "series-measured",
				});
			}
			return items;
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
			// Prefer first forecast slot when now is between history and plan.
			const firstForecast = this.parsedSlots.findIndex((s) => !s.measured);
			return firstForecast >= 0 ? firstForecast : 0;
		},
		chartOption(): Record<string, unknown> {
			const slots = this.parsedSlots;
			if (!slots.length) {
				return {};
			}

			const houseColor = SERIES_COLORS.house;
			const solarColor = SERIES_COLORS.solar;
			const priceColor = SERIES_COLORS.price;
			const socColor = SERIES_COLORS.soc;
			const gridColor = colors.grid || "#dc2626";
			const loadColor = SERIES_COLORS.charging;
			const muted = colors.muted || "#9ca3af";
			const border = colors.border || "#e5e7eb";
			const hasCharger = this.hasChargerPlan;
			const hasPrices = this.hasPrices;
			const threshold = this.gridThreshold > 0 ? this.gridThreshold : undefined;

			const house: {
				value: [number, number];
				itemStyle: { color: string; opacity: number };
			}[] = [];
			const load: [number, number][] = [];
			const solar: [number, number][] = [];
			const soc: [number, number][] = [];
			const cover: [number, number][] = [];
			const price: [number, number | null][] = [];
			const batteryStrip: {
				value: [number, number];
				itemStyle: { color: string; opacity: number };
			}[] = [];
			const chargerStrip: {
				value: [number, number];
				itemStyle: { color: string; opacity: number };
			}[] = [];

			for (const s of slots) {
				const t = s.start.getTime();
				const mid = t + (s.end.getTime() - t) / 2;
				house.push({
					value: [mid, (s.homeW || 0) / 1000],
					itemStyle: { color: houseColor, opacity: s.measured ? 0.45 : 0.9 },
				});
				load.push([mid, s.measured ? 0 : (s.loadW || 0) / 1000]);
				solar.push([t, (s.solarW || 0) / 1000]);
				soc.push([t, s.soc || 0]);
				if (!s.measured && (s.coverSoc || 0) > 0) {
					cover.push([t, s.coverSoc || 0]);
				}
				price.push([t, s.hasPrice && s.price ? s.price * 100 : null]);
				batteryStrip.push({
					value: [t, s.end.getTime()],
					itemStyle: {
						color:
							s.reason === "export"
								? EXPORT_COLOR
								: ACTION_COLORS_SOLID[s.action] || ACTION_COLORS_SOLID.normal,
						opacity: s.measured ? 0.45 : 1,
					},
				});
				if (hasCharger) {
					const active = !s.measured && (s.loadW || 0) > 50;
					chargerStrip.push({
						value: [t, s.end.getTime()],
						itemStyle: {
							color: active ? SERIES_COLORS.charging : "#e2e8f0",
							opacity: s.measured ? 0.35 : 1,
						},
					});
				}
			}

			const mainBottom = hasCharger ? "28%" : "16%";
			const batTop = hasCharger ? "76%" : "88%";
			const batHeight = "8%";
			const chargerTop = "88%";
			const chargerHeight = "8%";

			const grids: Record<string, unknown>[] = [
				{
					top: 28,
					right: hasPrices ? 72 : 40,
					bottom: mainBottom,
					left: 44,
					borderWidth: 0,
				},
				{
					top: batTop,
					right: hasPrices ? 72 : 40,
					height: batHeight,
					left: 44,
					borderWidth: 0,
				},
			];
			if (hasCharger) {
				grids.push({
					top: chargerTop,
					right: hasPrices ? 72 : 40,
					height: chargerHeight,
					left: 44,
					borderWidth: 0,
				});
			}

			const xAxes: Record<string, unknown>[] = [
				{
					type: "time",
					gridIndex: 0,
					min: this.windowStartMs,
					max: this.windowEndMs,
					axisLine: { show: false },
					axisTick: { show: false },
					splitLine: { show: false },
					axisLabel: { show: false },
					axisPointer: { show: true, label: { show: false } },
				},
				{
					type: "time",
					gridIndex: 1,
					min: this.windowStartMs,
					max: this.windowEndMs,
					axisLine: { show: false },
					axisTick: { show: false },
					splitLine: { show: false },
					axisLabel: {
						show: !hasCharger,
						color: muted,
						fontSize: 11,
						fontFamily: FONT_FAMILY,
						formatter: (value: number) => this.axisLabel(value),
					},
					axisPointer: { show: true, label: { show: false } },
				},
			];
			if (hasCharger) {
				xAxes.push({
					type: "time",
					gridIndex: 2,
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
					axisPointer: { show: true, label: { show: false } },
				});
			}

			const yAxes: Record<string, unknown>[] = [
				{
					type: "value",
					gridIndex: 0,
					name: "kW",
					nameTextStyle: { color: muted, fontSize: 10, fontFamily: FONT_FAMILY },
					min: 0,
					splitLine: { lineStyle: { color: border } },
					axisLabel: {
						color: muted,
						fontSize: 11,
						fontFamily: FONT_FAMILY,
						formatter: (v: number) => this.fmtNumber(v, 0),
					},
				},
				{
					type: "value",
					gridIndex: 0,
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
				{
					type: "value",
					gridIndex: 1,
					min: 0,
					max: 1,
					show: false,
				},
			];
			if (hasPrices) {
				yAxes.splice(2, 0, {
					type: "value",
					gridIndex: 0,
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
			if (hasCharger) {
				yAxes.push({
					type: "value",
					gridIndex: 2,
					min: 0,
					max: 1,
					show: false,
				});
			}

			const priceAxis = hasPrices ? 2 : -1;
			const batYAxis = hasPrices ? 3 : 2;
			const chargerYAxis = hasPrices ? 4 : 3;

			const stripRender = (
				params: { coordSys: { y: number; height: number } },
				api: {
					value: (dim: number) => number;
					coord: (val: number[]) => number[];
					style: () => Record<string, unknown>;
				}
			) => {
				const start = api.coord([api.value(0), 0]);
				const end = api.coord([api.value(1), 0]);
				const y = params.coordSys.y + 2;
				const h = Math.max(4, params.coordSys.height - 4);
				return {
					type: "rect",
					transition: [],
					shape: {
						x: start[0],
						y,
						width: Math.max(1, end[0] - start[0] - 1),
						height: h,
					},
					style: api.style(),
				};
			};

			const series: Record<string, unknown>[] = [
				{
					name: this.$t("peakShave.plan.house"),
					type: "bar",
					xAxisIndex: 0,
					yAxisIndex: 0,
					stack: "load",
					barMaxWidth: 10,
					data: house,
				},
				{
					name: this.$t("peakShave.plan.charging"),
					type: "bar",
					xAxisIndex: 0,
					yAxisIndex: 0,
					stack: "load",
					barMaxWidth: 10,
					itemStyle: { color: loadColor },
					data: load,
				},
				{
					name: this.$t("peakShave.plan.solar"),
					type: "line",
					xAxisIndex: 0,
					yAxisIndex: 0,
					showSymbol: false,
					smooth: 0.2,
					lineStyle: { width: 2, color: solarColor },
					itemStyle: { color: solarColor },
					areaStyle: { color: solarColor, opacity: 0.16 },
					data: solar,
				},
				{
					name: this.$t("peakShave.plan.soc"),
					type: "line",
					xAxisIndex: 0,
					yAxisIndex: 1,
					showSymbol: false,
					lineStyle: { width: 2.5, color: socColor },
					itemStyle: { color: socColor },
					data: soc,
				},
			];

			if (cover.length) {
				series.push({
					name: this.$t("peakShave.plan.cover"),
					type: "line",
					xAxisIndex: 0,
					yAxisIndex: 1,
					showSymbol: false,
					lineStyle: { width: 1.5, type: "dashed", color: COVER_COLOR },
					itemStyle: { color: COVER_COLOR },
					data: cover,
				});
			}

			if (hasPrices) {
				series.push({
					name: this.$t("peakShave.plan.price"),
					type: "line",
					xAxisIndex: 0,
					yAxisIndex: priceAxis,
					showSymbol: false,
					connectNulls: false,
					lineStyle: { width: 1.5, type: "dashed", color: priceColor },
					itemStyle: { color: priceColor },
					data: price,
				});
			}

			series.push({
				name: this.$t("peakShave.plan.batteryStrip"),
				type: "custom",
				xAxisIndex: 1,
				yAxisIndex: batYAxis,
				renderItem: stripRender,
				encode: { x: [0, 1] },
				data: batteryStrip,
				tooltip: { show: false },
			});

			if (hasCharger) {
				series.push({
					name: this.$t("peakShave.plan.chargerStrip"),
					type: "custom",
					xAxisIndex: 2,
					yAxisIndex: chargerYAxis,
					renderItem: stripRender,
					encode: { x: [0, 1] },
					data: chargerStrip,
					tooltip: { show: false },
				});
			}

			if (this.hasMeasured && this.nowInWindow) {
				series.push({
					type: "line",
					xAxisIndex: 0,
					yAxisIndex: 0,
					data: [],
					silent: true,
					markArea: {
						silent: true,
						itemStyle: { color: muted, opacity: 0.06 },
						data: [[{ xAxis: this.windowStartMs }, { xAxis: this.nowMs }]],
					},
				});
			}

			if (this.nowInWindow) {
				series.push({
					type: "line",
					xAxisIndex: 0,
					yAxisIndex: 0,
					data: [],
					silent: true,
					markLine: {
						symbol: "none",
						label: { show: false },
						lineStyle: { color: muted, width: 1.5, type: "solid" },
						data: [{ xAxis: this.nowMs }],
					},
				});
			}

			if (threshold) {
				series.push({
					type: "line",
					xAxisIndex: 0,
					yAxisIndex: 0,
					data: [],
					silent: true,
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
				});
			}

			const axisPointerLink = xAxes.map((_, i) => ({ xAxisIndex: i }));

			return {
				animation: false,
				axisPointer: {
					link: axisPointerLink,
					snap: true,
				},
				grid: grids,
				tooltip: {
					...tooltipStyle(colors.text || "#111", () => this.chart),
					trigger: "axis",
					axisPointer: { type: "line" },
					formatter: (params: unknown) => this.tooltipHtml(params),
				},
				xAxis: xAxes,
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
		nowMs() {
			this.$nextTick(() => this.ensureChart());
		},
	},
	mounted() {
		window.addEventListener("resize", this.resize);
		this.nowTimer = window.setInterval(() => {
			this.nowMs = Date.now();
		}, 30000);
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
			this.chart?.off("updateAxisPointer");
			this.chart?.off("globalout");
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
				this.chart.on("updateAxisPointer", (event: unknown) => {
					this.onAxisPointer(event);
				});
				this.chart.on("globalout", () => {
					this.activeIndex = null;
				});
			}
			this.chart.setOption(this.chartOption, { notMerge: true });
		},
		async replan() {
			if (this.replanning) {
				return;
			}
			this.replanning = true;
			try {
				await api.post("batteryplan");
			} finally {
				this.replanning = false;
			}
		},
		onAxisPointer(event: unknown) {
			const ev = event as {
				axesInfo?: { value?: number }[];
			};
			const value = ev.axesInfo?.[0]?.value;
			if (typeof value !== "number") {
				return;
			}
			const idx = this.slotIndexAt(value);
			if (idx >= 0) {
				this.activeIndex = idx;
			}
		},
		slotIndexAt(timeMs: number): number {
			const slots = this.parsedSlots;
			for (let i = 0; i < slots.length; i++) {
				const s = slots[i];
				if (timeMs >= s.start.getTime() && timeMs < s.end.getTime()) {
					return i;
				}
			}
			// Nearest slot by start (axis snap can land on boundary).
			let best = 0;
			let bestDist = Infinity;
			for (let i = 0; i < slots.length; i++) {
				const mid =
					slots[i].start.getTime() +
					(slots[i].end.getTime() - slots[i].start.getTime()) / 2;
				const dist = Math.abs(mid - timeMs);
				if (dist < bestDist) {
					bestDist = dist;
					best = i;
				}
			}
			return slots.length ? best : -1;
		},
		isNowSlot(slot: ParsedSlot) {
			return this.nowMs >= slot.start.getTime() && this.nowMs < slot.end.getTime();
		},
		activeSlotLoads(slot: ParsedSlot): BatteryPlanSlotLoad[] {
			if (slot.measured) {
				return [];
			}
			if (slot.loads?.length) {
				return slot.loads.filter((l) => (l.loadW || 0) > 0);
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
		tooltipHtml(params: unknown) {
			const list = Array.isArray(params) ? params : [params];
			const first = list[0] as {
				axisValue?: number | string;
				dataIndex?: number;
				value?: number | [number, number];
			};
			let idx = -1;
			if (typeof first?.axisValue === "number") {
				idx = this.slotIndexAt(first.axisValue);
			} else if (typeof first?.axisValue === "string") {
				idx = this.slotIndexAt(new Date(first.axisValue).getTime());
			} else if (Array.isArray(first?.value) && typeof first.value[0] === "number") {
				idx = this.slotIndexAt(first.value[0]);
			} else if (typeof first?.dataIndex === "number") {
				idx = first.dataIndex;
			}
			const slot = this.parsedSlots[idx];
			if (!slot) {
				return "";
			}
			this.activeIndex = idx;

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
			if ((slot.loadW || 0) > 50 && !slot.measured) {
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
				rows.push({
					name: this.$t("peakShave.plan.soc"),
					values: [`${Math.round(slot.soc)}%`],
				});
			}
			if (!slot.measured && (slot.coverSoc || 0) > 0) {
				rows.push({
					name: this.$t("peakShave.plan.cover"),
					values: [`${Math.round(slot.coverSoc || 0)}%`],
				});
			}
			if (slot.hasPrice && slot.price) {
				rows.push({
					name: this.$t("peakShave.plan.price"),
					values: [this.fmtPricePerKWh(slot.price, this.currency)],
				});
			}
			if (slot.peak) {
				rows.push({
					name: this.$t("peakShave.plan.threshold"),
					values: [this.$t("peakShave.plan.overLimit")],
				});
			}
			const head = this.isNowSlot(slot)
				? `${this.$t("peakShave.plan.now")} · ${this.slotRange(slot)}`
				: this.slotRange(slot);
			return tooltipTable(head, rows);
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
.action-charge {
	background: #1d4ed8;
}
.action-discharge {
	background: #0d9488;
}
.action-hold {
	background: #ea580c;
}
.action-normal {
	background: #94a3b8;
}
.action-export {
	background: #be185d;
}
.action-charger {
	background: #7c3aed;
}
.series-cover {
	background: transparent;
	border: 2px dashed #c026d3;
}
.series-house {
	background: #475569;
}
.series-solar {
	background: #ca8a04;
}
.series-soc {
	background: #16a34a;
}
.series-price {
	background: #0891b2;
}
.series-measured {
	background: #475569;
	opacity: 0.45;
}
.legend-defs {
	display: grid;
	grid-template-columns: auto 1fr;
	column-gap: 0.75rem;
	row-gap: 0.35rem;
	margin: 0;
}
.legend-defs dt {
	font-weight: 600;
}
.legend-defs dd {
	margin: 0;
	color: var(--bs-secondary-color, #6c757d);
}
.forecast-chart {
	height: 320px;
	width: 100%;
}
.forecast-chart.has-charger {
	height: 360px;
}
.slot-summary {
	min-height: 4.5rem;
}
</style>
