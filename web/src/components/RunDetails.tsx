// "How this report was built": time per stage and the AI calls behind it.
// Styled with the current text color so it fits every design.

import type { ReportBody, Usage } from "../api";

const STAGES: [string, string][] = [
  ["ingesting", "Fetch filings, prices and estimates"],
  ["computing", "Compute metrics, phase and scores"],
  ["analyzing", "AI agents read the 10-K"],
  ["scoring", "Verdict and its summary"],
];

const AGENTS: [string, string][] = [
  ["business_overview", "Business overview"],
  ["business_quality", "Business quality"],
  ["segments", "Segments"],
];

const duration = (ms?: number) => (ms == null ? "–" : ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`);
const tokens = (u?: Usage) => (u ? u.inputTokens + u.outputTokens : 0);

interface Call {
  label: string;
  usage?: Usage;
  cached: boolean;
  ms?: number;
}

export function RunDetails({ body }: { body?: ReportBody }) {
  const t = body?.timingsMs;
  if (!t?.total) return null;

  const calls: Call[] = AGENTS.filter(([key]) => body?.agents?.[key as keyof NonNullable<ReportBody["agents"]>]).map(([key, label]) => {
    const s = body!.agents![key as keyof NonNullable<ReportBody["agents"]>]!;
    return { label, usage: s.usage, cached: Boolean(s.cached), ms: t[`agent:${key}`] };
  });
  const v = body?.verdict;
  if (v && v.narrativeStatus !== "skipped") {
    calls.push({ label: "Verdict summary", usage: v.narrativeUsage, cached: !v.narrativeUsage && v.narrativeStatus === "ready", ms: t["agent:verdict_narrative"] });
  }

  const fresh = calls.filter((c) => !c.cached && c.usage);
  const spent = fresh.reduce((n, c) => n + tokens(c.usage), 0);
  const cost = !fresh.length
    ? "no new AI calls"
    : fresh.every((c) => c.usage!.costKnown)
      ? `$${(body?.costUsd ?? 0).toFixed(4)}`
      : "cost not reported by the provider";

  return (
    <details className="text-xs">
      <summary className="cursor-pointer opacity-70 hover:opacity-100">
        Built in {duration(t.total)} · {spent.toLocaleString()} AI tokens · {cost}
      </summary>
      <div className="mt-2 overflow-x-auto">
        <table className="w-full min-w-[30rem] text-left tabular-nums">
          <thead className="opacity-60">
            <tr>
              <th className="py-1 font-semibold">Step</th>
              <th className="py-1 text-right font-semibold">Time</th>
              <th className="py-1 pl-4 font-semibold">Model</th>
              <th className="py-1 text-right font-semibold">Tokens in / out</th>
              <th className="py-1 text-right font-semibold">Cost</th>
            </tr>
          </thead>
          <tbody>
            {STAGES.filter(([key]) => t[key] != null).map(([key, label]) => (
              <tr key={key} className="border-t border-current/10">
                <td className="py-1">{label}</td>
                <td className="text-right">{duration(t[key])}</td>
                <td colSpan={3} />
              </tr>
            ))}
            {calls.map((c) => (
              <tr key={c.label} className="border-t border-current/10 opacity-80">
                <td className="py-1 pl-3">↳ {c.label}</td>
                <td className="text-right">{c.cached ? "cached" : duration(c.ms)}</td>
                <td className="pl-4">{c.usage?.model ?? "–"}</td>
                <td className="text-right">{c.usage ? `${c.usage.inputTokens.toLocaleString()} / ${c.usage.outputTokens.toLocaleString()}` : "–"}</td>
                <td className="text-right">{c.cached ? "–" : c.usage?.costKnown ? `$${c.usage.costUsd.toFixed(4)}` : "n/a"}</td>
              </tr>
            ))}
            <tr className="border-t border-current/20 font-semibold">
              <td className="py-1">Total</td>
              <td className="text-right">{duration(t.total)}</td>
              <td />
              <td className="text-right">{spent.toLocaleString()}</td>
              <td className="text-right">{fresh.length && fresh.every((c) => c.usage!.costKnown) ? `$${(body?.costUsd ?? 0).toFixed(4)}` : "n/a"}</td>
            </tr>
          </tbody>
        </table>
        <p className="mt-1 opacity-60">Cached answers are reused when the same filing was already analyzed with the same prompt and model, so they cost nothing.</p>
      </div>
    </details>
  );
}
