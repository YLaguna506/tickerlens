// Prototype design: the report as a printed research note. The verdict is the
// headline, the analysis reads as prose, and charts are quiet figures.

import type { ReactNode } from "react";
import { Area, AreaChart, Line, LineChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { isFinished, type MetricsReport, type Quality, type ReportBody } from "../api";
import { DesignSwitch } from "../components/DesignSwitch";
import { metricValue, monthYear, multiple, pct, price, shortDate, signedPct, titleCase, usd, wholePrice } from "../format";
import { STAGES, incomeRows, isPending, metricOf, useLiveReport } from "../live";
import { navigate } from "../router";

const INK = "#1b1a17";
const ACCENT = "#b3261e";
const DIM = "#6e6a62";
const AXIS = { fontSize: 11, fill: DIM, fontFamily: "Source Serif 4, Georgia, serif" };
const TIP = {
  contentStyle: { background: "#fbf8f1", border: "1px solid #d9d3c5", borderRadius: 0, fontFamily: "Source Serif 4, Georgia, serif", fontSize: 13 },
  labelStyle: { color: DIM },
  itemStyle: { color: INK },
};

function Chapter({ n, title, children }: { n: string; title: string; children: ReactNode }) {
  return (
    <section className="border-t border-e-ink pt-6">
      <p className="font-serif text-sm tracking-[0.2em] text-e-accent uppercase">{n}</p>
      <h2 className="mt-1 font-serif text-3xl font-semibold tracking-tight sm:text-4xl">{title}</h2>
      <div className="mt-5 space-y-5 text-[1.125rem] leading-[1.7]">{children}</div>
    </section>
  );
}

function Figure({ caption, children }: { caption: string; children: ReactNode }) {
  return (
    <figure className="my-2">
      {children}
      <figcaption className="mt-2 border-t border-e-rule pt-2 text-sm text-e-dim italic">{caption}</figcaption>
    </figure>
  );
}

function Pending({ children }: { children: ReactNode }) {
  return <p className="text-e-dim italic">{children}</p>;
}

/** Label ....... value, like a printed table of figures. */
function Leader({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline gap-2 text-base">
      <span>{label}</span>
      <span className="flex-1 translate-y-[-0.25em] border-b border-dotted border-e-dim/60" />
      <span className="font-semibold tabular-nums">{value}</span>
    </div>
  );
}

// --- Chapters ---------------------------------------------------------------------------

const QUALITY: { key: keyof Quality; question: string }[] = [
  { key: "revenuePredictability", question: "Is revenue predictable?" },
  { key: "pricingPower", question: "Can it raise prices?" },
  { key: "recessionResistance", question: "Does it hold up in a recession?" },
  { key: "competitivePosition", question: "How strong is its position?" },
];

function BusinessChapter({ body, name }: { body?: ReportBody; name: string }) {
  const o = body?.agents?.business_overview;
  const segs = body?.segments;
  const quote = o?.evidence?.find((e) => e.quote.split(" ").length >= 8)?.quote;
  return (
    <Chapter n="I" title="What the company does">
      {isPending(body, "business_overview") ? (
        <Pending>Our analysts are still reading the annual report…</Pending>
      ) : o?.output ? (
        <>
          <p className="first-letter:float-left first-letter:mt-1 first-letter:mr-2 first-letter:font-serif first-letter:text-[4.2rem] first-letter:leading-[0.8] first-letter:font-semibold">
            {o.output.whatItDoes} {o.output.howItMakesMoney}
          </p>
          <p>
            <span className="font-serif font-semibold">Who pays.</span> {o.output.payingCustomers}
          </p>
          <p>
            <span className="font-serif font-semibold">Why they choose {name}.</span> {o.output.whyTheyBuy}
          </p>
          {quote && (
            <blockquote className="my-8 border-y border-e-ink py-6 text-center">
              <p className="font-serif text-2xl leading-snug italic sm:text-3xl">“{quote.trim()}”</p>
              <footer className="mt-3 text-sm tracking-[0.15em] text-e-dim uppercase">From the annual report</footer>
            </blockquote>
          )}
        </>
      ) : (
        <Pending>The business overview is not available for this note.</Pending>
      )}

      {segs?.segments?.length ? (
        <Figure caption={`Revenue by segment, ${segs.fiscalYear}, with change from the prior year.`}>
          <div className="space-y-3">
            {[...segs.segments].sort((a, b) => b.revenue - a.revenue).map((s) => (
              <div key={s.name}>
                <div className="flex items-baseline justify-between gap-3 text-base">
                  <span className="font-serif font-semibold">{s.name}</span>
                  <span className="tabular-nums text-e-dim">
                    {usd(s.revenue)} · {pct(s.share, 0)}
                    {s.yoy != null && <span className={s.yoy >= 0 ? "text-e-ink" : "text-e-accent"}> · {signedPct(s.yoy, 0)}</span>}
                  </span>
                </div>
                <div className="mt-1 h-1.5 bg-e-rule">
                  <div className="h-full bg-e-ink" style={{ width: `${s.share * 100}%` }} />
                </div>
              </div>
            ))}
          </div>
        </Figure>
      ) : null}
    </Chapter>
  );
}

function QualityChapter({ body }: { body?: ReportBody }) {
  const q = body?.agents?.business_quality;
  const score = body?.businessQuality;
  return (
    <Chapter n="II" title="Is it a good business?">
      {isPending(body, "business_quality") ? (
        <Pending>Still weighing the evidence…</Pending>
      ) : q?.output ? (
        <>
          {score && (
            <p>
              On our four questions it earns <strong>{score.points} of 8 points</strong>, a quality score of {score.score.toFixed(1)} out of 5.
            </p>
          )}
          <div className="divide-y divide-e-rule border-y border-e-rule">
            {QUALITY.map((item) => {
              const a = q.output![item.key];
              return (
                <div key={item.key} className="grid gap-1 py-4 sm:grid-cols-[13rem_1fr] sm:gap-6">
                  <div>
                    <p className="font-serif text-lg font-semibold leading-snug">{item.question}</p>
                    <p className="font-serif text-sm tracking-[0.15em] text-e-accent uppercase">{a?.answer}</p>
                  </div>
                  <p className="text-base leading-relaxed text-e-dim">{a?.justification}</p>
                </div>
              );
            })}
          </div>
        </>
      ) : (
        <Pending>Business quality could not be judged for this note.</Pending>
      )}
    </Chapter>
  );
}

function NumbersChapter({ m, body }: { m: MetricsReport; body?: ReportBody }) {
  const rows = incomeRows(body);
  const g5 = metricOf(m, "revenueCagr5y")?.value;
  const op = metricOf(m, "operatingMargin")?.value;
  const fcf = metricOf(m, "fcfMargin")?.value;
  const de = metricOf(m, "debtToEquity");
  const figures = [
    ...m.profitability,
    ...m.growth.filter((g) => g.key.endsWith("5y") || g.key.endsWith("10y")),
    ...m.health.filter((h) => ["debtToEquity", "interestCoverage"].includes(h.key)),
    ...m.returns.filter((r) => r.key === "shareholderYield"),
  ];
  return (
    <Chapter n="III" title="The numbers">
      <p>
        {g5 != null && <>Revenue has grown {pct(g5)} a year over the last five years. </>}
        {op != null && <>For every dollar of sales, {Math.round(op * 100)} cents is left as operating profit</>}
        {fcf != null && <> and {Math.round(fcf * 100)} cents arrives as free cash</>}.
        {de?.value != null && <> Debt to equity is {metricValue(de)}.</>} Its growth scores {m.growthScore.value?.toFixed(1) ?? "n/a"} and its financial
        health {m.healthScore.value?.toFixed(1) ?? "n/a"}, both out of 5.
      </p>
      {rows.length > 1 && (
        <Figure caption="Revenue (black) and net income (red), last five fiscal years and the latest twelve months.">
          <div className="h-56">
            <ResponsiveContainer>
              <LineChart data={rows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                <XAxis dataKey="label" tick={AXIS} axisLine={{ stroke: INK }} tickLine={false} />
                <YAxis tickFormatter={(v: number) => usd(v)} tick={AXIS} axisLine={false} tickLine={false} width={56} />
                <Tooltip {...TIP} formatter={(v) => usd(Number(v))} />
                <Line dataKey="revenue" name="Revenue" stroke={INK} strokeWidth={2} dot={{ r: 3, fill: INK }} isAnimationActive={false} />
                <Line dataKey="netIncome" name="Net income" stroke={ACCENT} strokeWidth={2} dot={{ r: 3, fill: ACCENT }} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </Figure>
      )}
      <div className="grid gap-x-10 gap-y-1.5 sm:grid-cols-2">
        {figures.map((f) => (
          <Leader key={f.key} label={f.label} value={metricValue(f)} />
        ))}
      </div>
    </Chapter>
  );
}

function PriceChapter({ m, name }: { m: MetricsReport; name: string }) {
  const b = m.valuationBands;
  if (!b) {
    return (
      <Chapter n="IV" title="The price">
        <Pending>There is not enough history to say what a normal price is: {m.valuationScore.reasons.join("; ")}.</Pending>
      </Chapter>
    );
  }
  const values = m.charts.valuation1y.map((p) => p.value);
  const lo = Math.min(...values, b.cheapBelow) * 0.95;
  const hi = Math.max(...values, b.expensiveAbove) * 1.05;
  return (
    <Chapter n="IV" title="The price">
      <p>
        At {price(b.price)} a share, {name} trades at {multiple(b.current)} its {b.perShareBasis.toLowerCase()}, more than on {pct(b.percentile, 0)} of
        the trading days since {b.historyFrom.slice(0, 4)}. Its typical multiple over that time was {multiple(b.median)}, about{" "}
        <strong>{wholePrice(b.medianPrice)}</strong> a share at today&apos;s figures. Below {wholePrice(b.cheapPrice)} it would look cheap; above{" "}
        {wholePrice(b.expensivePrice)}, expensive.
      </p>
      <Figure caption={`${b.metric} over the last year. Shaded: cheap (bottom) and expensive (top) ranges; dashed: the typical level.`}>
        <div className="h-56">
          <ResponsiveContainer>
            <LineChart data={m.charts.valuation1y} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
              <ReferenceArea y1={lo} y2={b.cheapBelow} fill={INK} fillOpacity={0.06} ifOverflow="hidden" />
              <ReferenceArea y1={b.expensiveAbove} y2={hi} fill={ACCENT} fillOpacity={0.1} ifOverflow="hidden" />
              <ReferenceLine y={b.median} stroke={DIM} strokeDasharray="4 4" />
              <XAxis dataKey="date" tickFormatter={monthYear} tick={AXIS} axisLine={{ stroke: INK }} tickLine={false} minTickGap={50} />
              <YAxis domain={[lo, hi]} tickFormatter={(v: number) => `${Math.round(v)}×`} tick={AXIS} axisLine={false} tickLine={false} width={44} />
              <Tooltip {...TIP} formatter={(v) => multiple(Number(v))} />
              <Line dataKey="value" name={b.metric} stroke={INK} strokeWidth={1.75} dot={false} isAnimationActive={false} />
            </LineChart>
          </ResponsiveContainer>
        </div>
      </Figure>
      <div className="grid grid-cols-3 border-y border-e-ink text-center">
        {[
          { label: "Cheap below", value: b.cheapPrice },
          { label: "Typical", value: b.medianPrice },
          { label: "Expensive above", value: b.expensivePrice },
        ].map((x) => (
          <div key={x.label} className="border-e-rule py-4 not-last:border-r">
            <p className="text-xs tracking-[0.15em] text-e-dim uppercase">{x.label}</p>
            <p className="font-serif text-3xl font-semibold tabular-nums">{wholePrice(x.value)}</p>
          </div>
        ))}
      </div>
    </Chapter>
  );
}

function VerdictChapter({ body }: { body?: ReportBody }) {
  const v = body?.verdict;
  return (
    <Chapter n="V" title="Our verdict">
      {!v ? (
        <Pending>The verdict comes last, once every score is in.</Pending>
      ) : (
        <>
          <p className="font-serif text-6xl font-semibold tracking-tight text-e-accent italic sm:text-7xl">{v.label}.</p>
          <p>{v.meaning.replace(/\.?$/, ".")}</p>
          <div className="grid gap-x-10 gap-y-1.5 sm:grid-cols-2">
            <Leader label="The business" value={`${v.business?.toFixed(1) ?? "–"} / 5`} />
            <Leader label="The valuation" value={`${v.valuation ?? "–"} / 5`} />
            {v.components.map((c) => (
              <Leader key={c.name} label={`${c.name} (${Math.round(c.weight * 100)}%)`} value={c.score?.toFixed(1) ?? "–"} />
            ))}
          </div>
          <p className="text-base text-e-dim italic">
            How we decided: {v.rule.replace(/\.?$/, ".")} Confidence is {v.confidenceLevel} ({v.confidence}/100).
          </p>
        </>
      )}
    </Chapter>
  );
}

// --- Page -----------------------------------------------------------------------------------

export function Editorial({ id }: { id: string }) {
  const { data: report, error, isLoading } = useLiveReport(id);
  const body = report?.report;
  const m = body?.metrics;
  const v = body?.verdict;
  const name = titleCase(body?.company.name || report?.companyName || report?.ticker || "");
  const running = report ? !isFinished(report.status) : false;
  const stage = report ? Math.max(STAGES.findIndex((s) => s.id === report.stage), 0) : 0;
  const headline = v?.tagline || (v?.meaning ? `${name}: ${v.meaning}` : `${name}, examined`);

  return (
    <div className="min-h-screen bg-e-paper font-text text-e-ink">
      <div className="mx-auto max-w-3xl px-4 pb-16 sm:px-6">
        <header className="flex flex-wrap items-center justify-between gap-3 border-b-2 border-e-ink py-4">
          <button onClick={() => navigate("/")} className="font-serif text-2xl font-bold tracking-tight italic" title="New search">
            TickerLens
          </button>
          <DesignSwitch look="editorial" />
        </header>
        <p className="border-b border-e-rule py-2 text-xs tracking-[0.2em] text-e-dim uppercase">
          Research note · {report?.ticker}
          {m && <> · {shortDate(m.priceDate)}</>}
        </p>

        {isLoading ? (
          <Pending>Loading…</Pending>
        ) : error || !report ? (
          <p className="pt-10">This note could not be found.</p>
        ) : report.status === "failed" ? (
          <p className="pt-10">This note could not be written: {report.error}</p>
        ) : (
          <article className="space-y-12 pt-10">
            <div className="space-y-5">
              <p className="font-serif text-sm tracking-[0.2em] text-e-accent uppercase">
                {name}
                {v?.label && <> · {v.label}</>}
              </p>
              <h1 className="font-serif text-5xl leading-[1.02] font-semibold tracking-tight text-balance sm:text-7xl">{headline}</h1>
              {v?.rationale && <p className="text-lg leading-relaxed text-e-dim italic sm:text-xl">{v.rationale}</p>}
              {running && (
                <p className="text-e-dim italic" aria-live="polite">
                  Writing this note: {STAGES[stage]?.label.toLowerCase()}…
                </p>
              )}
            </div>

            {m && (
              <div className="grid grid-cols-2 border-y-2 border-e-ink sm:grid-cols-4">
                {[
                  { label: "Share price", value: price(m.price) },
                  { label: "Five years", value: signedPct(m.priceChange5y.value, 0) },
                  { label: "Market value", value: usd(m.marketCap.value) },
                  { label: m.valuationBands?.metric ?? "P/E", value: multiple(m.valuationBands?.current ?? metricOf(m, "pe")?.value) },
                ].map((x, i) => (
                  <div key={x.label} className={`py-4 sm:px-4 ${i % 2 ? "pl-4" : ""} ${i ? "sm:border-l sm:border-e-rule" : "sm:pl-0"}`}>
                    <p className="text-xs tracking-[0.15em] text-e-dim uppercase">{x.label}</p>
                    <p className="font-serif text-3xl font-semibold tabular-nums sm:text-4xl">{x.value}</p>
                  </div>
                ))}
              </div>
            )}

            {m && m.charts.price.length > 1 && (
              <Figure caption={`Share price over the last five years, ${signedPct(m.priceChange5y.value, 0)} in all.`}>
                <div className="h-48">
                  <ResponsiveContainer>
                    <AreaChart data={m.charts.price.filter((_, i, all) => i % 5 === 0 || i === all.length - 1)} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                      <XAxis dataKey="date" tickFormatter={(d: string) => d.slice(0, 4)} tick={AXIS} axisLine={{ stroke: INK }} tickLine={false} minTickGap={60} />
                      <YAxis domain={["auto", "auto"]} tickFormatter={(v: number) => `$${Math.round(v)}`} tick={AXIS} axisLine={false} tickLine={false} width={48} />
                      <Tooltip {...TIP} formatter={(v) => price(Number(v))} />
                      <Area dataKey="value" name="Price" stroke={INK} strokeWidth={1.5} fill={INK} fillOpacity={0.05} isAnimationActive={false} />
                    </AreaChart>
                  </ResponsiveContainer>
                </div>
              </Figure>
            )}

            <BusinessChapter body={body} name={name} />
            <QualityChapter body={body} />
            {m && <NumbersChapter m={m} body={body} />}
            {m && <PriceChapter m={m} name={name} />}
            <VerdictChapter body={body} />

            <footer className="border-t-2 border-e-ink pt-4 text-sm text-e-dim">
              {m && (
                <p>
                  Financials through {shortDate(m.financialsThrough)}
                  {body?.filing && <>, from the 10-K for the period ended {shortDate(body.filing.reportDate)}</>}. Prices as of {shortDate(m.priceDate)}.
                </p>
              )}
              <p className="mt-2 italic">For education only. This is not financial advice.</p>
            </footer>
          </article>
        )}
      </div>
    </div>
  );
}
