import { useQueries, useQuery } from "@tanstack/react-query";
import { useEffect, useState, type ReactNode } from "react";
import { createReport, getReport, isFinished, listReports, searchTickers, type Metric, type MetricsReport, type Report } from "../api";
import { LangSwitch, ModelSwitch } from "../components/Controls";
import { Disclaimer } from "../components/ui";
import { ZoneBar } from "../components/ZoneBar";
import { metricValue, pct, price, shortDate, titleCase, usd } from "../format";
import { t, tx } from "../i18n";
import { EXPECTATION_LABEL, SCORE_WORDS, STAGES, scoreWord } from "../live";
import { navigate } from "../router";

const MAX_COMPARE = 3;

const VERDICT_STYLE: Record<string, string> = {
  Buy: "bg-good text-white",
  Watch: "bg-good-soft text-good-strong",
  Hold: "bg-mid text-[#1a1404]",
  Pass: "bg-bad-soft text-bad",
  Sell: "bg-bad text-white",
};
const ZONE_STYLE = { cheap: "text-good", fair: "text-mid", expensive: "text-bad" };

// Which way is better for each metric; others are not ranked.
const HIGHER = new Set([
  "grossMargin", "operatingMargin", "netMargin", "fcfMargin",
  "revenueCagr1y", "revenueCagr3y", "revenueCagr5y", "epsCagr3y", "epsCagr5y", "fcfCagr5y",
  "interestCoverage", "fcfToDebt", "cashAndInvestments",
  "dividendYield", "buybackYield", "shareholderYield",
]); // prettier-ignore
const LOWER = new Set(["debtToEquity", "totalDebt", "pe", "ps", "pb", "pfcf"]);

/** The metric rows compared, by group, in report order. */
const GROUPS: { title: string; pick: (m: MetricsReport) => Metric[]; keys?: string[] }[] = [
  { title: "Profitability", pick: (m) => m.profitability },
  { title: "Growth", pick: (m) => m.growth, keys: ["revenueCagr1y", "revenueCagr3y", "revenueCagr5y", "epsCagr3y", "epsCagr5y", "fcfCagr5y"] },
  { title: "Financial health", pick: (m) => m.health },
  { title: "Valuation", pick: (m) => m.valuation },
  { title: "Shareholder returns", pick: (m) => m.returns },
];

function tickersFromURL(): string[] {
  const raw = new URLSearchParams(window.location.search).get("t") ?? "";
  return [...new Set(raw.split(",").map((s) => s.trim().toUpperCase()).filter(Boolean))].slice(0, MAX_COMPARE);
}

/** The latest finished report for a ticker, or a new one when there is none
 * (followed until it finishes). */
async function latestReport(ticker: string): Promise<Report> {
  const list = await listReports(ticker);
  const done = list.find((r) => r.status === "done" || r.status === "partial");
  const running = list.find((r) => !isFinished(r.status));
  const id = done?.id ?? running?.id ?? (await createReport(ticker)).id;
  return getReport(id);
}

