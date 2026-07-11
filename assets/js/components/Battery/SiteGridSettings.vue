<template>
	<div class="site-grid-settings">
		<!-- Grid threshold -->
		<div class="mb-4">
			<label for="gridThreshold" class="form-label fw-bold">
				{{ $t("peakShave.gridThreshold") }}
			</label>
			<p class="text-muted small mb-2">{{ $t("peakShave.gridThresholdHelp") }}</p>
			<div class="input-group">
				<input
					id="gridThreshold"
					v-model.number="localGridThreshold"
					type="number"
					min="0"
					step="0.5"
					class="form-control"
					@change="saveGridThreshold"
				/>
				<span class="input-group-text">kW</span>
			</div>
			<p v-if="gridPowerKw !== null" class="text-muted small mt-2 mb-0">
				{{ $t("peakShave.gridPowerLive", { power: gridPowerKw.toFixed(1) }) }}
			</p>
		</div>
	</div>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import api from "@/api";

export default defineComponent({
	name: "SiteGridSettings",
	props: {
		gridThreshold: { type: Number, default: 0 },
		gridPower: { type: Number, default: undefined },
	},
	data() {
		return {
			localGridThreshold: this.gridThreshold,
		};
	},
	computed: {
		gridPowerKw(): number | null {
			if (this.gridPower === undefined) {
				return null;
			}
			return Math.max(0, this.gridPower) / 1000;
		},
	},
	watch: {
		gridThreshold(v: number) {
			this.localGridThreshold = v;
		},
	},
	methods: {
		async saveGridThreshold() {
			try {
				await api.post(`gridthreshold/${encodeURIComponent(this.localGridThreshold)}`);
			} catch (err) {
				console.error(err);
			}
		},
	},
});
</script>

<style scoped>
</style>
