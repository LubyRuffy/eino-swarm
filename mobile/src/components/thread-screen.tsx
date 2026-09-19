import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import type { ThreadDetail } from "@/lib/rpc"

export function ThreadScreen({
  detail,
  onBack,
  onSend,
  onStop,
  onAnswer,
}: {
  detail: ThreadDetail
  onBack: () => void
  onSend: (text: string) => void
  onStop: () => void
  onAnswer: (text: string) => void
}) {
  return (
    <main className="mx-auto flex max-w-lg flex-col gap-4 p-4">
      <header className="flex items-center gap-2">
        <Button variant="ghost" onClick={onBack}>
          Back
        </Button>
        <h1 className="text-lg font-semibold">{detail.title}</h1>
      </header>
      {detail.goal_on && detail.goal ? (
        <p className="text-sm text-muted-foreground">{detail.goal}</p>
      ) : null}
      {detail.running ? (
        <div className="rounded-md border border-border p-3 text-sm">
          {detail.running.ask_user ? "Waiting for an answer" : detail.running.action || "running"}
          <Button className="ml-2" variant="destructive" onClick={onStop}>
            Stop
          </Button>
        </div>
      ) : null}
      <ol className="flex flex-col gap-3">
        {(detail.turns ?? []).map((t) => (
          <li key={t.id} className="rounded-md bg-secondary p-3 text-sm">
            <div className="text-xs text-muted-foreground">{t.status}</div>
            <p className="whitespace-pre-wrap">{t.text}</p>
          </li>
        ))}
      </ol>
      <form
        className="flex flex-col gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          const fd = new FormData(e.currentTarget)
          const text = String(fd.get("text") ?? "").trim()
          if (!text) return
          if (detail.running?.ask_user) onAnswer(text)
          else onSend(text)
          e.currentTarget.reset()
        }}
      >
        <Input
          name="text"
          aria-label={detail.running?.ask_user ? "Answer" : "Message"}
        />
        <Button type="submit">
          {detail.running?.ask_user ? "Answer" : detail.running ? "Follow-up" : "Send"}
        </Button>
      </form>
    </main>
  )
}
