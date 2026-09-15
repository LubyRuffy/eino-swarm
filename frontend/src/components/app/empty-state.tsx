import { FileText, Layers, Search } from "lucide-react"

/** Shown before the first message. Its job is to answer "what do I type?",
 *  so the examples describe shapes of work rather than a canned prompt. */
export function EmptyState({ onPick }: { onPick: (text: string) => void }) {
  const ideas = [
    {
      icon: Search,
      title: "Research something broad",
      hint: "Several sub-agents look into different angles at once.",
      text: "Research a topic from several angles and give me one merged brief with sources.",
    },
    {
      icon: FileText,
      title: "Work through files",
      hint: "Attach files with the paperclip, then say what to do with them.",
      text: "Go through the files in the workspace and summarise what each one contains.",
    },
    {
      icon: Layers,
      title: "Split a big task",
      hint: "Say the goal; it decides how many workers it needs.",
      text: "Break this goal into parallel pieces, work them in parallel, then combine the results.",
    },
  ]

  return (
    <div className="flex flex-1 items-center justify-center px-6 py-10">
      <div className="w-full max-w-2xl">
        <h1 className="text-center text-2xl font-semibold tracking-tight">
          What should we work on?
        </h1>
        <p className="mt-2 text-center text-sm text-muted-foreground">
          Describe the outcome you want. It delegates to sub-agents when the
          work is worth splitting up, and you can steer it while it runs.
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
