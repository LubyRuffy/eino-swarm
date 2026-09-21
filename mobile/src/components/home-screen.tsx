import type { ReactNode } from "react"
import { ChevronRight } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { localeSwitchLabel, t } from "@/lib/i18n"
import { collectLive } from "@/lib/resume"
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
  const live = collectLive(running, threads)
  const skip = new Set(live.map((r) => r.thread_id))
  const grouped = groupThreads(projects, threads, skip)
  return (
    <main className="mx-auto flex h-full max-w-lg flex-col overflow-hidden">
      <header className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
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

      <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-4 py-4">
        {live.length > 0 ? (
          <InboxSection title={t("home.inProgress")}>
            {live.map((r) => (
              <ThreadRow
                key={r.thread_id}
                id={r.thread_id}
                title={r.title || r.thread_id}
                detail={r.ask_user ? t("home.ask") : r.action || t("thread.running")}
                live
                onOpen={onOpen}
              />
            ))}
          </InboxSection>
        ) : null}

        {grouped.map((g) => (
          <InboxSection key={g.id || "recent"} title={g.name}>
            {g.threads.map((th) => (
              <ThreadRow
                key={th.id}
                id={th.id}
                title={th.title || th.id}
                detail={th.summary}
                onOpen={onOpen}
              />
            ))}
          </InboxSection>
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

function InboxSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h2 className="px-1 text-sm font-medium text-muted-foreground">{title}</h2>
      <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border bg-card">
        {children}
      </ul>
    </section>
  )
}

function ThreadRow({
  id,
  title,
  detail,
  live,
  onOpen,
}: {
  id: string
  title: string
  detail?: string
  live?: boolean
  onOpen: (id: string) => void
}) {
  return (
    <li>
      <button
        type="button"
        aria-label={t("home.open", { title })}
        className="flex min-h-11 w-full items-center gap-3 px-3 py-3 text-left hover:bg-accent active:bg-accent"
        onClick={() => onOpen(id)}
      >
        {live ? (
          <span
            className="size-2 shrink-0 animate-pulse rounded-full bg-[hsl(var(--running))]"
            aria-hidden
          />
        ) : null}
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">{title}</span>
          {detail ? (
            <span
              className={cn(
                "mt-0.5 block truncate text-xs",
                live ? "text-[hsl(var(--running))]" : "text-muted-foreground",
              )}
            >
              {detail}
            </span>
          ) : null}
        </span>
        <ChevronRight className="size-4 shrink-0 text-muted-foreground" aria-hidden />
      </button>
    </li>
  )
}

function groupThreads(projects: ProjectView[], threads: ThreadView[], skip: Set<string>) {
  const names = new Map(projects.map((p) => [p.id, p.name]))
  const by = new Map<string, ThreadView[]>()
  const rest: ThreadView[] = []
  for (const th of threads) {
    if (skip.has(th.id)) continue
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
      className="flex shrink-0 flex-col gap-2 border-t border-border bg-background px-3 py-2"
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
      {projects.length > 0 ? (
        <select
          name="project"
          aria-label={t("home.project")}
          className="flex h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="">{t("home.defaultProject")}</option>
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      ) : null}
      <div className="flex items-center gap-2">
        <Input
          id="new-thread"
          name="text"
          aria-label={t("home.newMessage")}
          placeholder={t("home.newMessage")}
          className="h-11 bg-muted"
        />
        <Button type="submit" className="h-11 shrink-0">
          {t("home.start")}
        </Button>
      </div>
    </form>
  )
}
