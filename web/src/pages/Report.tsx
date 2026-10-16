import { useEffect, useState } from "react";
import { createReport, isFinished, type Report as ReportT } from "../api";
import { PriceChart } from "../components/charts";
import { DesignSwitch } from "../components/DesignSwitch";
import { RunDetails } from "../components/RunDetails";
import { BusinessSection, FinancialsSection, PhaseSection, ValuationSection, VerdictSection } from "../components/sections";
import { Disclaimer } from "../components/ui";
import { price, shortDate, signedPct, titleCase } from "../format";
import { STAGES, useLiveReport } from "../live";
import { navigate } from "../router";

const TABS = [
  { id: "business", label: "Business" },
  { id: "financials", label: "Financials" },
  { id: "phase", label: "Phase" },
  { id: "valuation", label: "Valuation" },
  { id: "verdict", label: "Verdict" },
];

// Highlights the tab of the section in view: the last section whose top has
// scrolled past a third of the screen. Subscribes once the sections exist.
function useActiveTab(ready: boolean) {
  const [active, setActive] = useState("business");
  useEffect(() => {
    if (!ready) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      const line = window.innerHeight / 3;
      let current = TABS[0]!.id;
      for (const t of TABS) {
        const el = document.getElementById(t.id);
        if (el && el.getBoundingClientRect().top <= line) current = t.id;
      }
      setActive(current);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [ready]);
  return active;
}
function Progress({ report }: { report: ReportT }) {
  const current = STAGES.findIndex((s) => s.id === report.stage);
  return (
    <div className="card fade-in space-y-3" aria-live="polite">
      <p className="font-display text-lg font-semibold">Building the report…</p>
      <ol className="space-y-2">
        {STAGES.map((s, i) => {
          const state = report.status === "queued" ? "todo" : i < current ? "done" : i === current ? "now" : "todo";
          return (
            <li key={s.id} className="flex items-center gap-3">
              <span
                className={`flex size-6 items-center justify-center rounded-full text-xs font-bold ${
                  state === "done" ? "bg-good text-white" : state === "now" ? "animate-pulse bg-good-soft text-good ring-2 ring-good" : "bg-[#ece9e1] text-muted"
                }`}
              >
                {state === "done" ? "✓" : i + 1}
              </span>
              <span className={state === "todo" ? "text-muted" : ""}>{s.label}</span>
            </li>
          );
        })}
      </ol>
      <p className="text-xs text-muted">Sections appear as soon as they are ready. The AI step can take a few minutes on a free model.</p>
    </div>
  );
}

export function Report({ id }: { id: string }) {
  const { data: report, error, isLoading } = useLiveReport(id);
  const active = useActiveTab(Boolean(report && report.status !== "failed"));
  const body = report?.report;
  const m = body?.metrics;
  const name = titleCase(body?.company.name || report?.companyName || report?.ticker || "");
  const running = report ? !isFinished(report.status) : false;
  const verdict = body?.verdict?.label;

  if (isLoading) return <p className="p-10 text-center text-muted">Loading…</p>;
  if (error || !report) {
    return (
      <div className="mx-auto max-w-xl p-10 text-center">
        <p className="font-semibold">Report not found.</p>
        <button className="mt-4 underline" onClick={() => navigate("/")}>Back to search</button>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-5xl px-4">
      <nav className="sticky top-0 z-20 -mx-4 flex items-center gap-2 overflow-x-auto [scrollbar-width:none] border-b border-line bg-paper/95 px-4 py-3 backdrop-blur">
        <button onClick={() => navigate("/")} className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-navy font-display text-xs font-extrabold text-white" title="New search">
          {report.ticker.slice(0, 4)}
        </button>
        {TABS.map((t) => (
          <a
            key={t.id}
            href={`#${t.id}`}
            className={`shrink-0 rounded-full px-4 py-2 font-display text-sm font-semibold ${active === t.id ? "bg-good text-white" : "text-muted hover:text-ink"}`}
          >
            {t.label}
          </a>
        ))}
      </nav>

      <div className="flex justify-end pt-4">
        <DesignSwitch look="classic" />
      </div>
      <header className="pt-6 text-center">
        <span className="inline-flex size-16 items-center justify-center rounded-2xl bg-navy font-display text-lg font-extrabold text-white">
          {report.ticker.slice(0, 4)}
        </span>
        <h1 className="mt-5 font-display text-4xl font-extrabold tracking-tight sm:text-6xl">
          {name}
          {m?.price != null && <span className="block text-3xl text-muted sm:text-4xl">{price(m.price)}</span>}
        </h1>
        {verdict && (
          <a href="#verdict" className="mt-4 inline-block rounded-full bg-good px-5 py-2 font-display font-semibold text-white">
            Verdict: {verdict}
          </a>
        )}
      </header>

      {report.status === "failed" && (
        <div className="card mt-8 bg-bad-soft" role="alert">
          <p className="font-semibold">This report could not be built.</p>
          <p className="text-sm">{report.error}</p>
          <button className="mt-3 rounded-lg bg-ink px-4 py-2 text-sm text-white" onClick={() => void createReport(report.ticker, true).then((r) => navigate(`/reports/${r.id}`))}>
            Try again
          </button>
        </div>
      )}

      {running && (
        <div className="mt-8">
          <Progress report={report} />
        </div>
      )}

      {m && m.charts.price.length > 1 && (
        <div className="card fade-in mt-8">
          <PriceChart points={m.charts.price} />
          <p className="text-center">
            <span className={`font-display text-xl font-bold ${(m.priceChange5y.value ?? 0) >= 0 ? "text-good" : "text-bad"}`}>
              {signedPct(m.priceChange5y.value, 0)}
            </span>{" "}
            <span className="text-muted">over 5 years</span>
          </p>
        </div>
      )}

      {report.status !== "failed" && (
        <>
          <BusinessSection body={body} />
          <FinancialsSection m={m} body={body} />
          <PhaseSection m={m} body={body} />
          <ValuationSection m={m} />
          <VerdictSection body={body} m={m} />
        </>
      )}

      {m && (
        <p className="mt-10 text-center text-xs text-muted">
          Financials through {shortDate(m.financialsThrough)}
          {body?.filing && <> · latest 10-K for the period ended {shortDate(body.filing.reportDate)}</>} · prices as of {shortDate(m.priceDate)}
          {report.status === "partial" && <> · some sections are missing ({report.failedSections.join(", ")})</>}
        </p>
      )}
      <div className="mx-auto mt-4 max-w-2xl text-muted">
        <RunDetails body={body} />
      </div>
      <Disclaimer />
    </div>
  );
}
