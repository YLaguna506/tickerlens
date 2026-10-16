import { DESIGNS, setDesign, useDesign } from "../design";

const STYLES = {
  classic: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] rounded-full bg-[#ece9e1] p-1 font-display text-xs font-semibold",
    item: "shrink-0 rounded-full px-3 py-1.5",
    on: "bg-white text-ink shadow-sm",
    off: "text-muted hover:text-ink",
  },
  terminal: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] border border-t-line font-mono text-[11px] uppercase",
    item: "shrink-0 px-2.5 py-1",
    on: "bg-t-amber font-bold text-black",
    off: "text-t-dim hover:text-white",
  },
  scorecard: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] border-2 border-black bg-white font-label text-xs font-bold uppercase",
    item: "shrink-0 px-2.5 py-1 not-last:border-r-2 not-last:border-black",
    on: "bg-black text-white",
    off: "text-black hover:bg-black/10",
  },
  bento: {
    group: "flex max-w-full overflow-x-auto [scrollbar-width:none] rounded-2xl bg-white/80 p-1 font-bento text-xs font-semibold shadow-[0_2px_12px_rgba(15,23,42,0.08)] backdrop-blur",
    item: "shrink-0 rounded-xl px-3 py-1.5",
    on: "bg-slate-900 text-white",
    off: "text-slate-500 hover:text-slate-900",
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
    <div role="group" aria-label="Report design" className={s.group}>
      {DESIGNS.map((d) => (
        <button key={d.id} type="button" aria-pressed={d.id === current} onClick={() => setDesign(d.id)} className={`${s.item} ${d.id === current ? s.on : s.off}`}>
          {d.label}
        </button>
      ))}
    </div>
  );
}
