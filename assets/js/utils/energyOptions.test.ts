import { describe, expect, test } from "vitest";
import {
  DEFAULT_HEATING_PLAN_HOURS,
  energyFromHours,
  hoursFromEnergy,
} from "./energyOptions";

describe("heating plan duration", () => {
  test("converts hours to energy at heater power", () => {
    expect(energyFromHours(2, 3000)).toBe(6);
    expect(energyFromHours(1.5, 3000)).toBe(4.5);
    expect(energyFromHours(DEFAULT_HEATING_PLAN_HOURS, 2750)).toBe(5.5);
  });

  test("converts energy back to hours", () => {
    expect(hoursFromEnergy(6, 3000)).toBe(2);
    expect(hoursFromEnergy(4.5, 3000)).toBe(1.5);
  });

  test("returns zero for invalid power", () => {
    expect(energyFromHours(2, 0)).toBe(0);
    expect(hoursFromEnergy(6, 0)).toBe(0);
  });
});
