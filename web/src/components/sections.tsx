// The report's sections, in page order.

import type { MetricsReport, Quality, ReportBody, Verdict } from "../api";
import { multiple, pct, price, signedPct, titleCase, toneFor, usd, wholePrice } from "../format";
import { BandChart, EarningsDots, GeographySplit, PriceBar, RevenueIncomeChart, SegmentBreakdown } from "./charts";
import { incomeRows, isPending } from "../live";
import { EvidenceList, MetricRow, ScoreBar, Section, Skeleton, StepScale, ThreeLevel, Unavailable } from "./ui";

const pending = isPending;

// --- Business --------------------------------------------------------------------

const QUESTIONS: { key: keyof Quality; question: string; options: [string, string, string] }[] = [
  { key: "revenuePredictability", question: "How predictable is revenue?", options: ["Unpredictable", "Modest", "Predictable"] },
  { key: "pricingPower", question: "Can the company raise prices?", options: ["No", "Sometimes", "Easily"] },
  { key: "recessionResistance", question: "How recession-proof is it?", options: ["Weak", "Okay", "Strong"] },
  { key: "competitivePosition", question: "What is its competitive position?", options: ["Weak", "Average", "Dominant"] },
];

function InfoCard({ icon, label, children }: { icon: string; label: string; children: React.ReactNode }) {
  return (
    <div className="card flex gap-4">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-good-soft text-lg" aria-hidden>
        {icon}
      </span>
      <div>
        <p className="eyebrow">{label}</p>
        <p className="mt-1.5 text-[17px] leading-relaxed">{children}</p>
      </div>
    </div>
  );
}

export function BusinessSection({ body }: { body?: ReportBody }) {
  const name = body?.company.name ? titleCase(body.company.name) : "the company";
  const overview = body?.agents?.business_overview;
  const quality = body?.agents?.business_quality;
  const segs = body?.segments;
  const url = body?.filing?.url;

  return (
    <Section id="business" title="Business">
      {pending(body, "business_overview") ? (
        <Skeleton lines={4} label="AI analysts are reading the latest 10-K…" />
      ) : overview?.status === "ready" && overview.output ? (
        <div className="fade-in space-y-3">
          {overview.output.mission && <InfoCard icon="🧭" label="Mission">{overview.output.mission}</InfoCard>}
          <InfoCard icon="📦" label={`What does ${name} do?`}>{overview.output.whatItDoes}</InfoCard>
          <InfoCard icon="💵" label="How does it make money?">{overview.output.howItMakesMoney}</InfoCard>
          <InfoCard icon="👥" label="Who are the paying customers?">{overview.output.payingCustomers}</InfoCard>
          <InfoCard icon="🏅" label={`Why do they buy from ${name}?`}>{overview.output.whyTheyBuy}</InfoCard>
          <EvidenceList evidence={overview.evidence} filingUrl={url} />
        </div>
      ) : (
        <Unavailable>The business overview is not available for this report.</Unavailable>
      )}

      {segs?.segments?.length ? (
        <div className="card fade-in">
          <SegmentBreakdown rows={segs.segments} title={`Revenue by segment · ${segs.fiscalYear}`} />
        </div>
      ) : null}
      {segs?.geography?.length ? (
        <div className="card fade-in">
          <GeographySplit rows={segs.geography} />
        </div>
      ) : null}

      <h3 className="pt-4 font-display text-3xl font-bold">Business quality</h3>
      {pending(body, "business_quality") ? (
        <Skeleton lines={3} label="Judging business quality…" />
      ) : quality?.status === "ready" && quality.output ? (
        <>
          <p className="text-muted">
            Score <span className="font-display text-2xl font-bold text-ink">{body?.businessQuality?.score.toFixed(1)}</span>/5 ·{" "}
            {body?.businessQuality?.points} of 8 points across four questions
          </p>
          <StepScale value={body?.businessQuality?.score ?? null} labels={["Poor", "Below avg", "Decent", "Good", "Excellent"]} />
          <div className="fade-in grid gap-4 md:grid-cols-2">
            {QUESTIONS.map((q) => {
              const a = quality.output![q.key];
              return (
                <ThreeLevel key={q.key} question={q.question} options={q.options} answer={a?.answer}>
                  <p className="leading-relaxed text-muted">{a?.justification}</p>
                  <EvidenceList evidence={a?.evidence} filingUrl={url} />
                </ThreeLevel>
              );
            })}
          </div>
        </>
      ) : (
        <Unavailable>Business quality could not be judged for this report{quality?.errors?.length ? `: ${quality.errors[0]}` : "."}</Unavailable>
      )}
    </Section>
  );
}

// --- Financials -----------------------------------------------------------------------

