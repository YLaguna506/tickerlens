import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ApiError, addToWatchlist, getWatchlist, refreshWatchlist, removeFromWatchlist, searchTickers, type WatchItem, type WatchSummary } from "../api";
import { LangSwitch, ModelSwitch } from "../components/Controls";
import { Disclaimer } from "../components/ui";
import { ZoneBar } from "../components/ZoneBar";
import { price, shortDate, titleCase } from "../format";
import { t, tx } from "../i18n";
import { STAGES } from "../live";
import { navigate } from "../router";

const VERDICT_STYLE: Record<string, string> = {
  Buy: "bg-good text-white",
  Watch: "bg-good-soft text-good-strong",
  Hold: "bg-mid text-[#1a1404]",
  Pass: "bg-bad-soft text-bad",
  Sell: "bg-bad text-white",
};
const VERDICT_ORDER = ["Buy", "Watch", "Hold", "Pass", "Sell"];
const ZONE_STYLE = { cheap: "text-good", fair: "text-mid", expensive: "text-bad" };

type Sort = "added" | "verdict" | "cheapest";

const DAY = 24 * 60 * 60 * 1000;
const ageDays = (iso: string) => Math.floor((Date.now() - new Date(iso).getTime()) / DAY);

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setV(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
  return v;
}

export function Watchlist() {
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["watchlist"],
    queryFn: getWatchlist,
    // Poll while any report is being built.
    refetchInterval: (q) => (q.state.data?.some((it) => it.running) ? 5000 : false),
  });
  const [sort, setSort] = useState<Sort>("added");
  const [picked, setPicked] = useState<string[]>([]);
  const togglePick = (tk: string) => setPicked((l) => (l.includes(tk) ? l.filter((x) => x !== tk) : l.length >= 3 ? l : [...l, tk]));
  const [notice, setNotice] = useState<string | null>(null);
  const [updating, setUpdating] = useState(false);
  const items = sorted(list.data ?? [], sort);
  const reload = () => void qc.invalidateQueries({ queryKey: ["watchlist"] });

  const update = async () => {
    setUpdating(true);
    setNotice(null);
    try {
      const r = await refreshWatchlist();
      setNotice(
        r.started.length === 0
          ? t("Every report is up to date.")
          : t("Building new reports for {list}.", { list: r.started.join(", ") }) +
              (r.waiting.length ? " " + t("{n} more next time: five at a time keeps the AI and price quotas safe.", { n: r.waiting.length }) : ""),
      );
      reload();
    } catch (e) {
      setNotice(e instanceof Error ? tx(e.message) : String(e));
    } finally {
      setUpdating(false);
    }
  };

  const counts = VERDICT_ORDER.map((v) => [v, items.filter((it) => it.latest?.verdict === v).length] as const).filter(([, n]) => n > 0);

  return (
    <div className="mx-auto min-h-screen max-w-5xl px-4">
      <header className="flex flex-wrap items-center justify-between gap-2 pt-4">
        <button onClick={() => navigate("/")} className="text-sm font-semibold text-muted hover:text-ink">
          ← {t("New search")}
        </button>
        <div className="flex flex-wrap items-center gap-2">
          <ModelSwitch look="classic" />
          <LangSwitch look="classic" />
        </div>
      </header>

      <div className="flex flex-wrap items-end justify-between gap-4 pt-8">
        <div>
          <h1 className="section-title">{t("Watchlist")}</h1>
          {counts.length > 0 && (
            <p className="mt-3 flex flex-wrap gap-2">
              {counts.map(([v, n]) => (
                <span key={v} className={`rounded-full px-3 py-1 text-sm font-semibold ${VERDICT_STYLE[v]}`}>
                  {n} {t(v)}
                </span>
              ))}
            </p>
          )}
        </div>
        <AddTicker onAdded={reload} />
      </div>

      {items.length > 0 && (
        <div className="mt-6 flex flex-wrap items-center justify-between gap-3">
          <div className="flex gap-1 rounded-full border border-line bg-card p-1 text-sm" role="group" aria-label={t("Sort by")}>
            {(["added", "verdict", "cheapest"] as const).map((s) => (
              <button key={s} onClick={() => setSort(s)} aria-pressed={sort === s} className={`rounded-full px-3 py-1 font-semibold ${sort === s ? "bg-good text-white" : "text-muted hover:text-ink"}`}>
                {t({ added: "Added", verdict: "Verdict", cheapest: "Cheapest" }[s])}
              </button>
            ))}
          </div>
          <span className="flex flex-wrap gap-2">
            <button
              onClick={() => navigate(`/compare?t=${picked.join(",")}`)}
              disabled={picked.length < 2}
              title={picked.length < 2 ? t("Tick two or three companies to compare them") : undefined}
              className="rounded-full border border-line bg-card px-4 py-2 text-sm font-semibold hover:border-good disabled:opacity-50"
            >
              ⇄ {t("Compare ({n})", { n: picked.length })}
            </button>
            <button onClick={() => void update()} disabled={updating} className="rounded-full border border-line bg-card px-4 py-2 text-sm font-semibold hover:border-good disabled:opacity-50">
              ↻ {updating ? t("Starting…") : t("Update stale reports")}
            </button>
          </span>
        </div>
      )}
      {notice && (
        <p className="card mt-4 text-sm" role="status">
          {notice}
        </p>
      )}

      <main className="mt-6 space-y-3">
        {list.isLoading ? (
          <p className="text-muted">{t("Loading…")}</p>
        ) : list.error ? (
          <p className="card bg-bad-soft text-sm">{t("Could not load the watchlist. Is the API running?")}</p>
        ) : items.length === 0 ? (
          <div className="card text-center text-muted">
            <p className="font-display text-lg font-semibold text-ink">{t("Your watchlist is empty.")}</p>
            <p className="mt-1 text-sm">{t("Add a ticker above, or press ☆ at the top of any report.")}</p>
          </div>
        ) : (
          items.map((it) => <Row key={it.ticker} it={it} onChange={reload} picked={picked.includes(it.ticker)} onPick={() => togglePick(it.ticker)} canPick={picked.length < 3} />)
        )}
      </main>
      <p className="mt-6 text-xs text-muted">{t("Prices are from each report's date. Reports older than 12 hours are rebuilt by Update stale reports.")}</p>
      <Disclaimer />
    </div>
  );
}

