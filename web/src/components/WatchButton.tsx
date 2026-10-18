// The ☆/★ button next to the resync button: adds the report's ticker to the
// watchlist or takes it off.

import { useEffect, useState } from "react";
import { addToWatchlist, removeFromWatchlist, watchStatus } from "../api";
import { t } from "../i18n";
import { navigate } from "../router";

const STYLES = {
  classic: "rounded-full border border-line bg-card p-2 text-muted hover:text-ink",
  evidence: "rounded-xl border border-zinc-200 bg-white p-2 text-zinc-500 hover:text-zinc-900",
  bento: "rounded-2xl bg-white/80 p-2 text-slate-500 shadow-[0_2px_12px_rgba(15,23,42,0.08)] hover:text-slate-900",
  decision: "rounded-md border border-slate-300 bg-white p-1.5 text-slate-600 hover:border-slate-400",
  terminal: "border border-t-line p-1 text-t-dim hover:text-white",
};

/** Opens the comparison page with this ticker in it. */
export function CompareButton({ ticker, look }: { ticker?: string; look: keyof typeof STYLES }) {
  if (!ticker) return null;
  const label = t("Compare {ticker} with other companies", { ticker });
  return (
    <button type="button" onClick={() => navigate(`/compare?t=${encodeURIComponent(ticker)}`)} title={label} aria-label={label} className={`shrink-0 ${STYLES[look]}`}>
      <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        <path d="M7 4 3 8l4 4M3 8h14M17 20l4-4-4-4M21 16H7" />
      </svg>
    </button>
  );
}

export function WatchButton({ ticker, look }: { ticker?: string; look: keyof typeof STYLES }) {
  const [watching, setWatching] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!ticker) return;
    let live = true;
    watchStatus(ticker)
      .then((s) => live && setWatching(s.watching))
      .catch(() => live && setWatching(null));
    return () => {
      live = false;
    };
  }, [ticker]);

  if (!ticker || watching == null) return null;
  const toggle = async () => {
    setBusy(true);
    try {
      const s = watching ? await removeFromWatchlist(ticker) : await addToWatchlist(ticker);
      setWatching(s.watching);
    } finally {
      setBusy(false);
    }
  };
  const label = watching ? t("Remove {ticker} from the watchlist", { ticker }) : t("Add {ticker} to the watchlist", { ticker });
  return (
    <button type="button" onClick={() => void toggle()} disabled={busy} title={label} aria-label={label} aria-pressed={watching} className={`shrink-0 disabled:opacity-40 ${STYLES[look]}`}>
      <svg viewBox="0 0 24 24" className={`size-4 ${watching ? "text-amber-400" : ""}`} fill={watching ? "currentColor" : "none"} stroke="currentColor" strokeWidth={2} strokeLinejoin="round" aria-hidden>
        <path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1 6.2L12 17.3 6.5 20.2l1-6.2L3 9.6l6.2-.9z" />
      </svg>
    </button>
  );
}
