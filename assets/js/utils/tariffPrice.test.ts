import { describe, expect, test, beforeEach } from "vitest";
import settings from "@/settings";
import {
  allInFromEnergy,
  canShowEnergyPrice,
  displayGridPrice,
  energyFromAllIn,
  isEnergyPriceDisplay,
  mapSlotPrices,
  roundPrice,
  storedGridPrice,
} from "./tariffPrice";

const charges = 0.15;
const tax = 0.06;

describe("tariffPrice", () => {
  beforeEach(() => {
    settings.showEnergyPrice = true;
  });

  test("energy and all-in round-trip", () => {
    const energy = 0.05;
    const allIn = allInFromEnergy(energy, charges, tax);
    expect(allIn).toBeCloseTo((0.05 + 0.15) * 1.06, 10);
    expect(energyFromAllIn(allIn, charges, tax)).toBeCloseTo(energy, 10);
  });

  test("canShowEnergyPrice", () => {
    expect(canShowEnergyPrice(0, 0, false)).toBe(false);
    expect(canShowEnergyPrice(0.15, 0, false)).toBe(true);
    expect(canShowEnergyPrice(0, 0.06, false)).toBe(true);
    expect(canShowEnergyPrice(0.15, 0.06, true)).toBe(false);
  });

  test("display and store convert when toggle is on", () => {
    expect(displayGridPrice(0.212, charges, tax)).toBeCloseTo(0.05, 10);
    expect(storedGridPrice(0.05, charges, tax)).toBeCloseTo(0.212, 10);
  });

  test("display and store stay all-in when toggle is off", () => {
    settings.showEnergyPrice = false;
    expect(isEnergyPriceDisplay(charges, tax)).toBe(false);
    expect(displayGridPrice(0.212, charges, tax)).toBe(0.212);
    expect(storedGridPrice(0.05, charges, tax)).toBe(0.05);
  });

  test("energy display is on even without charges or tax", () => {
    expect(isEnergyPriceDisplay(0, 0)).toBe(true);
    expect(displayGridPrice(0.15, 0, 0)).toBeCloseTo(0.15, 10);
  });

  test("formula still converts with charges and tax", () => {
    expect(isEnergyPriceDisplay(charges, tax, true)).toBe(true);
    expect(displayGridPrice(0.212, charges, tax, true)).toBeCloseTo(0.05, 10);
  });

  test("mapSlotPrices converts values", () => {
    const slots = mapSlotPrices(
      [
        { start: "a", value: 0.212 },
        { start: "b", value: 0.3392 },
      ],
      charges,
      tax
    );
    expect(slots[0].value).toBeCloseTo(0.05, 10);
    expect(slots[1].value).toBeCloseTo(0.17, 10);
  });

  test("roundPrice", () => {
    expect(roundPrice(0.0496)).toBe(0.05);
  });
});
