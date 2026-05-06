import { useEffect, useRef } from "react";
import { AlertTriangle } from "lucide-react";

// ConfirmModal renders a small confirm/cancel dialog over the page.
// Esc cancels. Auto-focuses the cancel button so a reflexive Enter
// fires Cancel (the button's native default-action behavior), not
// Confirm — defense against a user who opens a destructive-action
// modal and immediately hits Enter.
export function ConfirmModal({
  open,
  title,
  message,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  danger = false,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  message: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    cancelRef.current?.focus();
    // Capture-phase Esc handler so the modal cancels itself before any
    // ancestor (e.g., the EventDrawer's window-level Esc handler) sees
    // the key and closes the drawer too. Enter is intentionally NOT
    // captured here: with focus on the Cancel button, the browser's
    // native button-activation handles Enter for us, so a reflexive
    // Enter cancels rather than confirms.
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onCancel();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [open, onCancel]);

  if (!open) return null;

  return (
    <>
      <div className="fixed inset-0 bg-black/50 z-[60]" onClick={onCancel} aria-hidden />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="fixed inset-0 z-[70] flex items-center justify-center p-4 pointer-events-none"
      >
        <div className="bg-card text-card-foreground border border-border rounded-lg shadow-xl max-w-md w-full p-4 pointer-events-auto">
          <div className="flex items-start gap-3">
            {danger && (
              <AlertTriangle className="size-5 text-warning shrink-0 mt-0.5" aria-hidden />
            )}
            <div className="min-w-0 flex-1">
              <h2 className="text-base font-semibold">{title}</h2>
              <div className="mt-2 text-sm space-y-1">{message}</div>
            </div>
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <button
              ref={cancelRef}
              onClick={onCancel}
              className="px-3 py-1.5 text-sm rounded border border-border hover:bg-muted"
            >
              {cancelLabel}
            </button>
            <button
              onClick={onConfirm}
              className={`px-3 py-1.5 text-sm rounded border ${
                danger
                  ? "border-destructive text-destructive hover:bg-destructive/10"
                  : "border-primary text-primary hover:bg-primary/10"
              }`}
            >
              {confirmLabel}
            </button>
          </div>
        </div>
      </div>
    </>
  );
}

// hasRedactedValue walks v looking for any string equal to the
// audit-pipeline's "[redacted]" marker. Mirrors the server-side
// hasRedactedParam check so the UI can disable Replay before the
// server has to refuse it. Returns true on a matching leaf at any
// depth.
export function hasRedactedValue(v: unknown): boolean {
  if (v === "[redacted]") return true;
  if (Array.isArray(v)) return v.some(hasRedactedValue);
  if (v !== null && typeof v === "object") {
    return Object.values(v as Record<string, unknown>).some(hasRedactedValue);
  }
  return false;
}
