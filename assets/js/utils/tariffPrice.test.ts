import { describe, expect, test, beforeEach } from "vitest";
import settings from "@/settings";
import {
  affineAllIn,
  allInFromEnergy,
  displayGridPrice,
  displayStoredLimit,
  energyFromAllIn,
  isEnergyPriceDisplay,
  mapSlotPrices,
  roundPrice,
  storedFromDisplay,
} from "./tariffPrice";

const slots = [
  { start: "a", value: 0.212, energy: 0.05 },
  { start: "b", value: 0.3392, energy: 0.17 },
];

describe("tariffPrice", () => {
  beforeEach(() => {
    settings.showEnergyPrice = true;
  });

  test("display uses source energy when present", () => {
    expect(displayGridPrice(0.21, 0.05)).toBe(0.05);
    expect(displayGridPrice(0.21, 0)).toBe(0);
  });

  test("display stays all-in without source energy", () => {
    expect(displayGridPrice(0.21)).toBe(0.21);
    expect(displayGridPrice(0.21, undefined)).toBe(0.21);
  });

  test("display stays all-in when toggle is off", () => {
    settings.showEnergyPrice = false;
    expect(isEnergyPriceDisplay()).toBe(false);
    expect(displayGridPrice(0.21, 0.05)).toBe(0.21);
  });

  test("mapSlotPrices uses energy and does not invert value", () => {
    const mapped = mapSlotPrices(slots);
    expect(mapped[0].value).toBe(0.05);
    expect(mapped[1].value).toBe(0.17);
  });

  test("mapSlotPrices keeps value when energy is missing", () => {
    expect(mapSlotPrices([{ start: "a", value: 0.21 }])[0].value).toBe(0.21);
  });

  test("affine fit recovers energy and all-in", () => {
    const c = affineAllIn(slots);
    expect(c).not.toBeNull();
    expect(energyFromAllIn(0.212, slots)).toBeCloseTo(0.05, 10);
    expect(allInFromEnergy(0.05, slots)).toBeCloseTo(0.212, 10);
    expect(allInFromEnergy(0.17, slots)).toBeCloseTo(0.3392, 10);
  });

  test("displayStoredLimit converts all-in 15ct to source energy", () => {
    const energy = displayStoredLimit(0.15, false, slots);
    expect(energy).not.toBeNull();
    expect(energy!).toBeCloseTo(energyFromAllIn(0.15, slots), 10);
    expect(energy!).toBeLessThan(0.15);
  });

  test("displayStoredLimit converts energy to all-in when toggle is off", () => {
    settings.showEnergyPrice = false;
    expect(displayStoredLimit(0.05, true, slots)).toBeCloseTo(0.212, 10);
  });

  test("storedFromDisplay keeps energy and converts all-in back", () => {
    expect(storedFromDisplay(0.05, slots)).toEqual({ value: 0.05, energy: true });
    settings.showEnergyPrice = false;
    const stored = storedFromDisplay(0.212, slots);
    expect(stored.energy).toBe(true);
    expect(stored.value).toBeCloseTo(0.05, 10);
  });

  test("roundPrice", () => {
    expect(roundPrice(0.0496)).toBe(0.05);
  });
});
