// Prototype design: the verdict as a chain of questions. Each step shows its
// answer and why, and opens to the numbers behind it.

import { MoreSections } from "../components/deepSections";
import type { ReactNode } from "react";
import { Line, LineChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { isFinished, type Metric, type MetricsReport, type ReportBody, type Verdict } from "../api";
import { LangNotice, LimitedNotice, ReportControls } from "../components/Controls";
import { RunDetails } from "../components/RunDetails";
import { metricValue, monthYear, multiple, multipleTick, pct, price, shortDate, titleCase, wholePrice } from "../format";
import { t, tx } from "../i18n";
import { QUALITY_QUESTIONS, SCORE_WORDS, STAGES, fallbackNote, metricOf, scoreWord, useLiveReport, valuedWith, withPrices, healthMetrics, sectorOf } from "../live";
import { navigate } from "../router";

type Outcome = "yes" | "partly" | "no" | "pending";

const outcomeOf = (v: number | null | undefined): Outcome => (v == null ? "pending" : v >= 3.5 ? "yes" : v >= 2.5 ? "partly" : "no");

const BADGE: Record<Outcome, { icon: string; circle: string; chip: string }> = {
  yes: { icon: "✓", circle: "bg-emerald-500 text-white", chip: "bg-emerald-50 text-emerald-700" },
  partly: { icon: "~", circle: "bg-amber-400 text-white", chip: "bg-amber-50 text-amber-700" },
  no: { icon: "✕", circle: "bg-rose-500 text-white", chip: "bg-rose-50 text-rose-700" },
  pending: { icon: "…", circle: "bg-slate-200 text-slate-500", chip: "bg-slate-100 text-slate-500" },
};

const valueOf = (x?: Metric) => (x ? metricValue(x) : "–");

const LABELS: Verdict["label"][] = ["Sell", "Pass", "Hold", "Watch", "Buy"];

/** One question in the chain: icon on the line, answer and reason, details on demand. */
function Step({ outcome, question, answer, because, children }: { outcome: Outcome; question: string; answer: string; because: ReactNode; children?: ReactNode }) {
  const b = BADGE[outcome];
  return (
    <li className="relative pb-6 pl-14">
      <span className="absolute inset-y-0 left-[19px] w-0.5 bg-slate-200 [li:first-child>&]:top-5" aria-hidden />
      <span className={`absolute top-3 left-0 z-10 flex size-10 items-center justify-center rounded-full text-lg font-bold ring-4 ring-slate-50 ${b.circle}`} aria-hidden>
        {b.icon}
      </span>
      <details className="group rounded-2xl border border-slate-200 bg-white shadow-sm open:shadow-md">
        <summary className="cursor-pointer list-none p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-lg font-bold">{question}</h2>
            <span className={`rounded-full px-3 py-1 text-sm font-bold ${b.chip}`}>{answer}</span>
          </div>
          <p className="mt-1 text-sm text-slate-500">{because}</p>
          {children && <p className="mt-2 text-xs font-semibold text-indigo-600 group-open:hidden">{t("Show the numbers")} ▾</p>}
        </summary>
        {children && <div className="border-t border-slate-100 p-4 text-sm">{children}</div>}
      </details>
    </li>
  );
}

/** A point where earlier answers combine into one number. */
function Merge({ children }: { children: ReactNode }) {
  return (
    <li className="relative pb-6 pl-14">
      <span className="absolute inset-y-0 left-[19px] w-0.5 bg-slate-200" aria-hidden />
      <span className="absolute top-2 left-[13px] z-10 size-3.5 rotate-45 bg-indigo-500 ring-4 ring-slate-50" aria-hidden />
      <div className="rounded-xl bg-indigo-50 px-4 py-3 text-sm text-indigo-900">{children}</div>
    </li>
  );
}

function MetricList({ items }: { items: (Metric | undefined)[] }) {
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-1">
      {items.filter((x): x is Metric => Boolean(x)).map((x) => (
        <div key={x.key} className="flex justify-between gap-2 border-b border-slate-100 py-1" title={tx(x.formula)}>
          <dt className="text-slate-500">{tx(x.label)}</dt>
          <dd className="font-semibold tabular-nums">{metricValue(x)}</dd>
        </div>
      ))}
    </dl>
  );
}

function Reasons({ items }: { items: string[] }) {
  if (!items.length) return null;
  return (
    <ul className="mt-3 list-disc space-y-0.5 pl-5 text-slate-500">
      {items.map((r) => (
        <li key={r}>{tx(r)}</li>
      ))}
    </ul>
  );
}

