import type { ReactNode } from "react"
import { Bot, CalendarClock, FolderKanban, MessageSquare } from "lucide-react"

import { Button } from "@/components/ui/button"
import { closeClient } from "@/lib/client-open"
import {
  scheduledReturnDest,
  setDeskDest,
  useDeskDest,
  visibleDest,
  type DeskDest,
} from "@/lib/desk-nav"
import { useClientCatalog } from "@/components/app/local-clients"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useApp } from "@/store/app"
import { useScheduleInboxUI } from "@/store/schedule-inbox-ui"

/** Rail clicks own the inbox flag. The task column only renders the
 *  editor; it must not also be the thing that opens the list. */
export function pickDest(next: DeskDest) {
  if (next === "scheduled") {
    if (!useApp.getState().scheduleInboxOpen) useScheduleInboxUI.getState().reset()
    setDeskDest("scheduled")
    closeClient()
    useApp.getState().openScheduleInbox()
    return
  }
  if (next === "clients") {
    setDeskDest("clients")
    useApp.getState().closeScheduleInbox()
    return
  }
  setDeskDest(next)
  closeClient()
  useApp.getState().closeScheduleInbox()
}

export function leaveScheduledPage() {
  setDeskDest(scheduledReturnDest())
  useApp.getState().closeScheduleInbox()
}

export function useVisibleDest(): DeskDest {
  const dest = useDeskDest()
  const inbox = useApp((s) => s.scheduleInboxOpen)
  return visibleDest(inbox, dest)
}

function RailButton({
  current,
  label,
  testId,
  onClick,
  children,
}: {
  current: boolean
  label: string
  testId?: string
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      data-testid={testId}
      aria-current={current ? "page" : undefined}
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        "relative text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground",
        current && "bg-sidebar-accent",
      )}
      style={{
        width: "var(--sidebar-row-height)",
        height: "var(--sidebar-row-height)",
      }}
    >
      {children}
    </Button>
  )
}

function UnreadCount({ n }: { n: number }) {
  if (n <= 0) return null
  return (
    <span
      data-testid="schedule-unread"
      aria-hidden="true"
      className="pointer-events-none absolute right-0 top-0 z-10 flex h-4 min-w-4 translate-x-1/4 -translate-y-1/4 items-center justify-center rounded-full bg-primary px-0.5 text-xs font-medium leading-none text-primary-foreground"
    >
      {n > 99 ? "99+" : n}
    </span>
  )
}

/** Leftmost destination rail. Projects, conversations, waits, and local
 *  clients are lists, not sections inside the open task. The rail stays
 *  when the list column is hidden; `edge` is the hairline against that
 *  column, and the aside draws the outer one when the column is gone. */
export function DestRail({ edge = true }: { edge?: boolean }) {
  const t = useT()
  const pane = useVisibleDest()
  const unread = useApp((s) => s.scheduleUnread)
  const catalog = useClientCatalog()
  const scheduledName =
    unread > 0 ? t("sidebar.scheduledUnread", { n: unread }) : t("sidebar.scheduled")

  return (
    <nav
      data-testid="dest-rail"
      aria-label={t("nav.destinations")}
      className={cn(
        "flex shrink-0 flex-col items-center gap-2 px-1.5 py-2",
        edge && "border-r border-sidebar-border",
      )}
      style={{ width: "var(--dest-rail-width)" }}
    >
      <RailButton
        current={pane === "projects"}
        label={t("nav.projects")}
        testId="dest-projects"
        onClick={() => pickDest("projects")}
      >
        <FolderKanban />
      </RailButton>
      <RailButton
        current={pane === "chats"}
        label={t("nav.chats")}
        testId="dest-chats"
        onClick={() => pickDest("chats")}
      >
        <MessageSquare />
      </RailButton>
      <RailButton
        current={pane === "scheduled"}
        label={scheduledName}
        testId="schedule-inbox"
        onClick={() => pickDest("scheduled")}
      >
        <CalendarClock />
        <UnreadCount n={unread} />
      </RailButton>
      {catalog.enabled ? (
        <RailButton
          current={pane === "clients"}
          label={t("sidebar.clients")}
          testId="dest-clients"
          onClick={() => pickDest("clients")}
        >
          <Bot />
        </RailButton>
      ) : null}
    </nav>
  )
}
