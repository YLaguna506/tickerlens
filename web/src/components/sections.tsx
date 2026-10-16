// The report's sections, in page order.

import type { MetricsReport, ReportBody, Verdict } from "../api";
import { multiple, pct, price, signedPct, titleCase, toneFor, usd, wholePrice } from "../format";
import { t, tx } from "../i18n";
import { BandChart, EarningsDots, GeographySplit, PriceBar, RevenueIncomeChart, SegmentBreakdown } from "./charts";
import { QUALITY_QUESTIONS, fallbackNote, incomeRows, isPending, valuedWith } from "../live";
import { EvidenceList, MetricRow, ScoreBar, Section, Skeleton, StepScale, ThreeLevel, Unavailable } from "./ui";

const pending = isPending;

// --- Business --------------------------------------------------------------------


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
  const name = body?.company.name ? titleCase(body.company.name) : t("the company");
  const overview = body?.agents?.business_overview;
  const quality = body?.agents?.business_quality;
  const segs = body?.segments;
  const url = body?.filing?.url;

  return (
    <Section id="business" title={t("Business")}>
      {pending(body, "business_overview") ? (
        <Skeleton lines={4} label={t("AI analysts are reading the latest 10-K…")} />
      ) : overview?.status === "ready" && overview.output ? (
        <div className="fade-in space-y-3">
          {overview.output.mission && <InfoCard icon="🧭" label={t("Mission")}>{overview.output.mission}</InfoCard>}
          <InfoCard icon="📦" label={t("What does {name} do?", { name })}>{overview.output.whatItDoes}</InfoCard>
          <InfoCard icon="💵" label={t("How does it make money?")}>{overview.output.howItMakesMoney}</InfoCard>
          <InfoCard icon="👥" label={t("Who are the paying customers?")}>{overview.output.payingCustomers}</InfoCard>
          <InfoCard icon="🏅" label={t("Why do they buy from {name}?", { name })}>{overview.output.whyTheyBuy}</InfoCard>
          <EvidenceList evidence={overview.evidence} filingUrl={url} />
        </div>
      ) : (
        <Unavailable>{t("The business overview is not available for this report.")}</Unavailable>
      )}

      {segs?.segments?.length ? (
        <div className="card fade-in">
          <SegmentBreakdown rows={segs.segments} title={t("Revenue by segment · {year}", { year: segs.fiscalYear })} />
        </div>
      ) : null}
      {segs?.geography?.length ? (
        <div className="card fade-in">
          <GeographySplit rows={segs.geography} />
        </div>
      ) : null}

      <h3 className="pt-4 font-display text-3xl font-bold">{t("Business quality")}</h3>
      {pending(body, "business_quality") ? (
        <Skeleton lines={3} label={t("Judging business quality…")} />
      ) : quality?.status === "ready" && quality.output ? (
        <>
          <p className="text-muted">
            {t("Score")} <span className="font-display text-2xl font-bold text-ink">{body?.businessQuality?.score.toFixed(1)}</span>/5 ·{" "}
            {t("{points} of 8 points across four questions", { points: body?.businessQuality?.points ?? 0 })}
          </p>
          <StepScale value={body?.businessQuality?.score ?? null} labels={["Poor", "Below avg", "Decent", "Good", "Excellent"]} />
          <div className="fade-in grid gap-4 md:grid-cols-2">
            {QUALITY_QUESTIONS.map((q) => {
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
        <Unavailable>
          {t("Business quality could not be judged for this report")}
          {quality?.errors?.length ? `: ${tx(quality.errors[0])}` : "."}
        </Unavailable>
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
      <Section id="financials" title={t("Financial overview")}>
        <Skeleton lines={6} label={t("Computing metrics from SEC filings…")} />
      </Section>
    );
  }
  const growth = m.growth.filter((g) => !g.key.endsWith("1y"));
  return (
    <Section id="financials" title={t("Financial overview")}>
      <div className="card fade-in grid gap-8 md:grid-cols-3">
        <div className="space-y-8">
          <Group title={t("Profitability")} metrics={m.profitability} />
          <Group title={t("Financial health")} metrics={m.health} />
        </div>
        <Group title={t("Growth · annualized")} metrics={growth} />
        <div className="space-y-8">
          <Group title={t("Valuation")} metrics={[m.marketCap, ...m.valuation]} />
          <Group title={t("Shareholder returns")} metrics={m.returns} />
        </div>
      </div>
      <p className="text-xs text-muted">{t("Tap any number to see how it was calculated.")}</p>

      <div className="grid gap-4 md:grid-cols-2">
        <ScoreCard title={t("Growth score")} score={m.growthScore} labels={["Low", "Below avg", "Average", "Above avg", "High"]} />
        <ScoreCard title={t("Financial health score")} score={m.healthScore} labels={["Weak", "Below avg", "Fair", "Good", "Strong"]} />
      </div>

      {body?.earnings?.length ? (
        <div className="card fade-in space-y-5">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h3 className="font-display text-xl font-bold">{t("Earnings per share vs. estimate")}</h3>
            <span className="font-semibold text-good">
              {t("Beat {beats}/{quarters}", { beats: m.earnings.beats, quarters: m.earnings.quarters })}
              {m.earnings.avgSurprisePercent != null && ` · ${t("avg")} ${signedPct(m.earnings.avgSurprisePercent / 100)}`}
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
        <h3 className="font-display text-lg font-semibold">{title}</h3>
        <span className="font-semibold">{score.value == null ? t("n/a") : `${score.value.toFixed(1)}/5 · ${t(labels[idx]!)}`}</span>
      </div>
      <ScoreBar value={score.value} tone={toneFor(score.value)} />
      <ul className="space-y-0.5 text-sm text-muted">
        {score.reasons.map((r) => (
          <li key={r}>{tx(r)}</li>
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
      <Section id="phase" title={t("Phase")}>
        <Skeleton lines={4} />
      </Section>
    );
  }
  const rows = incomeRows(body);
  return (
    <Section id="phase" title={t("Phase")}>
      <div className="fade-in grid gap-4 md:grid-cols-3">
        <div className="card space-y-1 md:col-span-2">
          {m.phase.checks.map((c) => (
            <div key={c.question} className="flex items-center justify-between gap-4 border-b border-line py-3 last:border-0">
              <div>
                <p className="font-display text-lg font-semibold">{tx(c.question)}</p>
                <p className="text-sm text-muted">{tx(c.detail)}</p>
              </div>
              <span
                className={`shrink-0 rounded-full px-4 py-1.5 font-semibold ${
                  c.answer == null ? "bg-line text-muted" : c.answer ? "bg-good text-white" : "bg-bad text-white"
                }`}
              >
                {c.answer == null ? "?" : c.answer ? `✓ ${t("Yes")}` : `✕ ${t("No")}`}
              </span>
            </div>
          ))}
        </div>
        <div className="flex flex-col justify-center rounded-2xl bg-good p-6 text-white">
          <p className="text-xs font-semibold tracking-[0.12em] uppercase opacity-80">{t("Valued with")}</p>
          <p className="mt-1 font-display text-3xl font-bold">{valuedWith(m)}</p>
          <p className="mt-2 text-sm opacity-90">{fallbackNote(m) || metricExplainer(valuedWith(m))}</p>
        </div>
      </div>

      {rows.length > 1 && (
        <div className="card fade-in">
          <RevenueIncomeChart rows={rows} />
          <p className="flex gap-4 text-sm text-muted">
            <span><span className="mr-1 inline-block h-1 w-4 rounded bg-good align-middle" /> {t("Revenue")}</span>
            <span><span className="mr-1 inline-block h-1 w-4 rounded bg-[#7fb889] align-middle" /> {t("Net income")}</span>
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
                  {i + 1}: {tx(p)}
                </p>
                {on && <p className="mt-1 text-xs font-semibold text-good">◆ {m.ticker}</p>}
              </li>
            );
          })}
        </ol>
        <p className="mt-3 text-sm text-muted">
          {tx(m.phase.reason)}
          {m.phase.lowConfidence && ` (${t("low confidence: the company is a weak fit for any phase")})`}
        </p>
      </div>
    </Section>
  );
}

function metricExplainer(metric: string) {
  switch (metric) {
    case "P/E":
      return t("Profits are steady and returned to shareholders, so price-to-earnings is the clearest signal.");
    case "P/FCF":
      return t("Profits are still scaling up, so free cash flow is the better measure of earning power.");
    default:
      return t("Profits are not yet meaningful, so the price is compared with sales.");
  }
}

// --- Valuation ------------------------------------------------------------------------------

export function ValuationSection({ m }: { m?: MetricsReport }) {
  if (!m) {
    return (
      <Section id="valuation" title={t("Valuation")}>
        <Skeleton lines={5} />
      </Section>
    );
  }
  const b = m.valuationBands;
  const company = m.name ? titleCase(m.name) : m.ticker;
  return (
    <Section id="valuation" title={t("Valuation")}>
      <StepScale value={m.valuationScore.value} labels={["Very expensive", "Expensive", "Fair value", "Attractive", "Very attractive"]} />
      {b ? (
        <>
          <div className="card flex items-center gap-3">
            <span className="text-xl" aria-hidden>📈</span>
            <p className="font-display text-lg font-semibold">
              {b.fellBackFrom
                ? t("{company} is in phase {n}, where {from} is the usual measure. But {from} {reason}, so {metric} is used instead.", {
                    company,
                    n: m.phase.number,
                    from: b.fellBackFrom,
                    reason: b.fallbackReason ? tx(b.fallbackReason) : t("was not usable"),
                    metric: b.metric,
                  })
                : t("{company} is in phase {n}, so {metric} is the useful measure.", { company, n: m.phase.number, metric: b.metric })}
            </p>
          </div>
          <div className="card fade-in space-y-3">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <h3 className="font-display text-xl font-bold">{t("{metric} over the last year", { metric: b.metric })}</h3>
              <span className="text-sm text-muted">{t("bands from {from}–{to}", { from: b.historyFrom.slice(0, 4), to: b.historyTo.slice(0, 4) })}</span>
            </div>
            <BandChart points={m.charts.valuation1y} bands={b} prices={m.charts.price} />
            <ul className="flex flex-wrap gap-x-5 gap-y-1 text-sm">
              <li><span className="mr-1.5 inline-block size-3 rounded-full bg-good-soft ring-1 ring-good" /> {t("Cheap below {x}", { x: multiple(b.cheapBelow) })} <span className="text-muted">({t("{price} today", { price: wholePrice(b.cheapPrice) })})</span></li>
              <li><span className="mr-1.5 inline-block size-3 rounded-full bg-mid-soft ring-1 ring-mid" /> {t("Fair · typical {x}", { x: multiple(b.median) })} <span className="text-muted">({wholePrice(b.medianPrice)})</span></li>
              <li><span className="mr-1.5 inline-block size-3 rounded-full bg-bad-soft ring-1 ring-bad" /> {t("Expensive above {x}", { x: multiple(b.expensiveAbove) })} <span className="text-muted">({wholePrice(b.expensivePrice)})</span></li>
            </ul>
            <p className="text-sm text-muted">
              {t("Today {x} at {price}: higher than {pct} of the last {days} trading days.", {
                x: multiple(b.current),
                price: price(b.price),
                pct: pct(b.percentile, 0),
                days: b.days.toLocaleString(),
              })}
            </p>
          </div>
          <div className="card fade-in">
            <h3 className="font-display text-xl font-bold">{t("What price is cheap?")}</h3>
            <p className="text-sm text-muted">
              {t("Band edges × {basis} of {value} ({period}). Typical price {typical}.", {
                basis: tx(b.perShareBasis),
                value: price(b.perShare),
                period: tx(b.basisPeriod),
                typical: price(b.medianPrice),
              })}
            </p>
            <PriceBar bands={b} />
          </div>
        </>
      ) : (
        <Unavailable>
          {t("Not enough history to compare today's valuation with the past:")} {m.valuationScore.reasons.map(tx).join("; ")}
        </Unavailable>
      )}
      <div className="grid grid-cols-2 gap-3">
        {m.valuation.map((v) => (
          <div key={v.key} className="card flex items-baseline justify-between">
            <span className="font-semibold text-muted">{tx(v.label)}</span>
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
      <Section id="verdict" title={t("Verdict")}>
        <Skeleton lines={5} label={t("The verdict comes last, once every score is in.")} />
      </Section>
    );
  }
  return (
    <Section id="verdict" title={t("Verdict")}>
      <div className="card fade-in space-y-4">
        <div className="grid grid-cols-5 gap-2">
          {LABELS.map((l) => (
            <span key={l} className={`rounded-xl py-2.5 text-center font-display text-sm font-semibold sm:text-lg ${l === v.label ? LABEL_STYLE[l] + " shadow-md" : LABEL_IDLE[l]}`}>
              {t(l)}
            </span>
          ))}
        </div>
        <p className="border-l-4 border-good pl-4 font-display text-2xl font-bold sm:text-3xl">{v.tagline || tx(v.meaning)}</p>
        {v.rationale && <p className="leading-relaxed text-muted">{v.rationale}</p>}
        <p className="rounded-lg bg-paper p-3 text-sm"><span className="font-semibold">{t("Rule:")} </span>{tx(v.rule)}</p>
      </div>

      <div className="card flex items-center gap-4 bg-good-soft/60">
        <span className="text-2xl" aria-hidden>📈</span>
        <div>
          <p className="eyebrow">{t("Phase")}</p>
          <p className="font-display text-2xl font-bold text-good-strong">
            {t("{n} of 5", { n: m.phase.number })} <span className="text-xl text-ink">{tx(m.phase.name)}</span>
          </p>
        </div>
      </div>

      <div className="card space-y-3">
        <div className="flex items-baseline justify-between">
          <p className="eyebrow">{t("The business")}</p>
          <p>
            {t("composite")} <span className="font-display text-3xl font-bold text-good">{v.business?.toFixed(1) ?? "–"}</span>/5
          </p>
        </div>
        {v.components.map((c) => (
          <div key={c.name} className="flex items-center justify-between gap-3 py-1">
            <span className="text-lg">{tx(c.name)}</span>
            <span className="flex items-center gap-3">
              <span className="text-sm text-muted">{c.weight ? `${Math.round(c.weight * 100)}%` : t("not scored")}</span>
              <ScoreBar value={c.score} tone={toneFor(c.score)} />
              <span className="w-8 text-right font-bold">{c.score?.toFixed(1) ?? "–"}</span>
            </span>
          </div>
        ))}
      </div>

      <div className="card space-y-2">
        <div className="flex items-center justify-between">
          <div>
            <p className="eyebrow">{t("The valuation")}</p>
            {m.valuationBands && (
              <p className="text-lg">
                {t("{metric} {x} vs {typical} typical", { metric: m.valuationBands.metric, x: multiple(m.valuationBands.current), typical: multiple(m.valuationBands.median) })}
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
          <span className="font-semibold">{t("Confidence: {level}", { level: t(v.confidenceLevel) })}</span> ({v.confidence}/100)
        </p>
        <ul className="text-muted">
          {v.confidenceReasons.map((r) => (
            <li key={r}>{tx(r)}</li>
          ))}
        </ul>
        <p className="pt-1 text-xs text-muted">
          {t("Market cap {cap} · 5-year price change {change} · scoring {version}", { cap: usd(m.marketCap.value), change: signedPct(m.priceChange5y.value, 0), version: v.scoringVersion })}
        </p>
      </div>
    </Section>
  );
}
