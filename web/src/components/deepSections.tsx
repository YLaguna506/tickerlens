// Moat, Risk and Management: the panels every design shows. Each renders its
// content only; a design wraps it in its own section header.

import { Fragment, useState } from "react";
import type {
  InsiderSummary,
  MetricsReport,
  MoatSourceAnswer,
  PeerGroup,
  ReportBody,
} from "../api";
import { pct, shortDate, signedPct, titleCase, usd } from "../format";
import { t, tx } from "../i18n";
import { SECTOR_TITLE, isPending } from "../live";
import { NextEarningsLine } from "./NextEarnings";
import { ReverseDCFCard } from "./ReverseDCF";
import { SectorPanel } from "./SectorPanel";
import { EvidenceList, Skeleton, StepScale, Unavailable } from "./ui";

const MOAT_WIDTHS = ["None", "Thin", "Narrow", "Broad", "Wide"];
const MOAT_DIRECTIONS = [
  "Narrowing",
  "Slightly narrowing",
  "Stable",
  "Slightly widening",
  "Widening",
];
const RISK_LEVELS = [
  "Very high risk",
  "High risk",
  "Moderate risk",
  "Low risk",
  "Very low risk",
];

const MOAT_SOURCES = [
  {
    key: "switchingCosts",
    label: "Switching costs",
    hint: "How hard it is for customers to leave",
  },
  {
    key: "networkEffects",
    label: "Network effects",
    hint: "More users make it more valuable",
  },
  {
    key: "intangibleAssets",
    label: "Intangible assets",
    hint: "Brands, patents, licenses",
  },
  {
    key: "costAdvantage",
    label: "Cost advantage",
    hint: "Scale or process rivals cannot match",
  },
  {
    key: "counterPositioning",
    label: "Counter-positioning",
    hint: "A model incumbents cannot copy",
  },
] as const;

const RISK_QUESTIONS = [
  { key: "revenueDiversification", question: "How spread out is revenue?" },
  { key: "disruptionThreat", question: "How big is the threat of disruption?" },
  {
    key: "externalExposure",
    question: "How exposed is it to things outside its control?",
  },
] as const;

const ratingStyle: Record<string, string> = {
  None: "bg-line text-muted",
  Weak: "bg-bad-soft text-bad",
  Moderate: "bg-mid-soft text-mid",
  Strong: "bg-good text-white",
};
const arrow: Record<string, string> = {
  Narrowing: "↘",
  Stable: "→",
  Widening: "↗",
};

/** Tone for a 1–5 value where 5 is best. */
const tone = (v: number | null | undefined) =>
  v == null
    ? "bg-line text-muted"
    : v >= 4
      ? "bg-good text-white"
      : v >= 3
        ? "bg-mid text-[#1a1404]"
        : "bg-bad text-white";

const running = (report?: { status: string }) =>
  report?.status === "queued" || report?.status === "running";

// --- Moat --------------------------------------------------------------------------------

export function MoatPanel({
  body,
  report,
}: {
  body?: ReportBody;
  report?: { status: string };
}) {
  const sec = body?.agents?.moat;
  if (isPending(body, "moat") && running(report))
    return (
      <Skeleton lines={4} label={t("Judging the moat against the industry…")} />
    );
  const out = sec?.status === "ready" ? sec.output : undefined;
  const url = body?.filing?.url;
  return (
    <div className="space-y-4">
      {out && body?.moat ? (
        <div className="fade-in space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <p className="mb-2 text-sm font-semibold text-muted">
                {t("Width")}
              </p>
              <StepScale value={body.moat.width} labels={MOAT_WIDTHS} />
            </div>
            <div>
              <p className="mb-2 text-sm font-semibold text-muted">
                {t("Direction")}
              </p>
              <StepScale
                value={body.moat.direction}
                labels={MOAT_DIRECTIONS}
                tone="mid"
              />
            </div>
          </div>
          <p className="card leading-relaxed">{out.summary}</p>
          <div className="card divide-y divide-line">
            {MOAT_SOURCES.map((s) => {
              const a: MoatSourceAnswer | undefined = out.sources[s.key];
              return (
                <div
                  key={s.key}
                  className="space-y-2 py-3 first:pt-0 last:pb-0"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <p className="font-display font-semibold">{t(s.label)}</p>
                      <p className="text-xs text-muted">{t(s.hint)}</p>
                    </div>
                    <div className="flex items-center gap-2">
                      <span
                        className={`rounded-full px-3 py-1 text-sm font-semibold ${ratingStyle[a?.rating ?? "None"]}`}
                      >
                        {t(a?.rating ?? "None")}
                      </span>
                      {a && a.rating !== "None" && (
                        <span
                          className="text-sm text-muted"
                          title={t(a.direction)}
                        >
                          {arrow[a.direction]} {t(a.direction)}
                        </span>
                      )}
                    </div>
                  </div>
                  {a?.justification && (
                    <p className="text-sm leading-relaxed text-muted">
                      {a.justification}
                    </p>
                  )}
                  <EvidenceList evidence={a?.evidence} filingUrl={url} />
                </div>
              );
            })}
          </div>
        </div>
      ) : (
        <Unavailable>
          {t("The moat could not be judged for this report")}
          {sec?.errors?.length ? `: ${tx(sec.errors[0])}` : "."}
        </Unavailable>
      )}
      {body?.peers && <PeerTable g={body.peers} />}
    </div>
  );
}

