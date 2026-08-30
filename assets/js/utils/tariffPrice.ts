import settings from "@/settings";

export function canShowEnergyPrice(charges = 0, tax = 0, formula = false): boolean {
  return !formula && (charges !== 0 || tax !== 0);
}

export function isEnergyPriceDisplay(_charges = 0, _tax = 0, formula = false): boolean {
  return settings.showEnergyPrice && !formula;
}

export function energyFromAllIn(allIn: number, charges = 0, tax = 0): number {
  return allIn / (1 + tax) - charges;
}

export function allInFromEnergy(energy: number, charges = 0, tax = 0): number {
  return (energy + charges) * (1 + tax);
}

export function roundPrice(value: number): number {
  return Math.round(value * 1000) / 1000;
}

export function displayGridPrice(allIn: number, charges = 0, tax = 0, formula = false): number {
  if (!isEnergyPriceDisplay(charges, tax, formula)) {
    return allIn;
  }
  return energyFromAllIn(allIn, charges, tax);
}

export function storedGridPrice(display: number, charges = 0, tax = 0, formula = false): number {
  if (!isEnergyPriceDisplay(charges, tax, formula)) {
    return display;
  }
  return allInFromEnergy(display, charges, tax);
}

export function mapSlotPrices<T extends { value: number }>(
  slots: T[] | undefined,
  charges = 0,
  tax = 0,
  formula = false
): T[] {
  if (!slots?.length) {
    return slots || [];
  }
  if (!isEnergyPriceDisplay(charges, tax, formula)) {
    return slots;
  }
  return slots.map((s) => ({ ...s, value: energyFromAllIn(s.value, charges, tax) }));
}