function Group({ title, metrics }: { title: string; metrics: MetricsReport["growth"] }) {
  return (
    <div>
      <h3 className="mb-1 font-display text-xl font-bold">{title}</h3>
      {metrics.map((m) => (
        <MetricRow key={m.key} m={m} />
      ))}
    </div>
  );
}

export function FinancialsSection({ m, body }: { m?: MetricsReport; body?: ReportBody }) {
  if (!m) {
    return (
      <Section id="financials" title="Financial overview">
        <Skeleton lines={6} label="Computing metrics from SEC filings…" />
      </Section>
    );
  }
  const growth = m.growth.filter((g) => !g.key.endsWith("1y"));
  return (
    <Section id="financials" title="Financial overview">
      <div className="card fade-in grid gap-8 md:grid-cols-3">
        <div className="space-y-8">
          <Group title="Profitability" metrics={m.profitability} />
          <Group title="Financial health" metrics={m.health} />
        </div>
        <Group title="Growth · annualized" metrics={growth} />
        <div className="space-y-8">
          <Group title="Valuation" metrics={[m.marketCap, ...m.valuation]} />
          <Group title="Shareholder returns" metrics={m.returns} />
        </div>
      </div>
      <p className="text-xs text-muted">Tap any number to see how it was calculated.</p>

      <div className="grid gap-4 md:grid-cols-2">
        <ScoreCard title="Growth" score={m.growthScore} labels={["Low", "Below avg", "Average", "Above avg", "High"]} />
        <ScoreCard title="Financial health" score={m.healthScore} labels={["Weak", "Below avg", "Fair", "Good", "Strong"]} />
      </div>

      {body?.earnings?.length ? (
        <div className="card fade-in space-y-5">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h3 className="font-display text-xl font-bold">Earnings per share vs. estimate</h3>
            <span className="font-semibold text-good">
              Beat {m.earnings.beats}/{m.earnings.quarters}
              {m.earnings.avgSurprisePercent != null && ` · avg ${signedPct(m.earnings.avgSurprisePercent / 100)}`}
            </span>
          </div>
          <EarningsDots quarters={body.earnings} />
        </div>
      ) : null}
    </Section>
  );
}

function ScoreCard({ title, score, labels }: { title: string; score: MetricsReport["growthScore"]; labels: string[] }) {
  const idx = score.value == null ? -1 : Math.round(score.value) - 1;
  return (
    <div className="card space-y-3">
      <div className="flex items-baseline justify-between">
        <h3 className="font-display text-lg font-semibold">{title} score</h3>
        <span className="font-semibold">{score.value == null ? "n/a" : `${score.value.toFixed(1)}/5 · ${labels[idx]}`}</span>
      </div>
      <ScoreBar value={score.value} tone={toneFor(score.value)} />
      <ul className="space-y-0.5 text-sm text-muted">
        {score.reasons.map((r) => (
          <li key={r}>{r}</li>
        ))}
      </ul>
    </div>
  );
}

// --- Phase ---------------------------------------------------------------------------------

const PHASES = ["Startup", "Hypergrowth", "Operating leverage", "Capital return", "Decline"];

export function PhaseSection({ m, body }: { m?: MetricsReport; body?: ReportBody }) {
  if (!m) {
    return (
      <Section id="phase" title="Phase">
        <Skeleton lines={4} />
      </Section>
    );
  }
  const rows = incomeRows(body);
  return (
    <Section id="phase" title="Phase">
      <div className="fade-in grid gap-4 md:grid-cols-3">
        <div className="card space-y-1 md:col-span-2">
          {m.phase.checks.map((c) => (
            <div key={c.question} className="flex items-center justify-between gap-4 border-b border-line py-3 last:border-0">
              <div>
                <p className="font-display text-lg font-semibold">{c.question}</p>
                <p className="text-sm text-muted">{c.detail}</p>
              </div>
              <span
                className={`shrink-0 rounded-full px-4 py-1.5 font-semibold ${
                  c.answer == null ? "bg-[#ece9e1] text-muted" : c.answer ? "bg-good text-white" : "bg-bad text-white"
                }`}
              >
                {c.answer == null ? "?" : c.answer ? "✓ Yes" : "✕ No"}
              </span>
            </div>
          ))}
        </div>
        <div className="flex flex-col justify-center rounded-2xl bg-good p-6 text-white">
          <p className="text-xs font-semibold tracking-[0.12em] uppercase opacity-80">Valued with</p>
          <p className="mt-1 font-display text-3xl font-bold">{m.phase.valuationMetric}</p>
          <p className="mt-2 text-sm opacity-90">{metricExplainer(m.phase.valuationMetric)}</p>
        </div>
      </div>

      {rows.length > 1 && (
        <div className="card fade-in">
          <RevenueIncomeChart rows={rows} />
          <p className="flex gap-4 text-sm text-muted">
            <span><span className="mr-1 inline-block h-1 w-4 rounded bg-good align-middle" /> Revenue</span>
            <span><span className="mr-1 inline-block h-1 w-4 rounded bg-[#7fb889] align-middle" /> Net income</span>
          </p>
        </div>
      )}

      <div className="card fade-in overflow-x-auto">
        <ol className="grid min-w-[520px] grid-cols-5 gap-2">
          {PHASES.map((p, i) => {
            const on = i + 1 === m.phase.number;
            return (
              <li key={p} className={`rounded-xl border-2 px-1.5 py-3 text-center sm:p-3 ${on ? "border-good bg-good-soft" : "border-line"}`}>
                <p className={`font-display text-xs font-bold break-words sm:text-sm ${on ? "text-good-strong" : "text-muted"}`}>
                  {i + 1}: {p}
                </p>
                {on && <p className="mt-1 text-xs font-semibold text-good">◆ {m.ticker}</p>}
              </li>
            );
          })}
        </ol>
        <p className="mt-3 text-sm text-muted">
          {m.phase.reason}
          {m.phase.lowConfidence && " (low confidence: the company is a weak fit for any phase)"}
        </p>
      </div>
    </Section>
  );
}