export function PeerTable({ g }: { g: PeerGroup }) {
  const cell = "px-2 py-1.5 text-right tabular-nums";
  const row = (
    name: string,
    c: Omit<PeerGroup["medians"], "revenue"> & { revenue: number | null },
    cls = "",
  ) => (
    <tr className={cls}>
      <td className="max-w-[14rem] truncate px-2 py-1.5">{name}</td>
      <td className={cell}>{usd(c.revenue)}</td>
      <td className={cell}>{signedPct(c.revenueGrowth)}</td>
      <td className={cell}>{pct(c.grossMargin)}</td>
      <td className={cell}>{pct(c.operatingMargin)}</td>
      <td className={cell}>{pct(c.netMargin)}</td>
    </tr>
  );
  return (
    <div className="card fade-in overflow-x-auto">
      <p className="font-display font-semibold">
        {t("Compared with its industry")}
      </p>
      <p className="mb-3 text-sm text-muted">
        {t(
          "{industry} ({basis}), calendar {year}: its {n} largest peers, of {of} companies with SEC filings.",
          {
            industry: titleCase(g.industry),
            basis: g.basis.replace("industry group", t("industry group")),
            year: g.year,
            n: g.peers.length,
            of: g.of,
          },
        )}{" "}
        {g.rank ? t("#{rank} by revenue.", { rank: g.rank }) : ""}
      </p>
      <table className="w-full min-w-[560px] text-sm">
        <thead className="text-xs text-muted">
          <tr>
            <th className="px-2 text-left font-semibold">{t("Company")}</th>
            <th className="px-2 text-right font-semibold">{t("Revenue")}</th>
            <th className="px-2 text-right font-semibold">{t("Growth")}</th>
            <th className="px-2 text-right font-semibold">
              {t("Gross margin")}
            </th>
            <th className="px-2 text-right font-semibold">
              {t("Operating margin")}
            </th>
            <th className="px-2 text-right font-semibold">{t("Net margin")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {g.company &&
            row(
              `${titleCase(g.company.name)}${g.company.ticker ? ` (${g.company.ticker})` : ""}`,
              g.company,
              "bg-good-soft font-semibold",
            )}
          {g.peers.map((p) => (
            <Fragment key={p.cik}>
              {row(
                `${titleCase(p.name)}${p.ticker ? ` (${p.ticker})` : ""}`,
                p,
              )}
            </Fragment>
          ))}
          {row(
            t("Median of these peers"),
            g.medians,
            "font-semibold text-muted",
          )}
        </tbody>
      </table>
      <p className="mt-2 text-xs text-muted">
        {t(
          "Peers share the SEC industry code, so some may not compete directly.",
        )}
      </p>
    </div>
  );
}

// --- Risk --------------------------------------------------------------------------------

export function RiskPanel({
  body,
  m,
  report,
}: {
  body?: ReportBody;
  m?: MetricsReport;
  report?: { status: string };
}) {
  const sec = body?.agents?.risk;
  if (isPending(body, "risk") && running(report))
    return <Skeleton lines={4} label={t("Reading the risk factors…")} />;
  const out = sec?.status === "ready" ? sec.output : undefined;
  const risk = body?.risk;
  if (!out || !risk) {
    return (
      <Unavailable>
        {t("Risk could not be judged for this report")}
        {sec?.errors?.length ? `: ${tx(sec.errors[0])}` : "."}
      </Unavailable>
    );
  }
  const url = body?.filing?.url;
  const health = risk.points.financialHealth;
  return (
    <div className="fade-in space-y-4">
      <p className="text-muted">
        {t("Risk score")}{" "}
        <span className="font-display text-2xl font-bold text-ink">
          {risk.score.toFixed(1)}
        </span>
        /5 · {t("5 is the lowest risk")}
      </p>
      <StepScale value={risk.score} labels={RISK_LEVELS} />
      <div className="grid gap-4 md:grid-cols-2">
        {RISK_QUESTIONS.map((q) => {
          const a = out[q.key];
          return (
            <div key={q.key} className="card space-y-2">
              <div className="flex items-start justify-between gap-3">
                <h3 className="font-display text-lg font-semibold">
                  {t(q.question)}
                </h3>
                <span
                  className={`shrink-0 rounded-full px-3 py-1 text-sm font-semibold ${tone(risk.points[q.key])}`}
                >
                  {t(a?.answer ?? "")}
                </span>
              </div>
              <p className="text-sm leading-relaxed text-muted">
                {a?.justification}
              </p>
              <EvidenceList evidence={a?.evidence} filingUrl={url} />
            </div>
          );
        })}
        <div className="card space-y-2">
          <div className="flex items-start justify-between gap-3">
            <h3 className="font-display text-lg font-semibold">
              {t("Are its finances healthy?")}
            </h3>
            <span
              className={`shrink-0 rounded-full px-3 py-1 text-sm font-semibold ${tone(health)}`}
            >
              {health == null ? "?" : `${health.toFixed(0)}/5`}
            </span>
          </div>
          <p className="text-sm text-muted">
            {t("From the balance sheet: the financial health score.")}
          </p>
          {m?.healthScore.reasons?.length ? (
            <ul className="space-y-0.5 text-xs text-muted">
              {m.healthScore.reasons.map((r) => (
                <li key={r}>{tx(r)}</li>
              ))}
            </ul>
          ) : null}
        </div>
      </div>
    </div>
  );
}

// --- Management ----------------------------------------------------------------------------

const USE_COLORS: Record<string, string> = {
  capex: "bg-[#7fb889]",
  acquisitions: "bg-mid",
  dividends: "bg-good",
  buybacks: "bg-navy",
  debtPaydown: "bg-muted",
};

export function ManagementPanel({
  body,
  m,
  report,
}: {
  body?: ReportBody;
  m?: MetricsReport;
  report?: { status: string };
}) {
  const sec = body?.agents?.management;
  const info = body?.management;
  const ceoReady = sec?.status === "ready" && info?.ceoName;
  const ca = m?.capitalAllocation;
  const e = m?.earnings;
  return (
    <div className="space-y-4">
      {isPending(body, "management") && running(report) ? (
        <Skeleton lines={3} label={t("Reading the proxy statement…")} />
      ) : ceoReady ? (
        <div className="card fade-in space-y-3">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <div>
              <p className="text-xs font-semibold tracking-[0.12em] text-muted uppercase">
                {t("Chief executive officer")}
              </p>
              <p className="font-display text-2xl font-bold">{info!.ceoName}</p>
            </div>
            {info!.ceoSince ? (
              <p className="text-right">
                <span className="font-display text-2xl font-bold">
                  {t("{n} years", { n: info!.tenureYears ?? 0 })}
                </span>
                <span className="block text-sm text-muted">
                  {t("CEO since {year}", { year: info!.ceoSince })}
                </span>
              </p>
            ) : null}
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <Fact
              label={t("Shares owned")}
              value={
                info!.sharesOwned != null
                  ? info!.sharesOwned.toLocaleString("en-US")
                  : "–"
              }
            />
            <Fact
              label={t("Worth at today's price")}
              value={usd(info!.ownedValueUsd)}
            />
            <Fact
              label={t("Of all shares")}
              value={
                info!.percentOfClass != null
                  ? `${info!.percentOfClass}%`
                  : info!.lessThanOnePercent
                    ? t("less than 1%")
                    : "–"
              }
            />
          </div>
          {info!.directorsAndOfficersPercent != null && (
            <p className="text-sm text-muted">
              {t("All directors and executive officers together own {x}%.", {
                x: info!.directorsAndOfficersPercent,
              })}
            </p>
          )}
          {info!.notes && <p className="text-sm text-muted">{info!.notes}</p>}
          <EvidenceList evidence={sec?.evidence} filingUrl={info?.proxy?.url} />
        </div>
      ) : (
        <Unavailable>
          {t("CEO facts are not available for this report")}
          {sec?.errors?.length ? `: ${tx(sec.errors[0])}` : "."}
        </Unavailable>
      )}

      <div className="card fade-in space-y-1">
        <p className="font-display font-semibold">
          {t("Earnings against analyst estimates")}
        </p>
        {e && e.quarters > 0 ? (
          <p className="text-muted">
            {t("Beat estimates in {beats} of the last {n} quarters.", {
              beats: e.beats,
              n: e.quarters,
            })}{" "}
            {e.avgSurprisePercent != null &&
              t("Average surprise {x}", {
                x: signedPct(e.avgSurprisePercent / 100),
              })}
          </p>
        ) : (
          <p className="text-sm text-muted">{t("No estimates available.")}</p>
        )}
        <NextEarningsLine n={m?.nextEarnings} />
      </div>

      {m?.insiders && <InsiderCard s={m.insiders} />}

      {ca && (
        <div className="card fade-in space-y-3">
          <p className="font-display font-semibold">
            {t("How the cash was spent, {from}–{to}", {
              from: ca.from,
              to: ca.to,
            })}
          </p>
          <p className="text-sm text-muted">
            {t(
              "Operating cash flow over {n} years: {x}. Each bar is a share of it.",
              { n: ca.years, x: usd(ca.operatingCashFlow) },
            )}
          </p>
          <ul className="space-y-2">
            {ca.uses.map((u) => {
              const share = u.share ?? 0;
              return (
                <li
                  key={u.key}
                  className="grid grid-cols-[9rem_1fr_6rem] items-center gap-3 text-sm sm:grid-cols-[11rem_1fr_7rem]"
                >
                  <span>{tx(u.label)}</span>
                  <span className="h-3 rounded-full bg-line">
                    <span
                      className={`block h-3 rounded-full ${USE_COLORS[u.key]}`}
                      style={{
                        width: `${Math.min(Math.abs(share), 1) * 100}%`,
                      }}
                    />
                  </span>
                  <span className="text-right tabular-nums">
                    {pct(Math.abs(share), 0)}{" "}
                    <span className="text-xs text-muted">{usd(Math.abs(u.amount))}</span>
                  </span>
                </li>
              );
            })}
          </ul>
          {ca.shareCountChange != null && (
            <p className="text-sm text-muted">
              {t("Diluted share count over the period: {x}.", {
                x: signedPct(ca.shareCountChange),
              })}{" "}
              {ca.shareCountChange < -0.01
                ? t(
                    "Buybacks shrank it, so each share owns more of the company.",
                  )
                : ca.shareCountChange > 0.01
                  ? t("New shares diluted existing owners.")
                  : ""}
            </p>
          )}
          {ca.notes?.map((n) => (
            <p key={n} className="text-xs text-muted">
              {tx(n)}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

/** Open-market buying and selling by directors and officers over a year. */
function InsiderCard({ s }: { s: InsiderSummary }) {
  const planned = s.soldUsd > 0 ? s.plannedSoldUsd / s.soldUsd : 0;
  const [all, setAll] = useState(false);
  const shown = all ? s.trades : s.trades.slice(0, 8);
  return (
    <div className="card fade-in space-y-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="font-display font-semibold">{t("Insider buying and selling")}</p>
        <p className="text-xs text-muted">
          {t("{from} to {to}, from {n} Form 4 filings", { from: shortDate(s.from), to: shortDate(s.to), n: s.filings })}
        </p>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-xl bg-good-soft p-3">
          <p className="text-xs text-muted">{t("Bought on the open market")}</p>
          <p className="font-display text-xl font-bold text-good-strong">{s.purchases ? usd(s.boughtUsd) : "–"}</p>
          <p className="text-xs text-muted">{s.buyers === 1 ? t("by one insider") : s.buyers ? t("by {n} insiders", { n: s.buyers }) : t("no purchases")}</p>
        </div>
        <div className="rounded-xl bg-bad-soft p-3">
          <p className="text-xs text-muted">{t("Sold on the open market")}</p>
          <p className="font-display text-xl font-bold text-bad">{s.sales ? usd(s.soldUsd) : "–"}</p>
          <p className="text-xs text-muted">
            {s.sellers === 1 ? t("by one insider") : s.sellers ? t("by {n} insiders", { n: s.sellers }) : t("no sales")}
            {s.sales > 0 && planned > 0 && ` · ${t("{x} marked as a pre-arranged plan", { x: pct(planned, 0) })}`}
          </p>
        </div>
      </div>
      <p className="text-xs text-muted">
        {t(
          "Insiders sell for many reasons (taxes, a house, diversifying), often under plans set months ahead. Buying with their own money on the open market is the stronger signal. Grants, option exercises and tax withholding are left out.",
        )}
      </p>
      {s.trades.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[520px] text-sm">
            <thead className="text-xs text-muted">
              <tr>
                <th className="py-1 pr-2 text-left font-semibold">{t("Date")}</th>
                <th className="py-1 pr-2 text-left font-semibold">{t("Insider")}</th>
                <th className="py-1 pr-2 text-left font-semibold">{t("Trade")}</th>
                <th className="py-1 pr-2 text-right font-semibold">{t("Shares")}</th>
                <th className="py-1 text-right font-semibold">{t("Value")}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {shown.map((tr, i) => (
                <tr key={`${tr.accession}-${i}`}>
                  <td className="py-1.5 pr-2 whitespace-nowrap text-muted">{shortDate(tr.date)}</td>
                  <td className="py-1.5 pr-2">
                    {titleCase(tr.name)}
                    {tr.role && <span className="block text-xs text-muted">{tr.role}</span>}
                  </td>
                  <td className="py-1.5 pr-2">
                    <span className={`font-semibold ${tr.code === "P" ? "text-good" : "text-bad"}`}>
                      {tr.code === "P" ? t("Bought") : t("Sold")}
                    </span>
                    {tr.planned && <span className="block text-xs text-muted">{t("planned")}</span>}
                  </td>
                  <td className="py-1.5 pr-2 text-right tabular-nums">{Math.round(tr.shares).toLocaleString("en-US")}</td>
                  <td className="py-1.5 text-right tabular-nums">{usd(tr.shares * tr.price)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {s.trades.length > 8 && (
            <button onClick={() => setAll((a) => !a)} className="mt-2 text-xs font-semibold text-good">
              {all ? t("Show fewer") : t("Show all {n} trades", { n: s.trades.length })}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl bg-paper p-3">
      <p className="text-xs text-muted">{label}</p>
      <p className="font-display text-lg font-semibold tabular-nums">{value}</p>
    </div>
  );
}

/** The three sections as a design shows them: its own wrapper and heading
 * classes, and a palette class that remaps the panels' colours. */
export function MoreSections({
  body,
  m,
  report,
  look,
  idPrefix,
  sectionClass,
  titleClass,
}: {
  body?: ReportBody;
  m?: MetricsReport;
  report?: { status: string };
  look: "look-light" | "look-terminal";
  idPrefix: string;
  sectionClass: string;
  titleClass: string;
}) {
  const sector = m?.limitedSupport ? undefined : m?.sector;
  const sections = [
    // Classic shows these inside its Financial overview and Valuation
    // sections; the other designs get them here.
    ...(sector ? [{ id: "sector", title: t(SECTOR_TITLE[sector.kind]), panel: <SectorPanel m={m} /> }] : []),
    ...(m?.reverseDcf
      ? [{ id: "expectations", title: t("What the price expects"), panel: <ReverseDCFCard m={m} /> }]
      : []),
    {
      id: "moat",
      title: t("Moat"),
      panel: <MoatPanel body={body} report={report} />,
    },
    {
      id: "risk",
      title: t("Risk"),
      panel: <RiskPanel body={body} m={m} report={report} />,
    },
    {
      id: "management",
      title: t("Management"),
      panel: <ManagementPanel body={body} m={m} report={report} />,
    },
  ];
  return (
    <>
      {sections.map((s) => (
        <section
          key={s.id}
          id={idPrefix + s.id}
          className={`${look} ${sectionClass}`}
        >
          <h2 className={titleClass}>{s.title}</h2>
          {s.panel}
        </section>
      ))}
    </>
  );
}
