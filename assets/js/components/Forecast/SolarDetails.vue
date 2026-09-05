<template>
	<div v-if="days.length || orientationText">
		<div v-if="days.length" class="row gx-2 mt-1">
			<div v-for="day in days" :key="day.key" class="col-4" :class="`text-${day.align}`">
				<small>
					<span class="text-gray">{{ day.label }}</span>
					<br />
					<div
						class="d-flex flex-column flex-md-row column-gap-2"
						:class="`justify-content-md-${day.align}`"
					>
						<span class="text-primary fw-bold">{{ day.energy }}</span>
						<span v-if="day.note" class="text-gray">{{ day.note }}</span>
					</div>
				</small>
			</div>
		</div>
		<p v-if="orientationText" class="text-muted small mt-3 mb-0">
			{{ orientationText }}.
			<router-link to="/config#tariffs">{{ $t("forecast.orientationSuggestLink") }}</router-link>
		</p>
	</div>
</template>

<script lang="ts">
import { defineComponent, type PropType } from "vue";
import formatter, { POWER_UNIT } from "@/mixins/formatter";
import type { SolarDetails } from "./types";

export default defineComponent({
	name: "SolarDetails",
	mixins: [formatter],
	props: {
		solar: { type: Object as PropType<SolarDetails> },
	},
	computed: {
		days(): {
			key: string;
			energy: string;
			label: string;
			align: string;
			note: string;
		}[] {
			const s = this.solar;
			if (!s) return [];
			const dayAfterTomorrow = new Date();
			dayAfterTomorrow.setDate(dayAfterTomorrow.getDate() + 2);
			const dayAfterLabel = this.weekdayLong(dayAfterTomorrow);
			const days = [
				{
					key: "today",
					data: s.today,
					label: this.$t("forecast.solar.today"),
					align: "start",
					note: this.$t("forecast.solar.remaining"),
				},
				{
					key: "tomorrow",
					data: s.tomorrow,
					label: this.$t("forecast.solar.tomorrow"),
					align: "center",
					note: "",
				},
				{
					key: "dayAfterTomorrow",
					data: s.dayAfterTomorrow,
					label: dayAfterLabel,
					align: "end",
					note:
						s.dayAfterTomorrow && !s.dayAfterTomorrow.complete
							? this.$t("forecast.solar.partly")
							: "",
				},
			];
			return days.map((d) => ({
				key: d.key,
				energy: d.data?.energy ? this.fmtWh(d.data.energy, POWER_UNIT.AUTO) : "-",
				label: d.label,
				align: d.align,
				note: d.data?.energy ? d.note : "",
			}));
		},
		orientationText(): string {
			const o = this.solar?.orientation;
			if (!o) {
				return "";
			}
			const params = {
				azimuth: Math.round(o.azimuth),
				decline: Math.round(o.decline),
				days: o.days,
				configuredAzimuth: o.configuredAzimuth != null ? Math.round(o.configuredAzimuth) : undefined,
				configuredDecline: o.configuredDecline != null ? Math.round(o.configuredDecline) : undefined,
			};
			if (
				o.configuredAzimuth != null &&
				o.configuredDecline != null &&
				(Math.abs(o.configuredAzimuth - o.azimuth) >= 15 ||
					Math.abs(o.configuredDecline - o.decline) >= 10)
			) {
				return this.$t("forecast.orientationSuggestDiff", params);
			}
			if (o.configuredAzimuth == null || o.configuredDecline == null) {
				return this.$t("forecast.orientationSuggest", params);
			}
			return this.$t("forecast.orientationSuggestApply", params);
		},
	},
});
</script>
