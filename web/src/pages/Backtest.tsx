// The backtest: the verdict (without the AI) as of past dates, on data filed
// by then, against how each stock did over the next one to three years.

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { cancelBacktest, getLatestBacktest, startBacktest, type BacktestBucket, type BacktestSignal, type BacktestState, type BacktestSummary } from "../api";
import { LangSwitch, ModelSwitch } from "../components/Controls";
import { Disclaimer } from "../components/ui";
import { pct, shortDate, signedPct } from "../format";
import { t, tx } from "../i18n";
import { navigate } from "../router";

const HORIZON = ["1 year", "2 years", "3 years"];

// The price feed's free tier: 50 requests an hour.
const SECONDS_PER_COMPANY = 72;

const STATUS_LABEL: Record<BacktestState["backtest"]["status"], string> = {
  running: "Running",
  done: "Finished",
  cancelled: "Cancelled",
  failed: "Failed",
};
const UNIVERSE_LABEL: Record<string, string> = { sp500: "S&P 500 sample", watchlist: "Watchlist", custom: "Custom list" };
const INPUT_LABEL: Record<BacktestSignal["input"], string> = {
  business: "Business score (no AI)",
  growth: "Growth score",
  health: "Financial health or strength",
  valuation: "Valuation score V",
  cheapness: "Cheapness against its own history",
};
const SIGNAL_STYLE: Record<string, string> = {
  helps: "bg-good-soft text-good-strong",
  hurts: "bg-bad-soft text-bad",
  "no clear signal": "bg-line text-muted",
  "too little data": "bg-line text-muted",
};
const LABEL_STYLE: Record<string, string> = {
  Buy: "bg-good text-white",
  Watch: "bg-good-soft text-good-strong",
  Hold: "bg-mid text-[#1a1404]",
  Pass: "bg-bad-soft text-bad",
  Sell: "bg-bad text-white",
};

export function Backtest() {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["backtest"],
    queryFn: getLatestBacktest,
    refetchInterval: (s) => (s.state.data?.running ? 10_000 : false),
  });
  const reload = () => void qc.invalidateQueries({ queryKey: ["backtest"] });
  const state = q.data ?? null;
  const [h, setH] = useState(0); // horizon index: 1, 2 or 3 years

  return (
    <div className="mx-auto min-h-screen max-w-6xl px-4">
      <header className="flex flex-wrap items-center justify-between gap-2 pt-4">
        <button onClick={() => navigate("/")} className="text-sm font-semibold text-muted hover:text-ink">
          ← {t("New search")}
        </button>
        <div className="flex flex-wrap items-center gap-2">
          <ModelSwitch look="classic" />
          <LangSwitch look="classic" />
        </div>
      </header>

      <div className="max-w-3xl pt-8">
        <h1 className="section-title">{t("Backtest")}</h1>
        <p className="mt-3 text-muted">
          {t(
            "How would the verdict have done? It is run as of past dates, on only what was filed and priced by then, and each stock is followed for the next one to three years against the S&P 500 (SPY, dividends included).",
          )}
        </p>
      </div>

      <Caveats />

      <div className="mt-6 grid gap-4 lg:grid-cols-[22rem_1fr]">
        <StartPanel running={state?.running ?? false} onStarted={reload} />
        {state ? <ProgressPanel state={state} onChange={reload} /> : !q.isLoading && <p className="card text-sm text-muted">{t("No backtest yet.")}</p>}
      </div>

      {state?.summary && (
        <Results s={state.summary} state={state} h={h} setH={setH} />
      )}

      <div className="mt-10">
        <Disclaimer />
      </div>
    </div>
  );
}

function Caveats() {
  return (
    <details className="card mt-6 text-sm" open>
      <summary className="cursor-pointer font-display font-semibold">{t("What this can and cannot tell you")}</summary>
      <ul className="mt-3 list-disc space-y-1.5 pl-5 text-muted">
        <li>{t("Filings as filed: each date sees only the SEC facts filed by then, so later restatements and quarters are left out. Prices stop at the date too.")}</li>
        <li>{t("Without the AI: business quality, moat and risk cannot be judged for past dates, because the model knows what happened since. The verdict here comes from growth, financial health and the valuation score, and a Buy does not need the quality score.")}</li>
        <li>{t("Survivorship: the companies are today's S&P 500 members, which did well enough to stay in or join the index. All of them beat the benchmark on average, so compare each group with “All stocks”, not with zero.")}</li>
        <li>{t("Dates are six months apart, so the two- and three-year periods overlap and the statistics for them look more certain than they are. Trading costs and taxes are left out. Past results do not promise future ones.")}</li>
      </ul>
    </details>
  );
}

