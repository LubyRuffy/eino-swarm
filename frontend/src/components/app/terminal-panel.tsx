import { Plus, SquareTerminal, X } from "lucide-react"

import { ResizeHandle } from "@/components/app/resize-handle"
import { TerminalSessionView } from "@/components/app/terminal-session"
import { Button } from "@/components/ui/button"
import { MAX_TERMINALS, terminalTabLabel } from "@/lib/terminal"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import { useTerminal, type TerminalSession } from "@/store/terminal"

export function TerminalPanel({
  onNew,
}: {
  onNew: () => void
}) {
  const t = useT()
  const open = useTerminal((s) => s.open)
  const height = useTerminal((s) => s.height)
  const sessions = useTerminal((s) => s.sessions)
  const activeId = useTerminal((s) => s.activeId)
  const select = useTerminal((s) => s.select)
  const closeSession = useTerminal((s) => s.closeSession)
  const closePanel = useTerminal((s) => s.closePanel)
  const setHeight = useTerminal((s) => s.setHeight)
  const setCwd = useTerminal((s) => s.setCwd)

  if (!open && sessions.length === 0) return null

  return (
    <div
      hidden={!open}
      className={cn(
        "relative flex shrink-0 flex-col overflow-hidden border-t border-border bg-card",
        !open && "hidden",
      )}
      style={{ height: open ? height : 0 }}
      data-testid="terminal-panel"
    >
      {open ? (
        <ResizeHandle
          width={height}
          onWidthChange={setHeight}
          edge="top"
          label={t("terminal.resize")}
          min={120}
          max={640}
        />
      ) : null}
      <div className="flex h-8 shrink-0 items-center gap-1 border-b border-border px-1">
        <SquareTerminal className="ml-1 size-3.5 shrink-0 text-muted-foreground" />
        <div className="flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto">
          {sessions.map((session, i) => (
            <TerminalTab
              key={session.id}
              session={session}
              index={i}
              active={session.id === activeId}
              onSelect={() => select(session.id)}
              onClose={() => closeSession(session.id)}
            />
          ))}
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={onNew}
          disabled={sessions.length >= MAX_TERMINALS}
          title={t("terminal.new")}
          aria-label={t("terminal.new")}
        >
          <Plus />
        </Button>
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={closePanel}
          title={t("terminal.close")}
          aria-label={t("terminal.close")}
        >
          <X />
        </Button>
      </div>
      <div className="relative min-h-0 flex-1">
        {sessions.map((session) => (
          <div
            key={session.id}
            className={cn(
              "absolute inset-0",
              session.id === activeId ? "block" : "hidden",
            )}
          >
            <TerminalSessionView
              session={session}
              visible={open && session.id === activeId}
              onReady={(cwd) => setCwd(session.id, cwd)}
            />
          </div>
        ))}
      </div>
    </div>
  )
}

function TerminalTab({
  session,
  index,
  active,
  onSelect,
  onClose,
}: {
  session: TerminalSession
  index: number
  active: boolean
  onSelect: () => void
  onClose: () => void
}) {
  const t = useT()
  const label = terminalTabLabel(session.cwd, t("terminal.untitled", { n: index + 1 }))
  return (
    <div
      className={cn(
        "flex max-w-48 shrink-0 items-center rounded-md",
        active ? "bg-accent text-accent-foreground" : "text-muted-foreground",
      )}
    >
      <button
        type="button"
        className="min-w-0 truncate px-2 py-1 text-xs"
        onClick={onSelect}
        title={session.cwd || label}
      >
        {label}
      </button>
      <Button
        variant="ghost"
        size="icon-xs"
        className="size-5"
        onClick={onClose}
        aria-label={t("terminal.closeTab")}
        title={t("terminal.closeTab")}
      >
        <X className="size-3" />
      </Button>
    </div>
  )
}