function BandsChart({ m }: { m: MetricsReport }) {
  const b = m.valuationBands!;
  const values = m.charts.valuation1y.map((p) => p.value);
  const lo = Math.min(...values, b.cheapBelow) * 0.95;
  const hi = Math.max(...values, b.expensiveAbove) * 1.05;
  return (
    <div className="h-44">
      <ResponsiveContainer>
        <LineChart data={withPrices(m.charts.valuation1y, m.charts.price)} margin={{ top: 4, right: 4, left: 0, bottom: 0 }}>
          <ReferenceArea y1={lo} y2={b.cheapBelow} fill="#10b981" fillOpacity={0.12} ifOverflow="hidden" />
          <ReferenceArea y1={b.cheapBelow} y2={b.expensiveAbove} fill="#facc15" fillOpacity={0.14} ifOverflow="hidden" />
          <ReferenceArea y1={b.expensiveAbove} y2={hi} fill="#f43f5e" fillOpacity={0.12} ifOverflow="hidden" />
          <ReferenceLine y={b.median} stroke="#ca8a04" strokeDasharray="4 4" />
          <XAxis dataKey="date" tickFormatter={monthYear} tick={{ fontSize: 11, fill: "#94a3b8" }} axisLine={false} tickLine={false} minTickGap={50} />
          <YAxis domain={[lo, hi]} tickFormatter={multipleTick} tick={{ fontSize: 11, fill: "#94a3b8" }} axisLine={false} tickLine={false} width={40} />
          {/* The share price, invisible on its own axis: shown in the tooltip only. */}
          <YAxis yAxisId="price" hide domain={["auto", "auto"]} />
          <Line yAxisId="price" dataKey="price" name={t("Price")} stroke="transparent" dot={false} activeDot={false} legendType="none" isAnimationActive={false} />
          <Tooltip formatter={(v, name) => (name === t("Price") ? price(Number(v)) : multiple(Number(v)))} />
          <Line dataKey="value" name={b.metric} stroke="#4f46e5" strokeWidth={2} dot={false} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

function Flow({ m, body }: { m?: MetricsReport; body?: ReportBody }) {
  const v = body?.verdict;
  const quality = body?.businessQuality;
  const answers = body?.agents?.business_quality?.output;
  const b = m?.valuationBands;
  const word = (o: Outcome, yes: string, partly: string, no: string) => t(o === "yes" ? yes : o === "partly" ? partly : o === "no" ? no : "Working…");

  const qOut = outcomeOf(quality?.score);
  const gOut = outcomeOf(m?.growthScore.value);
  const hOut = outcomeOf(m?.healthScore.value);
  const vOut = outcomeOf(m?.valuationScore.value);

  return (
    <ol className="relative mt-8">

      <Step
        outcome={qOut}
        question={t("Is it a good business?")}
        answer={`${word(qOut, "Yes", "Partly", "No")}${quality ? ` · ${quality.score.toFixed(1)}/5` : ""}`}
        because={answers ? QUALITY_QUESTIONS.map((x) => `${t(x.short)}: ${answers[x.key]?.answer ? t(answers[x.key]!.answer) : "–"}`).join(" · ") : t("AI analysts are reading the 10-K…")}
      >
        {answers && (
          <ul className="space-y-3">
            {QUALITY_QUESTIONS.map((x) => (
              <li key={x.key}>
                <p className="font-semibold">
                  {t(x.question)} <span className="text-indigo-600">{answers[x.key]?.answer ? t(answers[x.key]!.answer) : ""}</span>
                </p>
                <p className="text-slate-500">{answers[x.key]?.justification}</p>
              </li>
            ))}
          </ul>
        )}
      </Step>

      <Step
        outcome={gOut}
        question={t("Is it growing?")}
        answer={`${word(gOut, "Yes", "Somewhat", "No")}${m?.growthScore.value != null ? ` · ${m.growthScore.value.toFixed(1)}/5` : ""}`}
        because={m ? t("Revenue {rev} a year over 5 years, EPS {eps}.", { rev: pct(metricOf(m, "revenueCagr5y")?.value), eps: pct(metricOf(m, "epsCagr5y")?.value) }) : t("Computing…")}
      >
        {m && (
          <>
            <MetricList items={m.growth.filter((g) => !g.key.endsWith("1y"))} />
            <Reasons items={m.growthScore.reasons} />
          </>
        )}
      </Step>

      <Step
        outcome={hOut}
        question={t("Are its finances healthy?")}
        answer={`${word(hOut, "Yes", "Mostly", "No")}${m?.healthScore.value != null ? ` · ${m.healthScore.value.toFixed(1)}/5` : ""}`}
        because={m ? healthBecause(m) : t("Computing…")}
      >
        {m && (
          <>
            <MetricList items={healthMetrics(m)} />
            <Reasons items={m.healthScore.reasons} />
          </>
        )}
      </Step>

      <Merge>
        <p className="font-bold">
          {t("The business scores {score} / 5", { score: v?.business?.toFixed(1) ?? "…" })} · {scoreWord(SCORE_WORDS.quality, v?.business ?? null)}
        </p>
        {v && (
          <p className="mt-0.5">
            {v.components
              .filter((c) => c.weight > 0)
              .map((c) => `${tx(c.name)} ${c.score?.toFixed(1) ?? "–"} × ${Math.round(c.weight * 100)}%`)
              .join("  +  ")}
            {v.adjustments?.map((a) => `  ${a.points > 0 ? "+" : "−"}  ${tx(a.name)} ${Math.abs(a.points).toFixed(2)}`).join("")}
          </p>
        )}
      </Merge>

      <Step
        outcome={m ? "yes" : "pending"}
        question={t("How should it be valued?")}
        answer={m ? t("With {metric}", { metric: valuedWith(m) }) : t("Working…")}
        because={
          m
            ? `${t("It is in phase {n} of 5, {name}: {reason}.", { n: m.phase.number, name: tx(m.phase.name).toLowerCase(), reason: tx(m.phase.reason) })} ${fallbackNote(m)}`
            : t("Computing…")
        }
      >
        {m && (
          <ul className="space-y-1">
            {m.phase.checks.map((c) => (
              <li key={c.question} className="flex gap-2">
                <span className={c.answer == null ? "text-slate-400" : c.answer ? "text-emerald-600" : "text-rose-600"}>{c.answer == null ? "?" : c.answer ? "✓" : "✕"}</span>
                <span>
                  {tx(c.question)} <span className="text-slate-500">{tx(c.detail)}</span>
                </span>
              </li>
            ))}
          </ul>
        )}
      </Step>

      <Step
        outcome={vOut}
        question={t("Is the price attractive?")}
        answer={`${scoreWord(SCORE_WORDS.valuation, m?.valuationScore.value ?? null)}${m?.valuationScore.value != null ? ` · ${m.valuationScore.value}/5` : ""}`}
        because={
          b
            ? t("{metric} is {x} against a typical {typical}: higher than on {pct} of days since {year}.", {
                metric: b.metric,
                x: multiple(b.current),
                typical: multiple(b.median),
                pct: pct(b.percentile, 0),
                year: b.historyFrom.slice(0, 4),
              })
            : m
              ? t("Not enough price history to compare.")
              : t("Computing…")
        }
      >
        {m && b && (
          <>
            <BandsChart m={m} />
            <div className="mt-3 grid grid-cols-3 gap-2 text-center">
              {[
                { label: "Cheap below", value: b.cheapPrice, tone: "text-emerald-700 bg-emerald-50" },
                { label: "Typical", value: b.medianPrice, tone: "text-amber-700 bg-amber-50" },
                { label: "Expensive above", value: b.expensivePrice, tone: "text-rose-700 bg-rose-50" },
              ].map((x) => (
                <div key={x.label} className={`rounded-xl p-2 ${x.tone}`}>
                  <p className="text-xs font-semibold">{t(x.label)}</p>
                  <p className="text-xl font-bold tabular-nums">{wholePrice(x.value)}</p>
                </div>
              ))}
            </div>
            <p className="mt-2 text-center text-slate-500">{t("Today {price}", { price: price(b.price) })}</p>
          </>
        )}
      </Step>

      <li className="relative pl-14">
        <span className="absolute top-0 left-[19px] h-5 w-0.5 bg-slate-200" aria-hidden />
        <span className="absolute top-5 left-0 z-10 flex size-10 items-center justify-center rounded-full bg-indigo-600 text-lg text-white ring-4 ring-slate-50" aria-hidden>
          ★
        </span>
        <div className="rounded-2xl border-2 border-indigo-600 bg-white p-5 shadow-lg">
          {!v ? (
            <p className="text-slate-500">{t("The verdict comes last, once every score is in.")}</p>
          ) : (
            <>
              <p className="text-xs font-bold tracking-wider text-indigo-600 uppercase">{t("Verdict")}</p>
              <p className="text-5xl font-bold tracking-tight">{v.label ? t(v.label) : t("n/a")}</p>
              <p className="mt-1 text-lg font-semibold">{v.tagline || tx(v.meaning)}</p>
              <div className="mt-4 grid grid-cols-5 gap-1 text-center text-xs font-semibold">
                {LABELS.map((l) => (
                  <span key={l} className={`rounded-md py-1.5 ${l === v.label ? "bg-indigo-600 text-white" : "bg-slate-100 text-slate-400"}`}>{t(l)}</span>
                ))}
              </div>
              <p className="mt-4 rounded-lg bg-slate-50 p-3 text-sm">
                <span className="font-semibold">{t("Rule applied:")} </span>
                {tx(v.rule)}
              </p>
              {v.rationale && <p className="mt-3 text-sm leading-relaxed text-slate-600">{v.rationale}</p>}
              <p className="mt-3 text-xs text-slate-400">
                {t("Confidence {level}", { level: t(v.confidenceLevel) })} ({v.confidence}/100)
                {v.confidenceReasons.length ? `: ${v.confidenceReasons.map(tx).join("; ")}` : ""}
              </p>
            </>
          )}
        </div>
      </li>
    </ol>
  );
}

export function Decision({ id }: { id: string }) {
  const { data: report, error, isLoading } = useLiveReport(id);
  const body = report?.report;
  const m = body?.metrics;
  const name = titleCase(body?.company.name || report?.companyName || "");
  const stage = report ? Math.max(STAGES.findIndex((s) => s.id === report.stage), 0) : 0;

  return (
    <div className="min-h-screen bg-slate-50 font-flow text-slate-900">
      <div className="mx-auto max-w-2xl px-4 py-5 pb-16">
        <header className="flex flex-wrap items-center justify-between gap-3">
          <button onClick={() => navigate("/")} className="text-sm font-semibold text-indigo-600">
            ← {t("New search")}
          </button>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <ReportControls report={report} look="decision" />
          </div>
        </header>
        <LangNotice report={report} className="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900" />
        <LimitedNotice report={report} className="mt-4 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900" />

        {isLoading ? (
          <p className="pt-10 text-slate-500">{t("Loading…")}</p>
        ) : error || !report ? (
          <p className="pt-10">{t("Report not found.")}</p>
        ) : report.status === "failed" ? (
          <p className="pt-10">
            {t("This report could not be built:")} {tx(report.error)}
          </p>
        ) : (
          <>
            <div className="pt-8">
              <p className="text-sm font-semibold text-indigo-600">
                {report.ticker}
                {m?.price != null && <> · {price(m.price)}</>}
              </p>
              <h1 className="mt-1 text-3xl font-bold tracking-tight sm:text-4xl">{t("How we reached a verdict on {name}", { name })}</h1>
              <p className="mt-2 text-slate-500">{t("Five questions, answered from SEC filings and prices. Tap a step to see the numbers.")}</p>
              {!isFinished(report.status) && (
                <p className="mt-3 animate-pulse text-sm font-semibold text-indigo-600" aria-live="polite">
                  {t("Step {n} of {total}: {label}…", { n: stage + 1, total: STAGES.length, label: t(STAGES[stage]?.label ?? "") })}
                </p>
              )}
            </div>
            <Flow m={m} body={body} />
            <MoreSections body={body} m={m} report={report} look="look-light" idPrefix="dec-" sectionClass="mt-10 space-y-4" titleClass="text-2xl font-bold tracking-tight" />
            <div className="mt-8 text-slate-500">
              <RunDetails report={report} />
            </div>
            <p className="mt-4 text-center text-xs text-slate-400">
              {m && <>{t("Financials through {date}", { date: shortDate(m.financialsThrough) })} · </>}
              {t("Educational only, not financial advice.")}
            </p>
          </>
        )}
      </div>
    </div>
  );
}

/** One line on the balance sheet, in the terms that fit the company. */
function healthBecause(m: MetricsReport): string {
  const kind = sectorOf(m)?.kind;
  const sec = (key: string) => valueOf(m.sector?.metrics.find((x) => x.key === key));
  if (kind === "reit") return t("Debt is {d} of assets; FFO covers interest {cov}.", { d: sec("debtToAssets"), cov: sec("ffoInterestCoverage") });
  if (kind) return t("Return on equity {roe}; equity is {eq} of assets.", { roe: sec("roe"), eq: sec("equityToAssets") });
  return t("Debt to equity {de}; operating profit covers interest {cov}.", { de: valueOf(metricOf(m, "debtToEquity")), cov: valueOf(metricOf(m, "interestCoverage")) });
}
