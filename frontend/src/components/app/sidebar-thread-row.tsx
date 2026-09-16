import { GripVertical, MoreHorizontal, Pencil, Pin, PinOff, Trash2 } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { StatusDot } from "@/components/app/transcript"
import { useSortableList } from "@/lib/sortable"
import type { Thread } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

type DragBind = ReturnType<ReturnType<typeof useSortableList>["bind"]>

/** One conversation in the sidebar: Recents, a project folder, or Pinned. */
export function SidebarThreadRow({
  thread,
  active,
  running,
  drag,
  indent,
  onOpen,
  onRename,
  onDelete,
  onPin,
}: {
  thread: Thread
  active: boolean
  running: boolean
  drag?: DragBind
  indent?: boolean
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
    return (
      <Input
        autoFocus
        value={draft}
        className={cn("my-0.5 h-8 text-sm", indent && "ml-4")}
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
      {...drag}
      data-testid="thread-row"
      data-id={thread.id}
      className={cn(
        "group flex select-none items-center gap-1 rounded-md py-1.5 pr-2 text-sm transition-colors",
        drag && "cursor-grab data-[dragging=true]:cursor-grabbing",
        indent ? "pl-5" : "pl-2",
        "data-[dragging=true]:opacity-60 data-[over=true]:bg-sidebar-accent",
        active
          ? "bg-sidebar-accent text-foreground"
          : "text-sidebar-foreground hover:bg-sidebar-accent/60",
      )}
    >
      {drag ? (
        <span
          data-drag-handle
          className="flex cursor-grab items-center self-stretch opacity-0 group-hover:opacity-50"
          aria-hidden="true"
          onClick={() => onOpen(thread.id)}
        >
          <GripVertical className="size-3.5 shrink-0" />
        </span>
      ) : null}
      <button
        type="button"
        onClick={() => onOpen(thread.id)}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        {running ? <StatusDot status="running" /> : null}
        <span className="truncate">{thread.title || t("sidebar.untitled")}</span>
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
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