export function Compare() {
  const [tickers, setTickers] = useState(tickersFromURL);
  useEffect(() => {
    const url = tickers.length ? `/compare?t=${tickers.join(",")}` : "/compare";
    window.history.replaceState(null, "", url);
  }, [tickers]);

  const results = useQueries({
    queries: tickers.map((tk) => ({
      queryKey: ["compare", tk],
      queryFn: () => latestReport(tk),
      // Follow a report that is still being built.
      refetchInterval: (q: { state: { data?: Report } }) => (q.state.data && !isFinished(q.state.data.status) ? 4000 : false),
    })),
  });

  const add = (tk: string) => setTickers((l) => (l.includes(tk) || l.length >= MAX_COMPARE ? l : [...l, tk]));
  const remove = (tk: string) => setTickers((l) => l.filter((x) => x !== tk));
  const cols = tickers.map((tk, i) => ({ ticker: tk, q: results[i]! }));
  const reports = cols.map((c) => c.q.data);
  const ms = reports.map((r) => r?.report?.metrics);

  return (
    <div className="mx-auto min-h-screen max-w-6xl px-4">
      <header className="flex flex-wrap items-center justify-between gap-2 pt-4">
        <span className="flex gap-4 text-sm font-semibold text-muted">
          <button onClick={() => navigate("/")} className="hover:text-ink">
            ← {t("New search")}
          </button>
          <button onClick={() => navigate("/watchlist")} className="hover:text-ink">
            <span className="text-amber-400">★</span> {t("Watchlist")}
          </button>
        </span>
        <div className="flex flex-wrap items-center gap-2">
          <ModelSwitch look="classic" />
          <LangSwitch look="classic" />
        </div>
      </header>

      <div className="flex flex-wrap items-end justify-between gap-4 pt-8">
        <div>
          <h1 className="section-title">{t("Compare")}</h1>
          <p className="mt-2 text-muted">{t("Up to three companies side by side, from each one's latest report.")}</p>
        </div>
        {tickers.length < MAX_COMPARE && <AddCompany onAdd={add} exclude={tickers} />}
      </div>

      {tickers.length === 0 ? (
        <div className="card mt-8 text-center text-muted">
          <p className="font-display text-lg font-semibold text-ink">{t("Pick two or three companies to compare.")}</p>
          <p className="mt-1 text-sm">{t("Add them above, select them on the watchlist, or press ⇄ at the top of a report.")}</p>
        </div>
      ) : (
        <div className="card mt-8 overflow-x-auto !p-0">
          <table className="w-full min-w-[640px] border-collapse text-sm">
            <colgroup>
              <col className="w-44 sm:w-52" />
              {cols.map((c) => (
                <col key={c.ticker} />
              ))}
            </colgroup>
            <thead>
              <tr className="border-b border-line align-top">
                <th className="sticky left-0 bg-card" />
                {cols.map((c) => (
                  <th key={c.ticker} className="px-4 py-4 text-left font-normal">
                    <Header ticker={c.ticker} report={c.q.data} error={c.q.error} onRemove={() => remove(c.ticker)} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              <Group title={t("Verdict")} n={cols.length} />
              <Row label={t("Verdict")}>
                {reports.map((r, i) => {
                  const v = r?.report?.verdict;
                  return (
                    <Cell key={i}>
                      {v?.label ? <span className={`rounded-full px-3 py-1 text-sm font-bold ${VERDICT_STYLE[v.label] ?? ""}`}>{t(v.label)}</span> : "–"}
                      {v?.label && <p className="mt-1.5 text-xs text-muted">{tx(v.rule)}</p>}
                    </Cell>
                  );
                })}
              </Row>
              <ScoreRow label={t("Business quality")} words={SCORE_WORDS.quality} values={reports.map((r) => r?.report?.businessQuality?.score ?? null)} />
              <ScoreRow label={t("Growth")} words={SCORE_WORDS.growth} values={ms.map((m) => m?.growthScore.value ?? null)} />
              <ScoreRow label={t("Financial health")} words={SCORE_WORDS.health} values={ms.map((m) => m?.healthScore.value ?? null)} />
              <ScoreRow label={t("Valuation")} words={SCORE_WORDS.valuation} values={ms.map((m) => m?.valuationScore.value ?? null)} />

              <Group title={t("Price against its history")} n={cols.length} />
              <Row label={t("Where the price sits")}>
                {ms.map((m, i) => {
                  const b = m?.valuationBands;
                  return (
                    <Cell key={i}>
                      {b && m?.price != null ? (
                        <>
                          <p className={`font-semibold ${ZONE_STYLE[b.zone]}`}>
                            {t({ cheap: "Cheap", fair: "Typical", expensive: "Expensive" }[b.zone])} · {b.metric} {b.current.toFixed(1)} {t("vs typical {x}", { x: b.median.toFixed(1) })}
                          </p>
                          <ZoneBar price={m.price} b={b} />
                        </>
                      ) : (
                        <span className="text-xs text-muted">{m ? tx(m.valuationScore.reasons[0] ?? "") || "–" : "–"}</span>
                      )}
                    </Cell>
                  );
                })}
              </Row>
              <Row label={t("Market cap")}>
                {ms.map((m, i) => (
                  <Cell key={i}>{usd(m?.marketCap.value)}</Cell>
                ))}
              </Row>
              <Row label={t("Growth the price implies")}>
                {ms.map((m, i) => {
                  const d = m?.reverseDcf;
                  return (
                    <Cell key={i}>
                      {d?.impliedGrowth != null ? (
                        <>
                          {pct(d.impliedGrowth, 1)} {t("a year")}
                          {d.expectations && <span className="block text-xs text-muted">{t(EXPECTATION_LABEL[d.expectations]!)}</span>}
                        </>
                      ) : (
                        "–"
                      )}
                    </Cell>
                  );
                })}
              </Row>
              <Row label={t("Phase")}>
                {ms.map((m, i) => (
                  <Cell key={i}>{m ? tx(m.phase.name) : "–"}</Cell>
                ))}
              </Row>

              {GROUPS.map((g) => (
                <MetricGroup key={g.title} title={t(g.title)} ms={ms} pick={g.pick} keys={g.keys} />
              ))}

              <Group title={t("Moat, risk and management")} n={cols.length} />
              <Row label={t("Moat")}>
                {reports.map((r, i) => {
                  const mo = r?.report?.moat;
                  return <Cell key={i}>{mo ? `${t(mo.widthName)} · ${t(mo.directionName)}` : "–"}</Cell>;
                })}
              </Row>
              <Row label={t("Risk score")}>
                {reports.map((r, i) => {
                  const s = r?.report?.risk?.score;
                  const b = best(reports.map((x) => x?.report?.risk?.score ?? null), "higher");
                  return (
                    <Cell key={i} best={b.includes(i)}>
                      {s != null ? `${s.toFixed(1)}/5` : "–"}
                    </Cell>
                  );
                })}
              </Row>
              <Row label={t("Chief executive officer")}>
                {reports.map((r, i) => {
                  const mg = r?.report?.management;
                  return (
                    <Cell key={i}>
                      {mg?.ceoName ? (
                        <>
                          {mg.ceoName}
                          {mg.tenureYears != null && <span className="block text-xs text-muted">{t("CEO since {year}", { year: mg.ceoSince ?? "" })}</span>}
                        </>
                      ) : (
                        "–"
                      )}
                    </Cell>
                  );
                })}
              </Row>
              <Row label={t("Beat estimates")}>
                {ms.map((m, i) => (
                  <Cell key={i}>{m && m.earnings.quarters > 0 ? t("{beats} of {n} quarters", { beats: m.earnings.beats, n: m.earnings.quarters }) : "–"}</Cell>
                ))}
              </Row>
              <Row label={t("Insiders, last 12 months")}>
                {ms.map((m, i) => {
                  const s = m?.insiders;
                  return (
                    <Cell key={i}>
                      {s ? (
                        <span className="text-xs">
                          <span className="text-good">
                            {t("bought")} {s.purchases ? usd(s.boughtUsd) : "–"}
                          </span>{" "}
                          ·{" "}
                          <span className="text-bad">
                            {t("sold")} {s.sales ? usd(s.soldUsd) : "–"}
                          </span>
                        </span>
                      ) : (
                        "–"
                      )}
                    </Cell>
                  );
                })}
              </Row>
              <Row label={t("Cash spent on")}>
                {ms.map((m, i) => {
                  const ca = m?.capitalAllocation;
                  return (
                    <Cell key={i}>
                      {ca ? (
                        <span className="text-xs">
                          {ca.uses
                            .filter((u) => u.amount > 0)
                            .sort((a, b) => b.amount - a.amount)
                            .map((u) => `${tx(u.label)} ${Math.round((u.share ?? 0) * 100)}%`)
                            .join(" · ")}
                          <span className="block text-muted">{t("of operating cash flow, {from}–{to}", { from: ca.from, to: ca.to })}</span>
                        </span>
                      ) : (
                        "–"
                      )}
                    </Cell>
                  );
                })}
              </Row>
            </tbody>
          </table>
        </div>
      )}
      <p className="mt-4 text-xs text-muted">{t("Each column comes from that company's latest report; open it for the details and sources. The best value in a row is highlighted where higher or lower is clearly better.")}</p>
      <Disclaimer />
    </div>
  );
}

function Header({ ticker, report, error, onRemove }: { ticker: string; report?: Report; error: Error | null; onRemove: () => void }) {
  const body = report?.report;
  const m = body?.metrics;
  const running = report && !isFinished(report.status);
  const stage = running ? STAGES.findIndex((s) => s.id === report.stage) : -1;
  return (
    <div className="space-y-1">
      <div className="flex items-start justify-between gap-2">
        <button onClick={() => report && navigate(`/reports/${report.id}`)} className="text-left">
          <span className="block font-display text-xl font-bold text-ink">{ticker}</span>
          <span className="block text-xs text-muted">{titleCase(body?.company.name || report?.companyName || "")}</span>
        </button>
        <button onClick={onRemove} className="rounded-full px-2 text-muted hover:text-bad" title={t("Remove {ticker} from the comparison", { ticker })} aria-label={t("Remove {ticker} from the comparison", { ticker })}>
          ×
        </button>
      </div>
      {error ? (
        <p className="text-xs text-bad">{tx(error.message)}</p>
      ) : running ? (
        <p className="animate-pulse text-xs font-semibold text-good">
          {t("Building…")} {stage >= 0 ? t(STAGES[stage]!.label) : ""}
        </p>
      ) : m ? (
        <p className="text-xs text-muted">
          <span className="font-display text-base font-bold text-ink">{price(m.price)}</span> · {shortDate(m.priceDate)}
        </p>
      ) : !report ? (
        <p className="text-xs text-muted">{t("Loading…")}</p>
      ) : null}
    </div>
  );
}

function Group({ title, n }: { title: string; n: number }) {
  return (
    <tr className="bg-paper/60">
      <th colSpan={n + 1} className="sticky left-0 px-4 pt-5 pb-2 text-left text-xs font-semibold tracking-[0.12em] text-muted uppercase">
        {title}
      </th>
    </tr>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <tr className="border-b border-line last:border-0">
      <th scope="row" className="sticky left-0 bg-card px-4 py-2.5 text-left align-top font-normal text-muted">
        {label}
      </th>
      {children}
    </tr>
  );
}

function Cell({ best = false, children }: { best?: boolean; children: ReactNode }) {
  return <td className={`px-4 py-2.5 align-top tabular-nums ${best ? "font-semibold text-good-strong" : ""}`}>{children}</td>;
}

/** Indexes of the best value (several when tied); none when fewer than two
 * values are known or they are all equal. */
function best(values: (number | null | undefined)[], dir: "higher" | "lower"): number[] {
  const known = values.filter((v): v is number => v != null);
  if (known.length < 2) return [];
  const top = dir === "higher" ? Math.max(...known) : Math.min(...known);
  const winners = values.flatMap((v, i) => (v != null && Math.abs(v - top) < 1e-9 ? [i] : []));
  return winners.length === known.length ? [] : winners;
}

function ScoreRow({ label, words, values }: { label: string; words: string[]; values: (number | null)[] }) {
  const b = best(values, "higher");
  return (
    <Row label={label}>
      {values.map((v, i) => (
        <Cell key={i} best={b.includes(i)}>
          {v == null ? "–" : `${v.toFixed(1)} · ${scoreWord(words, v)}`}
        </Cell>
      ))}
    </Row>
  );
}

function MetricGroup({ title, ms, pick, keys }: { title: string; ms: (MetricsReport | undefined)[]; pick: (m: MetricsReport) => Metric[]; keys?: string[] }) {
  // Rows in report order, from whichever company has them.
  const rows: Metric[] = [];
  for (const m of ms) {
    for (const x of m ? pick(m) : []) {
      if ((!keys || keys.includes(x.key)) && !rows.some((r) => r.key === x.key)) rows.push(x);
    }
  }
  if (!rows.length) return null;
  return (
    <>
      <Group title={title} n={ms.length} />
      {rows.map((row) => {
        const cells = ms.map((m) => (m ? pick(m).find((x) => x.key === row.key) : undefined));
        const dir = HIGHER.has(row.key) ? "higher" : LOWER.has(row.key) ? "lower" : null;
        // A negative P/E or debt/equity is not "best".
        const vals = cells.map((c) => (c?.value != null && !(dir === "lower" && c.value <= 0) ? c.value : null));
        const b = dir ? best(vals, dir) : [];
        return (
          <Row key={row.key} label={tx(row.label)}>
            {cells.map((c, i) => (
              <Cell key={i} best={b.includes(i)}>
                {c ? metricValue(c) : "–"}
              </Cell>
            ))}
          </Row>
        );
      })}
    </>
  );
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setV(value), ms);
    return () => clearTimeout(timer);
  }, [value, ms]);
  return v;
}

