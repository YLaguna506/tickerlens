// An in-app confirmation dialog, in place of the browser's window.confirm.
// The native <dialog> opened with showModal() keeps focus inside, closes on
// Escape and sits above everything else on the page.

import { useEffect, useRef, type ReactNode } from "react";
import { t } from "../i18n";

export function ConfirmDialog({
  open,
  title,
  children,
  confirmLabel,
  danger = false,
  className = "",
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  children?: ReactNode;
  confirmLabel: string;
  danger?: boolean;
  /** Theme class for the design it opens from (look-light, look-terminal). */
  className?: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      aria-labelledby="confirm-title"
      onCancel={(e) => {
        // Escape: let React state close it.
        e.preventDefault();
        onCancel();
      }}
      onClick={(e) => {
        // A click on the backdrop (the dialog element itself, outside the panel).
        if (e.target === e.currentTarget) onCancel();
      }}
      className={`confirm-dialog m-auto w-[min(28rem,calc(100vw-2rem))] rounded-2xl border border-line bg-card p-0 text-ink shadow-2xl ${className}`}
    >
      <div className="space-y-3 p-6">
        <h2 id="confirm-title" className="font-display text-lg font-bold">
          {title}
        </h2>
        <div className="space-y-2 text-sm leading-relaxed text-muted">{children}</div>
      </div>
      <div className="flex justify-end gap-2 border-t border-line px-6 py-4">
        <button type="button" autoFocus onClick={onCancel} className="rounded-full border border-line px-4 py-2 text-sm font-semibold text-ink hover:bg-paper">
          {t("Cancel")}
        </button>
        <button
          type="button"
          onClick={onConfirm}
          className={`rounded-full px-4 py-2 text-sm font-semibold text-white ${danger ? "bg-bad hover:brightness-110" : "bg-good hover:brightness-110"}`}
        >
          {confirmLabel}
        </button>
      </div>
    </dialog>
  );
}