function StartPanel({ running, onStarted }: { running: boolean; onStarted: () => void }) {
  const [universe, setUniverse] = useState<"sp500" | "watchlist" | "custom">("sp500");
  const [size, setSize] = useState(100);
  const [tickers, setTickers] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const start = async () => {
    setBusy(true);
    setError(null);
    try {
      await startBacktest({ universe, sampleSize: universe === "sp500" ? size : undefined, tickers: universe === "custom" ? tickers : undefined });
      onStarted();
    } catch (e) {
      setError(e instanceof Error ? tx(e.message) : String(e));
    } finally {
      setBusy(false);
    }
  };
  const hours = (size * SECONDS_PER_COMPANY) / 3600;
  return (
    <div className="card space-y-4">
      <h2 className="font-display text-lg font-bold">{t("New backtest")}</h2>
      <fieldset className="space-y-2" disabled={running}>
        <legend className="mb-1 text-sm font-semibold">{t("Companies")}</legend>
        {(["sp500", "watchlist", "custom"] as const).map((u) => (
          <label key={u} className="flex items-center gap-2 text-sm">
            <input type="radio" name="bt-universe" checked={universe === u} onChange={() => setUniverse(u)} className="accent-[var(--color-good)]" />
            {t(UNIVERSE_LABEL[u]!)}
            {u === "sp500" && universe === "sp500" && (
              <select value={size} onChange={(e) => setSize(Number(e.target.value))} aria-label={t("Sample size")} className="ml-auto rounded-lg border border-line bg-paper px-2 py-1">
                {[50, 100, 200].map((n) => (
                  <option key={n} value={n}>
                    {t("{n} companies", { n })}
                  </option>
                ))}
              </select>
            )}
          </label>
        ))}
        {universe === "custom" && (
          <textarea
            value={tickers}
            onChange={(e) => setTickers(e.target.value)}
            rows={3}
            placeholder="AAPL, MSFT, KO, JPM"
            aria-label={t("Tickers")}
            className="w-full rounded-lg border border-line bg-paper p-2 text-sm outline-none focus:border-good"
          />
        )}
      </fieldset>
      <p className="text-xs leading-relaxed text-muted">
        {t(
          "Each company needs about ten years of prices: one request to Tiingo's free tier (50 an hour, 500 different symbols a month). The first run takes up to {hours} hours; price history is kept for a month, so runs after that are quick.",
          { hours: hours < 1 ? hours.toFixed(1) : Math.ceil(hours) },
        )}
      </p>
      {error && <p className="text-sm text-bad" role="alert">{error}</p>}
      <button onClick={() => void start()} disabled={busy || running || (universe === "custom" && !tickers.trim())} className="w-full rounded-full bg-good px-4 py-2 font-semibold text-white disabled:opacity-40">
        {running ? t("A backtest is running") : busy ? t("Starting…") : t("Start backtest")}
      </button>
    </div>
  );
}

function ProgressPanel({ state, onChange }: { state: BacktestState; onChange: () => void }) {
  const { backtest: bt, progress: p, running } = state;
  const [busy, setBusy] = useState(false);
  const left = (p.total - p.done) * SECONDS_PER_COMPANY;
  const leftText = left >= 3600 ? t("about {h} h {m} min", { h: Math.floor(left / 3600), m: Math.round((left % 3600) / 60) }) : t("about {m} min", { m: Math.max(1, Math.round(left / 60)) });
  const pctDone = p.total ? Math.min(100, (p.done / p.total) * 100) : 0;
  const dates = bt.params.dates;
  return (
    <div className="card space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="font-display text-lg font-bold">
          {t(UNIVERSE_LABEL[bt.universe] ?? bt.universe)} · <span className={running ? "text-good" : ""}>{t(STATUS_LABEL[bt.status])}</span>
        </h2>
        <span className="text-xs text-muted">{t("Started {date}", { date: new Date(bt.createdAt).toLocaleString() })}</span>
      </div>
      <div className="space-y-1">
        <p className="flex justify-between text-sm">
          <span className="font-semibold">{t("Companies tested")}</span>
          <span className="tabular-nums text-muted">
            {p.done} / {p.total}
          </span>
        </p>
        <div className="h-2 overflow-hidden rounded-full bg-line" role="progressbar" aria-valuenow={p.done} aria-valuemax={p.total} aria-label={t("Companies tested")}>
          <div className="h-full rounded-full bg-good transition-[width]" style={{ width: `${pctDone}%` }} />
        </div>
        <p className="text-xs text-muted">
          {[
            t("{n} observations", { n: p.observations }),
            p.failed ? t("{n} could not be read", { n: p.failed }) : "",
            running && p.done < p.total ? t("{left} left at most", { left: leftText }) : "",
          ]
            .filter(Boolean)
            .join(" · ")}
        </p>
      </div>
      {dates.length > 0 && (
        <p className="text-sm text-muted">
          {t("{n} dates, every six months from {from} to {to}; scoring {version}.", { n: dates.length, from: shortDate(dates[0]!), to: shortDate(dates[dates.length - 1]!), version: bt.params.scoringVersion })}
        </p>
      )}
      {bt.error && <p className="text-sm text-bad">{tx(bt.error)}</p>}
      {running && (
        <button
          onClick={() => {
            setBusy(true);
            void cancelBacktest(bt.id).finally(() => {
              setBusy(false);
              onChange();
            });
          }}
          disabled={busy}
          className="rounded-full border border-line px-4 py-1.5 text-sm text-muted hover:text-bad"
        >
          {t("Cancel backtest")}
        </button>
      )}
      {running && <p className="text-xs text-muted">{t("Results fill in as companies finish. It keeps running if you leave this page, and resumes after a restart.")}</p>}
    </div>
  );
}

