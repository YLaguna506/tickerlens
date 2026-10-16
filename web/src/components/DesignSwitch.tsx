import { DESIGNS, setDesign, useDesign } from "../design";

const STYLES = {
  classic: {
    group: "flex rounded-full bg-[#ece9e1] p-1 font-display text-xs font-semibold",
    item: "rounded-full px-3 py-1.5",
    on: "bg-white text-ink shadow-sm",
    off: "text-muted hover:text-ink",
  },
  terminal: {
    group: "flex border border-t-line font-mono text-[11px] uppercase",
    item: "px-2.5 py-1",
    on: "bg-t-amber font-bold text-black",
    off: "text-t-dim hover:text-white",
  },
  editorial: {
    group: "flex gap-4 font-serif text-sm italic",
    item: "py-1",
    on: "text-e-ink underline decoration-e-accent decoration-2 underline-offset-4",
    off: "text-e-dim hover:text-e-ink",
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
