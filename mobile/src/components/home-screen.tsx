import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { localeSwitchLabel, t } from "@/lib/i18n"
import type { ProjectView, RunningView, ThreadView } from "@/lib/rpc"
import { cn } from "@/lib/cn"

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
  onToggleLocale,
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
  onToggleLocale?: () => void
}) {
  const grouped = groupThreads(projects, threads)
  return (
    <main className="mx-auto flex min-h-[100dvh] max-w-lg flex-col">
      <header className="flex items-center justify-between gap-2 px-4 pb-2 pt-4">
        <div className="flex items-center gap-2">
          <h1 className="text-lg font-semibold tracking-tight">{t("home.app")}</h1>
          <span
            className={cn(
              "size-2 rounded-full",
              path === "direct" ? "bg-[hsl(var(--running))]" : "bg-muted-foreground/50",
            )}
            title={path === "direct" ? t("home.direct") : t("home.relay")}
            aria-label={`path=${path}`}
          />
        </div>
        <div className="flex items-center gap-1">
          {onToggleLocale ? (
            <Button variant="ghost" onClick={onToggleLocale} aria-label={localeSwitchLabel()}>
              {localeSwitchLabel()}
            </Button>
          ) : null}
          <Button variant="ghost" onClick={onUnlink}>
            {t("home.unlink")}
          </Button>
        </div>
      </header>

      <div className="flex flex-1 flex-col gap-6 px-4 pb-4">
        {running.length > 0 ? (
          <section className="flex flex-col gap-1">
            <h2 className="text-sm font-medium text-muted-foreground">
              {t("home.inProgress")}
            </h2>
            <ul className="divide-y divide-border">
              {running.map((r) => (
                <li key={r.thread_id}>
                  <button
                    type="button"
                    className="w-full py-3 text-left"
                    onClick={() => onOpen(r.thread_id)}
                  >
                    <div className="text-sm font-medium">{r.title}</div>
                    <div className="text-xs text-[hsl(var(--running))]">
                      {r.ask_user ? t("home.ask") : r.action || t("thread.running")}
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        ) : null}

        {grouped.map((g) => (
          <section key={g.id || "recent"} className="flex flex-col gap-1">
            <h2 className="text-sm font-medium text-muted-foreground">{g.name}</h2>
            <ul className="divide-y divide-border">
              {g.threads.map((th) => (
                <li key={th.id}>
                  <button
                    type="button"
                    className="w-full py-3 text-left"
                    onClick={() => onOpen(th.id)}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm font-medium">{th.title || th.id}</span>
                      {th.running ? (
                        <span className="text-xs text-[hsl(var(--running))]">
                          {t("home.live")}
                        </span>
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
          </section>
        ))}
        {more ? (
          <Button variant="outline" onClick={onMore}>
            {t("home.more")}
          </Button>
        ) : null}
      </div>

      <NewThreadForm projects={projects} onStart={onStart} />
    </main>
  )
}

function groupThreads(projects: ProjectView[], threads: ThreadView[]) {
  const names = new Map(projects.map((p) => [p.id, p.name]))
  const by = new Map<string, ThreadView[]>()
  const rest: ThreadView[] = []
  for (const th of threads) {
    if (th.project_id && names.has(th.project_id)) {
      const list = by.get(th.project_id) ?? []
      list.push(th)
      by.set(th.project_id, list)
    } else {
      rest.push(th)
    }
  }
  const out: { id: string; name: string; threads: ThreadView[] }[] = []
  for (const p of projects) {
    const list = by.get(p.id)
    if (list?.length) out.push({ id: p.id, name: p.name, threads: list })
  }
  if (rest.length) out.push({ id: "", name: t("home.recent"), threads: rest })
  return out
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
      className="sticky bottom-0 flex flex-col gap-2 border-t border-border bg-background px-4 py-3"
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
        {t("home.new")}
      </label>
      {projects.length > 0 ? (
        <label className="text-sm">
          {t("home.project")}
          <select
            name="project"
            aria-label={t("home.project")}
            className="mt-1 flex h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
          >
            <option value="">{t("home.defaultProject")}</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
      ) : null}
      <Input id="new-thread" name="text" aria-label={t("home.newMessage")} />
      <Button type="submit">{t("home.start")}</Button>
    </form>
  )
}