function sorted(items: WatchItem[], sort: Sort): WatchItem[] {
  const list = [...items];
  if (sort === "verdict") {
    const rank = (it: WatchItem) => (it.latest?.verdict ? VERDICT_ORDER.indexOf(it.latest.verdict) : 99);
    list.sort((a, b) => rank(a) - rank(b) || (b.latest?.business ?? 0) - (a.latest?.business ?? 0));
  } else if (sort === "cheapest") {
    const pct = (it: WatchItem) => it.latest?.bands?.percentile ?? 2;
    list.sort((a, b) => pct(a) - pct(b));
  }
  return list;
}

function Row({ it, onChange, picked, onPick, canPick }: { it: WatchItem; onChange: () => void; picked: boolean; onPick: () => void; canPick: boolean }) {
  const s = it.latest;
  const [busy, setBusy] = useState(false);
  const remove = async () => {
    setBusy(true);
    try {
      await removeFromWatchlist(it.ticker);
      onChange();
    } finally {
      setBusy(false);
    }
  };
  const open = () => {
    const id = s?.reportId ?? it.running?.id;
    if (id) navigate(`/reports/${id}`);
  };
  const stage = it.running ? STAGES.findIndex((x) => x.id === it.running!.stage) : -1;

  return (
    <article className={`card fade-in relative grid gap-4 !p-4 sm:!p-5 md:grid-cols-[auto_13rem_1fr_auto] md:items-center ${picked ? "ring-2 ring-good" : ""}`}>
      <input
        type="checkbox"
        checked={picked}
        onChange={onPick}
        disabled={!picked && !canPick}
        aria-label={t("Select {ticker} to compare", { ticker: it.ticker })}
        className="absolute top-5 right-5 size-5 accent-[var(--color-good)] md:static md:size-4"
      />
      <button onClick={open} disabled={!s && !it.running} className="flex items-center gap-3 text-left disabled:cursor-default">
        <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-navy font-display text-xs font-extrabold text-white">{it.ticker.slice(0, 5)}</span>
        <span className="min-w-0">
          <span className="block font-display text-lg font-bold">{it.ticker}</span>
          <span className="block truncate text-sm text-muted">{titleCase(it.companyName)}</span>
        </span>
      </button>

      {s ? <Summary s={s} /> : <p className="text-sm text-muted">{it.running ? t("Building the first report…") : t("No report yet. Update stale reports builds one.")}</p>}

      <div className="flex items-center justify-between gap-2 md:flex-col md:items-end">
        {it.running ? (
          <span className="animate-pulse text-xs font-semibold text-good">
            {t("Building…")} {stage >= 0 ? t(STAGES[stage]!.label) : ""}
          </span>
        ) : s ? (
          <span className={`text-xs ${ageDays(s.createdAt) >= 7 ? "font-semibold text-mid" : "text-muted"}`} title={new Date(s.createdAt).toLocaleString()}>
            {ageDays(s.createdAt) === 0 ? t("Report from today") : t("Report {n} days old", { n: ageDays(s.createdAt) })}
          </span>
        ) : null}
        <span className="flex gap-2">
          {(s || it.running) && (
            <button onClick={open} className="rounded-full bg-good px-3 py-1 text-xs font-semibold text-white">
              {t("Open")}
            </button>
          )}
          <button onClick={() => void remove()} disabled={busy} className="rounded-full border border-line px-3 py-1 text-xs text-muted hover:text-bad" title={t("Remove {ticker} from the watchlist", { ticker: it.ticker })}>
            {t("Remove")}
          </button>
        </span>
      </div>
    </article>
  );
}