function metricExplainer(metric: string) {
  switch (metric) {
    case "P/E":
      return "Profits are steady and returned to shareholders, so price-to-earnings is the clearest signal.";
    case "P/FCF":
      return "Profits are still scaling up, so free cash flow is the better measure of earning power.";
    default:
      return "Profits are not yet meaningful, so the price is compared with sales.";
  }
}

// --- Valuation ------------------------------------------------------------------------------

export function ValuationSection({ m }: { m?: MetricsReport }) {
  if (!m) {
    return (
      <Section id="valuation" title="Valuation">
        <Skeleton lines={5} />
      </Section>
    );
  }
  const b = m.valuationBands;
  return (
    <Section id="valuation" title="Valuation">
      <StepScale value={m.valuationScore.value} labels={["Very expensive", "Expensive", "Fair value", "Attractive", "Very attractive"]} />
      {b ? (
        <>
          <div className="card flex items-center gap-3">
            <span className="text-xl" aria-hidden>📈</span>
            <p className="font-display text-lg font-semibold">
              {m.name ? titleCase(m.name) : m.ticker} is in phase {m.phase.number}, so {b.metric} is the useful measure
              {b.fellBackFrom && ` (${b.fellBackFrom} lacked history)`}.
            </p>
          </div>
          <div className="card fade-in space-y-3">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <h3 className="font-display text-xl font-bold">{b.metric} over the last year</h3>
              <span className="text-sm text-muted">bands from {b.historyFrom.slice(0, 4)}–{b.historyTo.slice(0, 4)}</span>
            </div>
            <BandChart points={m.charts.valuation1y} bands={b} prices={m.charts.price} />
            <ul className="flex flex-wrap gap-x-5 gap-y-1 text-sm">
              <li><span className="mr-1.5 inline-block size-3 rounded-full bg-good-soft ring-1 ring-good" /> Cheap below {multiple(b.cheapBelow)} <span className="text-muted">({wholePrice(b.cheapPrice)} today)</span></li>
              <li><span className="mr-1.5 inline-block size-3 rounded-full bg-mid-soft ring-1 ring-mid" /> Fair · typical {multiple(b.median)} <span className="text-muted">({wholePrice(b.medianPrice)})</span></li>
              <li><span className="mr-1.5 inline-block size-3 rounded-full bg-bad-soft ring-1 ring-bad" /> Expensive above {multiple(b.expensiveAbove)} <span className="text-muted">({wholePrice(b.expensivePrice)})</span></li>
            </ul>
            <p className="text-sm text-muted">
              Today {multiple(b.current)} at {price(b.price)}: higher than {pct(b.percentile, 0)} of the last {b.days.toLocaleString()} trading days.
            </p>
          </div>
          <div className="card fade-in">
            <h3 className="font-display text-xl font-bold">What price is cheap?</h3>
            <p className="text-sm text-muted">
              Band edges × {b.perShareBasis} of {price(b.perShare)} ({b.basisPeriod}). Typical price {price(b.medianPrice)}.
            </p>
            <PriceBar bands={b} />
          </div>
        </>
      ) : (
        <Unavailable>Not enough history to compare today&apos;s valuation with the past: {m.valuationScore.reasons.join("; ")}</Unavailable>
      )}
      <div className="grid grid-cols-2 gap-3">
        {m.valuation.map((v) => (
          <div key={v.key} className="card flex items-baseline justify-between">
            <span className="font-semibold text-muted">{v.label}</span>
            <span className="font-display text-2xl font-bold">{multiple(v.value)}</span>
          </div>
        ))}
      </div>
    </Section>
  );
}

