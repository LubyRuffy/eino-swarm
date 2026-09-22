import { useState, type ReactNode } from "react"

import { Composer } from "@/components/composer"
import { HostChrome } from "@/components/host-chrome"
import { InboxRow, type RowState } from "@/components/inbox-row"
import { InboxSkeleton } from "@/components/inbox-skeleton"
import { PullToRefresh } from "@/components/pull-to-refresh"
import { Button } from "@/components/ui/button"
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
  onRefresh,
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
  onRefresh?: () => Promise<void> | void
  path: string
  connected?: boolean
  reconnecting?: boolean
  connecting?: boolean
  error?: string
  onToggleLocale?: () => void
}) {
  const [project, setProject] = useState("")
  const live = collectLive(running, threads)
  const skip = new Set(live.map((r) => r.thread_id))
  const grouped = groupThreads(projects, threads, skip)
  const waiting = connecting || (!connected && live.length === 0 && grouped.length === 0)
  const empty = live.length === 0 && grouped.length === 0
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
        <PullToRefresh onRefresh={onRefresh} className="flex-1 px-3 py-3">
          <div className="flex flex-col gap-5">
            {error ? (
              <p className="px-1 text-sm text-destructive" role="alert">
                {error}
              </p>
            ) : null}
            {live.length > 0 ? (
              <InboxSection title={t("home.inProgress")}>
                {live.map((r) => (
                  <InboxRow
                    key={r.thread_id}
                    id={r.thread_id}
                    title={r.title || r.thread_id}
                    // A parked wait has no live turn to preview; the host
                    // sends the thread's own summary as the line instead.
                    detail={inboxPreview(r.action) || undefined}
                    state={liveState(r)}
                    // Running is happening now. Only a wait has an age, and
                    // one armed last week must not read like today's work.
                    at={r.waiting && !r.ask_user ? r.last_active_at : undefined}
                    onOpen={onOpen}
                  />
                ))}
              </InboxSection>
            ) : null}

            {grouped.map((g) => (
              <InboxSection key={g.id || "recent"} title={g.name}>
                {g.threads.map((th) => (
                  <InboxRow
                    key={th.id}
                    id={th.id}
                    title={th.title || th.id}
                    detail={inboxPreview(th.summary) || undefined}
                    state="idle"
                    at={th.last_active_at}
                    onOpen={onOpen}
                  />
                ))}
              </InboxSection>
            ))}
            {empty && !error ? <EmptyInbox /> : null}
            {more ? (
              <Button variant="outline" onClick={onMore}>
                {t("home.more")}
              </Button>
            ) : null}
          </div>
        </PullToRefresh>
      )}

      <Composer
        label={t("home.newMessage")}
        sendLabel={t("home.start")}
        disabled={waiting || !connected}
        above={
          projects.length > 0 ? (
            <ProjectChips projects={projects} value={project} onChange={setProject} />
          ) : null
        }
        onSubmit={(text) => onStart(text, project)}
      />
    </main>
  )
}

function liveState(r: RunningView): RowState {
  if (r.ask_user) return "ask"
  if (r.waiting) return "waiting"
  return "running"
}

function EmptyInbox() {
  return (
    <div className="flex flex-col items-center gap-1 px-6 py-16 text-center">
      <p className="text-sm font-medium">{t("home.emptyTitle")}</p>
      <p className="text-[13px] text-muted-foreground">{t("home.emptyHint")}</p>
    </div>
  )
}

function ProjectChips({
  projects,
  value,
  onChange,
}: {
  projects: ProjectView[]
  value: string
  onChange: (id: string) => void
}) {
  const rows = [{ id: "", name: t("home.defaultProject") }, ...projects]
  return (
    <div
      role="radiogroup"
      aria-label={t("home.project")}
      className="rail mb-2 flex gap-1.5 overflow-x-auto pb-0.5"
    >
      {rows.map((p) => (
        <button
          key={p.id || "default"}
          type="button"
          role="radio"
          aria-checked={p.id === value}
          className={cn(
            "h-7 shrink-0 rounded-full px-3 text-xs transition-colors",
            p.id === value
              ? "bg-primary text-primary-foreground"
              : "bg-muted text-muted-foreground",
          )}
          onClick={() => onChange(p.id)}
        >
          {p.name}
        </button>
      ))}
    </div>
  )
}

function InboxSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-1.5">
      <h2 className="px-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </h2>
      <ul className="divide-y divide-border overflow-hidden rounded-2xl border border-border bg-card">
        {children}
      </ul>
    </section>
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