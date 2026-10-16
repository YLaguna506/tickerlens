// Report data shared by every design: live loading and small lookups.

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { getReport, isFinished, type Metric, type MetricsReport, type Quality, type ReportBody } from "./api";

export const STAGES = [
  { id: "ingesting", label: "Fetching SEC filings, prices and estimates" },
  { id: "computing", label: "Computing metrics, phase and scores" },
  { id: "analyzing", label: "AI analysts reading the 10-K" },
  { id: "scoring", label: "Deciding the verdict" },
];

/** Live progress: an EventSource that refreshes the report on every event. */
export function useLiveReport(id: string) {
  const client = useQueryClient();
  const query = useQuery({ queryKey: ["report", id], queryFn: () => getReport(id) });
  const running = query.data ? !isFinished(query.data.status) : true;

  useEffect(() => {
    if (!running) return;
    const es = new EventSource(`/api/reports/${id}/events`);
    const refresh = () => void client.invalidateQueries({ queryKey: ["report", id] });
    const types = ["stage.started", "stage.completed", "section.ready", "section.failed", "job.done", "job.failed"];
    types.forEach((t) => es.addEventListener(t, refresh));
    es.addEventListener("job.done", () => es.close());
    es.addEventListener("job.failed", () => es.close());
    return () => es.close();
  }, [id, running, client]);

  return query;
}

/** True while an AI section has not reported back yet. */
export const isPending = (body: ReportBody | undefined, key: string) => !body || body.sections?.[key] === undefined;

/** The last five fiscal years plus the latest twelve months. */
export function incomeRows(body?: ReportBody) {
  const fin = body?.financials;
  return [
    ...(fin?.annual ?? []).slice(-5).map((p) => ({ label: `FY${String(p.fiscalYear).slice(2)}`, revenue: p.revenue, netIncome: p.netIncome })),
    ...(fin?.ttm ? [{ label: "LTM", revenue: fin.ttm.revenue, netIncome: fin.ttm.netIncome }] : []),
  ];
}

/** Any metric in the report by key, e.g. "operatingMargin" or "revenueCagr5y". */
export function metricOf(m: MetricsReport, key: string): Metric | undefined {
  return [m.marketCap, ...m.profitability, ...m.growth, ...m.health, ...m.valuation, ...m.returns].find((x) => x.key === key);
}

/** The multiple the valuation was judged on: the phase's choice, unless its
 * history was missing or distorted. */
export const valuedWith = (m: MetricsReport) => m.valuationBands?.metric ?? m.phase.valuationMetric;

/** "P/E has a distorted history: …", when the phase's metric was not used. */
export const fallbackNote = (m: MetricsReport) => {
  const b = m.valuationBands;
  return b?.fellBackFrom ? `${b.fellBackFrom} ${b.fallbackReason ?? "was not usable"}, so ${b.metric} is used instead.` : "";
};

/** The four business-quality questions, in report order. */
export const QUALITY_QUESTIONS: { key: keyof Quality; question: string; short: string; options: [string, string, string] }[] = [
  { key: "revenuePredictability", question: "How predictable is revenue?", short: "Predictability", options: ["Unpredictable", "Modest", "Predictable"] },
  { key: "pricingPower", question: "Can the company raise prices?", short: "Pricing power", options: ["No", "Sometimes", "Easily"] },
  { key: "recessionResistance", question: "How recession-proof is it?", short: "Recession", options: ["Weak", "Okay", "Strong"] },
  { key: "competitivePosition", question: "What is its competitive position?", short: "Position", options: ["Weak", "Average", "Dominant"] },
];

/** Words for a 1–5 score, lowest first. */
export const SCORE_WORDS = {
  growth: ["Low", "Below avg", "Average", "Above avg", "High"],
  health: ["Weak", "Below avg", "Fair", "Good", "Strong"],
  quality: ["Poor", "Below avg", "Decent", "Good", "Excellent"],
  valuation: ["Very expensive", "Expensive", "Fair value", "Attractive", "Very attractive"],
};

export const scoreWord = (words: string[], v: number | null) =>
  v == null ? "n/a" : words[Math.min(Math.max(Math.round(v), 1), 5) - 1]!;
