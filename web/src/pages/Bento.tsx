// Prototype design: a light "bento" grid of tiles in different sizes, like a
// modern product page. As complete as the terminal, friendlier to read.

import type { ReactNode } from "react";
import { Area, AreaChart, Bar, BarChart, Cell, Line, LineChart, Pie, PieChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { isFinished, type MetricsReport, type ReportBody, type Verdict } from "../api";
import { DesignSwitch } from "../components/DesignSwitch";
import { ResyncButton } from "../components/ResyncButton";
import { RunDetails } from "../components/RunDetails";
import { metricValue, monthYear, multiple, multipleTick, pct, price, shortDate, signedPct, titleCase, toneFor, usd, wholePrice, yearTicks } from "../format";
import { QUALITY_QUESTIONS, SCORE_WORDS, STAGES, fallbackNote, incomeRows, isPending, metricOf, scoreWord, useLiveReport, valuedWith } from "../live";
import { navigate } from "../router";

const INDIGO = "#6366f1";
const EMERALD = "#10b981";
const AMBER = "#f59e0b";
const ROSE = "#f43f5e";
const YELLOW = "#facc15";
const SLATE = "#94a3b8";
const TONE = { good: EMERALD, mid: AMBER, bad: ROSE };
const AXIS = { fontSize: 11, fill: SLATE };
const TIP = { contentStyle: { borderRadius: 12, border: "none", boxShadow: "0 8px 24px rgba(15,23,42,0.12)", fontSize: 12 } };
const SEGMENT_COLORS = [INDIGO, "#8b5cf6", "#06b6d4", EMERALD, AMBER, "#ec4899", SLATE];

const VERDICT_GRADIENT: Record<string, string> = {
  Buy: "from-emerald-500 to-teal-600",
  Watch: "from-emerald-400 to-cyan-600",
  Hold: "from-amber-400 to-orange-500",
  Pass: "from-rose-400 to-rose-600",
  Sell: "from-rose-500 to-red-700",
};

function Tile({ title, right, className = "", children }: { title?: string; right?: ReactNode; className?: string; children: ReactNode }) {
  return (
    <section className={`min-w-0 rounded-3xl bg-white p-5 shadow-[0_1px_2px_rgba(15,23,42,0.04),0_8px_24px_rgba(15,23,42,0.06)] ${className}`}>
      {title && (
        <header className="mb-3 flex items-baseline justify-between gap-3">
          <h2 className="text-xs font-bold tracking-wider text-slate-400 uppercase">{title}</h2>
          {right && <span className="text-xs font-semibold text-slate-500">{right}</span>}
        </header>
      )}
      {children}
    </section>
  );
}

function Waiting({ text = "Loading…" }: { text?: string }) {
  return (
    <div className="space-y-2" aria-busy>
      <p className="text-sm text-slate-400">{text}</p>
      <div className="h-3 w-3/4 animate-pulse rounded-full bg-slate-100" />
      <div className="h-3 w-1/2 animate-pulse rounded-full bg-slate-100" />
    </div>
  );
}

/** A 0–5 score as a ring. */
function RingTile({ title, value, words }: { title: string; value: number | null; words: string[] }) {
  const r = 40;
  const c = 2 * Math.PI * r;
  const filled = value == null ? 0 : value / 5;
  return (
    <Tile className="flex flex-col items-center justify-center text-center">
      <div className="relative size-28">
        <svg viewBox="0 0 100 100" className="size-28 -rotate-90" aria-hidden>
          <circle cx="50" cy="50" r={r} fill="none" stroke="#eef0f4" strokeWidth="10" />
          <circle cx="50" cy="50" r={r} fill="none" stroke={value == null ? SLATE : TONE[toneFor(value)]} strokeWidth="10" strokeLinecap="round" strokeDasharray={c} strokeDashoffset={c * (1 - filled)} />
        </svg>
        <span className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-3xl font-extrabold tabular-nums">{value == null ? "–" : value.toFixed(1)}</span>
          <span className="text-[11px] font-semibold text-slate-400">of 5</span>
        </span>
      </div>
      <p className="mt-2 font-bold">{title}</p>
      <p className="text-sm text-slate-500">{scoreWord(words, value)}</p>
    </Tile>
  );
}

function HeroTile({ m, name, ticker }: { m?: MetricsReport; name: string; ticker: string }) {
  const change = m?.priceChange5y.value ?? null;
  const data = m?.charts.price.filter((_, i, all) => i % 5 === 0 || i === all.length - 1) ?? [];
  return (
    <Tile className="flex flex-col sm:col-span-2 lg:row-span-2">
      <div className="flex items-center gap-3">
        <span className="flex size-12 items-center justify-center rounded-2xl bg-gradient-to-br from-indigo-500 to-violet-500 text-sm font-extrabold text-white">{ticker.slice(0, 4)}</span>
        <h1 className="text-xl leading-tight font-extrabold sm:text-2xl">{name}</h1>
      </div>
      <div className="mt-5 flex flex-wrap items-baseline gap-3">
        <span className="text-5xl font-extrabold tracking-tight tabular-nums">{price(m?.price)}</span>
        {change != null && (
          <span className={`rounded-full px-3 py-1 text-sm font-bold ${change >= 0 ? "bg-emerald-50 text-emerald-600" : "bg-rose-50 text-rose-600"}`}>
            {signedPct(change, 0)} in 5 years
          </span>
        )}
      </div>
      {m && <p className="mt-1 text-sm text-slate-500">Market cap {usd(m.marketCap.value)} · prices as of {shortDate(m.priceDate)}</p>}
      <div className="mt-4 min-h-48 flex-1">
        {data.length > 1 && (
          <ResponsiveContainer>
            <AreaChart data={data} margin={{ top: 4, right: 0, left: 0, bottom: 0 }}>
              <defs>
                <linearGradient id="bentoPrice" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={INDIGO} stopOpacity={0.35} />
                  <stop offset="100%" stopColor={INDIGO} stopOpacity={0} />
                </linearGradient>
              </defs>
              <XAxis dataKey="date" ticks={yearTicks(data)} tickFormatter={(d: string) => d.slice(0, 4)} tick={AXIS} axisLine={false} tickLine={false} />
              <YAxis hide domain={["auto", "auto"]} />
              <Tooltip {...TIP} formatter={(v) => price(Number(v))} />
              <Area type="monotone" dataKey="value" name="Price" stroke={INDIGO} strokeWidth={2.5} fill="url(#bentoPrice)" isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
    </Tile>
  );
}

function VerdictTile({ v }: { v?: Verdict }) {
  if (!v) {
    return (
      <Tile className="sm:col-span-2">
        <Waiting text="The verdict comes last, once every score is in." />
      </Tile>
    );
  }
  return (
    <section className={`rounded-3xl bg-gradient-to-br p-6 text-white shadow-[0_8px_24px_rgba(15,23,42,0.12)] sm:col-span-2 ${VERDICT_GRADIENT[v.label] ?? "from-slate-500 to-slate-700"}`}>
      <p className="text-xs font-bold tracking-wider uppercase opacity-80">Verdict · confidence {v.confidenceLevel}</p>
      <p className="mt-1 text-6xl font-extrabold tracking-tight">{v.label || "n/a"}</p>
      <p className="mt-2 text-lg font-semibold">{v.tagline || v.meaning}</p>
      <div className="mt-4 flex gap-6 text-sm">
        <span>
          Business <strong className="text-lg">{v.business?.toFixed(1) ?? "–"}</strong>/5
        </span>
        <span>
          Valuation <strong className="text-lg">{v.valuation ?? "–"}</strong>/5
        </span>
      </div>
    </section>
  );
}

const PHASES = ["Startup", "Hypergrowth", "Operating leverage", "Capital return", "Decline"];

function PhaseTile({ m }: { m: MetricsReport }) {
  return (
    <Tile title="Life-cycle phase" right={`valued with ${valuedWith(m)}`} className="sm:col-span-2">
      <div className="grid grid-cols-5 gap-1.5">
        {PHASES.map((p, i) => (
          <div key={p} className={`h-2 rounded-full ${i + 1 === m.phase.number ? "bg-indigo-500" : i + 1 < m.phase.number ? "bg-indigo-200" : "bg-slate-100"}`} title={p} />
        ))}
      </div>
      <p className="mt-3 text-2xl font-extrabold">
        <span className="text-indigo-500">{m.phase.number}</span> {m.phase.name}
      </p>
      {fallbackNote(m) && <p className="mt-1 text-sm text-amber-700">{fallbackNote(m)}</p>}
      <ul className="mt-2 space-y-1 text-sm">
        {m.phase.checks.map((c) => (
          <li key={c.question} className="flex items-center gap-2">
            <span className={`flex size-5 items-center justify-center rounded-full text-[11px] font-bold text-white ${c.answer == null ? "bg-slate-300" : c.answer ? "bg-emerald-500" : "bg-rose-500"}`}>{c.answer == null ? "?" : c.answer ? "✓" : "✕"}</span>
            <span className="text-slate-600">{c.question}</span>
          </li>
        ))}
      </ul>
    </Tile>
  );
}

function BandsTile({ m }: { m: MetricsReport }) {
  const b = m.valuationBands;
  if (!b) {
    return (
      <Tile title="Valuation" className="sm:col-span-2 lg:col-span-3">
        <p className="text-sm text-slate-500">Not enough history to compare: {m.valuationScore.reasons.join("; ")}</p>
      </Tile>
    );
  }
  const values = m.charts.valuation1y.map((p) => p.value);
  const lo = Math.min(...values, b.cheapBelow) * 0.95;
  const hi = Math.max(...values, b.expensiveAbove) * 1.05;
  return (
    <Tile title={`${b.metric} over the last year`} right={`higher than ${pct(b.percentile, 0)} of days since ${b.historyFrom.slice(0, 4)}`} className="sm:col-span-2 lg:col-span-3">
      <div className="h-56">
        <ResponsiveContainer>
          <LineChart data={m.charts.valuation1y} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
            <ReferenceArea y1={lo} y2={b.cheapBelow} fill={EMERALD} fillOpacity={0.1} ifOverflow="hidden" />
            <ReferenceArea y1={b.cheapBelow} y2={b.expensiveAbove} fill={YELLOW} fillOpacity={0.14} ifOverflow="hidden" />
            <ReferenceArea y1={b.expensiveAbove} y2={hi} fill={ROSE} fillOpacity={0.1} ifOverflow="hidden" />
            <ReferenceLine y={b.median} stroke="#ca8a04" strokeDasharray="4 4" />
            <XAxis dataKey="date" tickFormatter={monthYear} tick={AXIS} axisLine={false} tickLine={false} minTickGap={50} />
            <YAxis domain={[lo, hi]} tickFormatter={multipleTick} tick={AXIS} axisLine={false} tickLine={false} width={40} />
            <Tooltip {...TIP} formatter={(v) => multiple(Number(v))} />
            <Line dataKey="value" name={b.metric} stroke={INDIGO} strokeWidth={2.5} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </Tile>
  );
}

function PriceTile({ m }: { m: MetricsReport }) {
  const b = m.valuationBands;
  if (!b) return null;
  const rows = [
    { label: "Expensive above", value: b.expensivePrice, dot: "bg-rose-500" },
    { label: "Typical", value: b.medianPrice, dot: "bg-amber-400" },
    { label: "Cheap below", value: b.cheapPrice, dot: "bg-emerald-500" },
  ];
  const vsTypical = b.price / b.medianPrice - 1;
  return (
    <Tile title="Fair price" className="flex flex-col">
      <ul className="space-y-3">
        {rows.map((r) => (
          <li key={r.label} className="flex items-center justify-between gap-2">
            <span className="flex items-center gap-2 text-sm text-slate-500">
              <span className={`size-2.5 rounded-full ${r.dot}`} />
              {r.label}
            </span>
            <span className="text-lg font-extrabold tabular-nums">{wholePrice(r.value)}</span>
          </li>
        ))}
      </ul>
      <div className="mt-auto rounded-2xl bg-slate-50 p-3 pt-3">
        <p className="text-xs font-bold text-slate-400 uppercase">Now</p>
        <p className="text-2xl font-extrabold">{price(b.price)}</p>
        <p className={`text-sm font-semibold ${vsTypical > 0 ? "text-rose-500" : "text-emerald-600"}`}>
          {signedPct(vsTypical, 0)} vs typical
        </p>
      </div>
    </Tile>
  );
}

function BusinessTile({ body }: { body?: ReportBody }) {
  const o = body?.agents?.business_overview;
  return (
    <Tile title="The business" className="sm:col-span-2">
      {isPending(body, "business_overview") ? (
        <Waiting text="AI analysts are reading the 10-K…" />
      ) : o?.output ? (
        <div className="space-y-3 leading-relaxed text-slate-700">
          <p className="text-lg font-semibold text-slate-900">{o.output.whatItDoes}</p>
          <p>{o.output.howItMakesMoney}</p>
          <p className="text-sm text-slate-500">
            <strong className="text-slate-700">Customers:</strong> {o.output.payingCustomers}
          </p>
        </div>
      ) : (
        <p className="text-sm text-slate-500">The overview is not available for this report.</p>
      )}
    </Tile>
  );
}

function SegmentsTile({ body }: { body?: ReportBody }) {
  const segs = body?.segments;
  const rows = [...(segs?.segments ?? [])].sort((a, b) => b.revenue - a.revenue);
  if (!isPending(body, "segments") && !rows.length) return null;
  return (
    <Tile title="Revenue by segment" right={segs?.fiscalYear} className="sm:col-span-2">
      {!rows.length ? (
        <Waiting text="Reading the segment table…" />
      ) : (
        <div className="flex flex-col items-center gap-4 sm:flex-row">
          <div className="size-40 shrink-0">
            <ResponsiveContainer>
              <PieChart>
                <Pie data={rows} dataKey="revenue" nameKey="name" innerRadius="62%" outerRadius="100%" paddingAngle={2} stroke="none" isAnimationActive={false}>
                  {rows.map((r, i) => (
                    <Cell key={r.name} fill={SEGMENT_COLORS[i % SEGMENT_COLORS.length]} />
                  ))}
                </Pie>
                <Tooltip {...TIP} formatter={(v) => usd(Number(v))} />
              </PieChart>
            </ResponsiveContainer>
          </div>
          <ul className="w-full space-y-2 text-sm">
            {rows.map((r, i) => (
              <li key={r.name} className="flex items-center gap-2">
                <span className="size-2.5 shrink-0 rounded-full" style={{ background: SEGMENT_COLORS[i % SEGMENT_COLORS.length] }} />
                <span className="min-w-0 flex-1 truncate font-semibold">{r.name}</span>
                <span className="tabular-nums text-slate-500">{pct(r.share, 0)}</span>
                {r.yoy != null && <span className={`w-12 text-right font-semibold tabular-nums ${r.yoy >= 0 ? "text-emerald-600" : "text-rose-500"}`}>{signedPct(r.yoy, 0)}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}
    </Tile>
  );
}

function MarginsTile({ m }: { m: MetricsReport }) {
  return (
    <Tile title="Profitability" right={m.profitability[0]?.period} className="sm:col-span-2">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {m.profitability.map((x) => (
          <div key={x.key} title={x.formula}>
            <p className="text-3xl font-extrabold tabular-nums">{metricValue(x)}</p>
            <p className="text-sm text-slate-500">{x.label.replace(" margin", "")}</p>
          </div>
        ))}
      </div>
    </Tile>
  );
}

function GrowthTile({ m }: { m: MetricsReport }) {
  const rows = ["3y", "5y", "10y"].map((y) => ({ y, v: metricOf(m, `revenueCagr${y}`)?.value ?? null }));
  const max = Math.max(...rows.map((r) => Math.abs(r.v ?? 0)), 0.01);
  return (
    <Tile title="Revenue growth / yr">
      <ul className="space-y-3">
        {rows.map((r) => (
          <li key={r.y}>
            <div className="flex justify-between text-sm">
              <span className="text-slate-500">{r.y}</span>
              <span className="font-bold tabular-nums">{pct(r.v)}</span>
            </div>
            <div className="mt-1 h-2 rounded-full bg-slate-100">
              <div className={`h-2 rounded-full ${(r.v ?? 0) >= 0 ? "bg-indigo-500" : "bg-rose-500"}`} style={{ width: `${(Math.abs(r.v ?? 0) / max) * 100}%` }} />
            </div>
          </li>
        ))}
      </ul>
    </Tile>
  );
}

function HealthTile({ m }: { m: MetricsReport }) {
  return (
    <Tile title="Balance sheet">
      <ul className="space-y-2 text-sm">
        {m.health.map((x) => (
          <li key={x.key} className="flex justify-between gap-2" title={x.formula}>
            <span className="truncate text-slate-500">{x.label.replace("Cash & short-term investments", "Cash")}</span>
            <span className="font-bold tabular-nums">{metricValue(x)}</span>
          </li>
        ))}
      </ul>
    </Tile>
  );
}

function IncomeTile({ body }: { body?: ReportBody }) {
  const rows = incomeRows(body);
  if (rows.length < 2) return null;
  return (
    <Tile title="Revenue and net income" right={<span><span className="text-indigo-500">●</span> revenue <span className="text-emerald-500">●</span> net income</span>} className="sm:col-span-2">
      <div className="h-52">
        <ResponsiveContainer>
          <BarChart data={rows} margin={{ top: 4, right: 0, left: 0, bottom: 0 }} barGap={3}>
            <XAxis dataKey="label" tick={AXIS} axisLine={false} tickLine={false} />
            <YAxis hide />
            <Tooltip {...TIP} cursor={{ fill: "#f1f5f9", radius: 8 }} formatter={(v) => usd(Number(v))} />
            <Bar dataKey="revenue" name="Revenue" fill={INDIGO} radius={[6, 6, 0, 0]} isAnimationActive={false} />
            <Bar dataKey="netIncome" name="Net income" fill={EMERALD} radius={[6, 6, 0, 0]} isAnimationActive={false} />
          </BarChart>
        </ResponsiveContainer>
      </div>
    </Tile>
  );
}

function EarningsTile({ m, body }: { m: MetricsReport; body?: ReportBody }) {
  const rows = (body?.earnings ?? []).slice(-8);
  if (!rows.length) return null;
  return (
    <Tile title="Earnings vs estimates" className="sm:col-span-2">
      <p className="text-4xl font-extrabold">
        {m.earnings.beats}
        <span className="text-xl text-slate-400">/{m.earnings.quarters} beats</span>
      </p>
      {m.earnings.avgSurprisePercent != null && <p className="text-sm text-slate-500">Average surprise {signedPct(m.earnings.avgSurprisePercent / 100)}</p>}
      <div className="mt-4 flex gap-2">
        {rows.map((r) => {
          const beat = r.reportedEPS != null && r.estimatedEPS != null && r.reportedEPS > r.estimatedEPS;
          return (
            <div key={r.fiscalDateEnding} className="flex flex-1 flex-col items-center gap-1" title={`${monthYear(r.fiscalDateEnding)}: $${r.reportedEPS?.toFixed(2)} vs $${r.estimatedEPS?.toFixed(2)}`}>
              <span className={`h-10 w-full rounded-lg ${beat ? "bg-emerald-400" : "bg-rose-400"}`} />
              <span className="text-[10px] text-slate-400">{monthYear(r.fiscalDateEnding)}</span>
            </div>
          );
        })}
      </div>
    </Tile>
  );
}

const ANSWER_STYLE = ["bg-rose-50 text-rose-600", "bg-amber-50 text-amber-700", "bg-emerald-50 text-emerald-700"];

function QualityTile({ body }: { body?: ReportBody }) {
  const q = body?.agents?.business_quality;
  return (
    <Tile title="Business quality" right={body?.businessQuality ? `${body.businessQuality.points} of 8 points` : undefined} className="sm:col-span-2 lg:col-span-4">
      {isPending(body, "business_quality") ? (
        <Waiting text="Judging business quality…" />
      ) : q?.output ? (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {QUALITY_QUESTIONS.map((item) => {
            const a = q.output![item.key];
            const level = item.options.indexOf(a?.answer ?? "");
            return (
              <div key={item.key} className="rounded-2xl bg-slate-50 p-4">
                <p className="text-sm font-bold">{item.question}</p>
                <span className={`mt-2 inline-block rounded-full px-3 py-1 text-sm font-bold ${ANSWER_STYLE[level] ?? "bg-slate-100 text-slate-500"}`}>{a?.answer ?? "–"}</span>
                <p className="mt-2 text-sm leading-relaxed text-slate-500">{a?.justification}</p>
              </div>
            );
          })}
        </div>
      ) : (
        <p className="text-sm text-slate-500">Business quality could not be judged for this report.</p>
      )}
    </Tile>
  );
}

export function Bento({ id }: { id: string }) {
  const { data: report, error, isLoading } = useLiveReport(id);
  const body = report?.report;
  const m = body?.metrics;
  const name = titleCase(body?.company.name || report?.companyName || "");
  const stage = report ? Math.max(STAGES.findIndex((s) => s.id === report.stage), 0) : 0;

  return (
    <div className="min-h-screen bg-[#f3f4f7] font-bento text-slate-900">
      <div className="mx-auto max-w-6xl px-4 py-5">
        <header className="mb-5 flex flex-wrap items-center justify-between gap-3">
          <button onClick={() => navigate("/")} className="flex items-center gap-2 text-lg font-extrabold" title="New search">
            <span className="size-7 rounded-lg bg-gradient-to-br from-indigo-500 to-emerald-400" aria-hidden />
            TickerLens
          </button>
          <div className="flex min-w-0 items-center gap-2">
            <ResyncButton report={report} look="bento" />
            <DesignSwitch look="bento" />
          </div>
        </header>

        {isLoading ? (
          <Tile>
            <Waiting />
          </Tile>
        ) : error || !report ? (
          <Tile>Report not found.</Tile>
        ) : report.status === "failed" ? (
          <Tile>This report could not be built: {report.error}</Tile>
        ) : (
          <>
            {!isFinished(report.status) && (
              <p className="mb-4 rounded-2xl bg-indigo-50 px-4 py-3 text-sm font-semibold text-indigo-700" aria-live="polite">
                Step {stage + 1} of {STAGES.length}: {STAGES[stage]?.label}…
              </p>
            )}
            <main className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
              <HeroTile m={m} name={name} ticker={report.ticker} />
              <VerdictTile v={body?.verdict} />
              {m && (
                <>
                  <RingTile title="Growth" value={m.growthScore.value} words={SCORE_WORDS.growth} />
                  <RingTile title="Financial health" value={m.healthScore.value} words={SCORE_WORDS.health} />
                  <RingTile title="Business quality" value={body?.businessQuality?.score ?? null} words={SCORE_WORDS.quality} />
                  <RingTile title="Valuation" value={m.valuationScore.value} words={SCORE_WORDS.valuation} />
                  <PhaseTile m={m} />
                  <BandsTile m={m} />
                  <PriceTile m={m} />
                </>
              )}
              <BusinessTile body={body} />
              <SegmentsTile body={body} />
              {m && (
                <>
                  <MarginsTile m={m} />
                  <GrowthTile m={m} />
                  <HealthTile m={m} />
                </>
              )}
              <IncomeTile body={body} />
              {m && <EarningsTile m={m} body={body} />}
              <QualityTile body={body} />
            </main>
            <div className="mx-auto mt-6 max-w-2xl text-slate-500">
              <RunDetails report={report} />
            </div>
            <p className="mt-4 text-center text-xs text-slate-400">
              {m && <>Financials through {shortDate(m.financialsThrough)} · </>}Educational only, not financial advice.
            </p>
          </>
        )}
      </div>
    </div>
  );
}
