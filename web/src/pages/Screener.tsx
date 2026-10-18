// The screener: scans a list of companies (the S&P 500, the watchlist, any
// tickers) without the AI, to find candidates worth a full report.

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { addToWatchlist, cancelScreen, createReport, getLatestScreen, startScreen, type ScreenResult, type ScreenState } from "../api";
import { LangSwitch, ModelSwitch } from "../components/Controls";
import { Disclaimer } from "../components/ui";
import { ZoneBar } from "../components/ZoneBar";
import { price, titleCase, usd } from "../format";
import { t, tx } from "../i18n";
import { EXPECTATION_LABEL, SCORE_WORDS, scoreWord } from "../live";
import { navigate } from "../router";

// The price feed's free tier: 50 requests an hour.
const SECONDS_PER_PRICE = 72;
const MAX_BUILD = 5;

const PRESETS = [
  { id: "strongCheap", label: "Strong business, cheap", minB: 3.5, minV: 4 },
  { id: "strong", label: "Strong business", minB: 3.5, minV: 0 },
  { id: "cheap", label: "Cheap against its history", minB: 0, minV: 4 },
  { id: "all", label: "All", minB: 0, minV: 0 },
] as const;
type PresetId = (typeof PRESETS)[number]["id"];

const STATUS_LABEL: Record<ScreenState["screen"]["status"], string> = {
  scanning: "Reading filings",
  pricing: "Looking up prices",
  done: "Finished",
  cancelled: "Cancelled",
  failed: "Failed",
};
const UNIVERSE_LABEL: Record<string, string> = { sp500: "S&P 500", watchlist: "Watchlist", custom: "Custom list" };
const KIND_LABEL: Record<string, string> = { bank: "Bank", broker: "Broker-dealer", insurer: "Insurer", reit: "REIT" };
const ZONE_STYLE = { cheap: "text-good", fair: "text-mid", expensive: "text-bad" };

export function Screener() {
  const qc = useQueryClient();
  const screen = useQuery({
    queryKey: ["screen"],
    queryFn: getLatestScreen,
    refetchInterval: (q) => (q.state.data?.running ? 5000 : false),
  });
  const reload = () => void qc.invalidateQueries({ queryKey: ["screen"] });
  const state = screen.data ?? null;

  const [preset, setPreset] = useState<PresetId>("strongCheap");
  const [sector, setSector] = useState("");
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [building, setBuilding] = useState(false);
  const [built, setBuilt] = useState<{ ticker: string; id: string }[]>([]);
  const [notice, setNotice] = useState<string | null>(null);

  const rows = useMemo(() => {
    const p = PRESETS.find((x) => x.id === preset)!;
    const q = query.trim().toUpperCase();
    return (state?.results ?? []).filter(
      (r) =>
        (p.minB === 0 || (r.business ?? 0) >= p.minB) &&
        (p.minV === 0 || (r.valuation ?? 0) >= p.minV) &&
        (!sector || r.sector === sector) &&
        (!q || r.ticker.includes(q) || r.name.toUpperCase().includes(q)),
    );
  }, [state, preset, sector, query]);
  const sectors = useMemo(() => [...new Set((state?.results ?? []).map((r) => r.sector).filter(Boolean) as string[])].sort(), [state]);

  const toggle = (tk: string) => setPicked((l) => (l.includes(tk) ? l.filter((x) => x !== tk) : l.length >= MAX_BUILD ? l : [...l, tk]));

  // Full reports for the chosen candidates, followed on the watchlist.
  const build = async () => {
    setBuilding(true);
    setNotice(null);
    const out: { ticker: string; id: string }[] = [];
    try {
      for (const tk of picked) {
        await addToWatchlist(tk).catch(() => undefined);
        const r = await createReport(tk);
        out.push({ ticker: tk, id: r.id });
      }
      setPicked([]);
    } catch (e) {
      setNotice(e instanceof Error ? tx(e.message) : String(e));
    } finally {
      setBuilt(out);
      setBuilding(false);
    }
  };

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
        <h1 className="section-title">{t("Screener")}</h1>
        <p className="mt-3 text-muted">
          {t(
            "Scans a list of companies without the AI: growth and financial health from SEC filings for every company, then prices for the strongest businesses. Build full reports only for the candidates.",
          )}
        </p>
      </div>

      <div className="mt-6 grid gap-4 lg:grid-cols-[22rem_1fr]">
        <StartPanel state={state} onStarted={reload} />
        {state ? <ProgressPanel state={state} onChange={reload} /> : !screen.isLoading && <EmptyPanel />}
      </div>

      {state && state.results.length > 0 && (
        <section className="mt-8 space-y-4">
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex flex-wrap gap-1 rounded-full border border-line bg-card p-1 text-sm" role="group" aria-label={t("Show")}>
              {PRESETS.map((p) => (
                <button
                  key={p.id}
                  onClick={() => setPreset(p.id)}
                  aria-pressed={preset === p.id}
                  className={`rounded-full px-3 py-1 font-semibold ${preset === p.id ? "bg-good text-white" : "text-muted hover:text-ink"}`}
                >
                  {t(p.label)}
                </button>
              ))}
            </div>
            {sectors.length > 1 && (
              <select value={sector} onChange={(e) => setSector(e.target.value)} aria-label={t("Sector")} className="rounded-full border border-line bg-card px-3 py-1.5 text-sm">
                <option value="">{t("Every sector")}</option>
                {sectors.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            )}
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("Find a company")}
              aria-label={t("Find a company")}
              className="w-40 rounded-full border border-line bg-card px-3 py-1.5 text-sm outline-none focus:border-good"
            />
            <span className="ml-auto flex flex-wrap items-center gap-2">
              <span className="text-sm text-muted">{t("{n} of {total} companies", { n: rows.length, total: state.results.length })}</span>
              <button
                onClick={() => void build()}
                disabled={picked.length === 0 || building}
                className="rounded-full bg-good px-4 py-2 text-sm font-semibold text-white disabled:opacity-40"
                title={t("Builds a full report, with the AI, for each company picked, and adds it to the watchlist")}
              >
                {building ? t("Starting…") : t("Build full reports ({n})", { n: picked.length })}
              </button>
            </span>
          </div>
          {picked.length >= MAX_BUILD && <p className="text-xs text-muted">{t("Five at a time keeps the AI quota safe.")}</p>}
          {notice && <p className="card text-sm" role="status">{notice}</p>}
          {built.length > 0 && (
            <p className="card text-sm" role="status">
              {t("Building full reports, also added to the watchlist:")}{" "}
              {built.map((b, i) => (
                <span key={b.id}>
                  {i > 0 && ", "}
                  <a href={`/reports/${b.id}`} onClick={(e) => (e.preventDefault(), navigate(`/reports/${b.id}`))} className="font-semibold text-good underline">
                    {b.ticker}
                  </a>
                </span>
              ))}
            </p>
          )}
          <ResultsTable rows={rows} picked={picked} onPick={toggle} canPick={picked.length < MAX_BUILD} minBusiness={state.screen.params.minBusiness} maxPriced={state.screen.params.maxPriced} />
        </section>
      )}

      <div className="mt-10">
        <Disclaimer />
      </div>
    </div>
  );
}

