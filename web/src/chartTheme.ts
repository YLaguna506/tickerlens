// Chart colors for the dark report and for the light printed (PDF) report.
// Recharts takes color strings, not CSS variables, so the palette is picked
// in code; the print page provides "light".

import { createContext, useContext } from "react";

const DARK = {
  green: "#4cb26a",
  secondary: "#7fb889",
  ink: "#e6edf3",
  grid: "#2a313c",
  zero: "#3b4350",
  axis: { fontSize: 12, fill: "#8b949e" },
  tip: {
    contentStyle: { background: "#161b22", border: "1px solid #2a313c", borderRadius: 8, color: "#e6edf3" },
    labelStyle: { color: "#8b949e" },
    itemStyle: { color: "#e6edf3" },
  },
  // A halo in the background color keeps point labels readable where they
  // cross a line.
  pointLabel: { fontSize: 12, fill: "#e6edf3", stroke: "#161b22", strokeWidth: 4, paintOrder: "stroke" },
  cheap: "#14301d",
  fair: "#2f2710",
  expensive: "#3a1d1c",
  median: "#d4a72c",
};

export type Palette = typeof DARK;

const LIGHT: Palette = {
  green: "#059669",
  secondary: "#34d399",
  ink: "#18181b",
  grid: "#e4e4e7",
  zero: "#a1a1aa",
  axis: { fontSize: 12, fill: "#71717a" },
  tip: {
    contentStyle: { background: "#ffffff", border: "1px solid #e4e4e7", borderRadius: 8, color: "#18181b" },
    labelStyle: { color: "#71717a" },
    itemStyle: { color: "#18181b" },
  },
  pointLabel: { fontSize: 12, fill: "#18181b", stroke: "#ffffff", strokeWidth: 4, paintOrder: "stroke" },
  cheap: "#d1fae5",
  fair: "#fef3c7",
  expensive: "#ffe4e6",
  median: "#d97706",
};

export const ChartTheme = createContext<"dark" | "light">("dark");

export function usePalette(): Palette {
  return useContext(ChartTheme) === "light" ? LIGHT : DARK;
}
