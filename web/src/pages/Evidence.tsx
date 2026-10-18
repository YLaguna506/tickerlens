// Prototype design: the verdict and its four scores stay pinned on the left;
// the right side holds the evidence, grouped by the score each number feeds.

import { MoreSections } from "../components/deepSections";
import { useEffect, useState, type ReactNode } from "react";
import { Area, AreaChart, Bar, BarChart, Line, LineChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { isFinished, type Bands, type Evidence as Quote, type Metric, type MetricsReport, type ReportBody, type Verdict } from "../api";
import { LangNotice, LimitedNotice, ReportControls } from "../components/Controls";
import { RunDetails } from "../components/RunDetails";
import { metricValue, monthYear, multiple, multipleTick, pct, price, shortDate, signedPct, titleCase, usd, wholePrice, yearTicks } from "../format";
import { t, tx } from "../i18n";
import { QUALITY_QUESTIONS, SCORE_WORDS, STAGES, fallbackNote, incomeRows, isPending, metricOf, scoreWord, useLiveReport, valuedWith } from "../live";
import { navigate } from "../router";

// Each score has its own color, used on the left and in its section.
const SCORES = {
  quality: { short: "Quality", title: "Business quality", text: "text-violet-700", bg: "bg-violet-600", soft: "bg-violet-50", border: "border-violet-600", hex: "#7c3aed", words: SCORE_WORDS.quality },
  growth: { short: "Growth", title: "Growth", text: "text-sky-700", bg: "bg-sky-600", soft: "bg-sky-50", border: "border-sky-600", hex: "#0284c7", words: SCORE_WORDS.growth },
  health: { short: "Health", title: "Financial health", text: "text-emerald-700", bg: "bg-emerald-600", soft: "bg-emerald-50", border: "border-emerald-600", hex: "#059669", words: SCORE_WORDS.health },
  valuation: { short: "Valuation", title: "Valuation", text: "text-amber-700", bg: "bg-amber-500", soft: "bg-amber-50", border: "border-amber-500", hex: "#d97706", words: SCORE_WORDS.valuation },
};
type ScoreKey = keyof typeof SCORES;
const ORDER: ScoreKey[] = ["quality", "growth", "health", "valuation"];
const SECTIONS = ["company", ...ORDER];

const AXIS = { fontSize: 11, fill: "#a1a1aa" };
const LABELS: Verdict["label"][] = ["Sell", "Pass", "Hold", "Watch", "Buy"];

function scoresOf(m?: MetricsReport, body?: ReportBody): Record<ScoreKey, number | null> {
  return {
    quality: body?.businessQuality?.score ?? null,
    growth: m?.growthScore.value ?? null,
    health: m?.healthScore.value ?? null,
    valuation: m?.valuationScore.value ?? null,
  };
}

/** The section in view, for highlighting its score on the left. */
function useActiveSection(ready: boolean) {
  const [active, setActive] = useState("company");
  useEffect(() => {
    if (!ready) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      let current = SECTIONS[0]!;
      for (const s of SECTIONS) {
        const el = document.getElementById(`ev-${s}`);
        if (el && el.getBoundingClientRect().top <= window.innerHeight / 3) current = s;
      }
      setActive(current);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [ready]);
  return active;
}

// --- Small pieces --------------------------------------------------------------------

function Meter({ value, color }: { value: number | null; color: string }) {
  const filled = value == null ? 0 : Math.round(value);
  return (
    <span className="flex gap-0.5" aria-hidden>
      {[1, 2, 3, 4, 5].map((i) => (
        <span key={i} className={`h-1.5 w-4 rounded-sm ${i <= filled ? color : "bg-zinc-200"}`} />
      ))}
    </span>
  );
}

/** A metric that opens to its formula and inputs. */
function MetricLine({ m }: { m: Metric }) {
  return (
    <details className="group border-b border-zinc-100 py-2 last:border-0">
      <summary className="flex cursor-pointer list-none items-baseline justify-between gap-3">
        <span className="text-sm text-zinc-600 group-open:text-zinc-900">{tx(m.label)}</span>
        <span className="font-semibold tabular-nums">{metricValue(m)}</span>
      </summary>
      <div className="mt-1.5 space-y-0.5 border-l-2 border-zinc-200 pl-3 text-xs text-zinc-500">
        {m.formula && <p>= {tx(m.formula)}</p>}
        {m.period && <p>{tx(m.period)}</p>}
        {m.inputs?.map((i) => (
          <p key={i.name}>
            {tx(i.name)}: <span className="tabular-nums">{i.value == null ? tx("not reported") : Math.abs(i.value) >= 1e6 ? usd(i.value) : i.value.toLocaleString("en-US", { maximumFractionDigits: 4 })}</span>
            {i.period ? ` (${tx(i.period)})` : ""}
          </p>
        ))}
        {m.note && <p className="text-amber-700">{tx(m.note)}</p>}
      </div>
    </details>
  );
}

function Quotes({ evidence, url }: { evidence?: Quote[]; url?: string }) {
  if (!evidence?.length) return null;
  return (
    <details className="mt-2 text-sm">
      <summary className="cursor-pointer text-xs font-semibold text-zinc-500 hover:text-zinc-800">
        {evidence.length > 1 ? t("{n} quotes from the 10-K", { n: evidence.length }) : t("1 quote from the 10-K")}
      </summary>
      <ul className="mt-2 space-y-2">
        {evidence.map((e, i) => (
          <li key={i} className="rounded-lg bg-zinc-50 p-3 text-zinc-600">
            <q className="italic">{e.quote}</q>
            <span className="mt-1 block text-xs text-zinc-400">
              {e.documentId.replace("10k-item", t("10-K Item "))}
              {url && (
                <>
                  {" · "}
                  <a href={url} target="_blank" rel="noreferrer" className="underline">
                    {t("open filing")}
                  </a>
                </>
              )}
            </span>
          </li>
        ))}
      </ul>
    </details>
  );
}

function Block({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-2xl border border-zinc-200 bg-white p-4 sm:p-5">
      <h3 className="mb-2 font-grotesk text-sm font-bold tracking-wide text-zinc-500 uppercase">{title}</h3>
      {children}
    </div>
  );
}

function Waiting({ text }: { text: string }) {
  return <p className="animate-pulse rounded-2xl border border-dashed border-zinc-300 p-4 text-sm text-zinc-500">{text}</p>;
}

/** A section for one score: its value, what it feeds, why, then the evidence. */
function ScoreSection({ k, value, feeds, reasons, children }: { k: ScoreKey; value: number | null; feeds: string; reasons?: string[]; children: ReactNode }) {
  const s = SCORES[k];
  return (
    <section id={`ev-${k}`} className="scroll-mt-20 space-y-4 pt-12 lg:scroll-mt-6">
      <header className={`flex items-center gap-4 border-b-2 pb-3 ${s.border}`}>
        <span className={`flex size-14 shrink-0 flex-col items-center justify-center rounded-2xl text-white ${s.bg}`}>
          <span className="font-grotesk text-xl leading-none font-bold">{value == null ? "–" : value.toFixed(1)}</span>
          <span className="text-[10px] opacity-80">{t("of 5")}</span>
        </span>
        <div>
          <h2 className="font-grotesk text-2xl font-bold tracking-tight sm:text-3xl">{t(s.title)}</h2>
          <p className="text-sm text-zinc-500">
            <span className={`font-semibold ${s.text}`}>{scoreWord(s.words, value)}</span> · {feeds}
          </p>
        </div>
      </header>
      {reasons?.length ? (
        <ul className={`space-y-1 rounded-2xl p-4 text-sm ${s.soft}`}>
          {reasons.map((r) => (
            <li key={r} className="flex gap-2">
              <span className={s.text}>→</span>
              <span>{tx(r)}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {children}
    </section>
  );
}

/** "40% of the business score", or a fallback when the weight is unknown. */
const feeds = (weight?: number) =>
  weight ? t("{pct}% of the business score", { pct: Math.round(weight * 100) }) : t("part of the business score");

// --- Left panel ------------------------------------------------------------------------

function Pinned({ ticker, name, m, body, active, running, stage }: { ticker: string; name: string; m?: MetricsReport; body?: ReportBody; active: string; running: boolean; stage: number }) {
  const v = body?.verdict;
  const scores = scoresOf(m, body);
  const spark = m?.charts.price.filter((_, i, all) => i % 10 === 0 || i === all.length - 1) ?? [];
  const change = m?.priceChange5y.value ?? null;
  return (
    <div className="space-y-5">
      <div>
        <div className="flex items-center gap-3">
          <span className="flex size-11 items-center justify-center rounded-xl bg-zinc-900 font-grotesk text-xs font-bold text-white">{ticker.slice(0, 4)}</span>
          <div className="min-w-0">
            <h1 className="truncate font-grotesk text-lg leading-tight font-bold">{name}</h1>
            {m && <p className="text-xs text-zinc-500">{t("Market cap {cap}", { cap: usd(m.marketCap.value) })}</p>}
          </div>
        </div>
        <div className="mt-3 flex items-end justify-between gap-3">
          <div>
            <p className="font-grotesk text-3xl font-bold tabular-nums">{price(m?.price)}</p>
            {change != null && <p className={`text-sm font-semibold ${change >= 0 ? "text-emerald-600" : "text-rose-600"}`}>{signedPct(change, 0)} {t("over 5 years")}</p>}
          </div>
          {spark.length > 1 && (
            <div className="h-12 w-28">
              <ResponsiveContainer>
                <LineChart data={spark}>
                  <YAxis hide domain={["auto", "auto"]} />
                  <Line dataKey="value" stroke={(change ?? 0) >= 0 ? "#059669" : "#e11d48"} strokeWidth={1.5} dot={false} isAnimationActive={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}
        </div>
      </div>

      {running && (
        <p className="animate-pulse rounded-xl bg-zinc-100 px-3 py-2 text-xs font-semibold text-zinc-600" aria-live="polite">
          {t("Step {n} of {total}: {label}…", { n: stage + 1, total: STAGES.length, label: t(STAGES[stage]?.label ?? "") })}
        </p>
      )}

      <div className="rounded-2xl bg-zinc-900 p-4 text-white">
        <p className="text-[11px] font-semibold tracking-widest text-zinc-400 uppercase">{t("Verdict")}</p>
        <p className="font-grotesk text-4xl font-bold">{v?.label ? t(v.label) : "…"}</p>
        <p className="mt-1 text-sm text-zinc-300">{v ? v.tagline || tx(v.meaning) : t("Comes last, once every score is in.")}</p>
        <div className="mt-3 grid grid-cols-5 gap-1 text-center text-[10px] font-semibold">
          {LABELS.map((l) => (
            <span key={l} className={`rounded py-1 ${l === v?.label ? "bg-white text-zinc-900" : "bg-white/10 text-zinc-400"}`}>{t(l)}</span>
          ))}
        </div>
      </div>

      <nav aria-label={t("Scores")} className="space-y-1">
        {ORDER.map((k) => {
          const s = SCORES[k];
          const on = active === k;
          return (
            <a key={k} href={`#ev-${k}`} className={`flex items-center justify-between gap-3 rounded-xl border-l-4 px-3 py-2.5 transition-colors ${on ? `${s.border} ${s.soft}` : "border-transparent hover:bg-zinc-100"}`}>
              <span>
                <span className="block text-sm font-semibold">{t(s.title)}</span>
                <span className="text-xs text-zinc-500">{scoreWord(s.words, scores[k])}</span>
              </span>
              <span className="flex items-center gap-2">
                <Meter value={scores[k]} color={s.bg} />
                <span className="w-7 text-right font-grotesk font-bold tabular-nums">{scores[k] == null ? "–" : scores[k]!.toFixed(1)}</span>
              </span>
            </a>
          );
        })}
      </nav>

      {v && (
        <div className="space-y-2 rounded-2xl border border-zinc-200 p-4 text-xs text-zinc-600">
          <p>
            <span className="font-grotesk text-base font-bold text-zinc-900">B {v.business?.toFixed(1) ?? "–"}</span> ={" "}
            {v.components.map((c, i) => (
              <span key={c.name}>
                {i > 0 && " + "}
                {Math.round(c.weight * 100)}% {tx(c.name).toLowerCase()}
              </span>
            ))}
            {v.adjustments?.map((a) => (
              <span key={a.name} title={tx(a.reason)}>
                {a.points > 0 ? " + " : " − "}
                {Math.abs(a.points).toFixed(2)} {tx(a.name).toLowerCase()}
              </span>
            ))}
          </p>
          <p>
            <span className="font-grotesk text-base font-bold text-zinc-900">V {v.valuation ?? "–"}</span> = {t("valuation score")}
          </p>
          <p className="border-t border-zinc-100 pt-2">
            <span className="font-semibold text-zinc-900">{t("Rule:")}</span> {tx(v.rule)}
          </p>
          <p>
            <span className="font-semibold text-zinc-900">{t("Confidence {level}", { level: t(v.confidenceLevel) })}</span> ({v.confidence}/100)
            {v.confidenceReasons.length ? `: ${v.confidenceReasons.map(tx).join("; ")}` : ""}
          </p>
          {v.rationale && (
            <details>
              <summary className="cursor-pointer font-semibold text-zinc-900">{t("Read the reasoning")}</summary>
              <p className="mt-1 leading-relaxed">{v.rationale}</p>
            </details>
          )}
        </div>
      )}

      {m && (
        <p className="text-xs text-zinc-500">
          {t("Phase {n} of 5", { n: m.phase.number })} · {tx(m.phase.name)} · {t("valued with {metric}", { metric: valuedWith(m) })}
        </p>
      )}
    </div>
  );
}

// --- Evidence sections ------------------------------------------------------------------

function CompanySection({ body }: { body?: ReportBody }) {
  const o = body?.agents?.business_overview;
  const rows: [string, string | null | undefined][] = o?.output
    ? [
        ["Mission", o.output.mission],
        ["What it does", o.output.whatItDoes],
        ["How it makes money", o.output.howItMakesMoney],
        ["Who pays", o.output.payingCustomers],
        ["Why they buy", o.output.whyTheyBuy],
      ]
    : [];
  return (
    <section id="ev-company" className="scroll-mt-20 space-y-4 lg:scroll-mt-6">
      <h2 className="font-grotesk text-sm font-bold tracking-widest text-zinc-400 uppercase">{t("The company")}</h2>
      {isPending(body, "business_overview") ? (
        <Waiting text={t("AI analysts are reading the latest 10-K…")} />
      ) : rows.length ? (
        <div className="rounded-2xl border border-zinc-200 bg-white p-4 sm:p-5">
          <dl className="space-y-3">
            {rows
              .filter(([, text]) => text)
              .map(([label, text]) => (
                <div key={label} className="grid gap-1 sm:grid-cols-[9rem_1fr] sm:gap-4">
                  <dt className="text-xs font-bold tracking-wide text-zinc-400 uppercase sm:pt-0.5">{t(label)}</dt>
                  <dd className="leading-relaxed">{text}</dd>
                </div>
              ))}
          </dl>
          <Quotes evidence={o?.evidence} url={body?.filing?.url} />
        </div>
      ) : (
        <p className="text-sm text-zinc-500">{t("The business overview is not available for this report.")}</p>
      )}
    </section>
  );
}

function QualitySection({ m, body }: { m?: MetricsReport; body?: ReportBody }) {
  const q = body?.agents?.business_quality;
  const v = body?.verdict;
  const weight = v?.components.find((c) => c.name === "Business quality")?.weight;
  const bq = body?.businessQuality;
  return (
    <ScoreSection k="quality" value={bq?.score ?? null} feeds={feeds(weight)} reasons={bq ? [t("{points} of 8 points across four questions judged from the 10-K", { points: bq.points })] : undefined}>
      {isPending(body, "business_quality") ? (
        <Waiting text={t("Judging business quality…")} />
      ) : q?.output ? (
        <div className="space-y-3">
          {QUALITY_QUESTIONS.map((item) => {
            const a = q.output![item.key];
            return (
              <div key={item.key} className="rounded-2xl border border-zinc-200 bg-white p-4 sm:p-5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h3 className="font-grotesk text-lg font-bold">{t(item.question)}</h3>
                  <div className="flex overflow-hidden rounded-lg border border-zinc-200 text-xs font-semibold">
                    {item.options.map((opt) => (
                      <span key={opt} className={`px-2.5 py-1 ${opt === a?.answer ? "bg-violet-600 text-white" : "text-zinc-400"}`}>{t(opt)}</span>
                    ))}
                  </div>
                </div>
                <p className="mt-2 leading-relaxed text-zinc-600">{a?.justification}</p>
                <Quotes evidence={a?.evidence} url={body?.filing?.url} />
              </div>
            );
          })}
        </div>
      ) : (
        <p className="text-sm text-zinc-500">
          {t("Business quality could not be judged for this report")}
          {q?.errors?.length ? `: ${tx(q.errors[0])}` : "."}
        </p>
      )}
      {m && (
        <Block title={t("Margins behind it")}>
          <div className="grid gap-x-8 sm:grid-cols-2">
            {m.profitability.map((x) => (
              <MetricLine key={x.key} m={x} />
            ))}
          </div>
        </Block>
      )}
    </ScoreSection>
  );
}

const CAGR_ROWS = [
  { key: "revenue", label: "Revenue" },
  { key: "eps", label: "EPS" },
  { key: "fcf", label: "Free cash flow" },
];

function GrowthSection({ m, body }: { m: MetricsReport; body?: ReportBody }) {
  const v = body?.verdict;
  const weight = v?.components.find((c) => c.name === "Growth")?.weight;
  const rows = incomeRows(body);
  const segs = body?.segments;
  const earnings = (body?.earnings ?? []).slice(-8);
  return (
    <ScoreSection k="growth" value={m.growthScore.value} feeds={feeds(weight)} reasons={m.growthScore.reasons}>
      <Block title={t("Growth a year")}>
        <table className="w-full text-sm tabular-nums">
          <thead>
            <tr className="text-xs text-zinc-400">
              <th className="text-left font-semibold" />
              {["3 years", "5 years", "10 years"].map((h) => (
                <th key={h} className="pb-1 text-right font-semibold">{t(h)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {CAGR_ROWS.map((r) => (
              <tr key={r.key} className="border-t border-zinc-100">
                <td className="py-2 text-zinc-600">{t(r.label)}</td>
                {["3y", "5y", "10y"].map((y) => {
                  const val = metricOf(m, `${r.key}Cagr${y}`)?.value;
                  return (
                    <td key={y} className={`text-right font-semibold ${val == null ? "text-zinc-300" : val >= 0 ? "text-sky-700" : "text-rose-600"}`}>{pct(val)}</td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
        <details className="mt-2">
          <summary className="cursor-pointer text-xs font-semibold text-zinc-500">{t("How each rate is calculated")}</summary>
          <div className="mt-1 grid gap-x-8 sm:grid-cols-2">
            {m.growth.filter((g) => !g.key.endsWith("1y")).map((g) => (
              <MetricLine key={g.key} m={g} />
            ))}
          </div>
        </details>
      </Block>

      {rows.length > 1 && (
        <Block title={t("Revenue and net income")}>
          <div className="h-56">
            <ResponsiveContainer>
              <BarChart data={rows} margin={{ top: 4, right: 0, left: 0, bottom: 0 }} barGap={2}>
                <XAxis dataKey="label" tick={AXIS} axisLine={false} tickLine={false} />
                <YAxis tickFormatter={(x: number) => usd(x)} tick={AXIS} axisLine={false} tickLine={false} width={56} />
                <Tooltip formatter={(x) => usd(Number(x))} cursor={{ fill: "#f4f4f5" }} />
                <ReferenceLine y={0} stroke="#e4e4e7" />
                <Bar dataKey="revenue" name={t("Revenue")} fill="#0284c7" radius={[4, 4, 0, 0]} isAnimationActive={false} />
                <Bar dataKey="netIncome" name={t("Net income")} fill="#7dd3fc" radius={[4, 4, 0, 0]} isAnimationActive={false} />
              </BarChart>
            </ResponsiveContainer>
          </div>
          <p className="mt-1 flex gap-4 text-xs text-zinc-500">
            <span><span className="mr-1 inline-block size-2 rounded-sm bg-sky-600" />{t("Revenue")}</span>
            <span><span className="mr-1 inline-block size-2 rounded-sm bg-sky-300" />{t("Net income")}</span>
          </p>
        </Block>
      )}

      {isPending(body, "segments") ? (
        <Waiting text={t("Reading the segment table…")} />
      ) : segs?.segments?.length || segs?.geography?.length ? (
        <Block title={t("Where revenue comes from · {year}", { year: segs.fiscalYear })}>
          {[segs.segments, segs.geography].map((list, i) =>
            list?.length ? (
              <div key={i} className={i ? "mt-4" : ""}>
                {i === 1 && <p className="mb-1 text-xs font-semibold text-zinc-400">{t("By geography")}</p>}
                <ul className="space-y-2">
                  {[...list].sort((a, b) => b.revenue - a.revenue).map((s) => (
                    <li key={s.name} className="grid grid-cols-[1fr_auto] items-center gap-x-3 text-sm">
                      <span className="truncate">{s.name}</span>
                      <span className="tabular-nums text-zinc-500">
                        {usd(s.revenue)} · {pct(s.share, 0)}
                        {s.yoy != null && <span className={`ml-2 font-semibold ${s.yoy >= 0 ? "text-sky-700" : "text-rose-600"}`}>{signedPct(s.yoy, 0)}</span>}
                      </span>
                      <span className="col-span-2 h-1.5 rounded-full bg-zinc-100">
                        <span className="block h-full rounded-full bg-sky-600" style={{ width: `${s.share * 100}%` }} />
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null,
          )}
        </Block>
      ) : null}

      {earnings.length ? (
        <Block title={t("EPS vs estimate · beat {beats} of {quarters}", { beats: m.earnings.beats, quarters: m.earnings.quarters })}>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[28rem] text-sm tabular-nums">
              <tbody>
                {[
                  { label: "Quarter", get: (q: (typeof earnings)[number]) => monthYear(q.fiscalDateEnding), cls: "text-zinc-400" },
                  { label: "Estimate", get: (q: (typeof earnings)[number]) => (q.estimatedEPS == null ? "–" : `$${q.estimatedEPS.toFixed(2)}`), cls: "text-zinc-500" },
                  { label: "Reported", get: (q: (typeof earnings)[number]) => (q.reportedEPS == null ? "–" : `$${q.reportedEPS.toFixed(2)}`), cls: "font-semibold" },
                ].map((row) => (
                  <tr key={row.label} className="border-t border-zinc-100 first:border-0">
                    <th className="py-1.5 pr-3 text-left text-xs font-semibold text-zinc-400">{t(row.label)}</th>
                    {earnings.map((q) => (
                      <td key={q.fiscalDateEnding} className={`text-right ${row.cls}`}>{row.get(q)}</td>
                    ))}
                  </tr>
                ))}
                <tr className="border-t border-zinc-100">
                  <th className="py-1.5 pr-3 text-left text-xs font-semibold text-zinc-400">{t("Surprise")}</th>
                  {earnings.map((q) => {
                    const beat = q.reportedEPS != null && q.estimatedEPS != null && q.reportedEPS > q.estimatedEPS;
                    return (
                      <td key={q.fiscalDateEnding} className={`text-right font-semibold ${beat ? "text-sky-700" : "text-rose-600"}`}>
                        {q.surprisePercent == null ? "–" : signedPct(q.surprisePercent / 100)}
                      </td>
                    );
                  })}
                </tr>
              </tbody>
            </table>
          </div>
        </Block>
      ) : null}
    </ScoreSection>
  );
}

function HealthSection({ m, body }: { m: MetricsReport; body?: ReportBody }) {
  const weight = body?.verdict?.components.find((c) => c.name === "Financial health")?.weight;
  return (
    <ScoreSection k="health" value={m.healthScore.value} feeds={feeds(weight)} reasons={m.healthScore.reasons}>
      <Block title={t("Balance sheet and coverage")}>
        {m.health.map((x) => (
          <MetricLine key={x.key} m={x} />
        ))}
      </Block>
    </ScoreSection>
  );
}

/** Cheap / typical / expensive as prices on one line, today marked. */
function PriceLine({ b }: { b: Bands }) {
  const min = Math.min(b.cheapPrice * 0.85, b.price * 0.92);
  const max = Math.max(b.expensivePrice * 1.15, b.price * 1.08);
  const at = (x: number) => ((x - min) / (max - min)) * 100;
  const now = at(b.price);
  return (
    <div>
      <div className="relative mt-8 h-2 rounded-full bg-zinc-100">
        <span className="absolute inset-y-0 left-0 rounded-l-full bg-emerald-400" style={{ width: `${at(b.cheapPrice)}%` }} />
        <span className="absolute inset-y-0 right-0 rounded-r-full bg-rose-400" style={{ left: `${at(b.expensivePrice)}%` }} />
        <span className="absolute inset-y-0 bg-yellow-300" style={{ left: `${at(b.cheapPrice)}%`, width: `${at(b.expensivePrice) - at(b.cheapPrice)}%` }} />
        <span className="absolute top-1/2 h-5 w-0.5 -translate-y-1/2 bg-yellow-600" style={{ left: `${at(b.medianPrice)}%` }} />
        <span className="absolute top-1/2 size-4 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white bg-zinc-900 shadow" style={{ left: `${now}%` }} />
        <span className="absolute -top-7 text-xs font-bold whitespace-nowrap" style={now < 15 ? { left: 0 } : now > 85 ? { right: 0 } : { left: `${now}%`, transform: "translateX(-50%)" }}>
          {t("Today {price}", { price: price(b.price) })}
        </span>
      </div>
      <div className="mt-4 grid grid-cols-3 gap-2 text-center text-xs">
        <div className="rounded-lg bg-emerald-50 p-2">
          <p className="text-emerald-700">{t("Cheap below")}</p>
          <p className="font-grotesk text-lg font-bold">{wholePrice(b.cheapPrice)}</p>
          <p className="text-zinc-400">{multiple(b.cheapBelow)}</p>
        </div>
        <div className="rounded-lg bg-yellow-50 p-2">
          <p className="text-yellow-700">{t("Typical")}</p>
          <p className="font-grotesk text-lg font-bold">{wholePrice(b.medianPrice)}</p>
          <p className="text-zinc-400">{multiple(b.median)}</p>
        </div>
        <div className="rounded-lg bg-rose-50 p-2">
          <p className="text-rose-700">{t("Expensive above")}</p>
          <p className="font-grotesk text-lg font-bold">{wholePrice(b.expensivePrice)}</p>
          <p className="text-zinc-400">{multiple(b.expensiveAbove)}</p>
        </div>
      </div>
      <p className="mt-2 text-xs text-zinc-500">
        {t("Each band edge × {basis} of {value} ({period}).", { basis: tx(b.perShareBasis), value: price(b.perShare), period: tx(b.basisPeriod) })}
      </p>
    </div>
  );
}

function ValuationSection({ m }: { m: MetricsReport }) {
  const b = m.valuationBands;
  const values = m.charts.valuation1y.map((p) => p.value);
  const lo = b ? Math.min(...values, b.cheapBelow) * 0.95 : 0;
  const hi = b ? Math.max(...values, b.expensiveAbove) * 1.05 : 0;
  return (
    <ScoreSection k="valuation" value={m.valuationScore.value} feeds={t("the V in the verdict rule")} reasons={m.valuationScore.reasons}>
      <Block title={t("Why {metric}", { metric: valuedWith(m) })}>
        <p className="text-sm">
          {t("Phase {n} of 5", { n: m.phase.number })}, <strong>{tx(m.phase.name)}</strong>: {tx(m.phase.reason)}
          {m.phase.lowConfidence && ` (${t("low confidence: a weak fit for any phase")})`}.
        </p>
        <ul className="mt-2 space-y-1 text-sm">
          {m.phase.checks.map((c) => (
            <li key={c.question} className="flex gap-2">
              <span className={c.answer == null ? "text-zinc-400" : c.answer ? "text-emerald-600" : "text-rose-600"}>{c.answer == null ? "?" : c.answer ? "✓" : "✕"}</span>
              <span>
                {tx(c.question)} <span className="text-zinc-500">{tx(c.detail)}</span>
              </span>
            </li>
          ))}
        </ul>
        {fallbackNote(m) && <p className="mt-2 rounded-lg bg-amber-50 p-2 text-sm text-amber-800">{fallbackNote(m)}</p>}
      </Block>

      {b ? (
        <>
          <Block title={t("{metric} over the last year", { metric: b.metric })}>
            <div className="h-56">
              <ResponsiveContainer>
                <LineChart data={m.charts.valuation1y} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
                  <ReferenceArea y1={lo} y2={b.cheapBelow} fill="#10b981" fillOpacity={0.1} ifOverflow="hidden" />
                  <ReferenceArea y1={b.cheapBelow} y2={b.expensiveAbove} fill="#facc15" fillOpacity={0.12} ifOverflow="hidden" />
                  <ReferenceArea y1={b.expensiveAbove} y2={hi} fill="#f43f5e" fillOpacity={0.1} ifOverflow="hidden" />
                  <ReferenceLine y={b.median} stroke="#ca8a04" strokeDasharray="4 4" />
                  <XAxis dataKey="date" tickFormatter={monthYear} tick={AXIS} axisLine={false} tickLine={false} minTickGap={50} />
                  <YAxis domain={[lo, hi]} tickFormatter={multipleTick} tick={AXIS} axisLine={false} tickLine={false} width={40} />
                  <Tooltip formatter={(x) => multiple(Number(x))} />
                  <Line dataKey="value" name={b.metric} stroke="#18181b" strokeWidth={2} dot={false} isAnimationActive={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
            <p className="text-sm text-zinc-500">
              {t("Today {x}: higher than {pct} of the last {days} trading days ({from}–{to}).", {
                x: multiple(b.current),
                pct: pct(b.percentile, 0),
                days: b.days.toLocaleString(),
                from: b.historyFrom.slice(0, 4),
                to: b.historyTo.slice(0, 4),
              })}
            </p>
          </Block>
          <Block title={t("What price is cheap?")}>
            <PriceLine b={b} />
          </Block>
        </>
      ) : (
        <p className="text-sm text-zinc-500">{t("Not enough history to compare today's valuation with the past.")}</p>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <Block title={t("Multiples")}>
          {[m.marketCap, ...m.valuation].map((x) => (
            <MetricLine key={x.key} m={x} />
          ))}
        </Block>
        <Block title={t("Returned to shareholders")}>
          {m.returns.map((x) => (
            <MetricLine key={x.key} m={x} />
          ))}
        </Block>
      </div>

    </ScoreSection>
  );
}

function PriceSection({ m }: { m: MetricsReport }) {
  const data = m.charts.price.filter((_, i, all) => i % 5 === 0 || i === all.length - 1);
  if (data.length < 2) return null;
  const change = m.priceChange5y.value ?? 0;
  const color = change >= 0 ? "#059669" : "#e11d48";
  // One tick per year: the first trading day shown in it.
  return (
    <section className="mb-10">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="font-grotesk text-sm font-bold tracking-widest text-zinc-400 uppercase">{t("Share price · 5 years")}</h2>
        <p className="text-sm text-zinc-500">
          <span className="font-grotesk text-2xl font-bold text-zinc-900 tabular-nums">{price(m.price)}</span>{" "}
          <span className={`font-semibold ${change >= 0 ? "text-emerald-600" : "text-rose-600"}`}>{signedPct(m.priceChange5y.value, 0)}</span> · {t("as of {date}", { date: shortDate(m.priceDate) })}
        </p>
      </div>
      <div className="mt-3 h-56 rounded-2xl border border-zinc-200 bg-white p-3 sm:h-64">
        <ResponsiveContainer>
          <AreaChart data={data} margin={{ top: 4, right: 0, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id="evPrice" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={color} stopOpacity={0.18} />
                <stop offset="100%" stopColor={color} stopOpacity={0} />
              </linearGradient>
            </defs>
            <XAxis dataKey="date" ticks={yearTicks(data)} tickFormatter={(d: string) => d.slice(0, 4)} tick={AXIS} axisLine={false} tickLine={false} />
            <YAxis domain={["auto", "auto"]} tickFormatter={(x: number) => `$${Math.round(x)}`} tick={AXIS} axisLine={false} tickLine={false} width={48} />
            <Tooltip formatter={(x) => price(Number(x))} />
            <Area dataKey="value" name={t("Price")} stroke={color} strokeWidth={2} fill="url(#evPrice)" isAnimationActive={false} />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </section>
  );
}

// --- Page ----------------------------------------------------------------------------------

export function Evidence({ id }: { id: string }) {
  const { data: report, error, isLoading } = useLiveReport(id);
  const body = report?.report;
  const m = body?.metrics;
  const name = titleCase(body?.company.name || report?.companyName || "");
  const running = report ? !isFinished(report.status) : false;
  const stage = report ? Math.max(STAGES.findIndex((s) => s.id === report.stage), 0) : 0;
  const active = useActiveSection(Boolean(report && report.status !== "failed"));
  const scores = scoresOf(m, body);

  return (
    <div className="min-h-screen bg-zinc-50 font-sans text-zinc-900">
      {isLoading ? (
        <p className="p-10 text-zinc-500">{t("Loading…")}</p>
      ) : error || !report ? (
        <p className="p-10">{t("Report not found.")}</p>
      ) : (
        <div className="mx-auto max-w-7xl lg:grid lg:grid-cols-[22rem_1fr]">
          {/* On phones the controls sit above the pinned panel. */}
          <div className="flex flex-wrap items-center justify-end gap-2 bg-white px-4 pt-4 lg:hidden">
            <ReportControls report={report} look="evidence" />
          </div>
          <aside className="border-b border-zinc-200 bg-white px-4 py-5 lg:sticky lg:top-0 lg:h-screen lg:overflow-y-auto lg:border-r lg:border-b-0 lg:px-6">
            <div className="mb-5 flex items-center justify-between gap-3">
              <button onClick={() => navigate("/")} className="text-sm font-semibold text-zinc-500 hover:text-zinc-900">
                ← {t("New search")}
              </button>
            </div>
            <Pinned ticker={report.ticker} name={name} m={m} body={body} active={active} running={running} stage={stage} />
          </aside>

          {/* On phones the scores become a sticky strip of shortcuts. */}
          <nav aria-label={t("Jump to a score")} className="sticky top-0 z-20 flex gap-2 overflow-x-auto border-b border-zinc-200 bg-white/95 px-4 py-2 backdrop-blur [scrollbar-width:none] lg:hidden">
            {ORDER.map((k) => (
              <a key={k} href={`#ev-${k}`} className={`flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-semibold ${active === k ? `${SCORES[k].bg} text-white` : "bg-zinc-100 text-zinc-600"}`}>
                {t(SCORES[k].short)}
                <span className="tabular-nums">{scores[k] == null ? "–" : scores[k]!.toFixed(1)}</span>
              </a>
            ))}
          </nav>

          <main className="min-w-0 px-4 py-6 lg:px-10 lg:py-8">
            <div className="mb-6 hidden flex-wrap items-center justify-end gap-2 lg:flex">
              <ReportControls report={report} look="evidence" />
            </div>
            <LangNotice report={report} className="mb-6 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900" />
            <LimitedNotice report={report} className="mb-6 rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900" />
            {report.status === "failed" ? (
              <p className="rounded-2xl bg-rose-50 p-4 text-rose-800">{t("This report could not be built:")} {tx(report.error)}</p>
            ) : (
              <>
                {m && <PriceSection m={m} />}
                <CompanySection body={body} />
                <QualitySection m={m} body={body} />
                {m ? (
                  <>
                    <GrowthSection m={m} body={body} />
                    <HealthSection m={m} body={body} />
                    <ValuationSection m={m} />
                  </>
                ) : (
                  <div className="pt-12">
                    <Waiting text={t("Computing metrics from SEC filings…")} />
                  </div>
                )}
                <MoreSections
                  body={body}
                  m={m}
                  report={report}
                  look="look-light"
                  idPrefix="ev-"
                  sectionClass="scroll-mt-20 space-y-4 pt-12 lg:scroll-mt-6"
                  titleClass="font-grotesk text-sm font-bold tracking-widest text-zinc-400 uppercase"
                />
              </>
            )}
            <footer className="mt-16 border-t border-zinc-200 pt-6 text-xs text-zinc-500">
              {m && (
                <p>
                  {t("Financials through {date}", { date: shortDate(m.financialsThrough) })}
                  {body?.filing && <> · {t("latest 10-K for the period ended {date}", { date: shortDate(body.filing.reportDate) })}</>} · {t("prices as of {date}", { date: shortDate(m.priceDate) })} · {t("scoring {version}", { version: m.scoringVersion })}
                  {report.status === "partial" && <> · {t("some sections are missing ({list})", { list: report.failedSections.join(", ") })}</>}
                </p>
              )}
              <div className="mt-3">
                <RunDetails report={report} />
              </div>
              <p className="mt-3 font-semibold text-zinc-700">{t("Educational project. Not financial advice.")}</p>
            </footer>
          </main>
        </div>
      )}
    </div>
  );
}
