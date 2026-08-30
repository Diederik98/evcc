import settings from "@/settings";

export type PricedSlot = { value: number; energy?: number };

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

function energyAllInPoints(slots?: PricedSlot[]): { energy: number; allIn: number }[] {
  return (slots || []).filter(
    (s) => s.energy != null && Number.isFinite(s.energy) && Number.isFinite(s.value)
  ).map((s) => ({ energy: s.energy as number, allIn: s.value }));
}

// Fit all-in = a * energy + b from forecast slots (matches charges, tax, and formula).
export function affineAllIn(slots?: PricedSlot[]): { a: number; b: number } | null {
  const pts = energyAllInPoints(slots);
  if (!pts.length) {
    return null;
  }
  if (pts.length === 1) {
    return { a: 1, b: pts[0].allIn - pts[0].energy };
  }

  const n = pts.length;
  let sumE = 0;
  let sumV = 0;
  let sumEE = 0;
  let sumEV = 0;
  for (const p of pts) {
    sumE += p.energy;
    sumV += p.allIn;
    sumEE += p.energy * p.energy;
    sumEV += p.energy * p.allIn;
  }
  const det = n * sumEE - sumE * sumE;
  if (Math.abs(det) < 1e-12) {
    return { a: 1, b: sumV / n - sumE / n };
  }
  const a = (n * sumEV - sumE * sumV) / det;
  const b = (sumV - a * sumE) / n;
  return { a, b };
}

export function energyFromAllIn(allIn: number, slots?: PricedSlot[]): number {
  const c = affineAllIn(slots);
  if (!c || Math.abs(c.a) < 1e-9) {
    return allIn;
  }
  return (allIn - c.b) / c.a;
}

export function allInFromEnergy(energy: number, slots?: PricedSlot[]): number {
  const c = affineAllIn(slots);
  if (!c) {
    return energy;
  }
  return c.a * energy + c.b;
}

export function displayStoredLimit(
  stored: number | null | undefined,
  storedIsEnergy: boolean,
  slots?: PricedSlot[]
): number | null {
  if (stored == null) {
    return stored ?? null;
  }
  const showEnergy = isEnergyPriceDisplay();
  if (showEnergy === storedIsEnergy) {
    return stored;
  }
  if (showEnergy) {
    return energyFromAllIn(stored, slots);
  }
  return allInFromEnergy(stored, slots);
}

export function storedFromDisplay(display: number, slots?: PricedSlot[]): { value: number; energy: boolean } {
  if (!affineAllIn(slots)) {
    return { value: display, energy: false };
  }
  if (isEnergyPriceDisplay()) {
    return { value: display, energy: true };
  }
  return { value: energyFromAllIn(display, slots), energy: true };
}
