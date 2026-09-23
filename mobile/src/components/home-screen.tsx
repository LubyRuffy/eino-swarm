import { ChevronRight, Loader2, SquarePen } from "lucide-react"
import { Children, useState, type ReactNode } from "react"

import { HomeBar } from "@/components/home-bar"
import { HostChrome } from "@/components/host-chrome"
import { InboxRow, type RowState } from "@/components/inbox-row"
import { InboxSkeleton } from "@/components/inbox-skeleton"
import { PullToRefresh } from "@/components/pull-to-refresh"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { groupInbox } from "@/lib/inbox-groups"
import { inboxPreview } from "@/lib/inbox-preview"
import { searchRunning, searchThreads } from "@/lib/inbox-search"
import { collectLive } from "@/lib/resume"
import type { ProjectView, RunningView, ThreadView } from "@/lib/rpc"
import type { SavedLink } from "@/lib/store"

// HomeScreen unmounts when a conversation opens. A fold that resets on
// Back would look like the tap did nothing.
const FOLD_KEY = "zwai.phone.project-folded"

function readFolded(): Set<string> {
  try {
    const raw = localStorage.getItem(FOLD_KEY)
    if (!raw) return new Set()
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((id): id is string => typeof id === "string"))
  } catch {
    return new Set()
  }
}

function writeFolded(ids: Set<string>) {
  try {
    localStorage.setItem(FOLD_KEY, JSON.stringify([...ids]))
  } catch {
    // A preference is not worth failing the inbox.
  }
}

export function HomeScreen({
  hosts,
  activeFingerprint,
  projects,
  threads,
  running,
  more,
  loadingMore = false,
  onOpen,
  onMore,
  onNewChat,
  onSelectHost,
  onAddHost,
  onUnlink,
  onRetry,
  onRefresh,
  showChat = false,
  onSelectChat,
  onModels,
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
  loadingMore?: boolean
  onOpen: (id: string) => void
  onMore: () => void
  onNewChat: (projectId: string) => void
  onSelectHost: (fingerprint: string) => void
  onAddHost: () => void
  onUnlink: () => void
  onRetry?: () => void
  onRefresh?: () => Promise<void> | void
  showChat?: boolean
  onSelectChat?: () => void
  onModels?: () => void
  path: string
  connected?: boolean
  reconnecting?: boolean
  connecting?: boolean
  error?: string
  onToggleLocale?: () => void
}) {
  const [query, setQuery] = useState("")
  const [folded, setFolded] = useState(readFolded)
  const roster = collectLive(running, threads)
  const liveById = new Map(roster.map((r) => [r.thread_id, r]))
  const live = searchRunning(roster, query)
  const grouped = groupInbox(projects, searchThreads(threads, query), live).filter(
    // A project with nothing left to show is still a place to start one,
    // unless a search just proved it has no match.
    (g) => g.threads.length > 0 || (!query && g.id !== ""),
  )
  const toggleProject = (id: string) => {
    setFolded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      writeFolded(next)
      return next
    })
  }
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
        showChat={showChat}
        onSelectChat={onSelectChat}
        onModels={onModels}
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
                collapsible={Boolean(g.id)}
                // A query paints the folder open so a match is not stuck
                // behind a fold. The saved fold returns when the query goes.
                open={Boolean(query) || !folded.has(g.id)}
                onToggle={() => toggleProject(g.id)}
              >
                {g.threads.map((th) => {
                  const row = projectRow(th, liveById.get(th.id))
                  return (
                    <InboxRow
                      key={th.id}
                      id={th.id}
                      title={th.title || th.id}
                      detail={row.detail}
                      state={row.state}
                      at={row.at}
                      onOpen={onOpen}
                    />
                  )
                })}
              </InboxSection>
            ))}
            {empty && !error ? query ? <NoMatch /> : <EmptyInbox /> : null}
            {more && !query ? (
              <Button
                variant="outline"
                onClick={onMore}
                disabled={loadingMore}
                aria-busy={loadingMore || undefined}
              >
                {loadingMore ? (
                  <Loader2 className="size-4 motion-safe:animate-spin motion-reduce:animate-none" aria-hidden />
                ) : null}
                {loadingMore ? t("home.loadingMore") : t("home.more")}
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

function projectRow(th: ThreadView, live?: RunningView): {
  state: RowState
  at?: string
  detail?: string
} {
  if (!live) {
    return { state: "idle", at: th.last_active_at, detail: inboxPreview(th.summary) || undefined }
  }
  return {
    state: liveState(live),
    at: live.waiting && !live.ask_user ? live.last_active_at || th.last_active_at : undefined,
    detail: inboxPreview(live.action) || inboxPreview(th.summary) || undefined,
  }
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
  collapsible = false,
  open = true,
  onToggle,
  children,
}: {
  title: string
  onNew?: () => void
  collapsible?: boolean
  open?: boolean
  onToggle?: () => void
  children: ReactNode
}) {
  const label = "truncate text-[11px] font-semibold uppercase tracking-wide text-muted-foreground"
  return (
    <section className="flex flex-col gap-1.5">
      <div className="flex min-h-7 items-center gap-1 px-1">
        {collapsible ? (
          <h2 className="min-w-0 flex-1">
            <button
              type="button"
              aria-expanded={open}
              className="flex w-full min-w-0 items-center gap-1 text-left"
              onClick={onToggle}
            >
              <ChevronRight
                className={cn(
                  "size-3.5 shrink-0 text-muted-foreground transition-transform",
                  open && "rotate-90",
                )}
                aria-hidden
              />
              <span className={cn("min-w-0 flex-1", label)}>{title}</span>
            </button>
          </h2>
        ) : (
          <h2 className={cn("min-w-0 flex-1", label)}>{title}</h2>
        )}
        {onNew ? (
          <Button
            type="button"
            variant="ghost"
            aria-label={t("home.newChatIn", { name: title })}
            className="h-11 w-11 shrink-0 rounded-full px-0 text-muted-foreground"
            onClick={onNew}
          >
            <SquarePen className="size-4" aria-hidden />
          </Button>
        ) : null}
      </div>
      {open && Children.count(children) > 0 ? (
        <ul className="divide-y divide-border overflow-hidden rounded-2xl border border-border bg-card">
          {children}
        </ul>
      ) : null}
    </section>
  )
}
