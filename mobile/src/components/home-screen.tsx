import type { ReactNode } from "react"
import { ChevronRight } from "lucide-react"

import { HostChrome } from "@/components/host-chrome"
import { InboxSkeleton } from "@/components/inbox-skeleton"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { t } from "@/lib/i18n"
import { inboxPreview } from "@/lib/inbox-preview"
import { collectLive } from "@/lib/resume"
import type { ProjectView, RunningView, ThreadView } from "@/lib/rpc"
import type { SavedLink } from "@/lib/store"
import { cn } from "@/lib/cn"

export function HomeScreen({
  hosts,
  activeFingerprint,
  projects,
  threads,
  running,
  more,
  onOpen,
  onMore,
  onStart,
  onSelectHost,
  onAddHost,
  onUnlink,
  onRetry,
  path,
  connected = true,
  reconnecting = false,
  connecting = false,
  error,
  onToggleLocale,
}: {
  hosts: SavedLink[]
  activeFingerprint: string
  projects: ProjectView[]
  threads: ThreadView[]
  running: RunningView[]
  more: boolean
  onOpen: (id: string) => void
  onMore: () => void
  onStart: (text: string, projectId: string) => void
  onSelectHost: (fingerprint: string) => void
  onAddHost: () => void
  onUnlink: () => void
  onRetry?: () => void
  path: string
  connected?: boolean
  reconnecting?: boolean
  connecting?: boolean
  error?: string
  onToggleLocale?: () => void
}) {
  const live = collectLive(running, threads)
  const skip = new Set(live.map((r) => r.thread_id))
  const grouped = groupThreads(projects, threads, skip)
  const waiting = connecting || (!connected && live.length === 0 && grouped.length === 0)
  return (
    <main className="mx-auto flex h-full max-w-lg flex-col overflow-hidden">
      <HostChrome
        hosts={hosts}
        activeFingerprint={activeFingerprint}
        path={path}
        connected={connected}
        reconnecting={reconnecting}
        onSelect={onSelectHost}
        onAdd={onAddHost}
        onUnlink={onUnlink}
        onToggleLocale={onToggleLocale}
      />

      {waiting ? (
        <InboxSkeleton pending={connecting || reconnecting} error={error} onRetry={onRetry} />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto px-4 py-4">
          {error ? (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          ) : null}
          {live.length > 0 ? (
            <InboxSection title={t("home.inProgress")}>
              {live.map((r) => (
                <ThreadRow
                  key={r.thread_id}
                  id={r.thread_id}
                  title={r.title || r.thread_id}
                  detail={
                    r.ask_user
                      ? t("home.ask")
                      : inboxPreview(r.action) || (r.waiting ? t("home.waiting") : t("thread.running"))
                  }
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
                  detail={inboxPreview(th.summary) || undefined}
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
      )}

      <NewThreadForm projects={projects} onStart={onStart} disabled={waiting || !connected} />
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
  disabled,
}: {
  projects: ProjectView[]
  onStart: (text: string, projectId: string) => void
  disabled?: boolean
}) {
  return (
    <form
      className="flex shrink-0 flex-col gap-2 border-t border-border bg-background px-3 py-2"
      onSubmit={(e) => {
        e.preventDefault()
        if (disabled) return
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
          disabled={disabled}
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
          disabled={disabled}
        />
        <Button type="submit" className="h-11 shrink-0" disabled={disabled}>
          {t("home.start")}
        </Button>
      </div>
    </form>
  )
}
