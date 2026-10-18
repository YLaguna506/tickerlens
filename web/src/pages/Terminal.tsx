// Prototype design: a dark, dense, one-screen dashboard in the spirit of a
// trading terminal. Same data as the classic report, laid out as panels.

import { MoreSections } from "../components/deepSections";
import type { ReactNode } from "react";
import { Area, AreaChart, Bar, BarChart, Line, LineChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { isFinished, type Bands, type MetricsReport, type ReportBody, type SegmentShare, type Verdict } from "../api";
import { LangNotice, LimitedNotice, ReportControls } from "../components/Controls";
import { RunDetails } from "../components/RunDetails";
import { metricValue, monthYear, multiple, multipleTick, pct, price, shortDate, signedPct, titleCase, usd, wholePrice } from "../format";
import { t, tx } from "../i18n";
import { QUALITY_QUESTIONS, STAGES, fallbackNote, incomeRows, isPending, metricOf, useLiveReport, valuedWith, withPrices } from "../live";
import { navigate } from "../router";

const AMBER = "#e3b341";
const BLUE = "#58a6ff";
const UP = "#3fb950";
const DOWN = "#f85149";
const DIM = "#6b7785";
const AXIS = { fontSize: 10, fill: DIM, fontFamily: "JetBrains Mono, monospace" };
const TIP = {
  contentStyle: { background: "#0a0d12", border: "1px solid #1e2631", fontFamily: "JetBrains Mono, monospace", fontSize: 11 },
  labelStyle: { color: DIM },
  itemStyle: { color: "#c9d1d9" },
};

const VERDICT_COLOR: Record<string, string> = { Buy: "text-t-up", Watch: "text-t-up", Hold: "text-t-amber", Pass: "text-t-down", Sell: "text-t-down" };
const LABELS: Verdict["label"][] = ["Sell", "Pass", "Hold", "Watch", "Buy"];

const signTone = (v: number | null | undefined) => (v == null ? "text-t-dim" : v >= 0 ? "text-t-up" : "text-t-down");

function Panel({ title, right, className = "", children }: { title: string; right?: ReactNode; className?: string; children: ReactNode }) {
  return (
    <section className={`min-w-0 bg-t-panel ${className}`}>
      <header className="flex items-center justify-between gap-3 border-b border-t-line px-3 py-1.5 font-mono text-[11px] tracking-wider uppercase">
        <span className="text-t-amber">{t(title)}</span>
        {right && <span className="text-t-dim">{right}</span>}
      </header>
      <div className="p-3">{children}</div>
    </section>
  );
}

function Waiting({ children = "loading" }: { children?: ReactNode }) {
  return (
    <p className="font-mono text-xs text-t-dim">
      {typeof children === "string" ? t(children) : children}
      <span className="animate-pulse">_</span>
    </p>
  );
}

/** ▮▮▮▯▯ blocks for a 1–5 score. */
function Blocks({ value }: { value: number | null }) {
  const filled = value == null ? 0 : Math.round(value);
  const color = value == null ? "bg-t-line" : value >= 3.5 ? "bg-t-up" : value >= 2.5 ? "bg-t-amber" : "bg-t-down";
  return (
    <span className="flex gap-0.5" aria-label={value == null ? "no score" : `${value.toFixed(1)} of 5`}>
      {[1, 2, 3, 4, 5].map((i) => (
        <span key={i} className={`h-3 w-2.5 ${i <= filled ? color : "bg-t-line"}`} />
      ))}
    </span>
  );
}

function ScoreRow({ label, value }: { label: string; value: number | null }) {
  return (
    <div className="flex items-center justify-between gap-3 py-1 font-mono text-xs">
      <span className="text-t-dim uppercase">{t(label)}</span>
      <span className="flex items-center gap-2">
        <Blocks value={value} />
        <span className="w-7 text-right text-t-text">{value == null ? "–" : value.toFixed(1)}</span>
      </span>
    </div>
  );
}

// --- Panels ---------------------------------------------------------------------------

function PricePanel({ m }: { m: MetricsReport }) {
  const data = m.charts.price.filter((_, i, all) => i % 3 === 0 || i === all.length - 1);
  const change = m.priceChange5y.value;
  return (
    <Panel title="Price · 5y" right={<span className={signTone(change)}>{signedPct(change, 1)}</span>} className="lg:col-span-8">
      <div className="h-56 sm:h-64">
        <ResponsiveContainer>
          <AreaChart data={data} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id="tPrice" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={BLUE} stopOpacity={0.25} />
                <stop offset="100%" stopColor={BLUE} stopOpacity={0} />
              </linearGradient>
            </defs>
            <XAxis dataKey="date" tickFormatter={monthYear} tick={AXIS} axisLine={false} tickLine={false} minTickGap={60} />
            <YAxis orientation="right" domain={["auto", "auto"]} tickFormatter={(v: number) => `${Math.round(v)}`} tick={AXIS} axisLine={false} tickLine={false} width={40} />
            <Tooltip {...TIP} formatter={(v) => price(Number(v))} />
            <Area dataKey="value" name={t("Price")} stroke={BLUE} strokeWidth={1.5} fill="url(#tPrice)" isAnimationActive={false} />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </Panel>
  );
}

function VerdictPanel({ body }: { body?: ReportBody }) {
  const v = body?.verdict;
  return (
    <Panel title="Verdict" right={v ? t("conf {level} {n}", { level: t(v.confidenceLevel), n: v.confidence }) : undefined} className="lg:col-span-4">
      {!v ? (
        <Waiting>scoring</Waiting>
      ) : (
        <div className="space-y-3">
          <p className={`font-mono text-4xl font-bold uppercase ${VERDICT_COLOR[v.label] ?? "text-t-text"}`}>{v.label ? t(v.label) : t("n/a")}</p>
          <div className="grid grid-cols-5 gap-px bg-t-line font-mono text-[10px] uppercase">
            {LABELS.map((l) => (
              <span key={l} className={`py-1 text-center ${l === v.label ? "bg-t-amber font-bold text-black" : "bg-t-panel text-t-dim"}`}>{t(l)}</span>
            ))}
          </div>
          {(v.tagline || v.meaning) && <p className="text-sm text-white">{v.tagline || tx(v.meaning)}</p>}
          <div className="border-t border-t-line pt-2">
            <ScoreRow label="Business" value={v.business} />
            {v.components.map((c) => (
              <ScoreRow key={c.name} label={`· ${tx(c.name)}`} value={c.score} />
            ))}
            <ScoreRow label="Valuation" value={v.valuation} />
          </div>
          <p className="font-mono text-[11px] leading-relaxed text-t-dim">{tx(v.rule)}</p>
        </div>
      )}
    </Panel>
  );
}

const GROWTH_ROWS = [
  { key: "revenue", label: "Revenue" },
  { key: "eps", label: "EPS" },
  { key: "fcf", label: "FCF" },
];

function KV({ label, value, tone = "text-t-text", title }: { label: string; value: string; tone?: string; title?: string }) {
  return (
    <div className="flex justify-between gap-3 py-0.5" title={title}>
      <span className="truncate text-t-dim">{label}</span>
      <span className={`shrink-0 tabular-nums ${tone}`}>{value}</span>
    </div>
  );
}

function FundamentalsPanel({ m }: { m: MetricsReport }) {
  const group = (title: string, list: MetricsReport["health"]) => (
    <div>
      <p className="mb-1 text-[10px] tracking-wider text-t-blue uppercase">{t(title)}</p>
      {list.map((x) => (
        <KV key={x.key} label={tx(x.label)} value={metricValue(x)} title={tx(x.formula)} />
      ))}
    </div>
  );
  return (
    <Panel title="Fundamentals" right={t("thru {date}", { date: m.financialsThrough })} className="lg:col-span-4">
      <div className="space-y-3 font-mono text-xs">
        {group("Profitability", m.profitability)}
        <div>
          <p className="mb-1 text-[10px] tracking-wider text-t-blue uppercase">{t("Growth · CAGR")}</p>
          <table className="w-full tabular-nums">
            <thead>
              <tr className="text-t-dim">
                <th className="text-left font-normal" />
                {["3y", "5y", "10y"].map((y) => (
                  <th key={y} className="text-right font-normal">{y}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {GROWTH_ROWS.map((r) => (
                <tr key={r.key}>
                  <td className="text-t-dim">{t(r.label)}</td>
                  {["3y", "5y", "10y"].map((y) => {
                    const v = metricOf(m, `${r.key}Cagr${y}`)?.value;
                    return <td key={y} className={`text-right ${signTone(v)}`}>{pct(v)}</td>;
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {group("Health", m.health)}
        {group("Valuation", [m.marketCap, ...m.valuation])}
        {group("Shareholder yield", m.returns)}
      </div>
    </Panel>
  );
}

function BandsPanel({ m }: { m: MetricsReport }) {
  const b = m.valuationBands;
  if (!b) {
    return (
      <Panel title="Valuation bands" className="lg:col-span-5">
        <p className="font-mono text-xs text-t-dim">
          {t("Not enough history:")} {m.valuationScore.reasons.map(tx).join("; ")}
        </p>
      </Panel>
    );
  }
  const values = m.charts.valuation1y.map((p) => p.value);
  const lo = Math.min(...values, b.cheapBelow) * 0.95;
  const hi = Math.max(...values, b.expensiveAbove) * 1.05;
  return (
    <Panel
      title={`${b.metric} · ${t("1y")}`}
      right={t("pctl {p} · {from}–{to}", { p: Math.round(b.percentile * 100), from: b.historyFrom.slice(0, 4), to: b.historyTo.slice(0, 4) })}
      className="lg:col-span-5"
    >
      <div className="h-44">
        <ResponsiveContainer>
          <LineChart data={withPrices(m.charts.valuation1y, m.charts.price)} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
            <ReferenceArea y1={lo} y2={b.cheapBelow} fill={UP} fillOpacity={0.12} ifOverflow="hidden" />
            <ReferenceArea y1={b.cheapBelow} y2={b.expensiveAbove} fill={AMBER} fillOpacity={0.12} ifOverflow="hidden" />
            <ReferenceArea y1={b.expensiveAbove} y2={hi} fill={DOWN} fillOpacity={0.12} ifOverflow="hidden" />
            <ReferenceLine y={b.median} stroke={AMBER} strokeOpacity={0.7} strokeDasharray="3 3" />
            <XAxis dataKey="date" tickFormatter={monthYear} tick={AXIS} axisLine={false} tickLine={false} minTickGap={50} />
            <YAxis orientation="right" domain={[lo, hi]} tickFormatter={multipleTick} tick={AXIS} axisLine={false} tickLine={false} width={40} />
            {/* The share price, invisible on its own axis: shown in the tooltip only. */}
            <YAxis yAxisId="price" hide domain={["auto", "auto"]} />
            <Line yAxisId="price" dataKey="price" name={t("Price")} stroke="transparent" dot={false} activeDot={false} legendType="none" isAnimationActive={false} />
            <Tooltip {...TIP} formatter={(v, name) => (name === t("Price") ? price(Number(v)) : multiple(Number(v)))} />
            <Line dataKey="value" name={b.metric} stroke="#e6edf3" strokeWidth={1.5} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
      <PriceLadder b={b} />
    </Panel>
  );
}

/** Band edges as share prices, with the distance from today's price. */
function PriceLadder({ b }: { b: Bands }) {
  const rows = [
    { label: `${t("Expensive")} > ${multiple(b.expensiveAbove)}`, value: b.expensivePrice, tone: "text-t-down" },
    { label: `${t("Typical")}   ${multiple(b.median)}`, value: b.medianPrice, tone: "text-t-text" },
    { label: `${t("Cheap")}   < ${multiple(b.cheapBelow)}`, value: b.cheapPrice, tone: "text-t-up" },
  ];
  return (
    <table className="mt-3 w-full font-mono text-xs tabular-nums">
      <tbody>
        {rows.map((r) => (
          <tr key={r.label} className="border-t border-t-line">
            <td className={`py-1 whitespace-pre ${r.tone}`}>{r.label}</td>
            <td className="text-right text-t-text">{wholePrice(r.value)}</td>
            <td className={`w-20 text-right ${signTone(r.value / b.price - 1)}`}>{signedPct(r.value / b.price - 1, 0)}</td>
          </tr>
        ))}
        <tr className="border-t border-t-line">
          <td className="py-1 whitespace-pre text-t-amber">{t("Now")}       {multiple(b.current)}</td>
          <td className="text-right font-bold text-white">{price(b.price)}</td>
          <td className="text-right text-t-dim uppercase">{t(b.zone)}</td>
        </tr>
      </tbody>
    </table>
  );
}

const PHASES = ["Startup", "Hypergrowth", "Op. leverage", "Capital return", "Decline"];

function ScoresPanel({ m, body }: { m: MetricsReport; body?: ReportBody }) {
  return (
    <Panel title="Scores · phase" className="lg:col-span-3">
      <ScoreRow label="Growth" value={m.growthScore.value} />
      <ScoreRow label="Health" value={m.healthScore.value} />
      <ScoreRow label="Quality" value={body?.businessQuality?.score ?? null} />
      <ScoreRow label="Valuation" value={m.valuationScore.value} />
      <div className="mt-3 grid grid-cols-5 gap-px bg-t-line font-mono text-[10px]">
        {PHASES.map((p, i) => (
          <span key={p} title={t(p)} className={`py-1.5 text-center ${i + 1 === m.phase.number ? "bg-t-amber font-bold text-black" : "bg-t-panel text-t-dim"}`}>{i + 1}</span>
        ))}
      </div>
      <p className="mt-2 font-mono text-xs text-white">
        {tx(m.phase.name)} <span className="text-t-dim">→ {valuedWith(m)}</span>
      </p>
      {fallbackNote(m) && <p className="mt-1 font-mono text-[11px] text-t-amber">! {fallbackNote(m)}</p>}
      <ul className="mt-2 space-y-1 font-mono text-[11px]">
        {m.phase.checks.map((c) => (
          <li key={c.question} className="flex gap-2" title={tx(c.detail)}>
            <span className={c.answer == null ? "text-t-dim" : c.answer ? "text-t-up" : "text-t-down"}>{c.answer == null ? "?" : c.answer ? "✓" : "✕"}</span>
            <span className="text-t-dim">{tx(c.question)}</span>
          </li>
        ))}
      </ul>
    </Panel>
  );
}

function IncomePanel({ body }: { body?: ReportBody }) {
  const rows = incomeRows(body);
  return (
    <Panel title="Revenue · net income" right={
        <span>
          <span className="text-t-blue">■</span> {t("rev")} <span className="text-t-up">■</span> {t("ni")}
        </span>
      } className="lg:col-span-6">
      {rows.length < 2 ? (
        <Waiting />
      ) : (
        <div className="h-52">
          <ResponsiveContainer>
            <BarChart data={rows} margin={{ top: 4, right: 4, left: 0, bottom: 0 }} barGap={2}>
              <XAxis dataKey="label" tick={AXIS} axisLine={false} tickLine={false} />
              <YAxis orientation="right" tickFormatter={(v: number) => usd(v)} tick={AXIS} axisLine={false} tickLine={false} width={52} />
              <Tooltip {...TIP} cursor={{ fill: "#1e2631" }} formatter={(v) => usd(Number(v))} />
              <ReferenceLine y={0} stroke="#1e2631" />
              <Bar dataKey="revenue" name={t("Revenue")} fill={BLUE} isAnimationActive={false} />
              <Bar dataKey="netIncome" name={t("Net income")} fill={UP} isAnimationActive={false} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </Panel>
  );
}

function ShareTable({ rows }: { rows: SegmentShare[] }) {
  const sorted = [...rows].sort((a, b) => b.revenue - a.revenue);
  return (
    <table className="w-full font-mono text-xs tabular-nums">
      <tbody>
        {sorted.map((r) => (
          <tr key={r.name} className="border-t border-t-line">
            <td className="max-w-0 truncate py-1 pr-2 text-t-text">{r.name}</td>
            <td className="w-24">
              <span className="block h-2 bg-t-blue" style={{ width: `${Math.max(r.share * 100, 2)}%` }} />
            </td>
            <td className="w-14 text-right text-t-dim">{pct(r.share, 0)}</td>
            <td className="w-16 text-right text-t-text">{usd(r.revenue)}</td>
            <td className={`w-16 text-right ${signTone(r.yoy)}`}>{r.yoy == null ? "–" : signedPct(r.yoy, 0)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function SegmentsPanel({ body }: { body?: ReportBody }) {
  const segs = body?.segments;
  return (
    <Panel title="Segments" right={segs?.fiscalYear} className="lg:col-span-6">
      {isPending(body, "segments") ? (
        <Waiting>reading 10-K</Waiting>
      ) : !segs?.segments?.length && !segs?.geography?.length ? (
        <p className="font-mono text-xs text-t-dim">{t("Not disclosed or not extracted.")}</p>
      ) : (
        <div className="space-y-3">
          {segs.segments?.length ? <ShareTable rows={segs.segments} /> : null}
          {segs.geography?.length ? (
            <div>
              <p className="mb-1 font-mono text-[10px] tracking-wider text-t-blue uppercase">{t("Geography")}</p>
              <ShareTable rows={segs.geography} />
            </div>
          ) : null}
        </div>
      )}
    </Panel>
  );
}


function BusinessPanel({ body }: { body?: ReportBody }) {
  const o = body?.agents?.business_overview;
  const q = body?.agents?.business_quality;
  const lines = o?.output
    ? [
        ["What", o.output.whatItDoes],
        ["How", o.output.howItMakesMoney],
        ["Who pays", o.output.payingCustomers],
        ["Why", o.output.whyTheyBuy],
      ]
    : [];
  return (
    <Panel title="Business brief" right={t("from the 10-K")} className="lg:col-span-8">
      {isPending(body, "business_overview") ? (
        <Waiting>reading 10-K</Waiting>
      ) : lines.length ? (
        <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-[6rem_1fr]">
          {lines.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="font-mono text-[11px] text-t-blue uppercase sm:pt-0.5">{t(k ?? "")}</dt>
              <dd className="leading-relaxed text-t-text">{v}</dd>
            </div>
          ))}
        </dl>
      ) : (
        <p className="font-mono text-xs text-t-dim">{t("Overview unavailable.")}</p>
      )}

      <div className="mt-4 border-t border-t-line pt-3">
        <p className="mb-2 font-mono text-[10px] tracking-wider text-t-blue uppercase">
          {t("Quality")} {body?.businessQuality ? `· ${body.businessQuality.score.toFixed(1)}/5` : ""}
        </p>
        {isPending(body, "business_quality") ? (
          <Waiting>judging</Waiting>
        ) : q?.output ? (
          <div className="space-y-1">
            {QUALITY_QUESTIONS.map((item) => {
              const a = q.output![item.key];
              return (
                <details key={item.key} className="group font-mono text-xs">
                  <summary className="flex cursor-pointer list-none items-center gap-3">
                    <span className="w-28 shrink-0 text-t-dim">{t(item.short)}</span>
                    <span className="grid flex-1 grid-cols-3 gap-px bg-t-line text-[10px] uppercase">
                      {item.options.map((opt, i) => (
                        <span key={opt} className={`truncate px-1 py-0.5 text-center ${opt === a?.answer ? (i === 2 ? "bg-t-up text-black" : i === 1 ? "bg-t-amber text-black" : "bg-t-down text-black") : "bg-t-panel text-t-dim"}`}>
                          {t(opt)}
                        </span>
                      ))}
                    </span>
                    <span className="text-t-dim group-open:rotate-90">›</span>
                  </summary>
                  <p className="py-2 pl-31 font-sans text-[13px] leading-relaxed text-t-dim">{a?.justification}</p>
                </details>
              );
            })}
          </div>
        ) : (
          <p className="font-mono text-xs text-t-dim">{t("Quality unavailable.")}</p>
        )}
      </div>
    </Panel>
  );
}

function EarningsPanel({ m, body }: { m: MetricsReport; body?: ReportBody }) {
  const rows = (body?.earnings ?? []).slice(-8).reverse();
  return (
    <Panel title="EPS vs estimate" right={rows.length ? t("beat {beats}/{quarters}", { beats: m.earnings.beats, quarters: m.earnings.quarters }) : undefined} className="lg:col-span-4">
      {!rows.length ? (
        <p className="font-mono text-xs text-t-dim">{t("No estimates available.")}</p>
      ) : (
        <table className="w-full font-mono text-xs tabular-nums">
          <thead>
            <tr className="text-t-dim">
              <th className="text-left font-normal">{t("Qtr")}</th>
              <th className="text-right font-normal">{t("Est")}</th>
              <th className="text-right font-normal">{t("Act")}</th>
              <th className="text-right font-normal">{t("Surp")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const beat = r.reportedEPS != null && r.estimatedEPS != null && r.reportedEPS > r.estimatedEPS;
              return (
                <tr key={r.fiscalDateEnding} className="border-t border-t-line">
                  <td className="py-1 text-t-dim">{monthYear(r.fiscalDateEnding)}</td>
                  <td className="text-right text-t-dim">{r.estimatedEPS?.toFixed(2) ?? "–"}</td>
                  <td className="text-right text-t-text">{r.reportedEPS?.toFixed(2) ?? "–"}</td>
                  <td className={`text-right ${beat ? "text-t-up" : "text-t-down"}`}>{r.surprisePercent == null ? "–" : signedPct(r.surprisePercent / 100)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </Panel>
  );
}

// --- Page ---------------------------------------------------------------------------------

function Stat({ label, value, tone = "text-t-text" }: { label: string; value: string; tone?: string }) {
  return (
    <span className="whitespace-nowrap">
      <span className="text-t-dim">{label} </span>
      <span className={tone}>{value}</span>
    </span>
  );
}

export function Terminal({ id }: { id: string }) {
  const { data: report, error, isLoading } = useLiveReport(id);
  const body = report?.report;
  const m = body?.metrics;
  const v = body?.verdict;
  const stage = report ? STAGES.findIndex((s) => s.id === report.stage) : -1;

  return (
    <div className="min-h-screen bg-t-bg text-t-text">
      <header className="sticky top-0 z-20 border-b border-t-line bg-t-bg/95 backdrop-blur">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 px-3 py-2 font-mono text-xs">
          <button onClick={() => navigate("/")} className="bg-t-amber px-2 py-0.5 font-bold text-black" title={t("New search")}>
            {report?.ticker ?? "…"}
          </button>
          <span className="max-w-[16rem] truncate text-white">{titleCase(body?.company.name || report?.companyName || "")}</span>
          {m?.price != null && <span className="text-base font-bold text-white">{price(m.price)}</span>}
          {m && <Stat label={t("5Y")} value={signedPct(m.priceChange5y.value, 0)} tone={signTone(m.priceChange5y.value)} />}
          {m && <Stat label={t("MCAP")} value={usd(m.marketCap.value)} />}
          {m?.valuationBands && <Stat label={m.valuationBands.metric} value={multiple(m.valuationBands.current)} tone="text-t-amber" />}
          {v?.label && <Stat label={t("VERDICT")} value={t(v.label).toUpperCase()} tone={VERDICT_COLOR[v.label]} />}
          <div className="ml-auto flex min-w-0 flex-wrap items-center gap-2">
            <ReportControls report={report} look="terminal" />
          </div>
        </div>
        {report && !isFinished(report.status) && (
          <p className="border-t border-t-line px-3 py-1 font-mono text-[11px] text-t-amber">
            [{Math.max(stage, 0) + 1}/{STAGES.length}] {t(STAGES[Math.max(stage, 0)]?.label ?? "").toLowerCase()}
            <span className="animate-pulse">_</span>
          </p>
        )}
      </header>
      <LangNotice report={report} className="border-b border-t-line px-3 py-2 font-mono text-xs text-t-amber" />
      <LimitedNotice report={report} className="border-b border-t-line px-3 py-2 font-mono text-xs text-t-amber" />

      {isLoading ? (
        <p className="p-6 font-mono text-xs text-t-dim">
          {t("loading")}
          <span className="animate-pulse">_</span>
        </p>
      ) : error || !report ? (
        <p className="p-6 font-mono text-xs text-t-down">ERR {t("report not found")}</p>
      ) : report.status === "failed" ? (
        <p className="p-6 font-mono text-xs text-t-down">ERR {tx(report.error)}</p>
      ) : (
        <main className="grid gap-px border-b border-t-line bg-t-line lg:grid-cols-12">
          {m ? <PricePanel m={m} /> : <Panel title="Price · 5y" className="lg:col-span-8"><Waiting /></Panel>}
          <VerdictPanel body={body} />
          {m && (
            <>
              <FundamentalsPanel m={m} />
              <BandsPanel m={m} />
              <ScoresPanel m={m} body={body} />
            </>
          )}
          <IncomePanel body={body} />
          <SegmentsPanel body={body} />
          <BusinessPanel body={body} />
          {m && <EarningsPanel m={m} body={body} />}
          <MoreSections
            body={body}
            m={m}
            report={report}
            look="look-terminal"
            idPrefix="t-"
            sectionClass="min-w-0 space-y-3 bg-t-panel p-3 lg:col-span-12"
            titleClass="border-b border-t-line pb-1.5 font-mono text-[11px] tracking-wider text-t-amber uppercase"
          />
        </main>
      )}

      <footer className="space-y-1 px-3 py-4 font-mono text-[10px] text-t-dim">
        {m && (
          <p>
            {t("FIN THRU")} {shortDate(m.financialsThrough).toUpperCase()} · {t("PX")} {shortDate(m.priceDate).toUpperCase()}
            {body?.filing && <> · 10-K {shortDate(body.filing.reportDate).toUpperCase()}</>} · {t("SCORING")} {m.scoringVersion}
          </p>
        )}
        <div className="max-w-3xl pt-1 text-t-text">
          <RunDetails report={report} />
        </div>
        <p>{t("EDUCATIONAL ONLY. NOT FINANCIAL ADVICE. DATA: SEC EDGAR, TIINGO, ALPHA VANTAGE.")}</p>
      </footer>
    </div>
  );
}
