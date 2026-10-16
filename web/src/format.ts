// Number formatting shared by every component.

import type { Metric } from "./api";
import { dateLocale } from "./i18n";

export const pct = (v: number | null | undefined, digits = 1) =>
  v == null ? "–" : `${(v * 100).toFixed(digits)}%`;

export const signedPct = (v: number | null | undefined, digits = 1) =>
  v == null ? "–" : `${v >= 0 ? "+" : ""}${(v * 100).toFixed(digits)}%`;

export const multiple = (v: number | null | undefined) => (v == null ? "–" : `${v.toFixed(1)}×`);

export const price = (v: number | null | undefined) =>
  v == null ? "–" : `$${v.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;

export const wholePrice = (v: number | null | undefined) => (v == null ? "–" : `$${Math.round(v)}`);

/** $1.23T, $44.5B, $512M */
export function usd(v: number | null | undefined): string {
  if (v == null) return "–";
  const sign = v < 0 ? "-" : "";
  const a = Math.abs(v);
  if (a >= 1e12) return `${sign}$${(a / 1e12).toFixed(2)}T`;
  if (a >= 1e9) return `${sign}$${(a / 1e9).toFixed(1)}B`;
  if (a >= 1e6) return `${sign}$${Math.round(a / 1e6).toLocaleString("en-US")}M`;
  return `${sign}$${Math.round(a).toLocaleString("en-US")}`;
}

export function metricValue(m: Metric): string {
  switch (m.unit) {
    case "percent":
      return pct(m.value);
    case "multiple":
      return m.key === "debtToEquity" ? (m.value == null ? "–" : `${m.value.toFixed(2)}×`) : multiple(m.value);
    case "usd/share":
      return price(m.value);
    default:
      return usd(m.value);
  }
}

export const shortDate = (iso: string) =>
  new Date(iso + "T00:00:00Z").toLocaleDateString(dateLocale(), { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" });

export const monthYear = (iso: string) =>
  new Date(iso + "T00:00:00Z").toLocaleDateString(dateLocale(), { month: "short", year: "2-digit", timeZone: "UTC" });

/** Axis label for a valuation multiple: one decimal below 10× so small
 * ranges (P/S from 0.7× to 3×) do not repeat the same label. */
export const multipleTick = (v: number) => (Math.abs(v) >= 10 ? `${Math.round(v)}×` : `${v.toFixed(1)}×`);

/** One x-axis tick per year (its first date in the data). A first year that
 * starts after June is left out, so its label does not crowd the next one. */
export function yearTicks(points: { date: string }[]): string[] {
  return points
    .filter((p, i) => (i === 0 ? p.date.slice(5, 7) <= "06" : p.date.slice(0, 4) !== points[i - 1]!.date.slice(0, 4)))
    .map((p) => p.date);
}

/** Color tone for a 1–5 score. */
export const toneFor = (v: number | null): "good" | "mid" | "bad" =>
  v == null ? "mid" : v >= 3.5 ? "good" : v >= 2.5 ? "mid" : "bad";

/** SEC names are often upper case ("VISA INC."); make them readable. */
export function titleCase(s: string): string {
  if (s !== s.toUpperCase()) return s;
  return s.toLowerCase().replace(/\b(\w)/g, (c) => c.toUpperCase());
}