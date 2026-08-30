import { describe, expect, test, beforeEach } from "vitest";
import settings from "@/settings";
import { displayGridPrice, isEnergyPriceDisplay, mapSlotPrices, roundPrice } from "./tariffPrice";

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
    const slots = mapSlotPrices([
      { start: "a", value: 0.21, energy: 0.05 },
      { start: "b", value: 0.34, energy: 0.17 },
    ]);
    expect(slots[0].value).toBe(0.05);
    expect(slots[1].value).toBe(0.17);
  });

  test("mapSlotPrices keeps value when energy is missing", () => {
    const slots = mapSlotPrices([{ start: "a", value: 0.21 }]);
    expect(slots[0].value).toBe(0.21);
  });

  test("formula slots still use source energy, not inverted value", () => {
    const slots = mapSlotPrices([{ start: "a", value: 0.85, energy: 0.05 }]);
    expect(slots[0].value).toBe(0.05);
  });

  test("roundPrice", () => {
    expect(roundPrice(0.0496)).toBe(0.05);
  });
});
