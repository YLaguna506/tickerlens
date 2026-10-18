import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ApiError, createReport, searchTickers, type Company } from "../api";
import { titleCase } from "../format";
import { LangSwitch, ModelSwitch } from "../components/Controls";
import { Disclaimer } from "../components/ui";
import { t, tx } from "../i18n";
import { navigate } from "../router";

const RECENT_KEY = "tickerlens.recent";

function loadRecent(): Company[] {
  try {
    return JSON.parse(localStorage.getItem(RECENT_KEY) ?? "[]") as Company[];
  } catch {
    return [];
  }
}

function saveRecent(c: Company) {
  try {
    const list = [c, ...loadRecent().filter((r) => r.ticker !== c.ticker)].slice(0, 6);
    localStorage.setItem(RECENT_KEY, JSON.stringify(list));
  } catch {
    // Storage can be unavailable (private mode); recent tickers are optional.
  }
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setV(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
  return v;
}

export function Home() {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [error, setError] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);
  const q = useDebounced(query.trim(), 150);
  const recent = loadRecent();

  const results = useQuery({
    queryKey: ["tickers", q],
    queryFn: () => searchTickers(q),
    enabled: q.length > 0,
    staleTime: 60_000,
  });
  const options = q ? (results.data ?? []).slice(0, 8) : [];

  async function open(c: Company | string) {
    const ticker = typeof c === "string" ? c : c.ticker;
    setBusy(true);
    setError(null);
    try {
      const r = await createReport(ticker);
      if (typeof c !== "string") saveRecent(c);
      navigate(`/reports/${r.id}`);
    } catch (e) {
      setError(e instanceof ApiError ? e : new ApiError(String(e), 0));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto flex min-h-screen max-w-2xl flex-col px-4">
      <header className="flex flex-wrap items-center justify-end gap-2 pt-4">
        <span className="mr-auto flex gap-2">
          <button onClick={() => navigate("/watchlist")} className="rounded-full border border-line bg-card px-4 py-2 text-sm font-semibold hover:border-good">
            <span className="text-amber-400">★</span> {t("Watchlist")}
          </button>
          <button onClick={() => navigate("/compare")} className="rounded-full border border-line bg-card px-4 py-2 text-sm font-semibold hover:border-good">
            ⇄ {t("Compare")}
          </button>
          <button onClick={() => navigate("/screener")} className="rounded-full border border-line bg-card px-4 py-2 text-sm font-semibold hover:border-good">
            ⌕ {t("Screener")}
          </button>
        </span>
        <ModelSwitch look="classic" />
        <LangSwitch look="classic" />
      </header>
      <main className="flex flex-1 flex-col justify-center py-16">
        <p className="eyebrow text-center">TickerLens</p>
        <h1 className="mt-3 text-center font-display text-4xl font-extrabold tracking-tight sm:text-6xl">
          {t("Is it a good business at a good price?")}
        </h1>
        <p className="mt-4 text-center text-lg text-muted">
          {t("Type a US stock. TickerLens reads its SEC filings, computes the numbers and gives a clear verdict.")}
        </p>

        <form
          className="relative mt-10"
          onSubmit={(e) => {
            e.preventDefault();
            if (options[active]) void open(options[active]);
            else if (query.trim()) void open(query.trim());
          }}
        >
          <label htmlFor="ticker" className="sr-only">{t("Ticker or company name")}</label>
          <input
            id="ticker"
            autoFocus
            autoComplete="off"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActive(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") setActive((a) => Math.min(a + 1, options.length - 1));
              if (e.key === "ArrowUp") setActive((a) => Math.max(a - 1, 0));
            }}
            placeholder="V, AAPL, Microsoft…"
            className="w-full rounded-2xl border-2 border-line bg-card px-5 py-4 font-display text-2xl font-semibold shadow-sm outline-none placeholder:font-normal placeholder:text-muted/60 focus:border-good"
            role="combobox"
            aria-expanded={options.length > 0}
            aria-controls="ticker-options"
          />
          <button
            type="submit"
            disabled={busy || !query.trim()}
            className="absolute top-2 right-2 bottom-2 rounded-xl bg-good px-5 font-semibold text-white disabled:opacity-40"
          >
            {busy ? t("Starting…") : t("Analyze")}
          </button>
          {options.length > 0 && (
            <ul id="ticker-options" role="listbox" className="absolute z-10 mt-2 w-full overflow-hidden rounded-2xl border border-line bg-card shadow-lg">
              {options.map((c, i) => (
                <li key={c.ticker} role="option" aria-selected={i === active}>
                  <button
                    type="button"
                    onMouseEnter={() => setActive(i)}
                    onClick={() => void open(c)}
                    className={`flex w-full items-baseline gap-3 px-5 py-3 text-left ${i === active ? "bg-good-soft" : ""}`}
                  >
                    <span className="w-16 font-display font-bold">{c.ticker}</span>
                    <span className="truncate text-muted">{titleCase(c.name)}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </form>

        {error && (
          <div className="mt-4 rounded-xl bg-bad-soft p-4 text-sm" role="alert">
            <p className="font-semibold">{tx(error.message)}</p>
            {error.suggestions.length > 0 && (
              <p className="mt-1">
                {t("Did you mean")}{" "}
                {error.suggestions.map((s, i) => (
                  <span key={s.ticker}>
                    {i > 0 && ", "}
                    <button className="font-semibold underline" onClick={() => void open(s)}>{s.ticker}</button>
                  </span>
                ))}
                ?
              </p>
            )}
          </div>
        )}

        {recent.length > 0 && (
          <div className="mt-10">
            <p className="eyebrow">{t("Recent")}</p>
            <div className="mt-3 flex flex-wrap gap-2">
              {recent.map((c) => (
                <button key={c.ticker} onClick={() => void open(c)} className="rounded-full border border-line bg-card px-4 py-2 text-sm hover:border-good">
                  <span className="font-bold">{c.ticker}</span> <span className="text-muted">{titleCase(c.name)}</span>
                </button>
              ))}
            </div>
          </div>
        )}
      </main>
      <Disclaimer />
    </div>
  );
}
