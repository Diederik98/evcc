<template>
	<div>
		<SmartTariffBase
			v-bind="labels"
			:current-limit="displayLimit"
			:last-limit="displayLastLimit"
			:is-co2="isCo2"
			:currency="currency"
			:apply-all="multipleLoadpoints && isLoadpoint"
			:possible="possible"
			:tariff="displayTariff"
			:form-id="formId"
			:is-slot-active="isSlotActive"
			limit-direction="below"
			:options-start-at-zero="isCo2"
			:show-energy-toggle="energyToggleVisible"
			:energy-price-display="energyPriceDisplay"
			@save-limit="saveLimit"
			@delete-limit="deleteLimit"
			@apply-to-all="applyToAll"
			@toggle-energy-price="toggleEnergyPrice"
		/>
	</div>
</template>

<script lang="ts">
import SmartTariffBase from "./SmartTariffBase.vue";
import { defineComponent, type PropType } from "vue";
import api from "@/api";
import { setLoadpointLastSmartCostLimit } from "@/uiLoadpoints";
import settings from "@/settings";
import { type CURRENCY, SMART_COST_TYPE } from "@/types/evcc";
import { type ForecastSlot } from "../Forecast/types";
import {
	displayGridPrice,
	isEnergyPriceDisplay,
	mapSlotPrices,
	roundPrice,
	storedGridPrice,
} from "@/utils/tariffPrice";

export default defineComponent({
	name: "SmartCostLimit",
	components: { SmartTariffBase },
	props: {
		currentLimit: {
			type: [Number, null] as PropType<number | null>,
			required: true,
		},
		smartCostType: String as PropType<SMART_COST_TYPE>,
		currency: String as PropType<CURRENCY>,
		multipleLoadpoints: Boolean,
		isLoadpoint: Boolean,
		loadpointId: String,
		possible: Boolean,
		lastLimit: Number,
		tariff: Array as PropType<ForecastSlot[]>,
		tariffCharges: { type: Number, default: 0 },
		tariffTax: { type: Number, default: 0 },
		tariffFormula: Boolean,
	},
	computed: {
		isCo2(): boolean {
			return this.smartCostType === SMART_COST_TYPE.CO2;
		},
		formId(): string {
			return `smartCostLimit-${this.loadpointId || "battery"}`;
		},
		energyToggleVisible(): boolean {
			return !this.isCo2 && !this.tariffFormula;
		},
		energyPriceDisplay(): boolean {
			return (
				!this.isCo2 &&
				isEnergyPriceDisplay(this.tariffCharges, this.tariffTax, this.tariffFormula)
			);
		},
		displayLimit(): number | null {
			if (this.currentLimit === null || this.isCo2) {
				return this.currentLimit;
			}
			return roundPrice(
				displayGridPrice(
					this.currentLimit,
					this.tariffCharges,
					this.tariffTax,
					this.tariffFormula
				)
			);
		},
		displayLastLimit(): number | undefined {
			if (this.lastLimit === undefined || this.isCo2 || !this.lastLimit) {
				return this.lastLimit;
			}
			return roundPrice(
				displayGridPrice(
					this.lastLimit,
					this.tariffCharges,
					this.tariffTax,
					this.tariffFormula
				)
			);
		},
		displayTariff(): ForecastSlot[] | undefined {
			if (this.isCo2) {
				return this.tariff;
			}
			return mapSlotPrices(
				this.tariff,
				this.tariffCharges,
				this.tariffTax,
				this.tariffFormula
			);
		},
		labels() {
			const t = (key: string) => this.$t(`smartCost.${key}`);
			const co2 = this.isCo2;
			const lp = this.isLoadpoint;
			return {
				title: lp ? (co2 ? t("cleanTitle") : t("cheapTitle")) : "",
				description: lp ? t("loadpointDescription") : t("batteryDescription"),
				limitLabel: co2 ? t("co2Limit") : t("priceLimit"),
				currentPriceLabel: co2 ? t("co2Label") : t("priceLabel"),
				resetWarningKey: "smartCost.resetWarning",
				activeHoursLabel: t("activeHoursLabel"),
			};
		},
	},
	methods: {
		isSlotActive(value: number | undefined): boolean {
			if (value === undefined || this.displayLimit === null) {
				return false;
			}
			return value <= this.displayLimit;
		},
		toStored(limit: number): number {
			if (this.isCo2) {
				return limit;
			}
			return storedGridPrice(limit, this.tariffCharges, this.tariffTax, this.tariffFormula);
		},
		async saveLimit(limit: number, active: boolean) {
			const stored = this.toStored(limit);
			this.saveLastLimit(stored);

			if (!active) return;

			const url = this.isLoadpoint
				? `loadpoints/${this.loadpointId}/smartcostlimit`
				: "batterygridchargelimit";

			await api.post(`${url}/${encodeURIComponent(stored)}`);
		},
		saveLastLimit(limit: number) {
			if (this.isLoadpoint) {
				setLoadpointLastSmartCostLimit(this.loadpointId!, limit);
			} else {
				settings.lastBatterySmartCostLimit = limit;
			}
		},
		async deleteLimit() {
			this.saveLastLimit(this.currentLimit || 0);

			const url = this.isLoadpoint
				? `loadpoints/${this.loadpointId}/smartcostlimit`
				: "batterygridchargelimit";

			await api.delete(url);
		},
		async applyToAll(selectedLimit: number | null) {
			if (selectedLimit === null) {
				await api.delete("smartcostlimit");
			} else {
				const stored = this.toStored(selectedLimit);
				await api.post(`smartcostlimit/${encodeURIComponent(stored)}`);
			}
		},
		toggleEnergyPrice() {
			settings.showEnergyPrice = !settings.showEnergyPrice;
		},
	},
});
</script>
