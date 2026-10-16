import { DESIGNS, setDesign, useDesign } from "../design";
import { t } from "../i18n";

const STYLES = {
  classic: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] rounded-full border border-line bg-card p-1 font-display text-xs font-semibold",
    item: "shrink-0 rounded-full px-3 py-1.5",
    on: "bg-good text-white",
    off: "text-muted hover:text-ink",
  },
  terminal: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] border border-t-line font-mono text-[11px] uppercase",
    item: "shrink-0 px-2.5 py-1",
    on: "bg-t-amber font-bold text-black",
    off: "text-t-dim hover:text-white",
  },
  bento: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] rounded-2xl bg-white/80 p-1 font-bento text-xs font-semibold shadow-[0_2px_12px_rgba(15,23,42,0.08)] backdrop-blur",
    item: "shrink-0 rounded-xl px-3 py-1.5",
    on: "bg-slate-900 text-white",
    off: "text-slate-500 hover:text-slate-900",
  },
  evidence: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] rounded-xl border border-zinc-200 bg-white p-0.5 text-xs font-semibold",
    item: "shrink-0 rounded-lg px-2.5 py-1.5",
    on: "bg-zinc-900 text-white",
    off: "text-zinc-500 hover:text-zinc-900",
  },
  decision: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] gap-1 font-flow text-xs font-medium",
    item: "shrink-0 rounded-md border px-2.5 py-1",
    on: "border-indigo-600 bg-indigo-600 text-white",
    off: "border-slate-300 bg-white text-slate-600 hover:border-slate-400",
  },
};

/** Switches the report between the prototype designs. */
export function DesignSwitch({ look }: { look: keyof typeof STYLES }) {
  const current = useDesign();
  const s = STYLES[look];
  return (
    <div role="group" aria-label={t("Report design")} className={s.group}>
      {DESIGNS.map((d) => (
        <button key={d.id} type="button" aria-pressed={d.id === current} onClick={() => setDesign(d.id)} className={`${s.item} ${d.id === current ? s.on : s.off}`}>
          {t(d.label)}
        </button>
      ))}
    </div>
  );
}
