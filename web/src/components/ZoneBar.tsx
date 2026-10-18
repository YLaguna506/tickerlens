// The share price against the prices at which the company would look cheap,
// typical or expensive by its own 5-year history.

import { price as fmt } from "../format";
import { t } from "../i18n";

export interface ZonePrices {
  cheapPrice: number;
  medianPrice: number;
  expensivePrice: number;
}

export function ZoneBar({ price, b }: { price: number; b: ZonePrices }) {
  const lo = Math.min(b.cheapPrice, price) * 0.85;
  const hi = Math.max(b.expensivePrice, price) * 1.1;
  const at = (v: number) => `${((v - lo) / (hi - lo)) * 100}%`;
  return (
    <div className="mt-2">
      <div
        className="relative h-2.5 rounded-full bg-bad/70"
        role="img"
        aria-label={t("Price {price}: cheap below {cheap}, expensive above {expensive}", { price: fmt(price), cheap: fmt(b.cheapPrice), expensive: fmt(b.expensivePrice) })}
      >
        <span className="absolute inset-y-0 left-0 rounded-l-full bg-good" style={{ width: at(b.cheapPrice) }} />
        <span className="absolute inset-y-0 bg-mid" style={{ left: at(b.cheapPrice), width: `calc(${at(b.expensivePrice)} - ${at(b.cheapPrice)})` }} />
        <span className="absolute -top-1 size-4.5 -translate-x-1/2 rounded-full border-2 border-card bg-ink shadow" style={{ left: at(price) }} />
      </div>
      <p className="mt-1 flex justify-between gap-2 text-[11px] text-muted tabular-nums">
        <span>
          {t("cheap")} &lt; {fmt(b.cheapPrice)}
        </span>
        <span>
          {t("typical")} {fmt(b.medianPrice)}
        </span>
        <span>
          {t("expensive")} &gt; {fmt(b.expensivePrice)}
        </span>
      </p>
    </div>
  );
}
