// Types for the API responses (mirrors the Go structs) and fetch helpers.

import { getLang } from "./i18n";
import { getModel } from "./model";

export type Unit = "percent" | "multiple" | "usd" | "usd/share";

export interface Input {
  name: string;
  value: number | null;
  period?: string;
}

export interface Metric {
  key: string;
  label: string;
  value: number | null;
  unit: Unit;
  period?: string;
  formula?: string;
  inputs?: Input[];
  note?: string;
}

export interface Score {
  value: number | null;
  max: number;
  reasons: string[];
}

export interface Point {
  date: string;
  value: number;
}

export interface PhaseCheck {
  question: string;
  answer: boolean | null;
  detail: string;
}

export interface Phase {
  number: number;
  name: string;
  valuationMetric: string;
  lowConfidence: boolean;
  reason: string;
  checks: PhaseCheck[];
}

export interface Bands {
  metric: string;
  fellBackFrom?: string;
  fallbackReason?: string;
  historyFrom: string;
  historyTo: string;
  days: number;
  current: number;
  cheapBelow: number;
  median: number;
  expensiveAbove: number;
  percentile: number;
  zone: "cheap" | "fair" | "expensive";
  perShareBasis: string;
  perShare: number;
  basisPeriod: string;
  price: number;
  cheapPrice: number;
  medianPrice: number;
  expensivePrice: number;
}

export interface MetricsReport {
  ticker: string;
  name: string;
  scoringVersion: string;
  priceDate: string;
  price: number | null;
  financialsThrough: string;
  latestFiscalYear: number;
  marketCap: Metric;
  profitability: Metric[];
  growth: Metric[];
  health: Metric[];
  valuation: Metric[];
  returns: Metric[];
  priceChange5y: Metric;
  totalReturn5y: Metric;
  earnings: { quarters: number; beats: number; avgSurprisePercent: number | null; latestQuarter?: string };
  capitalAllocation?: CapitalAllocation;
  phase: Phase;
  growthScore: Score;
  healthScore: Score;
  valuationScore: Score;
  valuationBands: Bands | null;
  charts: { price: Point[]; revenueFY: Point[]; netIncomeFY: Point[]; valuation1y: Point[] };
  notes?: string[];
}

export interface FinancialPeriod {
  periodType: "FY" | "Q" | "TTM";
  fiscalYear: number;
  fiscalQuarter?: number;
  endDate: string;
  revenue: number | null;
  netIncome: number | null;
}

export interface Evidence {
  claim: string;
  documentId: string;
  quote: string;
}

export interface Usage {
  model: string;
  inputTokens: number;
  outputTokens: number;
  costUsd: number;
  costKnown: boolean;
}

export interface AgentSection<T> {
  status: "ready" | "failed";
  output?: T;
  evidence?: Evidence[];
  errors?: string[];
  usage?: Usage;
  cached?: boolean;
}

export interface Overview {
  mission: string | null;
  whatItDoes: string;
  howItMakesMoney: string;
  payingCustomers: string;
  whyTheyBuy: string;
}

export interface QualityAnswer {
  answer: string;
  justification: string;
  evidence: Evidence[];
}

export type Quality = Record<
  "revenuePredictability" | "pricingPower" | "recessionResistance" | "competitivePosition",
  QualityAnswer
>;

export interface CashUse {
  key: "capex" | "acquisitions" | "dividends" | "buybacks" | "debtPaydown";
  label: string;
  amount: number;
  share: number | null;
}

export interface CapitalAllocation {
  from: string;
  to: string;
  years: number;
  operatingCashFlow: number;
  uses: CashUse[];
  shareCountChange: number | null;
  notes?: string[];
}

export interface MoatSourceAnswer {
  rating: "None" | "Weak" | "Moderate" | "Strong";
  direction: "Narrowing" | "Stable" | "Widening";
  justification: string;
  evidence: Evidence[];
}

export interface MoatOutput {
  width: string;
  direction: string;
  summary: string;
  sources: Record<"switchingCosts" | "networkEffects" | "intangibleAssets" | "costAdvantage" | "counterPositioning", MoatSourceAnswer>;
}

export type RiskOutput = Record<"revenueDiversification" | "disruptionThreat" | "externalExposure", QualityAnswer>;

export interface ManagementOutput {
  ceoName: string;
  ceoSince: number | null;
  notes: string;
}

export interface PeerCompany {
  cik: string;
  ticker?: string;
  name: string;
  revenue: number;
  revenueGrowth: number | null;
  grossMargin: number | null;
  operatingMargin: number | null;
  netMargin: number | null;
}

export interface PeerGroup {
  sic: string;
  industry: string;
  basis: string;
  year: number;
  company: PeerCompany | null;
  peers: PeerCompany[];
  medians: Omit<PeerCompany, "cik" | "ticker" | "name" | "revenue"> & { revenue: number | null };
  rank?: number;
  of: number;
}

export interface SegmentShare {
  name: string;
  revenue: number;
  share: number;
  priorRevenue: number | null;
  yoy: number | null;
}

export interface Verdict {
  label: "Buy" | "Watch" | "Hold" | "Pass" | "Sell" | "";
  meaning: string;
  business: number | null;
  valuation: number | null;
  components: { name: string; score: number | null; weight: number }[];
  rule: string;
  confidence: number;
  confidenceLevel: "high" | "medium" | "low";
  confidenceReasons: string[];
  scoringVersion: string;
  tagline?: string;
  rationale?: string;
  narrativeStatus: string;
  narrativeUsage?: Usage;
}

