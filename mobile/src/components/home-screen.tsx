import { SquarePen } from "lucide-react"
import { Children, useState, type ReactNode } from "react"

import { HomeBar } from "@/components/home-bar"
import { HostChrome } from "@/components/host-chrome"
import { InboxRow, type RowState } from "@/components/inbox-row"
import { InboxSkeleton } from "@/components/inbox-skeleton"
import { PullToRefresh } from "@/components/pull-to-refresh"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import { inboxPreview } from "@/lib/inbox-preview"
import { searchRunning, searchThreads } from "@/lib/inbox-search"
import { collectLive } from "@/lib/resume"
import type { ProjectView, RunningView, ThreadView } from "@/lib/rpc"
import type { SavedLink } from "@/lib/store"

export function HomeScreen({
  hosts,
  activeFingerprint,
  projects,
  threads,
  running,
  more,
  onOpen,
  onMore,
  onNewChat,
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
  onNewChat: (projectId: string) => void
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
  const [query, setQuery] = useState("")
  const roster = collectLive(running, threads)
  const skip = new Set(roster.map((r) => r.thread_id))
  const live = searchRunning(roster, query)
  const grouped = groupThreads(projects, searchThreads(threads, query), skip).filter(
    // A project with nothing left to show is still a place to start one,
    // unless a search just proved it has no match.
    (g) => g.threads.length > 0 || (!query && g.id !== ""),
  )
  // The skeleton means "nothing has arrived yet", so it reads the roster
  // rather than the filtered rows: a query that matches nothing is an
  // answer, not a reason to paint a dead link as still loading.
  const waiting = connecting || (!connected && running.length === 0 && threads.length === 0)
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
        onNewChat={() => onNewChat("")}
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
              <InboxSection
                key={g.id || "recent"}
                title={g.name}
                onNew={g.id ? () => onNewChat(g.id) : undefined}
              >
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
            {empty && !error ? query ? <NoMatch /> : <EmptyInbox /> : null}
            {more && !query ? (
              <Button variant="outline" onClick={onMore}>
                {t("home.more")}
              </Button>
            ) : null}
          </div>
        </PullToRefresh>
      )}

      <HomeBar query={query} onQuery={setQuery} onNewChat={() => onNewChat("")} />
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

function NoMatch() {
  return (
    <p className="px-6 py-16 text-center text-[13px] text-muted-foreground">
      {t("home.searchEmpty")}
    </p>
  )
}

function InboxSection({
  title,
  onNew,
  children,
}: {
  title: string
  onNew?: () => void
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-1.5">
      <div className="flex min-h-7 items-center gap-2 px-1">
        <h2 className="min-w-0 flex-1 truncate text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
          {title}
        </h2>
        {onNew ? (
          <button
            type="button"
            aria-label={t("home.newChatIn", { name: title })}
            className="inline-flex h-7 shrink-0 items-center gap-1 rounded-full bg-muted px-2.5 text-xs font-medium active:bg-accent"
            onClick={onNew}
          >
            <SquarePen className="size-3.5" aria-hidden />
            {t("home.newChat")}
          </button>
        ) : null}
      </div>
      {Children.count(children) > 0 ? (
        <ul className="divide-y divide-border overflow-hidden rounded-2xl border border-border bg-card">
          {children}
        </ul>
      ) : null}
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
    out.push({ id: p.id, name: p.name, threads: by.get(p.id) ?? [] })
  }
  if (rest.length) out.push({ id: "", name: t("home.recent"), threads: rest })
  return out
}
