<template>
	<div class="container px-4 safe-area-inset">
		<TopHeader :title="$t('peakShave.plan.pageTitle')" />
		<div class="row">
			<main class="col-12">
				<p class="text-muted mb-4">
					{{ $t("peakShave.plan.pageIntro") }}
				</p>
				<p v-if="!batteryPlan" class="text-muted">
					{{ $t("peakShave.plan.noForecast") }}
				</p>
				<template v-else>
					<div class="mb-4">
						<router-link to="/battery" class="text-decoration-none">
							{{ $t("peakShave.plan.backToBattery") }}
						</router-link>
					</div>

					<section class="mb-5">
						<h3 class="fw-normal mb-3">{{ $t("peakShave.plan.howTitle") }}</h3>
						<ul class="list-unstyled">
							<li v-for="(fact, i) in facts" :key="i" class="mb-2">
								{{ factLabel(fact) }}
							</li>
						</ul>
					</section>

					<section v-if="explain" class="mb-5">
						<h3 class="fw-normal mb-3">{{ $t("peakShave.plan.inputsTitle") }}</h3>
						<p class="mb-1">
							{{ $t("peakShave.plan.inputSoc", { soc: Math.round(explain.soc || 0) }) }}
						</p>
						<p class="mb-1">
							{{
								$t("peakShave.plan.inputReserve", {
									min: Math.round(explain.minSoc || 0),
									reserve: Math.round(explain.reserveSoc || 0),
									target: Math.round(explain.targetSoc || 0),
								})
							}}
						</p>
						<p v-if="explain.gridThresholdW" class="mb-1">
							{{
								$t("peakShave.plan.inputLimit", {
									power: fmtW(explain.gridThresholdW, POWER_UNIT.KW, true, 1),
								})
							}}
						</p>
						<p v-if="explain.peakWh" class="mb-1">
							{{
								$t("peakShave.plan.inputPeak", {
									energy: fmtWh(explain.peakWh, POWER_UNIT.KW, true, 1),
								})
							}}
						</p>
						<p class="mb-0 text-muted small">
							{{
								$t("peakShave.plan.inputLive", {
									power: fmtW(explain.liveResidualW || 0, POWER_UNIT.KW, true, 1),
								})
							}}
						</p>
					</section>

					<section class="mb-5">
						<h3 class="fw-normal mb-3">{{ $t("peakShave.plan.pricesTitle") }}</h3>
						<p class="mb-0">{{ $t("peakShave.plan.pricesHelp") }}</p>
					</section>

					<section v-if="loads.length" class="mb-5">
						<h3 class="fw-normal mb-3">{{ $t("peakShave.plan.loadsTitle") }}</h3>
						<div v-for="(load, i) in loads" :key="i" class="mb-3">
							<p class="fw-bold mb-1">
								{{ load.title || $t("peakShave.plan.loadFallback") }}
								<span v-if="load.estimated" class="badge text-bg-secondary ms-1">{{
									$t("peakShave.plan.estimated")
								}}</span>
							</p>
							<p class="text-muted small mb-0">
								{{ loadSummary(load) }}
							</p>
						</div>
					</section>

					<section class="mb-5">
						<h3 class="fw-normal mb-3">{{ $t("peakShave.plan.horizonTitle") }}</h3>
						<BatteryPlanForecast
							:slots="batteryPlan.slots || []"
							:grid-threshold="state.gridThreshold"
							:currency="state.currency"
							:peak-shave-state="state.peakShaveState ?? 'idle'"
						/>
						<div class="table-responsive">
							<table class="table table-sm small">
								<thead>
									<tr>
										<th>{{ $t("peakShave.plan.colTime") }}</th>
										<th>{{ $t("peakShave.plan.colAction") }}</th>
										<th>{{ $t("peakShave.plan.house") }}</th>
										<th>{{ $t("peakShave.plan.charging") }}</th>
										<th>{{ $t("peakShave.plan.solar") }}</th>
										<th>SoC</th>
									</tr>
								</thead>
								<tbody>
									<tr v-for="(slot, i) in tableSlots" :key="i" :class="{ 'table-warning': slot.peak }">
										<td>{{ slotRange(slot) }}</td>
										<td>
											{{ $t("peakShave.plan.action." + (slot.action || "normal")) }}
											<span v-if="slot.reason" class="text-muted">
												· {{ $t("peakShave.plan.reason." + slot.reason) }}
											</span>
										</td>
										<td>{{ fmtW(slot.homeW || 0, POWER_UNIT.KW, true, 1) }}</td>
										<td>{{ fmtW(slot.loadW || 0, POWER_UNIT.KW, true, 1) }}</td>
										<td>{{ fmtW(slot.solarW || 0, POWER_UNIT.KW, true, 1) }}</td>
										<td>{{ Math.round(slot.soc || 0) }}%</td>
									</tr>
								</tbody>
							</table>
						</div>
					</section>

					<section v-if="log.length" class="mb-5">
						<h3 class="fw-normal mb-3">{{ $t("peakShave.plan.logTitle") }}</h3>
						<ul class="list-unstyled">
							<li v-for="(entry, i) in log" :key="i" class="mb-2 small">
								<span class="text-muted">{{ fmtTime(entry.time) }}</span>
								· {{ $t("peakShave.plan.log." + entry.code) }}
								<span v-if="entry.detail" class="text-muted"> ({{ entry.detail }})</span>
							</li>
						</ul>
					</section>
				</template>
			</main>
		</div>
	</div>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import Header from "../components/Top/Header.vue";
