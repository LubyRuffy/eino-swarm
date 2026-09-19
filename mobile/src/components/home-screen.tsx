import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import type { ProjectView, RunningView, ThreadView } from "@/lib/rpc"

export function HomeScreen({
  projects,
  threads,
  running,
  more,
  onOpen,
  onMore,
  onStart,
  onUnlink,
  path,
}: {
  projects: ProjectView[]
  threads: ThreadView[]
  running: RunningView[]
  more: boolean
  onOpen: (id: string) => void
  onMore: () => void
  onStart: (text: string, projectId: string) => void
  onUnlink: () => void
  path: string
}) {
  return (
    <main className="mx-auto flex max-w-lg flex-col gap-6 p-4">
      <header className="flex items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">zwai</h1>
          <p className="text-xs text-muted-foreground">path={path}</p>
        </div>
        <Button variant="ghost" onClick={onUnlink}>
          Unlink
        </Button>
      </header>

      {running.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h2 className="text-sm font-medium">In progress</h2>
          <ul className="flex flex-col gap-2">
            {running.map((r) => (
              <li key={r.thread_id}>
                <button
                  type="button"
                  className="w-full rounded-md border border-border bg-card p-3 text-left"
                  onClick={() => onOpen(r.thread_id)}
                >
                  <div className="text-sm font-medium">{r.title}</div>
                  <div className="text-xs text-muted-foreground">
                    {r.ask_user ? "ask_user" : r.action || "running"}
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <NewThreadForm projects={projects} onStart={onStart} />

      <section className="flex flex-col gap-2">
        <h2 className="text-sm font-medium">Recent</h2>
        <ul className="flex flex-col gap-2">
          {threads.map((th) => (
            <li key={th.id}>
              <button
                type="button"
                className="w-full rounded-md border border-border bg-card p-3 text-left"
                onClick={() => onOpen(th.id)}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm font-medium">{th.title || th.id}</span>
                  {th.running ? (
                    <span className="text-xs text-[hsl(var(--running))]">live</span>
                  ) : null}
                </div>
                {th.summary ? (
                  <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
                    {th.summary}
                  </p>
                ) : null}
              </button>
            </li>
          ))}
        </ul>
        {more ? (
          <Button variant="outline" onClick={onMore}>
            More
          </Button>
        ) : null}
      </section>
    </main>
  )
}

function NewThreadForm({
  projects,
  onStart,
}: {
  projects: ProjectView[]
  onStart: (text: string, projectId: string) => void
}) {
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        const fd = new FormData(e.currentTarget)
        const text = String(fd.get("text") ?? "").trim()
        const projectId = String(fd.get("project") ?? "")
        if (text) {
          onStart(text, projectId)
          e.currentTarget.reset()
        }
      }}
    >
      <label className="text-sm font-medium" htmlFor="new-thread">
        New conversation
      </label>
      {projects.length > 0 ? (
        <label className="text-sm">
          Project
          <select
            name="project"
            aria-label="Project"
            className="mt-1 flex h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
          >
            <option value="">Default</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      ) : null}
      <Input id="new-thread" name="text" aria-label="New message" />
      <Button type="submit">Start</Button>
    </form>
  )
}
