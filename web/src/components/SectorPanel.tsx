// Banks, broker-dealers, insurers and REITs read on their own terms: return
// on equity and book value, or funds from operations for REITs. Shown in the
// Financial overview (Classic, print) and as its own section in the other
// designs.

import type { MetricsReport, Sector } from "../api";
import { pct, price } from "../format";
import { t, tx } from "../i18n";
import { SECTOR_TITLE } from "../live";
import { MetricRow } from "./ui";

/** The kind's metrics, how it is read, and its trend by year. */
export function SectorPanel({ m, title = false }: { m?: MetricsReport; title?: boolean }) {
  const s = m?.sector;
  if (!s || m?.limitedSupport) return null;
  const half = Math.ceil(s.metrics.length / 2);
  return (
    <div className="card fade-in space-y-5">
      {title && <h3 className="font-display text-xl font-bold">{t(SECTOR_TITLE[s.kind])}</h3>}
      <p className="text-sm leading-relaxed text-muted">{tx(s.about)}</p>
      <div className="grid gap-x-8 md:grid-cols-2">
        <div>
          {s.metrics.slice(0, half).map((x) => (
            <MetricRow key={x.key} m={x} />
          ))}
        </div>
        <div>
          {s.metrics.slice(half).map((x) => (
            <MetricRow key={x.key} m={x} />
          ))}
        </div>
      </div>
      {s.trend && <TrendBars trend={s.trend} />}
    </div>
  );
}

/** One bar per fiscal year; negative years hang below the line. */
function TrendBars({ trend }: { trend: NonNullable<Sector["trend"]> }) {
  const fmt = (v: number) => (trend.unit === "percent" ? pct(v) : price(v));
  const values = trend.points.map((p) => p.value);
  const top = Math.max(...values, 0);
  const bottom = Math.min(...values, 0);
  const span = top - bottom || 1;
  return (
    <figure className="space-y-2">
      <figcaption className="text-sm font-semibold">{t("{label} by fiscal year", { label: tx(trend.label) })}</figcaption>
      <div
        className={`flex h-36 items-stretch gap-1.5 pt-4 ${bottom < 0 ? "pb-4" : ""}`}
        role="img"
        aria-label={trend.points.map((p) => `${p.date.slice(0, 4)}: ${fmt(p.value)}`).join(", ")}
      >
        {trend.points.map((p) => {
          // Share of the area above (below) zero this bar fills.
          const up = top > 0 ? (Math.max(p.value, 0) / top) * 100 : 0;
          const down = bottom < 0 ? (Math.max(-p.value, 0) / -bottom) * 100 : 0;
          return (
            <div key={p.date} className="flex min-w-0 flex-1 flex-col" title={`${p.date.slice(0, 4)}: ${fmt(p.value)}`}>
              <div className="relative flex flex-col justify-end" style={{ height: `${(top / span) * 100}%` }}>
                {p.value >= 0 && (
                  <span className="absolute inset-x-0 hidden text-center text-[10px] text-muted tabular-nums sm:block" style={{ bottom: `calc(${up}% + 2px)` }}>
                    {fmt(p.value)}
                  </span>
                )}
                <div className="rounded-t bg-good" style={{ height: `${up}%` }} />
              </div>
              <div className="relative" style={{ height: `${(-bottom / span) * 100}%` }}>
                <div className="rounded-b bg-bad" style={{ height: `${down}%` }} />
                {p.value < 0 && (
                  <span className="absolute inset-x-0 hidden text-center text-[10px] text-bad tabular-nums sm:block" style={{ top: `calc(${down}% + 2px)` }}>
                    {fmt(p.value)}
                  </span>
                )}
              </div>
            </div>
          );
        })}
      </div>
      <div className="flex gap-1.5 border-t border-line pt-1">
        {trend.points.map((p) => (
          <span key={p.date} className="min-w-0 flex-1 text-center text-[10px] text-muted">
            ’{p.date.slice(2, 4)}
          </span>
        ))}
      </div>
    </figure>
  );
}
