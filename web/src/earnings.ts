// Dates of the next quarterly earnings report.

import type { NextEarnings } from "./api";
import { dateLocale, t } from "./i18n";

const DAY = 24 * 60 * 60 * 1000;

/** Whole days from today to an ISO date; negative once it has passed. */
export function daysUntil(iso: string): number {
  const now = new Date();
  const today = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
  return Math.round((Date.parse(iso + "T00:00:00Z") - today) / DAY);
}

/** "Tue, Oct 27" */
export const earningsDay = (iso: string, year = false) =>
  new Date(iso + "T00:00:00Z").toLocaleDateString(dateLocale(), {
    weekday: "short",
    month: "short",
    day: "numeric",
    ...(year ? { year: "numeric" } : {}),
    timeZone: "UTC",
  });

/** "in 9 days", "tomorrow", "today", "passed" */
export function inDays(n: number): string {
  if (n < 0) return t("passed");
  if (n === 0) return t("today");
  if (n === 1) return t("tomorrow");
  return t("in {n} days", { n });
}

const TIME_LABEL: Record<string, string> = { "pre-market": "before the open", "post-market": "after the close" };
export const earningsTime = (n: NextEarnings) => (n.time && TIME_LABEL[n.time] ? t(TIME_LABEL[n.time]!) : "");

/** Within a week: worth a highlight. */
export const isSoon = (n: NextEarnings) => {
  const d = daysUntil(n.date);
  return d >= 0 && d <= 7;
};

/** Short form for tables: "Oct 27 · in 9 days". */
export function earningsShort(n?: NextEarnings | null): string {
  if (!n) return "–";
  const day = new Date(n.date + "T00:00:00Z").toLocaleDateString(dateLocale(), { month: "short", day: "numeric", timeZone: "UTC" });
  return `${day} · ${inDays(daysUntil(n.date))}${n.source === "estimated" ? ` (${t("estimated")})` : ""}`;
}
