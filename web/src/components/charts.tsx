// Charts and visual bars for the report.

import {
  Area,
  AreaChart,
  CartesianGrid,
  LabelList,
  Line,
  LineChart,
  ReferenceArea,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { Bands, EarningsQuarter, Point, SegmentShare } from "../api";
import { monthYear, pct, price, signedPct, usd, wholePrice } from "../format";

const GREEN = "#3f8f4a";
const INK = "#34343c";
const GRID = "#e7e4dc";
const AXIS = { fontSize: 12, fill: "#7a7a80" };

/** 5-year price, area under the line. */
export function PriceChart({ points }: { points: Point[] }) {
  // One point per week keeps the SVG light; the shape is unchanged.
  const data = points.filter((_, i) => i % 5 === 0 || i === points.length - 1);
  const years = [...new Set(data.map((p) => p.date.slice(0, 4)))];
  return (
    <div className="h-64 w-full sm:h-72">
      <ResponsiveContainer>
        <AreaChart data={data} margin={{ top: 10, right: 12, left: 0, bottom: 0 }}>
          <defs>
            <linearGradient id="priceFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={GREEN} stopOpacity={0.18} />
              <stop offset="100%" stopColor={GREEN} stopOpacity={0.02} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke={GRID} />
          <XAxis
            dataKey="date"
            ticks={years.map((y) => data.find((p) => p.date.startsWith(y))!.date)}
            tickFormatter={(d: string) => d.slice(0, 4)}
            tick={AXIS}
            axisLine={false}
            tickLine={false}
          />
          <YAxis tickFormatter={(v: number) => `$${Math.round(v)}`} tick={AXIS} axisLine={false} tickLine={false} width={48} domain={["auto", "auto"]} />
          <Tooltip formatter={(v) => price(Number(v))} labelFormatter={(d) => String(d)} />
          <Area type="monotone" dataKey="value" stroke={GREEN} strokeWidth={2.5} fill="url(#priceFill)" isAnimationActive={false} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

/** Revenue and net income by fiscal year, plus the latest twelve months. */
export function RevenueIncomeChart({ rows }: { rows: { label: string; revenue: number | null; netIncome: number | null }[] }) {
  return (
    <div className="h-64 w-full">
      <ResponsiveContainer>
        <LineChart data={rows} margin={{ top: 28, right: 24, left: 24, bottom: 4 }}>
          <XAxis dataKey="label" tick={AXIS} axisLine={false} tickLine={false} tickMargin={12} />
          <YAxis hide domain={[(min: number) => Math.min(0, min), "auto"]} />
          <ReferenceLine y={0} stroke="#c9c5bb" strokeDasharray="4 4" />
          <Line dataKey="revenue" name="Revenue" stroke={GREEN} strokeWidth={3} dot={{ r: 4 }} isAnimationActive={false}>
            <LabelList dataKey="revenue" position="top" formatter={(v) => usd(Number(v))} style={{ fontSize: 12, fill: INK }} />
          </Line>
          <Line dataKey="netIncome" name="Net income" stroke="#7fb889" strokeWidth={3} dot={{ r: 4 }} isAnimationActive={false}>
            {/* Above the point: below it, small profits collide with the year labels. */}
            <LabelList dataKey="netIncome" position="top" offset={10} formatter={(v) => usd(Number(v))} style={{ fontSize: 12, fill: INK }} />
          </Line>
          <Tooltip formatter={(v) => usd(Number(v))} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

/** One year of the valuation multiple with cheap / fair / expensive bands.
 * The tooltip also shows that day's share price. */
export function BandChart({ points, bands, prices }: { points: Point[]; bands: Bands; prices: Point[] }) {
  const priceOn = new Map(prices.map((p) => [p.date, p.value]));
  const data = points.map((p) => ({ ...p, price: priceOn.get(p.date) ?? null }));
  const values = points.map((p) => p.value);
  const lo = Math.min(...values, bands.cheapBelow) * 0.97;
  const hi = Math.max(...values, bands.expensiveAbove) * 1.03;
  return (
    <div className="h-72 w-full">
      <ResponsiveContainer>
        <LineChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
          <ReferenceArea y1={lo} y2={bands.cheapBelow} fill="#e3efe1" fillOpacity={0.9} ifOverflow="hidden" />
          <ReferenceArea y1={bands.cheapBelow} y2={bands.expensiveAbove} fill="#f4ead2" fillOpacity={0.8} ifOverflow="hidden" />
          <ReferenceArea y1={bands.expensiveAbove} y2={hi} fill="#f7e3e0" fillOpacity={0.9} ifOverflow="hidden" />
          <ReferenceLine y={bands.median} stroke="#9a968c" strokeDasharray="5 5" />
          <XAxis dataKey="date" tickFormatter={monthYear} tick={AXIS} axisLine={false} tickLine={false} minTickGap={40} />
          <YAxis domain={[lo, hi]} tickFormatter={(v: number) => `${v.toFixed(1)}×`} tick={AXIS} axisLine={false} tickLine={false} width={52} />
          <Tooltip content={<BandTooltip metric={bands.metric} />} />
          <Line dataKey="value" name={bands.metric} stroke={INK} strokeWidth={2} dot={false} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

function BandTooltip({ active, payload, metric }: { active?: boolean; payload?: { payload: { date: string; value: number; price: number | null } }[]; metric: string }) {
  const row = active ? payload?.[0]?.payload : undefined;
  if (!row) return null;
  return (
    <div className="rounded-lg border border-line bg-white px-3 py-2 text-sm shadow-sm">
      <p className="text-muted">{row.date}</p>
      <p>{metric} <span className="font-semibold">{row.value.toFixed(1)}×</span></p>
      {row.price != null && <p>Price <span className="font-semibold">{price(row.price)}</span></p>}
    </div>
  );
}

/** Cheap / fair / expensive price ranges with today's price marked. */
export function PriceBar({ bands }: { bands: Bands }) {
  // Scale: from 15% below the cheap edge to 15% above the expensive edge.
  const min = Math.min(bands.cheapPrice * 0.85, bands.price * 0.97);
  const max = Math.max(bands.expensivePrice * 1.15, bands.price * 1.03);
  const at = (v: number) => `${((v - min) / (max - min)) * 100}%`;
  return (
    <div className="pt-8 pb-7">
      <div className="relative h-9 rounded-full">
        <div className="absolute inset-y-0 left-0 flex items-center rounded-l-full bg-good-soft pl-3 text-sm font-semibold text-good" style={{ width: at(bands.cheapPrice) }}>
          Cheap
        </div>
        <div className="absolute inset-y-0 flex items-center justify-center bg-mid-soft text-sm font-semibold text-mid" style={{ left: at(bands.cheapPrice), width: `calc(${at(bands.expensivePrice)} - ${at(bands.cheapPrice)})` }}>
          Fair
        </div>
        <div className="absolute inset-y-0 right-0 flex items-center justify-end rounded-r-full bg-bad-soft pr-3 text-sm font-semibold text-bad" style={{ left: at(bands.expensivePrice) }}>
          Expensive
        </div>
        <div className="absolute -top-7 flex -translate-x-1/2 flex-col items-center" style={{ left: at(bands.price) }}>
          <span className="text-sm font-bold whitespace-nowrap">Now {price(bands.price)}</span>
          <span className="h-11 w-1.5 rounded-full bg-ink" />
        </div>
      </div>
      <div className="relative mt-2 text-sm font-semibold text-muted">
        <span className="absolute -translate-x-1/2" style={{ left: at(bands.cheapPrice) }}>{wholePrice(bands.cheapPrice)}</span>
        <span className="absolute -translate-x-1/2" style={{ left: at(bands.expensivePrice) }}>{wholePrice(bands.expensivePrice)}</span>
      </div>
    </div>
  );
}

const SEGMENT_COLORS = ["#2f6f3a", "#3f8f4a", "#5fa569", "#7fb889", "#a5cfa9", "#c7e2c9"];

/** Stacked bar plus table of revenue by segment. */
export function SegmentBreakdown({ rows, title }: { rows: SegmentShare[]; title: string }) {
  const total = rows.reduce((s, r) => s + r.revenue, 0);
  const sorted = [...rows].sort((a, b) => b.revenue - a.revenue);
  return (
    <div className="space-y-4">
      <div className="flex items-baseline justify-between">
        <h3 className="font-display text-2xl font-bold">{title}</h3>
        <span className="font-display text-xl font-bold">{usd(total)}</span>
      </div>
      <div className="flex h-9 overflow-hidden rounded-xl" role="img" aria-label={sorted.map((r) => `${r.name} ${pct(r.share)}`).join(", ")}>
        {sorted.map((r, i) => (
          <div key={r.name} className="flex items-center justify-center text-xs font-bold text-white sm:text-sm" style={{ width: `${r.share * 100}%`, background: SEGMENT_COLORS[i % SEGMENT_COLORS.length] }}>
            {r.share >= 0.07 ? pct(r.share) : ""}
          </div>
        ))}
      </div>
      <table className="w-full text-sm sm:text-base">
        <thead>
          <tr className="text-left text-xs text-muted sm:text-sm">
            <th className="py-2 font-semibold">Segment</th>
            <th className="py-2 text-right font-semibold">Share</th>
            <th className="py-2 text-right font-semibold">Revenue</th>
            <th className="py-2 text-right font-semibold">1-yr</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((r, i) => (
            <tr key={r.name} className="border-t border-line">
              <td className="py-2.5">
                <span className="mr-2 inline-block size-2.5 rounded-sm" style={{ background: SEGMENT_COLORS[i % SEGMENT_COLORS.length] }} />
                {r.name}
              </td>
              <td className="py-2.5 text-right font-semibold tabular-nums">{pct(r.share)}</td>
              <td className="py-2.5 text-right tabular-nums">{usd(r.revenue)}</td>
              <td className={`py-2.5 text-right font-semibold tabular-nums ${r.yoy == null ? "text-muted" : r.yoy >= 0 ? "text-good" : "text-bad"}`}>
                {r.yoy == null ? "–" : `${r.yoy >= 0 ? "▲" : "▼"} ${Math.abs(r.yoy * 100).toFixed(0)}%`}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Geographic split as two-tone bar with legend. */
export function GeographySplit({ rows }: { rows: SegmentShare[] }) {
  const sorted = [...rows].sort((a, b) => b.revenue - a.revenue);
  const colors = ["#c3a878", "#3f8f4a", "#7fb889", "#d9c7a3", "#a5cfa9", "#8a7a5a"];
  return (
    <div className="space-y-3">
      <p className="eyebrow">Revenue by geography</p>
      <div className="flex h-5 overflow-hidden rounded-full">
        {sorted.map((r, i) => (
          <div key={r.name} style={{ width: `${r.share * 100}%`, background: colors[i % colors.length] }} />
        ))}
      </div>
      <ul className="flex flex-wrap gap-x-5 gap-y-1 text-sm">
        {sorted.map((r, i) => (
          <li key={r.name} className="flex items-center gap-2">
            <span className="size-3 rounded-full" style={{ background: colors[i % colors.length] }} />
            <span className="font-semibold">{r.name}</span> · {pct(r.share, 0)}
          </li>
        ))}
      </ul>
    </div>
  );
}

/** EPS reported vs estimate, one column per quarter. */
export function EarningsDots({ quarters }: { quarters: EarningsQuarter[] }) {
  const recent = quarters.slice(-8);
  return (
    <div className="grid grid-cols-4 gap-y-6 sm:grid-cols-8">
      {recent.map((q) => {
        const beat = q.reportedEPS != null && q.estimatedEPS != null && q.reportedEPS > q.estimatedEPS;
        return (
          <div key={q.fiscalDateEnding} className="flex flex-col items-center gap-1 text-center">
            <span className={`text-sm font-bold ${beat ? "text-good" : "text-bad"}`}>{q.reportedEPS == null ? "–" : `$${q.reportedEPS.toFixed(2)}`}</span>
            <span className={`size-3.5 rounded-full ${beat ? "bg-good" : "bg-bad"}`} title={beat ? "Beat" : "Miss"} />
            <span className="text-[11px] text-muted">est. {q.estimatedEPS == null ? "–" : `$${q.estimatedEPS.toFixed(2)}`}</span>
            <span className="text-xs text-muted">{monthYear(q.fiscalDateEnding)}</span>
            {q.surprisePercent != null && <span className="text-[11px] text-muted">{signedPct(q.surprisePercent / 100)}</span>}
          </div>
        );
      })}
    </div>
  );
}
