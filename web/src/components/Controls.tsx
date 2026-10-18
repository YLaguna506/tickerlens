// The controls at the top of every page: resync, AI model, language and design.

import { WatchButton } from "./WatchButton";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { createReport, isFinished, listModels, type Report } from "../api";
import { getLang, setLang, t, useLang, type Lang } from "../i18n";
import { setModel, useModel } from "../model";
import { navigate } from "../router";
import { DesignSwitch } from "./DesignSwitch";
import { ResyncButton } from "./ResyncButton";

export type Look = "classic" | "evidence" | "bento" | "decision" | "terminal";

const LANG_STYLES: Record<Look, { group: string; item: string; on: string; off: string }> = {
  classic: { group: "flex rounded-full border border-line bg-card p-1 font-display text-xs font-semibold", item: "rounded-full px-2.5 py-1.5", on: "bg-good text-white", off: "text-muted hover:text-ink" },
  evidence: { group: "flex rounded-xl border border-zinc-200 bg-white p-0.5 text-xs font-semibold", item: "rounded-lg px-2 py-1.5", on: "bg-zinc-900 text-white", off: "text-zinc-500 hover:text-zinc-900" },
  bento: { group: "flex rounded-2xl bg-white/80 p-1 font-bento text-xs font-semibold shadow-[0_2px_12px_rgba(15,23,42,0.08)]", item: "rounded-xl px-2.5 py-1.5", on: "bg-slate-900 text-white", off: "text-slate-500 hover:text-slate-900" },
  decision: { group: "flex gap-1 font-flow text-xs font-medium", item: "rounded-md border px-2 py-1", on: "border-indigo-600 bg-indigo-600 text-white", off: "border-slate-300 bg-white text-slate-600 hover:border-slate-400" },
  terminal: { group: "flex border border-t-line font-mono text-[11px] uppercase", item: "px-2 py-1", on: "bg-t-amber font-bold text-black", off: "text-t-dim hover:text-white" },
};

const SELECT_STYLES: Record<Look, string> = {
  classic: "rounded-full border border-line bg-card py-1.5 pr-7 pl-3 font-display text-xs font-semibold text-ink",
  evidence: "rounded-xl border border-zinc-200 bg-white py-1.5 pr-7 pl-2.5 text-xs font-semibold text-zinc-700",
  bento: "rounded-2xl border-0 bg-white/80 py-1.5 pr-7 pl-3 font-bento text-xs font-semibold text-slate-700 shadow-[0_2px_12px_rgba(15,23,42,0.08)]",
  decision: "rounded-md border border-slate-300 bg-white py-1 pr-7 pl-2 font-flow text-xs font-medium text-slate-700",
  terminal: "border border-t-line bg-t-bg py-1 pr-6 pl-2 font-mono text-[11px] text-t-text uppercase",
};

/** EN | ES. */
export function LangSwitch({ look }: { look: Look }) {
  const lang = useLang();
  const s = LANG_STYLES[look];
  return (
    <div role="group" aria-label={t("Language")} className={`shrink-0 ${s.group}`}>
      {(["en", "es"] as Lang[]).map((l) => (
        <button key={l} type="button" aria-pressed={l === lang} onClick={() => setLang(l)} className={`${s.item} ${l === lang ? s.on : s.off}`}>
          {l.toUpperCase()}
        </button>
      ))}
    </div>
  );
}

/** The AI model for new reports (the open report keeps its own). */
export function ModelSwitch({ look }: { look: Look }) {
  const model = useModel();
  const { data: models } = useQuery({ queryKey: ["models"], queryFn: listModels, staleTime: 5 * 60_000 });
  if (!models?.length) return null;
  const value = models.some((m) => m.id === model) ? model : (models.find((m) => m.default)?.id ?? "");
  return (
    <label className="shrink-0" title={`${t("AI model for new reports")} (★ ${t("default")})`}>
      <span className="sr-only">{t("AI model for new reports")}</span>
      <select value={value} onChange={(e) => setModel(models.find((m) => m.id === e.target.value)?.default ? "" : e.target.value)} className={`max-w-60 truncate ${SELECT_STYLES[look]}`}>
        {models.map((m) => (
          <option key={m.id} value={m.id}>
            {m.default ? "★ " : ""}
            {m.model}
          </option>
        ))}
      </select>
    </label>
  );
}

/** Resync, model, language and design, in that order. */
export function ReportControls({ report, look }: { report?: Report; look: Look }) {
  return (
    <>
      <WatchButton ticker={report?.ticker} look={look} />
      <ResyncButton report={report} look={look} />
      <ModelSwitch look={look} />
      <LangSwitch look={look} />
      <DesignSwitch look={look} />
    </>
  );
}

/** Shown when the report's AI-written text is in the other language. */
export function LangNotice({ report, className }: { report?: Report; className: string }) {
  const lang = useLang();
  const [busy, setBusy] = useState(false);
  if (!report || !isFinished(report.status) || report.status === "failed" || (report.lang ?? "en") === lang) return null;
  const name = (l: string) => (l === "es" ? t("Spanish") : t("English"));
  const rebuild = async () => {
    setBusy(true);
    try {
      // Reuses a recent report in this language when there is one.
      const r = await createReport(report.ticker);
      navigate(`/reports/${r.id}${window.location.search}`);
    } finally {
      setBusy(false);
    }
  };
  return (
    <p className={className} role="status">
      {t("The AI-written parts of this report are in {lang}.", { lang: name(report.lang ?? "en") })}{" "}
      <button type="button" onClick={() => void rebuild()} disabled={busy} className="font-semibold underline">
        {busy ? t("Starting…") : t("Get it in {lang}", { lang: name(getLang()) })}
      </button>
    </p>
  );
}
