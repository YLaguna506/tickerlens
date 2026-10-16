// Small building blocks shared across the report.

import type { ReactNode } from "react";
import type { Evidence, Metric } from "../api";
import { metricValue } from "../format";

export function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section id={id} className="scroll-mt-24 space-y-5 pt-10">
      <h2 className="section-title border-b border-line pb-4">{title}</h2>
      {children}
    </section>
  );
}

/** 1–5 scale with labelled steps, the chosen one highlighted. */
export function StepScale({ value, labels, tone = "good" }: { value: number | null; labels: string[]; tone?: "good" | "mid" }) {
  const chosen = value == null ? -1 : Math.min(Math.max(Math.round(value), 1), 5) - 1;
  const ring = tone === "good" ? "bg-good ring-good-soft" : "bg-mid ring-mid-soft";
  const text = tone === "good" ? "text-good" : "text-mid";
  return (
    <div className="card" role="img" aria-label={value == null ? "No score" : `${labels[chosen]}, ${chosen + 1} of 5`}>
      <ol className="relative flex justify-between">
        <span className="absolute top-4 right-[10%] left-[10%] h-px bg-line" aria-hidden />
        {labels.map((label, i) => (
          <li key={label} className="relative flex w-1/5 flex-col items-center gap-2 text-center">
            <span
              className={`flex size-8 items-center justify-center rounded-full text-sm font-semibold ${
                i === chosen ? `${ring} text-white ring-4 sm:size-11 sm:text-lg` : "bg-line text-muted"
              }`}
            >
              {i + 1}
            </span>
            <span className={`text-[11px] leading-tight sm:text-sm ${i === chosen ? `font-semibold ${text}` : "text-muted"}`}>
              {label}
            </span>
          </li>
        ))}
      </ol>
    </div>
  );
}

const levelStyles = [
  { icon: "✕", on: "bg-bad text-white", off: "bg-bad-soft text-bad/60" },
  { icon: "–", on: "bg-mid text-[#1a1404]", off: "bg-mid-soft text-mid/70" },
  { icon: "✓", on: "bg-good text-white", off: "bg-good-soft text-good/70" },
];

/** A question answered on a three-level scale (✕ – ✓). */
export function ThreeLevel({ question, options, answer, children }: {
  question: string;
  options: [string, string, string];
  answer?: string;
  children?: ReactNode;
}) {
  const chosen = options.indexOf(answer ?? "");
  return (
    <div className="card flex flex-col gap-4">
      <h3 className="font-display text-lg font-semibold">{question}</h3>
      <div className="flex justify-between border-b border-line pb-4">
        {options.map((o, i) => {
          const style = levelStyles[i]!;
          return (
          <div key={o} className="flex w-1/3 flex-col items-center gap-1.5">
            <span
              className={`flex items-center justify-center rounded-full font-bold ${
                i === chosen ? `size-12 text-xl ${style.on}` : `size-9 ${style.off}`
              }`}
              aria-hidden
            >
              {style.icon}
            </span>
            <span className={`text-sm ${i === chosen ? "font-semibold" : "text-muted"}`}>{o}</span>
          </div>
          );
        })}
      </div>
      {children}
    </div>
  );
}

/** Horizontal 1–5 score bar. */
export function ScoreBar({ value, tone }: { value: number | null; tone: "good" | "mid" | "bad" }) {
  const filled = value == null ? 0 : Math.round(value);
  const color = { good: "bg-good", mid: "bg-mid", bad: "bg-bad" }[tone];
  return (
    <span className="flex gap-1" aria-hidden>
      {[1, 2, 3, 4, 5].map((i) => (
        <span key={i} className={`h-2 w-6 rounded-full sm:w-8 ${i <= filled ? color : "bg-line"}`} />
      ))}
    </span>
  );
}

/** Expandable "how was this calculated?" for a metric. */
export function MetricRow({ m }: { m: Metric }) {
  return (
    <details className="group border-b border-line py-2.5 last:border-0">
      <summary className="flex cursor-pointer list-none items-baseline justify-between gap-3">
        <span className="text-[15px] text-muted group-open:text-ink">{m.label}</span>
        <span className={`font-semibold tabular-nums ${m.value == null ? "text-muted" : ""}`}>{metricValue(m)}</span>
      </summary>
      <div className="mt-2 space-y-1 rounded-lg bg-paper p-3 text-xs text-muted">
        {m.formula && <p><span className="font-semibold text-ink">Formula:</span> {m.formula}</p>}
        {m.period && <p><span className="font-semibold text-ink">Period:</span> {m.period}</p>}
        {m.inputs?.map((i) => (
          <p key={i.name}>
            {i.name}: <span className="tabular-nums">{i.value == null ? "not reported" : formatInput(i.value)}</span>
            {i.period ? ` (${i.period})` : ""}
          </p>
        ))}
        {m.note && <p className="text-mid">{m.note}</p>}
      </div>
    </details>
  );
}

function formatInput(v: number) {
  return Math.abs(v) >= 1e6 ? `$${(v / 1e6).toLocaleString("en-US", { maximumFractionDigits: 0 })}M` : v.toLocaleString("en-US", { maximumFractionDigits: 4 });
}

/** Verbatim filing quotes behind an AI judgment. */
export function EvidenceList({ evidence, filingUrl }: { evidence?: Evidence[]; filingUrl?: string }) {
  if (!evidence?.length) return null;
  return (
    <details className="text-sm">
      <summary className="cursor-pointer text-xs font-semibold text-good">Evidence from the 10-K ({evidence.length})</summary>
      <ul className="mt-2 space-y-2">
        {evidence.map((e, i) => (
          <li key={i} className="border-l-2 border-good-soft pl-3 text-muted">
            <q className="italic">{e.quote}</q>
            <span className="block text-xs">
              {e.documentId.replace("10k-item", "10-K Item ")}
              {filingUrl && (
                <>
                  {" · "}
                  <a href={filingUrl} target="_blank" rel="noreferrer" className="underline">
                    filing
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

export function Skeleton({ lines = 3, label }: { lines?: number; label?: string }) {
  return (
    <div className="card animate-pulse space-y-3" aria-busy="true" aria-label={label ?? "Loading"}>
      {Array.from({ length: lines }, (_, i) => (
        <div key={i} className="h-4 rounded bg-line" style={{ width: `${90 - i * 15}%` }} />
      ))}
      {label && <p className="text-xs text-muted">{label}</p>}
    </div>
  );
}

export function Unavailable({ children }: { children: ReactNode }) {
  return <div className="card text-sm text-muted">{children}</div>;
}

export function Disclaimer() {
  return (
    <footer className="mt-16 border-t border-line py-8 text-center text-xs text-muted">
      <p className="font-semibold text-ink">Educational project. Not financial advice.</p>
      <p className="mt-1">
        TickerLens builds reports from SEC filings and market data. Nothing here is a recommendation to buy or sell any security.
      </p>
    </footer>
  );
}
