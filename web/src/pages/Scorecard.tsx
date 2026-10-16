// Prototype design: the whole report on one card, styled like a nutrition
// label. Verdict and four scores up top; each score opens to its reasons.

import type { ReactNode } from "react";
import { isFinished, type MetricsReport, type ReportBody } from "../api";
import { DesignSwitch } from "../components/DesignSwitch";
import { metricValue, multiple, pct, price, shortDate, signedPct, titleCase, toneFor, usd, wholePrice } from "../format";
import { QUALITY_QUESTIONS, SCORE_WORDS, STAGES, metricOf, scoreWord, useLiveReport } from "../live";
import { navigate } from "../router";

const TONE_DOT = { good: "bg-good", mid: "bg-mid", bad: "bg-bad" };

function Rule({ size }: { size: "thin" | "mid" | "thick" }) {
  const h = size === "thick" ? "h-3" : size === "mid" ? "h-1.5" : "h-px";
  return <div className={`${h} my-1 bg-black`} aria-hidden />;
}

/** One score line; opens to the reasons behind it. */
function ScoreLine({ label, value, words, children }: { label: string; value: number | null; words: string[]; children?: ReactNode }) {
  return (
    <details className="group border-t border-black first:border-t-0">
      <summary className="flex cursor-pointer list-none items-center gap-2 py-1.5">
        <span className="text-[15px] font-extrabold">{label}</span>
        <span className="text-xs text-black/50 group-open:rotate-90">▸</span>
        <span className="ml-auto flex items-center gap-2">
          <span className="text-sm">{scoreWord(words, value)}</span>
          <span className={`size-3 rounded-full ${value == null ? "bg-black/20" : TONE_DOT[toneFor(value)]}`} aria-hidden />
          <span className="w-8 text-right text-[15px] font-extrabold tabular-nums">{value == null ? "–" : value.toFixed(1)}</span>
        </span>
      </summary>
      <div className="pb-2 pl-3 text-[13px] leading-snug">{children}</div>
    </details>
  );
}

function Fact({ label, value, indent = false, bold = false }: { label: string; value: string; indent?: boolean; bold?: boolean }) {
  return (
    <div className={`flex justify-between gap-3 border-t border-black py-1 text-[15px] ${indent ? "pl-4" : ""}`}>
      <span className={bold ? "font-extrabold" : ""}>{label}</span>
      <span className="font-extrabold tabular-nums">{value}</span>
    </div>
  );
}

function Reasons({ items }: { items: string[] }) {
  return (
    <ul className="list-disc space-y-0.5 pl-4">
      {items.map((r) => (
        <li key={r}>{r}</li>
      ))}
    </ul>
  );
}