function AddCompany({ onAdd, exclude }: { onAdd: (ticker: string) => void; exclude: string[] }) {
  const [query, setQuery] = useState("");
  const q = useDebounced(query.trim(), 150);
  const results = useQuery({ queryKey: ["tickers", q], queryFn: () => searchTickers(q), enabled: q.length > 0, staleTime: 60_000 });
  const options = q ? (results.data ?? []).filter((c) => !exclude.includes(c.ticker)).slice(0, 6) : [];
  const pick = (tk: string) => {
    onAdd(tk.toUpperCase());
    setQuery("");
  };
  return (
    <form
      className="relative w-full sm:w-80"
      onSubmit={(e) => {
        e.preventDefault();
        const tk = options[0]?.ticker ?? query.trim();
        if (tk) pick(tk);
      }}
    >
      <label htmlFor="compare-add" className="sr-only">
        {t("Add a company")}
      </label>
      <input
        id="compare-add"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t("Add a company…")}
        autoComplete="off"
        className="w-full rounded-2xl border-2 border-line bg-card px-4 py-2.5 font-semibold outline-none placeholder:font-normal placeholder:text-muted/60 focus:border-good"
      />
      <button type="submit" disabled={!query.trim()} className="absolute top-1.5 right-1.5 bottom-1.5 rounded-xl bg-good px-3 text-sm font-semibold text-white disabled:opacity-40">
        {t("Add")}
      </button>
      {options.length > 0 && (
        <ul className="absolute right-0 z-10 mt-2 w-full overflow-hidden rounded-2xl border border-line bg-card shadow-lg">
          {options.map((c) => (
            <li key={c.ticker}>
              <button type="button" onClick={() => pick(c.ticker)} className="flex w-full items-baseline gap-3 px-4 py-2 text-left hover:bg-good-soft">
                <span className="w-14 font-display font-bold">{c.ticker}</span>
                <span className="truncate text-sm text-muted">{titleCase(c.name)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </form>
  );
}
