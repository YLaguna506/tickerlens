// The AI model for new reports, remembered per browser. Empty means the
// server's default model.

import { useEffect, useState } from "react";

const KEY = "tickerlens.model";
const EVENT = "tickerlens:model";

function read(): string {
  try {
    return localStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}

let current = read();

export const getModel = () => current;

export function setModel(id: string) {
  current = id;
  try {
    localStorage.setItem(KEY, id);
  } catch {
    // Not remembered; the choice still applies until the page reloads.
  }
  window.dispatchEvent(new Event(EVENT));
}

export function useModel(): string {
  const [model, set] = useState(current);
  useEffect(() => {
    const onChange = () => set(current);
    window.addEventListener(EVENT, onChange);
    return () => window.removeEventListener(EVENT, onChange);
  }, []);
  return model;
}
