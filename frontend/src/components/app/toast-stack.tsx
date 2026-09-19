import { AlertCircle, X } from "lucide-react"
import { createPortal } from "react-dom"

import { Button } from "@/components/ui/button"
import { useT } from "@/lib/use-t"
import { useToasts, type AppToast } from "@/store/toasts"

/** Portaled onto `document.body` so a toast clears an open Settings sheet.
 *  The sheet is a full-page dialog at z-50; painting inside React's root
 *  lets that overlay cover the error the human just caused. */
export function ToastStack() {
  const t = useT()
  const toasts = useToasts((s) => s.toasts)
  if (toasts.length === 0) return null

  return createPortal(
    <div
      className="pointer-events-none toast-layer fixed inset-x-0 top-4 flex justify-center px-4"
      role="region"
      aria-label={t("toast.region")}
    >
      <div className="flex w-full max-w-lg flex-col gap-2">
        {toasts.map((toast) => (
          <ToastCard key={toast.id} toast={toast} />
        ))}
      </div>
    </div>,
    document.body,
  )
}

function ToastCard({ toast }: { toast: AppToast }) {
  const t = useT()
  const dismiss = useToasts((s) => s.dismiss)
  return (
    <div
      role="alert"
      className="pointer-events-auto flex items-start gap-2 rounded-lg border border-destructive/40 bg-popover/95 px-3 py-2 text-sm text-destructive shadow-lg backdrop-blur-md"
    >
      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
      <div className="min-w-0 flex-1">
        {toast.title ? (
          <p className="font-medium text-foreground">{toast.title}</p>
        ) : null}
        <p className="mt-0.5 max-h-24 overflow-y-auto whitespace-pre-wrap break-words">
          {toast.message}
        </p>
      </div>
      <Button
        type="button"
        size="icon-xs"
        variant="ghost"
        className="-mt-0.5 shrink-0 text-muted-foreground hover:text-foreground"
        aria-label={t("toast.dismiss")}
        onClick={() => dismiss(toast.id)}
      >
        <X />
      </Button>
    </div>
  )
}
