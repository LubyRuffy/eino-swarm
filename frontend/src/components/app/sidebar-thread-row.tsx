import { MoreHorizontal, Pencil, Pin, PinOff, Trash2 } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { SidebarKindSlot, sidebarRowClass } from "@/components/app/sidebar-slots"
import { StatusDot } from "@/components/app/transcript"
import { AskMark } from "@/components/app/ask-mark"
import { WaitMark } from "@/components/app/wait-mark"
import { chromeTypeClass } from "@/lib/chrome-type"
import { useSortableList } from "@/lib/sortable"
import type { Thread } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

type DragBind = ReturnType<ReturnType<typeof useSortableList>["bind"]>

/** One conversation in the sidebar: Recents, a project folder, or Pinned.
 *  The leading size-4 slot is the folder column — empty, a progress dot
 *  while a turn is live, a ping question mark while ask_user is blocked,
 *  or a clock while a wait is parked — so titles line up with the
 *  project name. */
export function SidebarThreadRow({
  thread,
  active,
  running,
  waiting,
  asking,
  drag,
  onOpen,
  onRename,
  onDelete,
  onPin,
}: {
  thread: Thread
  active: boolean
  running: boolean
  waiting?: boolean
  asking?: boolean
  drag?: DragBind
  onOpen: (id: string) => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
  onPin?: (id: string, pinned: boolean) => void
}) {
  const t = useT()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(thread.title)
  const canPin = Boolean(onPin && thread.project_id)

  if (editing) {
    const commit = () => {
      const title = draft.trim()
      if (title && title !== thread.title) onRename(thread.id, title)
      setEditing(false)
    }
    const field = (
      <Input
        autoFocus
        value={draft}
        className={cn("h-[28px]", chromeTypeClass)}
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
    return (
      <div className={sidebarRowClass}>
        <SidebarKindSlot />
        <div className="min-w-0 flex-1">{field}</div>
      </div>
    )
  }

  return (
    <div
      {...drag}
      data-testid="thread-row"
      data-id={thread.id}
      aria-current={active ? "true" : undefined}
      className={cn(
        sidebarRowClass,
        drag && "data-[dragging=true]:cursor-grabbing",
        "data-[dragging=true]:opacity-60 data-[over=true]:bg-sidebar-accent",
        active
          ? "bg-sidebar-accent text-foreground"
          : "text-sidebar-foreground hover:bg-sidebar-accent/60",
      )}
    >
      <button
        type="button"
        onClick={() => onOpen(thread.id)}
        className="flex min-w-0 flex-1 items-center gap-1 text-left"
      >
        <SidebarKindSlot>
          {asking ? (
            <AskMark />
          ) : running ? (
            <StatusDot status="running" />
          ) : waiting ? (
            <WaitMark />
          ) : null}
        </SidebarKindSlot>
        <span data-testid="row-label" className="truncate">
          {thread.title || t("sidebar.untitled")}
        </span>
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-xs"
            data-no-drag
            className="opacity-0 transition-opacity group-hover:opacity-100 data-[state=open]:opacity-100"
            title={t("sidebar.more")}
            aria-label={t("sidebar.more")}
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {canPin ? (
            <DropdownMenuItem
              onSelect={() => onPin?.(thread.id, !thread.pinned)}
            >
              {thread.pinned ? <PinOff /> : <Pin />}
              {thread.pinned ? t("sidebar.unpin") : t("sidebar.pin")}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuItem
            onSelect={() => {
              setDraft(thread.title)
              setEditing(true)
            }}
          >
            <Pencil />
            {t("sidebar.rename")}
          </DropdownMenuItem>
          <DropdownMenuItem destructive onSelect={() => onDelete(thread.id)}>
            <Trash2 />
            {t("sidebar.delete")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
