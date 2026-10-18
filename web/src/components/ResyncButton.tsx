// The resync button next to the design switch: deletes the ticker's saved
// reports and cached data (filings, prices, AI answers) and builds a fresh one.

import { useState } from "react";
import { clearTickerCache, createReport, isFinished, type Report } from "../api";
import { t } from "../i18n";
import { navigate } from "../router";
import { ConfirmDialog } from "./ConfirmDialog";

const STYLES = {
  classic: "rounded-full border border-line bg-card p-2 text-muted hover:text-ink",
  evidence: "rounded-xl border border-zinc-200 bg-white p-2 text-zinc-500 hover:text-zinc-900",
  bento: "rounded-2xl bg-white/80 p-2 text-slate-500 shadow-[0_2px_12px_rgba(15,23,42,0.08)] hover:text-slate-900",
  decision: "rounded-md border border-slate-300 bg-white p-1.5 text-slate-600 hover:border-slate-400",
  terminal: "border border-t-line p-1 text-t-dim hover:text-white",
};
// The dialog takes each design's palette.
const DIALOG_LOOK = {
  classic: "",
  evidence: "look-light",
  bento: "look-light",
  decision: "look-light",
  terminal: "look-terminal font-mono",
};

export function ResyncButton({ report, look }: { report?: Report; look: keyof typeof STYLES }) {
  const [state, setState] = useState<"idle" | "working" | "error">("idle");
  const [asking, setAsking] = useState(false);
  if (!report) return null;
  const running = !isFinished(report.status);
  const ticker = report.ticker;

  const run = async () => {
    setAsking(false);
    setState("working");
    try {
      await clearTickerCache(ticker);
      const r = await createReport(ticker, true);
      navigate(`/reports/${r.id}${window.location.search}`);
    } catch {
      setState("error");
    }
  };

  const label =
    state === "error"
      ? t("Could not clear the cache. Is the API running? Click to try again.")
      : running
        ? t("The report is still being built")
        : t("Clear cached {ticker} data and rebuild from scratch", { ticker });
  return (
    <>
      <button
        type="button"
        onClick={() => setAsking(true)}
        disabled={running || state === "working"}
        title={label}
        aria-label={label}
        className={`shrink-0 disabled:cursor-not-allowed disabled:opacity-40 ${STYLES[look]} ${state === "error" ? "text-red-600" : ""}`}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" className={`size-4 ${state === "working" ? "animate-spin" : ""}`} aria-hidden>
          <path d="M21 12a9 9 0 0 1-15.5 6.2L3 16" />
          <path d="M3 21v-5h5" />
          <path d="M3 12a9 9 0 0 1 15.5-6.2L21 8" />
          <path d="M21 3v5h-5" />
        </svg>
      </button>
      <ConfirmDialog
        open={asking}
        title={t("Rebuild {ticker} from scratch?", { ticker })}
        confirmLabel={t("Delete and rebuild")}
        danger
        className={DIALOG_LOOK[look]}
        onConfirm={() => void run()}
        onCancel={() => setAsking(false)}
      >
        <p>{t("Deletes every saved {ticker} report and its cached data (filings, prices, AI answers), then builds a new report.", { ticker })}</p>
        <p>{t("The new report calls the AI again, which uses your daily quota.")}</p>
      </ConfirmDialog>
    </>
  );
}
