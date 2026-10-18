// "What the price expects": the reverse DCF. The growth the market price
// implies, set against the company's own track record.

import type { MetricsReport } from "../api";
import { pct, usd } from "../format";
import { t, tx } from "../i18n";
import { EXPECTATION_LABEL } from "../live";

const EXPECTATION_STYLE: Record<string, string> = {
  low: "bg-good text-white",
  "in line": "bg-good-soft text-good-strong",
  high: "bg-mid text-[#1a1404]",
  "very high": "bg-bad text-white",
};

const EXPECTATION_TEXT: Record<string, string> = {
  low: "The price asks for less growth than the company has delivered.",
  "in line": "The price asks for about the growth the company has delivered.",
  high: "The price asks for more growth than the company has delivered.",
  "very high": "The price asks for far more growth than the company has delivered.",
};

const CAPPED_TEXT: Record<string, string> = {
  "in line": "The price asks for about as much growth as companies usually keep up for 10 years.",
  high: "The price asks for more growth than companies usually keep up for 10 years.",
  "very high": "The price asks for far more growth than companies usually keep up for 10 years.",
};

export function ReverseDCFCard({ m }: { m?: MetricsReport }) {
  const d = m?.reverseDcf;
  if (!d) return null;
  const g = d.impliedGrowth;
  // One scale for every bar: from the lowest value (or 0) to the highest.
  const values = [g, ...d.history.map((h) => h.value)].filter((v): v is number => v != null);
  const lo = Math.min(0, ...values);
  const hi = Math.max(0.05, ...values);
  const width = (v: number) => `${Math.max(((v - lo) / (hi - lo)) * 100, 1)}%`;

  return (
    <div className="card fade-in space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <p className="font-display text-lg font-semibold">{t("What the price expects")}</p>
          {g != null ? (
            <p className="mt-1 text-muted">
              {t("Today's market cap of {cap} assumes free cash flow grows about", { cap: usd(d.marketCap) })}{" "}
              <span className="font-display text-2xl font-bold text-ink">{pct(g, 1)}</span> {t("a year for {n} years.", { n: d.years })}
            </p>
          ) : (
            <p className="mt-1 text-sm text-muted">{tx(d.note ?? "")}</p>
          )}
        </div>
        {d.expectations && (
          <span className={`shrink-0 rounded-full px-3 py-1 text-sm font-bold ${EXPECTATION_STYLE[d.expectations] ?? ""}`}>
            {t(EXPECTATION_LABEL[d.expectations]!)}
          </span>
        )}
      </div>

      {g != null && (
        <>
          <ul className="space-y-2 text-sm">
            <li className="grid grid-cols-[10rem_1fr_4rem] items-center gap-3 sm:grid-cols-[14rem_1fr_4rem]">
              <span className="font-semibold">{t("Implied by the price")}</span>
              <span className="h-3 rounded-full bg-line">
                <span className="block h-3 rounded-full bg-navy" style={{ width: width(g) }} />
              </span>
              <span className="text-right font-semibold tabular-nums">{pct(g, 1)}</span>
            </li>
            {d.history.map((h) =>
              h.value == null ? null : (
                <li key={h.name} className="grid grid-cols-[10rem_1fr_4rem] items-center gap-3 sm:grid-cols-[14rem_1fr_4rem]">
                  <span className="text-muted">{t("Actual {what}", { what: tx(h.name) })}</span>
                  <span className="h-3 rounded-full bg-line">
                    <span className={`block h-3 rounded-full ${h.name === d.referenceName ? "bg-good" : "bg-good/50"}`} style={{ width: width(h.value) }} />
                  </span>
                  <span className="text-right tabular-nums">{pct(h.value, 1)}</span>
                </li>
              ),
            )}
          </ul>
          {d.expectations && (
            <div className="space-y-1 text-sm">
              <p>
                {t(
                  // Against a capped record, the comparison is with what
                  // companies usually sustain, not with the company itself.
                  d.capped && CAPPED_TEXT[d.expectations] ? CAPPED_TEXT[d.expectations]! : EXPECTATION_TEXT[d.expectations]!,
                )}
              </p>
              {d.judgedAgainst != null && (
                <p className="text-muted">
                  {d.capped
                    ? t("Judged against {x} a year: its {what} was {actual}, counted as at most {x} because few companies keep growing faster for 10 years.", {
                        x: pct(d.judgedAgainst, 0),
                        what: tx(d.referenceName ?? ""),
                        actual: pct(d.reference, 1),
                      })
                    : t("Judged against its {what} of {x}.", { what: tx(d.referenceName ?? ""), x: pct(d.judgedAgainst, 1) })}{" "}
                  {d.vAdjustment > 0
                    ? t("This raised the valuation score V by {n}.", { n: d.vAdjustment })
                    : d.vAdjustment < 0
                      ? t("This lowered the valuation score V by {n}.", { n: -d.vAdjustment })
                      : t("The valuation score V is unchanged.")}
                </p>
              )}
            </div>
          )}
          <p className="text-xs text-muted">
            {t(
              "Reverse DCF: starting from {fcf} of free cash flow ({period}), discounted at {r} a year, with {gt} growth after year {n}. It finds the growth that makes those cash flows worth today's price; a higher discount rate or a lower starting point would raise it.",
              { fcf: usd(d.baseFcf), period: tx(d.basePeriod ?? ""), r: pct(d.discountRate, 0), gt: pct(d.terminalGrowth, 1), n: d.years },
            )}
            {d.note && <> {tx(d.note)}</>}
          </p>
        </>
      )}
    </div>
  );
}
