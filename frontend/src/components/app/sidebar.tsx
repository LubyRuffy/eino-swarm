import {
  MessageSquarePlus,
  MoreHorizontal,
  Pencil,
  Search,
  Settings,
  Trash2,
} from "lucide-react"
import { useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { StatusDot } from "@/components/app/transcript"
import type { Thread } from "@/lib/types"
import { relativeDay } from "@/lib/utils"

/** Conversations, grouped the way people remember them. */
export function Sidebar({
  threads,
  activeId,
  runningId,
  onNew,
  onOpen,
  onRename,
  onDelete,
  onSearch,
  onSettings,
}: {
  threads: Thread[]
  activeId?: string
  runningId?: string
  onNew: () => void
  onOpen: (id: string) => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
  onSearch: () => void
  onSettings: () => void
}) {
  const groups = useMemo(() => groupByDay(threads), [threads])

  return (
    <aside className="flex h-full w-64 shrink-0 flex-col border-r border-sidebar-border bg-sidebar">
      <div
        className="flex items-center gap-1 px-3 pb-2 pt-3"
        data-drag-region
      >
        <Button
          variant="secondary"
          size="sm"
          className="flex-1 justify-start gap-2"
          onClick={onNew}
        >
          <MessageSquarePlus />
          New conversation
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onSearch}
          title="Search conversations (⌘K)"
        >
          <Search />
        </Button>
      </div>

      <div className="thin-scrollbar flex-1 overflow-y-auto px-2 pb-2">
        {threads.length === 0 ? (
          <p className="px-2 py-6 text-xs text-sidebar-foreground/70">
            No conversations yet. Start one and it will appear here.
          </p>
        ) : (
          groups.map(([label, items]) => (
            <div key={label} className="mb-2">
              <p className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/60">
                {label}
              </p>
              {items.map((thread) => (
                <Row
                  key={thread.id}
                  thread={thread}
                  active={thread.id === activeId}
                  running={thread.running || thread.id === runningId}
                  onOpen={onOpen}
                  onRename={onRename}
                  onDelete={onDelete}
                />
              ))}
            </div>
          ))
        )}
      </div>

      <div className="flex items-center justify-between border-t border-sidebar-border px-3 py-2">
        <Button variant="ghost" size="sm" className="gap-2" onClick={onSettings}>
          <Settings />
          Settings
        </Button>
      </div>
    </aside>
  )
}

function Row({
  thread,
  active,
  running,
  onOpen,
  onRename,
  onDelete,
}: {
  thread: Thread
  active: boolean
  running: boolean
  onOpen: (id: string) => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(thread.title)

  if (editing) {
    const commit = () => {
      const title = draft.trim()
      if (title && title !== thread.title) onRename(thread.id, title)
      setEditing(false)
    }
    return (
      <Input
        autoFocus
        value={draft}
        className="my-0.5 h-8 text-sm"
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit()
          if (e.key === "Escape") {
            setDraft(thread.title)
            setEditing(false)
          }
        }}
      />
    )
  }

  return (
    <div
      className={`group flex items-center gap-1 rounded-md px-2 py-1.5 text-sm transition-colors ${
        active
          ? "bg-sidebar-accent text-foreground"
          : "text-sidebar-foreground hover:bg-sidebar-accent/60"
      }`}
    >
      <button
        type="button"
        onClick={() => onOpen(thread.id)}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        {running ? <StatusDot status="running" /> : null}
        <span className="truncate">{thread.title || "Untitled"}</span>
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            className="opacity-0 transition-opacity group-hover:opacity-100 data-[state=open]:opacity-100"
            title="More"
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onSelect={() => {
              setDraft(thread.title)
              setEditing(true)
            }}
          >
            <Pencil />
            Rename
          </DropdownMenuItem>
          <DropdownMenuItem destructive onSelect={() => onDelete(thread.id)}>
            <Trash2 />
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

const ORDER = ["Today", "Yesterday", "This week", "This month", "Earlier"]

function groupByDay(threads: Thread[]): [string, Thread[]][] {
  const buckets = new Map<string, Thread[]>()
  for (const t of threads) {
    const label = relativeDay(t.last_active_at)
    const list = buckets.get(label) ?? []
    list.push(t)
    buckets.set(label, list)
  }
  return ORDER.filter((label) => buckets.has(label)).map((label) => [
    label,
    buckets.get(label)!,
  ])
}