function Results({ s, state, h, setH }: { s: BacktestSummary; state: BacktestState; h: number; setH: (h: number) => void }) {
  const all = s.all;
  return (
    <section className="mt-8 space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-display text-2xl font-bold">{t("Results")}</h2>
        <div className="flex gap-1 rounded-full border border-line bg-card p-1 text-sm" role="group" aria-label={t("Holding period")}>
          {[0, 1, 2].map((i) => (
            <button key={i} onClick={() => setH(i)} aria-pressed={h === i} className={`rounded-full px-3 py-1 font-semibold ${h === i ? "bg-good text-white" : "text-muted hover:text-ink"}`}>
              {t(HORIZON[i]!)}
            </button>
          ))}
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-4">
        <Stat label={t("Observations")} value={String(all.count[h])} note={t("{c} companies × up to {d} dates", { c: s.companies, d: s.dates.length })} />
        <Stat label={t("All stocks, average")} value={signedPct(all.meanReturn[h])} note={t("total return over {period}", { period: t(HORIZON[h]!) })} />
        <Stat label={t("All stocks vs SPY")} value={signedPct(all.meanExcess[h])} note={t("the baseline for every group below")} />
        <Stat label={t("Beat SPY")} value={pct(all.hitRate[h], 0)} note={t("share of observations")} />
      </div>

      <Group title={t("By verdict (without the AI)")} hint={t("A verdict that works shows Buy above Hold above Pass and Sell, each compared with all stocks.")} rows={s.byLabel} all={all} h={h} label={(k) => <span className={`rounded-full px-2.5 py-0.5 text-xs font-bold ${LABEL_STYLE[k] ?? ""}`}>{t(k)}</span>} />

      <Signals signals={s.signals} h={h} />

      <div className="grid gap-6 xl:grid-cols-2">
        <Group title={t("By valuation score V")} rows={s.byValuation} all={all} h={h} label={(k) => `V ${k}`} />
        <Group title={t("By business score (no AI)")} rows={s.byBusiness} all={all} h={h} label={(k) => k} />
        <Group title={t("By where the price sat in its own history")} rows={s.byZone} all={all} h={h} label={(k) => t({ cheap: "Cheap", fair: "Typical", expensive: "Expensive" }[k] ?? k)} />
        <Group title={t("By growth score")} rows={s.byGrowth} all={all} h={h} label={(k) => k} />
        <Group title={t("By financial health or strength")} rows={s.byHealth} all={all} h={h} label={(k) => k} />
      </div>

      <p className="flex flex-wrap gap-4 text-sm">
        <a href={`/api/backtests/${state.backtest.id}/observations.csv`} className="font-semibold text-good underline" download>
          {t("Download every observation (CSV)")}
        </a>
      </p>
      {state.failures.length > 0 && (
        <details className="text-sm text-muted">
          <summary className="cursor-pointer">{t("{n} companies could not be tested", { n: state.failures.length })}</summary>
          <ul className="mt-2 space-y-1">
            {state.failures.map((f) => (
              <li key={f.ticker}>
                <span className="font-semibold text-ink">{f.ticker}</span>: {tx(f.error)}
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}

function Stat({ label, value, note }: { label: string; value: string; note: string }) {
  return (
    <div className="card !p-4">
      <p className="text-xs text-muted">{label}</p>
      <p className="font-display text-2xl font-bold tabular-nums">{value}</p>
      <p className="text-xs text-muted">{note}</p>
    </div>
  );
}

/** One grouping, with a bar for each group's return against all stocks. */
function Group({ title, hint, rows, all, h, label }: { title: string; hint?: string; rows: BacktestBucket[]; all: BacktestBucket; h: number; label: (k: string) => ReactNode }) {
  const rel = (b: BacktestBucket) => (b.meanExcess[h] != null && all.meanExcess[h] != null ? b.meanExcess[h]! - all.meanExcess[h]! : null);
  const scale = Math.max(0.05, ...rows.map((b) => Math.abs(rel(b) ?? 0)));
  return (
    <div className="card space-y-3 overflow-x-auto">
      <div>
        <h3 className="font-display text-lg font-bold">{title}</h3>
        {hint && <p className="text-xs text-muted">{hint}</p>}
      </div>
      <table className="w-full min-w-[34rem] text-sm">
        <thead>
          <tr className="text-left text-xs text-muted">
            <th className="py-1 pr-2 font-semibold">{t("Group")}</th>
            <th className="py-1 pr-2 text-right font-semibold">{t("Count")}</th>
            <th className="py-1 pr-2 text-right font-semibold">{t("vs SPY")}</th>
            <th className="py-1 pr-2 text-right font-semibold">{t("Median vs SPY")}</th>
            <th className="py-1 pr-2 text-right font-semibold">{t("Beat SPY")}</th>
            <th className="w-40 py-1 font-semibold">{t("vs all stocks")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((b) => {
            const r = rel(b);
            const w = r == null ? 0 : (Math.abs(r) / scale) * 50;
            return (
              <tr key={b.key} className="border-t border-line/60">
                <td className="py-1.5 pr-2">{label(b.key)}</td>
                <td className="py-1.5 pr-2 text-right tabular-nums text-muted">{b.count[h]}</td>
                <td className="py-1.5 pr-2 text-right font-semibold tabular-nums">{signedPct(b.meanExcess[h])}</td>
                <td className="py-1.5 pr-2 text-right tabular-nums">{signedPct(b.medianExcess[h])}</td>
                <td className="py-1.5 pr-2 text-right tabular-nums">{pct(b.hitRate[h], 0)}</td>
                <td className="py-1.5">
                  <div className="relative h-4 rounded bg-line/40" title={r == null ? "" : signedPct(r)}>
                    <div className="absolute inset-y-0 left-1/2 w-px bg-muted/60" />
                    {r != null && <div className={`absolute inset-y-0.5 rounded ${r >= 0 ? "bg-good" : "bg-bad"}`} style={r >= 0 ? { left: "50%", width: `${w}%` } : { right: "50%", width: `${w}%` }} />}
                  </div>
                  <span className="sr-only">{signedPct(r)}</span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {rows.some((b) => (b.count[h] ?? 0) < 30) && <p className="text-xs text-muted">{t("Groups with fewer than 30 observations are too small to read much into.")}</p>}
    </div>
  );
}

function Signals({ signals, h }: { signals: BacktestSignal[]; h: number }) {
  return (
    <div className="card space-y-3 overflow-x-auto">
      <div>
        <h3 className="font-display text-lg font-bold">{t("Which inputs carry signal")}</h3>
        <p className="text-xs text-muted">
          {t(
            "Rank correlation between each input and the return against SPY, measured on each date and averaged (0 is none, 1 a perfect ordering). Above 0.05 is meaningful for stock picking. “Helps” needs it to hold up across dates (t ≥ 2).",
          )}
        </p>
      </div>
      <table className="w-full min-w-[30rem] text-sm">
        <thead>
          <tr className="text-left text-xs text-muted">
            <th className="py-1 pr-2 font-semibold">{t("Input")}</th>
            <th className="py-1 pr-2 text-right font-semibold">{t("Correlation")}</th>
            <th className="py-1 pr-2 text-right font-semibold">t</th>
            <th className="py-1 pr-2 text-right font-semibold">{t("Dates")}</th>
            <th className="py-1 font-semibold">{t("Reading")}</th>
          </tr>
        </thead>
        <tbody>
          {signals.map((s) => (
            <tr key={s.input} className="border-t border-line/60">
              <td className="py-1.5 pr-2">{t(INPUT_LABEL[s.input])}</td>
              <td className="py-1.5 pr-2 text-right font-semibold tabular-nums">{s.ic[h] == null ? "–" : s.ic[h]!.toFixed(3)}</td>
              <td className="py-1.5 pr-2 text-right tabular-nums text-muted">{s.t[h] == null ? "–" : s.t[h]!.toFixed(1)}</td>
              <td className="py-1.5 pr-2 text-right tabular-nums text-muted">{s.dates[h]}</td>
              <td className="py-1.5">
                <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${SIGNAL_STYLE[s.verdict[h] ?? ""] ?? ""}`}>{t(s.verdict[h] ?? "")}</span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