export interface EarningsQuarter {
  fiscalDateEnding: string;
  reportedEPS: number | null;
  estimatedEPS: number | null;
  surprisePercent: number | null;
}

export interface ReportBody {
  version: string;
  company: { ticker: string; cik: string; name: string };
  sections: Record<string, "ready" | "failed" | "pending">;
  metrics?: MetricsReport;
  financials?: { annual: FinancialPeriod[]; quarterly: FinancialPeriod[]; ttm: FinancialPeriod | null };
  earnings?: EarningsQuarter[];
  notes?: string[];
  warnings?: string[];
  timingsMs?: Record<string, number>;
  costUsd?: number;
  filing?: { accession: string; reportDate: string; url: string };
  agents?: {
    business_overview?: AgentSection<Overview>;
    business_quality?: AgentSection<Quality>;
    segments?: AgentSection<unknown>;
    moat?: AgentSection<MoatOutput>;
    risk?: AgentSection<RiskOutput>;
    management?: AgentSection<ManagementOutput>;
  };
  businessQuality?: { score: number; points: number; answers: Record<string, string> };
  segments?: { fiscalYear: string; segments?: SegmentShare[]; geography?: SegmentShare[] };
  peers?: PeerGroup;
  moat?: { width: number; widthName: string; direction: number; directionName: string; sources: Record<string, { rating: string; direction: string }> };
  risk?: { score: number; answers: Record<string, string>; points: Record<string, number> };
  management?: {
    ceoName?: string;
    ceoSince?: number | null;
    tenureYears?: number | null;
    sharesOwned?: number | null;
    ownedValueUsd?: number | null;
    percentOfClass?: number | null;
    lessThanOnePercent?: boolean;
    directorsAndOfficersPercent?: number | null;
    notes?: string;
    proxy?: { accession: string; reportDate: string; url: string };
  };
  verdict?: Verdict;
}

export type Status = "queued" | "running" | "done" | "partial" | "failed";

export interface Report {
  id: string;
  ticker: string;
  companyName: string;
  lang?: "en" | "es";
  model?: string;
  status: Status;
  stage: string;
  error?: string;
  failedSections: string[];
  report?: ReportBody;
  createdAt: string;
}

export interface Company {
  ticker: string;
  cik: string;
  name: string;
}

export class ApiError extends Error {
  status: number;
  suggestions: Company[];
  constructor(message: string, status: number, suggestions: Company[] = []) {
    super(message);
    this.status = status;
    this.suggestions = suggestions;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, init);
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new ApiError(body.error ?? `Request failed (${resp.status})`, resp.status, body.suggestions ?? []);
  }
  return body as T;
}

export const searchTickers = (q: string) => request<Company[]>(`/api/tickers?q=${encodeURIComponent(q)}`);

/** Starts a report in the current language with the chosen AI model. */
export const createReport = (ticker: string, refresh = false) =>
  request<{ id: string; status: Status; cached: boolean }>("/api/reports", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ticker, refresh, lang: getLang(), model: getModel() || undefined }),
  });

export interface ModelChoice {
  id: string;
  provider: string;
  model: string;
  default: boolean;
}

export const listModels = () => request<ModelChoice[]>("/api/models");

export const getReport = (id: string) => request<Report>(`/api/reports/${id}`);

/** Deletes a ticker's saved reports and cached data (filings, prices, AI answers). */
export const clearTickerCache = (ticker: string) =>
  request<{ reports: number; cacheEntries: number }>(`/api/tickers/${encodeURIComponent(ticker)}/cache`, { method: "DELETE" });

export const isFinished = (s: Status) => s === "done" || s === "partial" || s === "failed";

// --- Watchlist ---------------------------------------------------------------------

export interface WatchSummary {
  reportId: string;
  status: "done" | "partial";
  lang: string;
  createdAt: string;
  finishedAt?: string;
  verdict?: Verdict["label"];
  business: number | null;
  valuation: number | null;
  price: number | null;
  priceDate?: string;
  phase?: string;
  moat?: string;
  riskScore: number | null;
  bands: {
    metric: string;
    zone: "cheap" | "fair" | "expensive";
    percentile: number;
    current: number;
    cheapPrice: number;
    medianPrice: number;
    expensivePrice: number;
  } | null;
  noBandsWhy?: string;
}

export interface WatchItem {
  ticker: string;
  companyName: string;
  addedAt: string;
  latest: WatchSummary | null;
  running?: { id: string; stage: string };
}

export const getWatchlist = () => request<WatchItem[]>("/api/watchlist");

export const watchStatus = (ticker: string) => request<{ ticker: string; watching: boolean }>(`/api/watchlist/${encodeURIComponent(ticker)}`);

export const addToWatchlist = (ticker: string) =>
  request<{ ticker: string; watching: boolean }>("/api/watchlist", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ticker }),
  });

export const removeFromWatchlist = (ticker: string) =>
  request<{ ticker: string; watching: boolean }>(`/api/watchlist/${encodeURIComponent(ticker)}`, { method: "DELETE" });

/** Starts new reports for stale tickers (at most five at a time). */
export const refreshWatchlist = () =>
  request<{ started: string[]; waiting: string[] }>("/api/watchlist/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ lang: getLang(), model: getModel() || undefined }),
  });