function Card({ m, body }: { m?: MetricsReport; body?: ReportBody }) {
  const v = body?.verdict;
  const q = body?.agents?.business_quality?.output;
  const b = m?.valuationBands;
  return (
    <>
      <div className="flex items-end justify-between gap-3">
        <div>
          <p className="text-[15px] font-extrabold">Verdict</p>
          <p className="text-sm leading-snug">{v ? v.tagline || v.meaning : "Comes last, once every score is in."}</p>
        </div>
        <p className="text-5xl leading-none font-black tracking-tight uppercase">{v?.label || "…"}</p>
      </div>
      <Rule size="mid" />
      <p className="text-right text-xs font-extrabold">Score out of 5</p>
      <ScoreLine label="Business quality" value={body?.businessQuality?.score ?? null} words={SCORE_WORDS.quality}>
        {q ? (
          <ul className="space-y-0.5">
            {QUALITY_QUESTIONS.map((x) => (
              <li key={x.key} className="flex justify-between gap-3">
                <span>{x.short}</span>
                <span className="font-bold">{q[x.key]?.answer ?? "–"}</span>
              </li>
            ))}
          </ul>
        ) : (
          "Not judged for this report."
        )}
      </ScoreLine>
      <ScoreLine label="Growth" value={m?.growthScore.value ?? null} words={SCORE_WORDS.growth}>
        <Reasons items={m?.growthScore.reasons ?? []} />
      </ScoreLine>
      <ScoreLine label="Financial health" value={m?.healthScore.value ?? null} words={SCORE_WORDS.health}>
        <Reasons items={m?.healthScore.reasons ?? []} />
      </ScoreLine>
      <ScoreLine label="Valuation" value={m?.valuationScore.value ?? null} words={SCORE_WORDS.valuation}>
        <Reasons items={m?.valuationScore.reasons ?? []} />
      </ScoreLine>
      <Rule size="thick" />

      {m && (
        <>
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-[15px] font-extrabold">Phase {m.phase.number} of 5</span>
            <span className="text-[15px]">{m.phase.name}</span>
          </div>
          <Rule size="mid" />

          {b && (
            <>
              <p className="flex justify-between text-[15px] font-extrabold">
                <span>Price</span>
                <span className="uppercase">{b.zone}</span>
              </p>
              <Fact label="Now" value={price(b.price)} bold />
              <Fact label={`Cheap, below ${multiple(b.cheapBelow)} ${b.metric}`} value={wholePrice(b.cheapPrice)} indent />
              <Fact label={`Typical, ${multiple(b.median)}`} value={wholePrice(b.medianPrice)} indent />
              <Fact label={`Expensive, above ${multiple(b.expensiveAbove)}`} value={wholePrice(b.expensivePrice)} indent />
              <Rule size="mid" />
            </>
          )}

          <Fact label="Revenue growth, 5y a year" value={pct(metricOf(m, "revenueCagr5y")?.value)} bold />
          <Fact label="3 years" value={pct(metricOf(m, "revenueCagr3y")?.value)} indent />
          <Fact label="EPS growth, 5y a year" value={pct(metricOf(m, "epsCagr5y")?.value)} bold />
          <Fact label="Margins" value="" bold />
          {m.profitability.filter((x) => x.value != null).map((x) => (
            <Fact key={x.key} label={x.label.replace(" margin", "")} value={metricValue(x)} indent />
          ))}
          {m.health
            .filter((x) => ["debtToEquity", "interestCoverage"].includes(x.key) && x.value != null)
            .map((x) => (
              <Fact key={x.key} label={x.label} value={metricValue(x)} bold />
            ))}
          <Fact label="Price multiples" value="" bold />
          {m.valuation.filter((x) => x.value != null).map((x) => (
            <Fact key={x.key} label={x.label === b?.metric ? `${x.label} (used for the verdict)` : x.label} value={metricValue(x)} indent />
          ))}
          <Fact label="Market cap" value={usd(m.marketCap.value)} bold />
          <Fact label="Price, 5 years" value={signedPct(m.priceChange5y.value, 0)} bold />
          <Rule size="thick" />
          <p className="text-[11px] leading-snug">
            * Business score {v?.business?.toFixed(1) ?? "–"} weighs {v?.components.map((c) => `${c.name.toLowerCase()} ${Math.round(c.weight * 100)}%`).join(", ")}. {v?.rule.replace(/\.?$/, ".")} Financials through {shortDate(m.financialsThrough)}, prices as of {shortDate(m.priceDate)}. Educational only, not financial advice.
          </p>
        </>
      )}
    </>
  );
}

export function Scorecard({ id }: { id: string }) {
  const { data: report, error, isLoading } = useLiveReport(id);
  const body = report?.report;
  const m = body?.metrics;
  const name = titleCase(body?.company.name || report?.companyName || "");
  const stage = report ? Math.max(STAGES.findIndex((s) => s.id === report.stage), 0) : 0;

  return (
    <div className="min-h-screen bg-[#e8e5dd] px-4 py-5 font-label text-black">
      <div className="mx-auto flex max-w-md flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <button onClick={() => navigate("/")} className="text-sm font-extrabold underline underline-offset-2">
            ← New search
          </button>
          <DesignSwitch look="scorecard" />
        </div>

        <article className="border-[3px] border-black bg-white p-3 sm:p-4">
          <h1 className="text-[2.75rem] leading-none font-black tracking-tight sm:text-5xl">Stock Facts</h1>
          <p className="mt-1 flex justify-between gap-3 border-b border-black pb-1 text-[15px]">
            <span className="truncate">{report && `${name} (${report.ticker})`}</span>
            {m?.price != null && <span className="font-extrabold">{price(m.price)}</span>}
          </p>
          <Rule size="thick" />
          {isLoading ? (
            <p className="py-2">Loading…</p>
          ) : error || !report ? (
            <p className="py-2">Report not found.</p>
          ) : report.status === "failed" ? (
            <p className="py-2">This report could not be built: {report.error}</p>
          ) : (
            <>
              {!isFinished(report.status) && (
                <p className="mb-1 animate-pulse text-sm font-bold">
                  Step {stage + 1} of {STAGES.length}: {STAGES[stage]?.label}…
                </p>
              )}
              <Card m={m} body={body} />
            </>
          )}
        </article>
      </div>
    </div>
  );
}
