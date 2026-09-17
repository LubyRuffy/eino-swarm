import { FileText, Layers, Search } from "lucide-react"

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
        <h1 className="text-center text-2xl font-semibold tracking-tight">
          {t("empty.title")}
        </h1>
        <p className="mt-2 text-center text-sm text-muted-foreground">
          {t("empty.lead")}
        </p>
        <div className="mt-8 grid gap-2 sm:grid-cols-3">
          {ideas.map(({ icon: Icon, title, hint, text }) => (
            <button
              key={title}
              type="button"
              onClick={() => onPick(text)}
              className="rounded-xl border border-border bg-card p-3 text-left transition-colors hover:border-ring hover:bg-accent/50"
            >
              <Icon className="size-4 text-muted-foreground" />
              <p className="mt-2 text-[13px] font-medium">{title}</p>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">{hint}</p>
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
