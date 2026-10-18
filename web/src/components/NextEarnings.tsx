// The next quarterly earnings report: the date, how far away it is, and
// whether it is announced or estimated from last year's schedule.

import type { NextEarnings } from "../api";
import { daysUntil, earningsDay, earningsTime, inDays, isSoon } from "../earnings";
import { price } from "../format";
import { t } from "../i18n";

const sourceNote = (n: NextEarnings) =>
  n.source === "calendar"
    ? t("Date from Alpha Vantage's earnings calendar.")
    : t("Not announced yet: estimated from the date of the same quarter's report last year.");

/** A pill for a report's header: "Next earnings Tue, Oct 27 · in 9 days". */
export function EarningsPill({ n, className = "" }: { n?: NextEarnings; className?: string }) {
  if (!n) return null;
  const d = daysUntil(n.date);
  const style = d < 0 ? "border-line text-muted" : isSoon(n) ? "border-mid/50 bg-mid-soft font-semibold" : "border-line";
  return (
    <span className={`inline-flex flex-wrap items-center gap-x-1.5 rounded-full border px-3 py-1 text-sm ${style} ${className}`} title={sourceNote(n)}>
      <span>{t("Next earnings")}</span>
      <span className="font-semibold">{earningsDay(n.date)}</span>
      <span className="text-muted">
        · {inDays(d)}
        {n.source === "estimated" && <> · {t("estimated")}</>}
      </span>
    </span>
  );
}

/** A sentence for the earnings card: date, time, the analysts' estimate. */
export function NextEarningsLine({ n }: { n?: NextEarnings }) {
  if (!n) return null;
  const d = daysUntil(n.date);
  const when = [earningsDay(n.date, true), earningsTime(n)].filter(Boolean).join(", ");
  return (
    <p className="text-sm">
      <span className={isSoon(n) ? "font-semibold" : ""}>
        {d < 0 ? t("The next report was due {when}; this report was built before it.", { when }) : t("Next report: {when} ({days}).", { when, days: inDays(d) })}
      </span>{" "}
      {n.estimateEps != null && <>{t("Analysts expect earnings per share of {eps}.", { eps: price(n.estimateEps) })} </>}
      <span className="text-muted">{sourceNote(n)}</span>
    </p>
  );
}
