import settings from "@/settings";

export function isEnergyPriceDisplay(): boolean {
  return settings.showEnergyPrice;
}

export function roundPrice(value: number): number {
  return Math.round(value * 1000) / 1000;
}

export function displayGridPrice(allIn: number, energy?: number | null): number {
  if (isEnergyPriceDisplay() && energy != null) {
    return energy;
  }
  return allIn;
}

export function mapSlotPrices<T extends { value: number; energy?: number }>(slots: T[] | undefined): T[] {
  if (!slots?.length) {
    return slots || [];
  }
  if (!isEnergyPriceDisplay()) {
    return slots;
  }
  return slots.map((s) => ({
    ...s,
    value: s.energy != null ? s.energy : s.value,
  }));
}
