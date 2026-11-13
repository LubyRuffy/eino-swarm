import { FileText, Layers, Search } from "lucide-react"

import { chromeTypeClass } from "@/lib/chrome-type"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"

/** Shown before the first message. Its job is to answer "what do I type?",
 *  so the examples describe shapes of work rather than a canned prompt. */
export function EmptyState({ onPick }: { onPick: (text: string) => void }) {
  const t = useT()
  const ideas = [
    {
      icon: Search,
      title: t("empty.research.title"),
      hint: t("empty.research.hint"),
      text: t("empty.research.text"),
    },
    {
      icon: FileText,
      title: t("empty.files.title"),
      hint: t("empty.files.hint"),
      text: t("empty.files.text"),
    },
    {
      icon: Layers,
      title: t("empty.split.title"),
      hint: t("empty.split.hint"),
      text: t("empty.split.text"),
    },
  ]

  return (
    <div className="content-gutter flex w-full min-h-0 flex-1 flex-col justify-center pt-10 pb-composer">
      <div className="content-column">
        <h1 className="text-center text-3xl font-semibold tracking-tight">
          {t("empty.title")}
        </h1>
        <p
          className={cn(
            chromeTypeClass,
            "mx-auto mt-3 max-w-xl text-balance text-center leading-relaxed text-muted-foreground",
          )}
        >
          {t("empty.lead")}
        </p>
        <div className="mt-10 grid gap-3 sm:grid-cols-3">
          {ideas.map(({ icon: Icon, title, hint, text }) => (
            <button
              key={title}
              type="button"
              onClick={() => onPick(text)}
              className="group flex h-full flex-col items-start gap-3 rounded-xl border border-border bg-card p-4 text-left shadow-xs transition-all hover:border-ring hover:bg-accent/40 hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span className="flex size-8 items-center justify-center rounded-lg bg-muted text-muted-foreground transition-colors group-hover:bg-background group-hover:text-foreground">
                <Icon className="size-4" />
              </span>
              <span className="flex flex-col gap-1">
                <span className={cn(chromeTypeClass, "font-medium")}>{title}</span>
                <span className={cn(chromeTypeClass, "leading-relaxed text-muted-foreground")}>
                  {hint}
                </span>
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