// --- Verdict --------------------------------------------------------------------------------

const LABELS: Verdict["label"][] = ["Sell", "Pass", "Hold", "Watch", "Buy"];
const LABEL_STYLE: Record<string, string> = {
  Sell: "bg-bad text-white",
  Pass: "bg-[#d98c84] text-white",
  Hold: "bg-mid text-white",
  Watch: "bg-[#7fb889] text-white",
  Buy: "bg-good text-white",
};
const LABEL_IDLE: Record<string, string> = {
  Sell: "bg-bad-soft text-ink/60",
  Pass: "bg-bad-soft/70 text-ink/60",
  Hold: "bg-mid-soft text-ink/60",
  Watch: "bg-good-soft/70 text-ink/60",
  Buy: "bg-good-soft text-ink/60",
};

export function VerdictSection({ body, m }: { body?: ReportBody; m?: MetricsReport }) {
  const v = body?.verdict;
  if (!v || !m) {
    return (
      <Section id="verdict" title="Verdict">
        <Skeleton lines={5} label="The verdict comes last, once every score is in." />
      </Section>
    );
  }
  return (
    <Section id="verdict" title="Verdict">
      <div className="card fade-in space-y-4">
        <div className="grid grid-cols-5 gap-2">
          {LABELS.map((l) => (
            <span key={l} className={`rounded-xl py-2.5 text-center font-display text-sm font-semibold sm:text-lg ${l === v.label ? LABEL_STYLE[l] + " shadow-md" : LABEL_IDLE[l]}`}>
              {l}
            </span>
          ))}
        </div>
        <p className="border-l-4 border-good pl-4 font-display text-2xl font-bold sm:text-3xl">{v.tagline || v.meaning}</p>
        {v.rationale && <p className="leading-relaxed text-muted">{v.rationale}</p>}
        <p className="rounded-lg bg-paper p-3 text-sm"><span className="font-semibold">Rule: </span>{v.rule}</p>
      </div>

      <div className="card flex items-center gap-4 bg-good-soft/60">
        <span className="text-2xl" aria-hidden>📈</span>
        <div>
          <p className="eyebrow">Phase</p>
          <p className="font-display text-2xl font-bold text-good-strong">
            {m.phase.number} of 5 <span className="text-xl text-ink">{m.phase.name}</span>
          </p>
        </div>
      </div>

      <div className="card space-y-3">
        <div className="flex items-baseline justify-between">
          <p className="eyebrow">The business</p>
          <p>
            composite <span className="font-display text-3xl font-bold text-good">{v.business?.toFixed(1) ?? "–"}</span>/5
          </p>
        </div>
        {v.components.map((c) => (
          <div key={c.name} className="flex items-center justify-between gap-3 py-1">
            <span className="text-lg">{c.name}</span>
            <span className="flex items-center gap-3">
              <span className="text-sm text-muted">{c.weight ? `${Math.round(c.weight * 100)}%` : "not scored"}</span>
              <ScoreBar value={c.score} tone={toneFor(c.score)} />
              <span className="w-8 text-right font-bold">{c.score?.toFixed(1) ?? "–"}</span>
            </span>
          </div>
        ))}
      </div>

      <div className="card space-y-2">
        <div className="flex items-center justify-between">
          <div>
            <p className="eyebrow">The valuation</p>
            {m.valuationBands && (
              <p className="text-lg">
                {m.valuationBands.metric} {multiple(m.valuationBands.current)} vs {multiple(m.valuationBands.median)} typical
              </p>
            )}
          </div>
          <span className="flex items-center gap-3">
            <ScoreBar value={v.valuation} tone={toneFor(v.valuation)} />
            <span className="w-8 text-right font-bold">{v.valuation ?? "–"}</span>
          </span>
        </div>
        {m.valuationBands && <PriceBar bands={m.valuationBands} />}
      </div>

      <div className="card space-y-1 text-sm">
        <p>
          <span className="font-semibold">Confidence: {v.confidenceLevel}</span> ({v.confidence}/100)
        </p>
        <ul className="text-muted">
          {v.confidenceReasons.map((r) => (
            <li key={r}>{r}</li>
          ))}
        </ul>
        <p className="pt-1 text-xs text-muted">
          Market cap {usd(m.marketCap.value)} · 5-year price change {signedPct(m.priceChange5y.value, 0)} · scoring {v.scoringVersion}
        </p>
      </div>
    </Section>
  );
}
