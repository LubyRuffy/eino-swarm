import { useId, useState } from "react"
import { ChevronRight } from "lucide-react"

import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"

export function PhoneThought({ text, className }: { text: string; className?: string }) {
  const [open, setOpen] = useState(false)
  const contentId = useId()
  return (
    <div className="min-w-0 max-w-full">
      <button
        type="button"
        data-testid="phone-thought-toggle"
        className="flex min-w-0 max-w-full items-center gap-1 text-left text-xs text-muted-foreground"
        aria-expanded={open}
        aria-controls={contentId}
        onClick={() => setOpen((value) => !value)}
      >
        <ChevronRight aria-hidden className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")} />
        {t("thread.thought")}
      </button>
      {open ? (
        <p
          id={contentId}
          data-testid="phone-thought"
          className={cn("whitespace-pre-wrap text-xs text-muted-foreground", className)}
        >
          {text}
        </p>
      ) : null}
    </div>
  )
}
