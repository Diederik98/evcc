<template>
	<div class="heating-comfort pt-3" data-testid="heating-comfort">
		<h5 class="fw-normal evcc-gray text-uppercase">{{ $t("heating.comfort.title") }}</h5>
		<p class="text-muted small">{{ $t("heating.comfort.help") }}</p>
		<p v-if="status?.estimated" class="small">
			<span class="badge text-bg-secondary">{{ $t("heating.estimated") }}</span>
			{{ $t("heating.estimatedHelp") }}
		</p>
		<p v-if="vehicleSoc" class="mb-2">
			{{ $t("heating.currentTemp", { temp: fmtTemp(vehicleSoc) }) }}
			<span v-if="status?.active" class="text-primary">
				· {{ $t("heating.reason." + (status.reason || "calendar")) }}
			</span>
		</p>
		<div class="row mb-3">
			<div class="col-md-4 mb-2">
				<label class="form-label" :for="formId('minTemp')">{{
					$t("heating.comfort.minTemp")
				}}</label>
				<div class="input-group">
					<input
						:id="formId('minTemp')"
						v-model.number="local.minTemp"
						type="number"
						step="0.5"
						min="0"
						max="80"
						class="form-control"
						@change="save"
					/>
					<span class="input-group-text">°C</span>
				</div>
			</div>
			<div class="col-md-4 mb-2">
				<label class="form-label" :for="formId('hysteresis')">{{
					$t("heating.comfort.hysteresis")
				}}</label>
				<div class="input-group">
					<input
						:id="formId('hysteresis')"
						v-model.number="local.hysteresis"
						type="number"
						step="0.5"
						min="0"
						max="10"
						class="form-control"
						@change="save"
					/>
					<span class="input-group-text">K</span>
				</div>
			</div>
			<div class="col-md-4 mb-2">
				<label class="form-label" :for="formId('stopTemp')">{{
					$t("heating.comfort.stopTemp")
				}}</label>
				<div class="input-group">
					<input
						:id="formId('stopTemp')"
						v-model.number="local.stopTemp"
						type="number"
						step="0.5"
						min="0"
						max="80"
						class="form-control"
						@change="save"
					/>
					<span class="input-group-text">°C</span>
				</div>
			</div>
		</div>
		<div class="row mb-3">
			<div class="col-md-4 mb-2">
				<label class="form-label" :for="formId('minOnTime')">{{
					$t("heating.comfort.minOnTime")
				}}</label>
				<select
					:id="formId('minOnTime')"
					v-model.number="local.minOnTime"
					class="form-select"
					@change="save"
				>
					<option :value="0">{{ $t("heating.comfort.minOnNone") }}</option>
					<option :value="900">15 min</option>
					<option :value="1800">30 min</option>
					<option :value="3600">60 min</option>
				</select>
			</div>
			<div class="col-md-4 mb-2">
				<label class="form-label" :for="formId('assumedPower')">{{
					$t("heating.comfort.assumedPower")
				}}</label>
				<div class="input-group">
					<input
						:id="formId('assumedPower')"
						v-model.number="assumedKw"
						type="number"
						step="0.1"
						min="0"
						max="20"
						class="form-control"
						@change="save"
					/>
					<span class="input-group-text">kW</span>
				</div>
			</div>
			<div class="col-md-4 mb-2">
				<label class="form-label" :for="formId('maxAssumed')">{{
					$t("heating.comfort.maxAssumedPower")
				}}</label>
				<div class="input-group">
					<input
						:id="formId('maxAssumed')"
						v-model.number="maxAssumedKw"
						type="number"
						step="0.1"
						min="0"
						max="20"
						class="form-control"
						@change="save"
					/>
					<span class="input-group-text">kW</span>
				</div>
			</div>
		</div>

		<template v-if="bands.length">
			<h6 class="mt-4">{{ $t("heating.pattern.title") }}</h6>
			<p v-for="(band, i) in bands" :key="i" class="small text-muted mb-1">
				{{
					$t("heating.pattern.band", {
						from: fmtTemp(band.minStartTemp || 0),
						to: fmtTemp(band.maxStartTemp || 0),
						minutes: Math.round(band.minutesPerK || 0),
						peak: fmtW(band.peakW || 0, POWER_UNIT.KW, true, 1),
						n: band.samples || 0,
					})
				}}
			</p>
		</template>

		<template v-if="boosts.length">
			<h6 class="mt-4">{{ $t("heating.boosts.title") }}</h6>
			<div class="table-responsive">
				<table class="table table-sm small">
					<thead>
						<tr>
							<th>{{ $t("heating.boosts.start") }}</th>
							<th>{{ $t("heating.boosts.temps") }}</th>
							<th>{{ $t("heating.boosts.energy") }}</th>
							<th>{{ $t("heating.boosts.quality") }}</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="(b, i) in boosts.slice(0, 8)" :key="i">
							<td>{{ fmtWhen(b.start) }}</td>
							<td>
								{{ fmtTemp(b.startTemp || 0) }} → {{ fmtTemp(b.endTemp || 0) }}
							</td>
							<td>{{ fmtWh(b.energyWh || 0, POWER_UNIT.KW, true, 1) }}</td>
							<td>{{ b.quality }}</td>
						</tr>
					</tbody>
				</table>
			</div>
		</template>
	</div>
