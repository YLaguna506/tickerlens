// Which report design to show: the URL's ?ui= wins, then the viewer's last
// choice, then the classic design.

import { useEffect, useState } from "react";

export type Design = "classic" | "evidence" | "bento" | "decision" | "terminal";

export const DESIGNS: { id: Design; label: string }[] = [
  { id: "classic", label: "Classic" },
  { id: "evidence", label: "Evidence" },
  { id: "bento", label: "Bento" },
  { id: "decision", label: "Decision" },
  { id: "terminal", label: "Terminal" },
];

const KEY = "tickerlens.design";
const isDesign = (v: string | null): v is Design => DESIGNS.some((d) => d.id === v);

function readDesign(): Design {
  const fromUrl = new URLSearchParams(window.location.search).get("ui");
  if (isDesign(fromUrl)) return fromUrl;
  try {
    const saved = localStorage.getItem(KEY);
    if (isDesign(saved)) return saved;
  } catch {
    // Storage can be blocked; the default is fine.
  }
  return "classic";
}

export function setDesign(d: Design) {
  const url = new URL(window.location.href);
  if (d === "classic") url.searchParams.delete("ui");
  else url.searchParams.set("ui", d);
  window.history.replaceState(null, "", url);
  try {
    localStorage.setItem(KEY, d);
  } catch {
    // Not remembered; the URL still carries the choice.
  }
  window.dispatchEvent(new PopStateEvent("popstate"));
  window.scrollTo(0, 0);
}

export function useDesign(): Design {
  const [design, set] = useState(readDesign);
  useEffect(() => {
    const onChange = () => set(readDesign());
    window.addEventListener("popstate", onChange);
    return () => window.removeEventListener("popstate", onChange);
  }, []);
  return design;
}