import BatteryPlanForecast from "../components/Battery/BatteryPlanForecast.vue";
import store from "../store";
import formatter, { POWER_UNIT } from "../mixins/formatter";
import type {
	BatteryPlanFact,
	BatteryPlanLoad,
	BatteryPlanLogEntry,
	BatteryPlanSlot,
} from "../types/evcc";

export default defineComponent({
	name: "BatteryPlan",
	components: {
		TopHeader: Header,
		BatteryPlanForecast,
	},
	mixins: [formatter],
	head() {
		return { title: this.$t("peakShave.plan.pageTitle") };
	},
	computed: {
		POWER_UNIT() {
			return POWER_UNIT;
		},
		state() {
			return store.state;
		},
		batteryPlan() {
			return this.state.batteryPlan;
		},
		explain() {
			return this.batteryPlan?.explain;
		},
		facts(): BatteryPlanFact[] {
			return this.explain?.facts || [];
		},
		loads(): BatteryPlanLoad[] {
			return this.explain?.loads || [];
		},
		log(): BatteryPlanLogEntry[] {
			return [...(this.batteryPlan?.log || [])].reverse();
		},
		tableSlots(): BatteryPlanSlot[] {
			return (this.batteryPlan?.slots || []).filter((_, i) => i % 4 === 0);
		},
	},
	methods: {
		factLabel(fact: BatteryPlanFact): string {
			const key = `peakShave.plan.fact.${fact.code}`;
			if (this.$te(key)) {
				return this.$t(key, fact.params || {});
			}
			return fact.code;
		},
		loadSummary(load: BatteryPlanLoad): string {
			const start = load.start ? this.fmtTime(load.start) : "";
			const end = load.end ? this.fmtTime(load.end) : "";
			return this.$t("peakShave.plan.loadSummary", {
				energy: this.fmtWh(load.energyWh || 0, POWER_UNIT.KW, true, 1),
				power: this.fmtW(load.powerW || 0, POWER_UNIT.KW, true, 1),
				start,
				end,
				pattern: load.pattern || "",
			});
		},
		slotRange(slot: BatteryPlanSlot): string {
			if (!slot.start) {
				return "";
			}
			return this.fmtTime(slot.start);
		},
		fmtTime(value: string): string {
			const d = new Date(value);
			if (Number.isNaN(d.getTime())) {
				return "";
			}
			const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
			const now = new Date();
			if (d.toDateString() === now.toDateString()) {
				return time;
			}
			return `${d.toLocaleDateString(undefined, { weekday: "short" })} ${time}`;
		},
	},
});
</script>
