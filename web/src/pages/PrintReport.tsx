// The report laid out for paper: the Classic sections in the light palette,
// at the width of a printed page. Opening it starts the browser's print
// dialog, where "Save as PDF" makes the file.

import { useEffect, useRef } from "react";
import { isFinished } from "../api";
import { ChartTheme } from "../chartTheme";
import { PriceChart } from "../components/charts";
import { ManagementPanel, MoatPanel, RiskPanel } from "../components/deepSections";
import { BusinessSection, FinancialsSection, PhaseSection, ValuationSection, VerdictSection } from "../components/sections";
import { EarningsPill } from "../components/NextEarnings";
import { Section } from "../components/ui";
import { price, shortDate, signedPct, titleCase, usd } from "../format";
import { t, tx } from "../i18n";
import { STAGES, useLiveReport } from "../live";
import { navigate } from "../router";

const VERDICT_STYLE: Record<string, string> = {
  Buy: "bg-good text-white",
  Watch: "bg-good-soft text-good-strong",
  Hold: "bg-mid text-[#1a1404]",
  Pass: "bg-bad-soft text-bad",
  Sell: "bg-bad text-white",
};

export function PrintReport({ id }: { id: string }) {
  const { data: report, error } = useLiveReport(id);
  const body = report?.report;
  const m = body?.metrics;
  const v = body?.verdict;
  const ready = Boolean(report && isFinished(report.status) && body);
  const printed = useRef(false);
  const auto = new URLSearchParams(window.location.search).get("auto") !== "0";

  // The file name comes from the page title; print once everything is drawn.
  useEffect(() => {
    if (!report) return;
    const day = (report.createdAt ?? "").slice(0, 10);
    document.title = `${report.ticker} – TickerLens – ${day}`;
    if (!ready || !auto || printed.current) return;
    printed.current = true;
    const timer = setTimeout(() => window.print(), 900);
    return () => clearTimeout(timer);
  }, [report, ready, auto]);

  if (error || (!report && !error)) {
    return <p className="p-10 text-center text-muted">{error ? t("Report not found.") : t("Loading…")}</p>;
  }
  const stage = STAGES.findIndex((s) => s.id === report!.stage);
  const name = titleCase(body?.company.name || report!.companyName || report!.ticker);

  return (
    <ChartTheme.Provider value="light">
      <div className="look-light print-report min-h-screen bg-white">
        <div className="no-print sticky top-0 z-10 border-b border-line bg-white/95 backdrop-blur">
          <div className="mx-auto flex max-w-[700px] flex-wrap items-center justify-between gap-3 px-2 py-3">
            <button onClick={() => navigate(`/reports/${id}`)} className="text-sm font-semibold text-muted hover:text-ink">
              ← {t("Back to the report")}
            </button>
            <div className="flex items-center gap-3">
              <span className="hidden text-xs text-muted sm:inline">{t("Choose “Save as PDF” as the printer.")}</span>
              <button onClick={() => window.print()} disabled={!ready} className="rounded-full bg-good px-4 py-2 text-sm font-semibold text-white disabled:opacity-40">
                {ready ? t("Save as PDF") : t("Building…")}
              </button>
            </div>
          </div>
        </div>

        <main className="mx-auto max-w-[700px] px-2 pb-10">
          {!ready && (
            <p className="no-print py-6 text-center text-sm text-muted">
              {t("The report is still being built")}
              {stage >= 0 ? ` · ${t(STAGES[stage]!.label)}` : ""}
            </p>
          )}

          <header className="space-y-3 border-b-2 border-ink pt-6 pb-5">
            <p className="flex justify-between text-xs font-semibold tracking-[0.12em] text-muted uppercase">
              <span>TickerLens</span>
              <span>{shortDate(report!.createdAt.slice(0, 10))}</span>
            </p>
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div>
                <h1 className="font-display text-4xl font-extrabold tracking-tight">{name}</h1>
                <p className="text-muted">
                  {report!.ticker}
                  {m?.industry && <> · {titleCase(m.industry.sicDescription)}</>}
                </p>
              </div>
              {m?.price != null && (
                <p className="text-right">
                  <span className="font-display text-3xl font-bold">{price(m.price)}</span>
                  <span className="block text-xs text-muted">{t("prices as of {date}", { date: shortDate(m.priceDate) })}</span>
                </p>
              )}
            </div>
            {v?.label && (
              <div className="flex flex-wrap items-center gap-3">
                <span className={`rounded-full px-4 py-1.5 font-display text-lg font-bold ${VERDICT_STYLE[v.label] ?? ""}`}>{t(v.label)}</span>
                <span className="font-semibold">{v.tagline || tx(v.meaning)}</span>
              </div>
            )}
            <dl className="grid grid-cols-3 gap-2 text-sm sm:grid-cols-6">
              <Stat label={t("Business")} value={v?.business != null ? `${v.business.toFixed(1)}/5` : "–"} />
              <Stat label={t("Valuation")} value={v?.valuation != null ? `${v.valuation}/5` : "–"} />
              <Stat label={t("Confidence")} value={v ? `${v.confidence}%` : "–"} />
              <Stat label={t("Market cap")} value={usd(m?.marketCap.value)} />
              <Stat label={t("5 years")} value={signedPct(m?.priceChange5y.value, 0)} />
              <Stat label={t("Phase")} value={m ? tx(m.phase.name) : "–"} />
            </dl>
            {m?.nextEarnings && <EarningsPill n={m.nextEarnings} className="!text-xs" />}
            {v?.rationale && <p className="text-sm leading-relaxed">{v.rationale}</p>}
          </header>

          {m && m.charts.price.length > 1 && (
            <div className="card mt-6">
              <PriceChart points={m.charts.price} />
            </div>
          )}

          <BusinessSection body={body} />
          <Section id="moat" title={t("Moat")}>
            <MoatPanel body={body} report={report} />
          </Section>
          <FinancialsSection m={m} body={body} />
          <PhaseSection m={m} body={body} />
          <ValuationSection m={m} />
          <Section id="risk" title={t("Risk")}>
            <RiskPanel body={body} m={m} report={report} />
          </Section>
          <Section id="management" title={t("Management")}>
            <ManagementPanel body={body} m={m} report={report} />
          </Section>
          <VerdictSection body={body} m={m} />

          <footer className="mt-10 space-y-1 border-t border-line pt-4 text-xs text-muted">
            {m && (
              <p>
                {t("Financials through {date}", { date: shortDate(m.financialsThrough) })}
                {body?.filing && <> · {t("latest 10-K for the period ended {date}", { date: shortDate(body.filing.reportDate) })}</>} ·{" "}
                {t("scoring {version}", { version: m.scoringVersion })}
              </p>
            )}
            {body?.filing?.url && <p>{t("10-K")}: {body.filing.url}</p>}
            {body?.management?.proxy?.url && <p>{t("Proxy statement")}: {body.management.proxy.url}</p>}
            <p>{t("Report {id}, built {date}.", { id, date: new Date(report!.createdAt).toLocaleString() })}</p>
            <p className="pt-2 font-semibold text-ink">{t("Educational project. Not financial advice.")}</p>
            <p>{t("TickerLens builds reports from SEC filings and market data. Nothing here is a recommendation to buy or sell any security.")}</p>
          </footer>
        </main>
      </div>
    </ChartTheme.Provider>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-paper px-2.5 py-1.5">
      <dt className="text-[11px] text-muted">{label}</dt>
      <dd className="font-semibold tabular-nums">{value}</dd>
    </div>
  );
}