function StartPanel({ state, onStarted }: { state: ScreenState | null; onStarted: () => void }) {
  const [universe, setUniverse] = useState<"sp500" | "watchlist" | "custom">("sp500");
  const [tickers, setTickers] = useState("");
  const [minBusiness, setMinBusiness] = useState(3.5);
  const [maxPriced, setMaxPriced] = useState(150);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const running = state?.running ?? false;

  const start = async () => {
    setBusy(true);
    setError(null);
    try {
      await startScreen({ universe, tickers: universe === "custom" ? tickers : undefined, minBusiness, maxPriced });
      onStarted();
    } catch (e) {
      setError(e instanceof Error ? tx(e.message) : String(e));
    } finally {
      setBusy(false);
    }
  };

  const hours = (maxPriced * SECONDS_PER_PRICE) / 3600;
  return (
    <div className="card space-y-4">
      <h2 className="font-display text-lg font-bold">{t("New screen")}</h2>
      <fieldset className="space-y-2" disabled={running}>
        <legend className="mb-1 text-sm font-semibold">{t("Companies")}</legend>
        {(["sp500", "watchlist", "custom"] as const).map((u) => (
          <label key={u} className="flex items-center gap-2 text-sm">
            <input type="radio" name="universe" checked={universe === u} onChange={() => setUniverse(u)} className="accent-[var(--color-good)]" />
            {t(UNIVERSE_LABEL[u]!)}
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
      <fieldset className="grid grid-cols-2 gap-3 text-sm" disabled={running}>
        <label className="space-y-1">
          <span className="block font-semibold">{t("Price the businesses scoring at least")}</span>
          <select value={minBusiness} onChange={(e) => setMinBusiness(Number(e.target.value))} className="w-full rounded-lg border border-line bg-paper px-2 py-1.5">
            {[3, 3.5, 4, 4.5].map((v) => (
              <option key={v} value={v}>
                {v.toFixed(1)} / 5
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1">
          <span className="block font-semibold">{t("At most")}</span>
          <select value={maxPriced} onChange={(e) => setMaxPriced(Number(e.target.value))} className="w-full rounded-lg border border-line bg-paper px-2 py-1.5">
            {[25, 50, 100, 150, 250].map((v) => (
              <option key={v} value={v}>
                {t("{n} companies", { n: v })}
              </option>
            ))}
          </select>
        </label>
      </fieldset>
      <p className="text-xs leading-relaxed text-muted">
        {t(
          "Filings are free and quick. Prices come from Tiingo's free tier, 50 requests an hour and 500 different symbols a month, so pricing {n} companies takes up to {hours} hours; prices already looked up today are instant.",
          { n: maxPriced, hours: hours < 1 ? hours.toFixed(1) : Math.ceil(hours) },
        )}
      </p>
      {error && <p className="text-sm text-bad" role="alert">{error}</p>}
      <button onClick={() => void start()} disabled={busy || running || (universe === "custom" && !tickers.trim())} className="w-full rounded-full bg-good px-4 py-2 font-semibold text-white disabled:opacity-40">
        {running ? t("A screen is running") : busy ? t("Starting…") : t("Start screen")}
      </button>
    </div>
  );
}

function ProgressPanel({ state, onChange }: { state: ScreenState; onChange: () => void }) {
  const { screen, progress: p, running } = state;
  const [busy, setBusy] = useState(false);
  const toPrice = p.queued + p.priced;
  const leftSeconds = p.queued * SECONDS_PER_PRICE;
  const left = leftSeconds >= 3600 ? t("about {h} h {m} min", { h: Math.floor(leftSeconds / 3600), m: Math.round((leftSeconds % 3600) / 60) }) : t("about {m} min", { m: Math.max(1, Math.round(leftSeconds / 60)) });
  const cancel = async () => {
    setBusy(true);
    try {
      await cancelScreen(screen.id);
      onChange();
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="card space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="font-display text-lg font-bold">
          {t(UNIVERSE_LABEL[screen.universe] ?? screen.universe)} · <span className={running ? "text-good" : ""}>{t(STATUS_LABEL[screen.status])}</span>
        </h2>
        <span className="text-xs text-muted">{t("Started {date}", { date: new Date(screen.createdAt).toLocaleString() })}</span>
      </div>
      <Bar label={t("Filings read")} done={p.scanned} total={p.total} detail={p.failed ? t("{n} could not be read", { n: p.failed }) : ""} />
      {screen.status !== "scanning" && (
        <Bar
          label={t("Prices looked up")}
          done={p.priced}
          total={toPrice}
          detail={[
            running && p.queued > 0 ? t("{left} left", { left }) : "",
            p.below ? t("{n} below {b}", { n: p.below, b: screen.params.minBusiness.toFixed(1) }) : "",
            p.capped ? t("{n} over the limit of {max}", { n: p.capped, max: screen.params.maxPriced }) : "",
          ]
            .filter(Boolean)
            .join(" · ")}
        />
      )}
      {screen.status === "scanning" && <p className="text-sm text-muted">{t("Prices start once every company's filings are read.")}</p>}
      {screen.error && <p className="text-sm text-bad">{tx(screen.error)}</p>}
      {running && (
        <button onClick={() => void cancel()} disabled={busy} className="rounded-full border border-line px-4 py-1.5 text-sm text-muted hover:text-bad">
          {t("Cancel screen")}
        </button>
      )}
      <p className="text-xs text-muted">{t("The screen keeps running if you leave this page, and resumes after a restart.")}</p>
    </div>
  );
}

function Bar({ label, done, total, detail }: { label: string; done: number; total: number; detail: string }) {
  const pct = total > 0 ? Math.min(100, (done / total) * 100) : 0;
  return (
    <div className="space-y-1">
      <p className="flex justify-between text-sm">
        <span className="font-semibold">{label}</span>
        <span className="tabular-nums text-muted">
          {done} / {total}
        </span>
      </p>
      <div className="h-2 overflow-hidden rounded-full bg-line" role="progressbar" aria-valuenow={done} aria-valuemax={total} aria-label={label}>
        <div className="h-full rounded-full bg-good transition-[width]" style={{ width: `${pct}%` }} />
      </div>
      {detail && <p className="text-xs text-muted">{detail}</p>}
    </div>
  );
}

function EmptyPanel() {
  return (
    <div className="card flex items-center text-sm text-muted">
      <p>{t("No screen yet. Start one to find strong businesses that are cheap against their own history.")}</p>
    </div>
  );
}

function ResultsTable({
  rows,
  picked,
  onPick,
  canPick,
  minBusiness,
  maxPriced,
}: {
  rows: ScreenResult[];
  picked: string[];
  onPick: (tk: string) => void;
  canPick: boolean;
  minBusiness: number;
  maxPriced: number;
}) {
  if (rows.length === 0) {
    return <p className="card text-sm text-muted">{t("No company matches yet.")}</p>;
  }
  return (
    <div className="card overflow-x-auto !p-0">
      <table className="w-full min-w-[56rem] text-sm">
        <thead>
          <tr className="border-b border-line text-left text-xs text-muted">
            <th className="w-10 px-3 py-2" />
            <th className="px-3 py-2 font-semibold">{t("Company")}</th>
            <th className="px-3 py-2 font-semibold" title={t("Growth and financial health, weighted as in the verdict; the full report adds the AI's quality, moat and risk")}>
              {t("Business (no AI)")}
            </th>
            <th className="px-3 py-2 font-semibold">{t("Valuation")}</th>
            <th className="px-3 py-2 font-semibold">{t("Price against its own history")}</th>
            <th className="px-3 py-2 font-semibold">{t("Market cap")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const on = picked.includes(r.ticker);
            return (
              <tr key={r.ticker} className={`border-b border-line/60 last:border-0 ${on ? "bg-good-soft/40" : ""}`}>
                <td className="px-3 py-2.5 align-top">
                  <input
                    type="checkbox"
                    checked={on}
                    disabled={!on && (!canPick || r.stage === "failed")}
                    onChange={() => onPick(r.ticker)}
                    aria-label={t("Pick {ticker} for a full report", { ticker: r.ticker })}
                    className="mt-1 size-4 accent-[var(--color-good)]"
                  />
                </td>
                <td className="px-3 py-2.5 align-top">
                  <span className="font-display font-bold">{r.ticker}</span>{" "}
                  <span className="text-muted">{titleCase(r.name)}</span>
                  <span className="block text-xs text-muted">
                    {/* The phase needs the market cap, known once priced. */}
                    {[r.sector, r.kind ? t(KIND_LABEL[r.kind]!) : "", r.stage === "priced" && r.phase ? tx(r.phase) : ""].filter(Boolean).join(" · ")}
                    {r.limited && <> · {t("limited support")}</>}
                  </span>
                </td>
                <td className="px-3 py-2.5 align-top">
                  <span className="font-semibold tabular-nums">{r.business != null ? `${r.business.toFixed(1)}/5` : "–"}</span>
                  <span className="block text-xs text-muted">
                    {t("Growth")} {r.growth?.toFixed(0) ?? "–"} · {r.kind ? t("Strength") : t("Health")} {r.health?.toFixed(1) ?? "–"}
                  </span>
                </td>
                <td className="px-3 py-2.5 align-top">
                  {r.stage === "priced" && r.valuation != null ? (
                    <>
                      <span className="font-semibold tabular-nums">{r.valuation.toFixed(0)}/5</span> <span className="text-muted">{scoreWord(SCORE_WORDS.valuation, r.valuation)}</span>
                      {r.expectations && <span className="block text-xs text-muted">{t(EXPECTATION_LABEL[r.expectations]!)}</span>}
                    </>
                  ) : (
                    <span className="text-xs text-muted">{stageNote(r, minBusiness, maxPriced)}</span>
                  )}
                </td>
                <td className="px-3 py-2.5 align-top">
                  {r.stage === "priced" && r.price != null && r.cheapPrice != null && r.medianPrice != null && r.expensivePrice != null ? (
                    <div className="max-w-xs">
                      <p className="flex flex-wrap items-baseline gap-x-2">
                        <span className="font-semibold tabular-nums">{price(r.price)}</span>
                        {r.zone && (
                          <span className={`text-xs font-semibold ${ZONE_STYLE[r.zone]}`}>
                            {t({ cheap: "Cheap", fair: "Typical", expensive: "Expensive" }[r.zone])} · {r.metric} {r.current?.toFixed(1)}
                          </span>
                        )}
                      </p>
                      <ZoneBar price={r.price} b={{ cheapPrice: r.cheapPrice, medianPrice: r.medianPrice, expensivePrice: r.expensivePrice }} />
                    </div>
                  ) : (
                    <span className="text-xs text-muted">–</span>
                  )}
                </td>
                <td className="px-3 py-2.5 align-top tabular-nums">{usd(r.marketCap)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** Why a company has no valuation yet. */
function stageNote(r: ScreenResult, minBusiness: number, maxPriced: number): string {
  switch (r.stage) {
    case "scanned":
      return t("Price comes after the filings");
    case "queued":
      return t("Waiting for its price");
    case "below":
      return t("Not priced: business below {b}", { b: minBusiness.toFixed(1) });
    case "capped":
      return t("Not priced: over the limit of {n}", { n: maxPriced });
    case "failed":
      return r.error ? tx(r.error) : t("Could not be read");
    default:
      return r.error ? tx(r.error) : t("No valuation");
  }
}