function Summary({ s }: { s: WatchSummary }) {
  return (
    <div className="grid min-w-0 gap-3 sm:grid-cols-[13rem_1fr] sm:items-center sm:gap-5">
      <div className="flex items-center gap-3 sm:block">
        <span className={`inline-block rounded-full px-3 py-1 text-sm font-bold ${s.verdict ? VERDICT_STYLE[s.verdict] : "bg-line text-muted"}`}>{s.verdict ? t(s.verdict) : "–"}</span>
        <p className="text-xs text-muted sm:mt-1.5">
          B {s.business?.toFixed(1) ?? "–"} · V {s.valuation?.toFixed(0) ?? "–"}
          {s.moat && <> · {t("Moat")}: {t(s.moat)}</>}
          {s.riskScore != null && <> · {t("Risk")}: {s.riskScore.toFixed(1)}</>}
        </p>
      </div>
      <div className="min-w-0">
        <p className="flex flex-wrap items-baseline gap-x-2">
          <span className="font-display text-xl font-bold tabular-nums">{price(s.price)}</span>
          {s.priceDate && <span className="text-xs text-muted">{shortDate(s.priceDate)}</span>}
          {s.bands && (
            <span className={`text-sm font-semibold ${ZONE_STYLE[s.bands.zone]}`}>
              {t({ cheap: "Cheap", fair: "Typical", expensive: "Expensive" }[s.bands.zone])} · {s.bands.metric} {s.bands.current.toFixed(1)}
            </span>
          )}
        </p>
        {s.bands && s.price != null ? (
          <ZoneBar price={s.price} b={s.bands} />
        ) : (
          <p className="mt-1 text-xs text-muted">{s.noBandsWhy ? tx(s.noBandsWhy) : t("No valuation bands for this report.")}</p>
        )}
      </div>
    </div>
  );
}

function AddTicker({ onAdded }: { onAdded: () => void }) {
  const [query, setQuery] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const q = useDebounced(query.trim(), 150);
  const results = useQuery({ queryKey: ["tickers", q], queryFn: () => searchTickers(q), enabled: q.length > 0, staleTime: 60_000 });
  const options = q ? (results.data ?? []).slice(0, 6) : [];

  const add = async (ticker: string) => {
    setBusy(true);
    setError(null);
    try {
      await addToWatchlist(ticker);
      setQuery("");
      onAdded();
    } catch (e) {
      setError(e instanceof ApiError ? tx(e.message) : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      className="relative w-full sm:w-80"
      onSubmit={(e) => {
        e.preventDefault();
        const pick = options[0]?.ticker ?? query.trim();
        if (pick) void add(pick);
      }}
    >
      <label htmlFor="watch-add" className="sr-only">
        {t("Add a ticker")}
      </label>
      <input
        id="watch-add"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t("Add a ticker…")}
        autoComplete="off"
        className="w-full rounded-2xl border-2 border-line bg-card px-4 py-2.5 font-semibold outline-none placeholder:font-normal placeholder:text-muted/60 focus:border-good"
      />
      <button type="submit" disabled={busy || !query.trim()} className="absolute top-1.5 right-1.5 bottom-1.5 rounded-xl bg-good px-3 text-sm font-semibold text-white disabled:opacity-40">
        {t("Add")}
      </button>
      {options.length > 0 && (
        <ul className="absolute z-10 mt-2 w-full overflow-hidden rounded-2xl border border-line bg-card shadow-lg">
          {options.map((c) => (
            <li key={c.ticker}>
              <button type="button" onClick={() => void add(c.ticker)} className="flex w-full items-baseline gap-3 px-4 py-2 text-left hover:bg-good-soft">
                <span className="w-14 font-display font-bold">{c.ticker}</span>
                <span className="truncate text-sm text-muted">{titleCase(c.name)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {error && <p className="mt-2 text-sm text-bad">{error}</p>}
    </form>
  );
}
