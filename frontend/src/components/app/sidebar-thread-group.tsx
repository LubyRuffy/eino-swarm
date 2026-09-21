import { useMemo, useState } from "react"

import { SidebarKindSlot, sidebarRowClass, sidebarStackClass } from "@/components/app/sidebar-slots"
import { SidebarThreadRow } from "@/components/app/sidebar-thread-row"
import { reorderById } from "@/lib/reorder"
import { splitSidebarPreview } from "@/lib/sidebar-preview"
import { useSortableList } from "@/lib/sortable"
import type { Thread } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

/** Conversations in one Recents list or one project folder, with older
 *  rows behind Show more so the sidebar stays a directory. */
export function SidebarThreadGroup({
  threads,
  activeId,
  runningId,
  waitingIds,
  askingIds,
  onOpen,
  onRename,
  onDelete,
  onPin,
  onReorder,
}: {
  threads: Thread[]
  activeId?: string
  runningId?: string
  waitingIds?: ReadonlySet<string>
  askingIds?: ReadonlySet<string>
  onOpen: (id: string) => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
  onPin?: (id: string, pinned: boolean) => void
  onReorder?: (ids: string[]) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const { visible, hidden } = useMemo(
    () =>
      splitSidebarPreview(threads, {
        keepIds: [
          ...[activeId, runningId].filter((id): id is string => Boolean(id)),
          ...threads.filter((th) => th.running || th.awaiting_answer).map((th) => th.id),
          ...(waitingIds ?? []),
          ...(askingIds ?? []),
        ],
      }),
    [threads, activeId, runningId, waitingIds, askingIds],
  )
  const shown = expanded ? threads : visible
  const sortable = useSortableList((from, to) => {
    onReorder?.(reorderById(threads, from, to).map((th) => th.id))
  })
  return (
    <div className={sidebarStackClass}>
      {shown.map((thread) => (
        <SidebarThreadRow
          key={thread.id}
          thread={thread}
          active={thread.id === activeId}
          running={thread.running || thread.id === runningId}
          waiting={waitingIds?.has(thread.id)}
          asking={Boolean(askingIds?.has(thread.id) || thread.awaiting_answer)}
          drag={onReorder ? sortable.bind(thread.id) : undefined}
          onOpen={onOpen}
          onRename={onRename}
          onDelete={onDelete}
          onPin={onPin}
        />
      ))}
      {hidden.length > 0 ? (
        <SidebarShowMore expanded={expanded} onToggle={() => setExpanded((open) => !open)} />
      ) : null}
    </div>
  )
}

function SidebarShowMore({
  expanded,
  onToggle,
}: {
  expanded: boolean
  onToggle: () => void
}) {
  const t = useT()
  return (
    <button
      type="button"
      data-testid="sidebar-show-more"
      aria-expanded={expanded}
      className={cn(
        sidebarRowClass,
        "w-full text-left text-sidebar-foreground/50 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground",
      )}
      onClick={onToggle}
    >
      <SidebarKindSlot />
      <span className="truncate">
        {expanded ? t("sidebar.showLess") : t("sidebar.showMore")}
      </span>
    </button>
  )
}