</template>

<script lang="ts">
import { defineComponent, type PropType } from "vue";
import api from "@/api";
import formatter, { POWER_UNIT } from "@/mixins/formatter";
import type { HeatingComfort, HeatingStatus } from "@/types/evcc";

export default defineComponent({
	name: "HeatingComfortSettings",
	mixins: [formatter],
	props: {
		id: [Number, String],
		comfort: Object as PropType<HeatingComfort>,
		status: Object as PropType<HeatingStatus>,
		vehicleSoc: Number,
	},
	data() {
		return {
			local: {
				minTemp: this.comfort?.minTemp || 0,
				hysteresis: this.comfort?.hysteresis || 1.5,
				minOnTime: this.comfort?.minOnTime || 1800,
				assumedPowerW: this.comfort?.assumedPowerW || 0,
				maxAssumedPowerW: this.comfort?.maxAssumedPowerW || 6000,
				stopTemp: this.comfort?.stopTemp || 0,
			},
		};
	},
	computed: {
		POWER_UNIT() {
			return POWER_UNIT;
		},
		assumedKw: {
			get(): number {
				return (this.local.assumedPowerW || 0) / 1000;
			},
			set(v: number) {
				this.local.assumedPowerW = (v || 0) * 1000;
			},
		},
		maxAssumedKw: {
			get(): number {
				return (this.local.maxAssumedPowerW || 0) / 1000;
			},
			set(v: number) {
				this.local.maxAssumedPowerW = (v || 0) * 1000;
			},
		},
		bands() {
			return this.status?.pattern?.bands || [];
		},
		boosts() {
			return [...(this.status?.boosts || [])].reverse();
		},
	},
	watch: {
		comfort: {
			deep: true,
			handler(v: HeatingComfort) {
				if (!v) {
					return;
				}
				this.local = {
					minTemp: v.minTemp || 0,
					hysteresis: v.hysteresis || 1.5,
					minOnTime: v.minOnTime || 1800,
					assumedPowerW: v.assumedPowerW || 0,
					maxAssumedPowerW: v.maxAssumedPowerW || 6000,
					stopTemp: v.stopTemp || 0,
				};
			},
		},
	},
	methods: {
		formId(name: string): string {
			return `heating-lp${this.id}-${name}`;
		},
		fmtTemp(v: number): string {
			return `${v.toFixed(1)}°C`;
		},
		fmtWhen(v?: string): string {
			if (!v) {
				return "";
			}
			const d = new Date(v);
			if (Number.isNaN(d.getTime())) {
				return "";
			}
			return d.toLocaleString(undefined, {
				month: "short",
				day: "numeric",
				hour: "2-digit",
				minute: "2-digit",
			});
		},
		save(): void {
			api.post(`loadpoints/${this.id}/heating/comfort`, this.local);
		},
	},
});
</script>
