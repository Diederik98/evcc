import formatter, { POWER_UNIT } from "../mixins/formatter";

export function optionStep(maxEnergy: number) {
  if (maxEnergy < 0.1) return 0.005;
  if (maxEnergy < 1) return 0.05;
  if (maxEnergy < 2) return 0.1;
  if (maxEnergy < 5) return 0.25;
  if (maxEnergy < 10) return 0.5;
  if (maxEnergy < 25) return 1;
  if (maxEnergy < 50) return 2;
  if (maxEnergy < 75) return 2.5;
  return 5;
}

export function fmtEnergy(
  energy: number = 0,
  step: number,
  fmtWh: InstanceType<typeof formatter>["fmtWh"],
  zeroText: any
) {
  if (energy === 0) {
    return zeroText;
  }
  const inKWh = step >= 0.1;
  const digits = inKWh && !Number.isInteger(step) ? 1 : 0;
  return fmtWh(energy * 1e3, inKWh ? POWER_UNIT.KW : POWER_UNIT.W, true, digits);
}

export function estimatedSoc(energy: number, socPerKwh?: number) {
  if (!socPerKwh) return null;
  return Math.round(energy * socPerKwh);
}

export const HEATING_PLAN_HOURS = [0.25, 0.5, 1, 1.5, 2, 3, 4, 6, 8];
export const DEFAULT_HEATING_PLAN_HOURS = 2;

export function energyFromHours(hours: number, maxPowerW: number): number {
  if (!(maxPowerW > 0) || !(hours > 0)) {
    return 0;
  }
  return parseFloat(((hours * maxPowerW) / 1000).toFixed(3));
}

export function hoursFromEnergy(energyKwh: number, maxPowerW: number): number {
  if (!(maxPowerW > 0) || !(energyKwh > 0)) {
    return 0;
  }
  return energyKwh / (maxPowerW / 1000);
}

export function energyOptions(
  fromEnergy: number,
  maxEnergy: number,
  fmtWh: InstanceType<typeof formatter>["fmtWh"],
  fmtPercentage: InstanceType<typeof formatter>["fmtPercentage"],
  zeroText: string,
  socPerKwh?: number,
  selectedValue?: number
) {
  const step = optionStep(maxEnergy);
  const result = [];

  // helper to create option
  const makeOption = (energy: number) => {
    let text = fmtEnergy(energy, step, fmtWh, zeroText);
    const disabled = energy < fromEnergy / 1e3 && energy !== 0;
    const soc = estimatedSoc(energy, socPerKwh);
    if (soc) {
      text += ` (+${fmtPercentage(soc)})`;
    }
    // prevent rounding errors
    const energyNormal = parseFloat(energy.toFixed(3));
    return { energy: energyNormal, text, disabled };
  };

  // add standard increments
  for (let energy = 0; energy <= maxEnergy; energy += step) {
    result.push(makeOption(energy));
  }

  // add selected value if it's not in the list
  if (selectedValue && !result.find((o) => o.energy === selectedValue)) {
    result.push(makeOption(selectedValue));
    result.sort((a, b) => a.energy - b.energy);
  }

  return result;
}
