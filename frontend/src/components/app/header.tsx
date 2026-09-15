import { Moon, PanelLeft, PanelRight, Sun, WifiOff } from "lucide-react"
import { useEffect, useState } from "react"

import { CopyButton } from "@/components/app/transcript"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import type { Meta, Project, Thread, ThreadStatus } from "@/lib/types"
import { cn, formatDuration } from "@/lib/utils"

/** The title bar. In desktop mode this doubles as the window's drag region,
 *  which is why the buttons sit in a non-draggable island on the right. */
export function Header({
  thread,
  project,
  status,
  meta,
  model,
  connected,
  panelOpen,
  sidebarOpen,
  trafficInset,
  onTogglePanel,
  onToggleSidebar,
  onToggleTheme,
  dark,
}: {
  thread?: Thread
  /** The project the open conversation belongs to, when it has one: its name
   *  is the only clue on screen that tools are pointed somewhere else. */
  project?: Project
  status: ThreadStatus
  meta?: Meta
  model?: string
  connected: boolean
  panelOpen: boolean
  sidebarOpen: boolean
  /** When the list is hidden the traffic lights sit on this bar. */
  trafficInset?: boolean
  onTogglePanel: () => void
  onToggleSidebar: () => void
  onToggleTheme: () => void
  dark: boolean
}) {
  // The elapsed clock ticks here, not in App: a once-a-second setState in
  // the shell used to re-parse every markdown block in the conversation.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!status.running) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [status.running])
  const elapsedMs =
    status.running && status.started_at
      ? Math.max(0, now - new Date(status.started_at).getTime())
      : 0
  return (
    <header
      data-drag-region
      className={cn(
        "flex h-12 shrink-0 items-center gap-2 border-b border-border pr-4",
        trafficInset ? "pl-traffic" : "pl-4",
      )}
    >
      {!sidebarOpen ? (
        <Button
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          onClick={onToggleSidebar}
          aria-label="Show conversations"
          title="Show conversations (⌘B)"
        >
          <PanelLeft />
        </Button>
      ) : null}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium" data-testid="thread-title">
          {thread?.title || "New conversation"}
        </p>
        {project ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <p
                className="truncate text-xs text-muted-foreground"
                data-testid="thread-project"
              >
                {project.name}
              </p>
            </TooltipTrigger>
            <TooltipContent>
              Working in {project.workdir || project.memory_dir}
            </TooltipContent>
          </Tooltip>
        ) : null}
      </div>

      {status.running ? (
        <Badge variant="warning" data-testid="status-badge">
          <span className="size-1.5 animate-breathe rounded-full bg-running" />
          {/* A running clock counts in seconds; "Working · 0ms" reads like a
              bug even when it is the truth. */}
          Working · {formatDuration(Math.max(elapsedMs, 1000))}
        </Badge>
      ) : (
        <Badge variant="outline" data-testid="status-badge">
          Idle
        </Badge>
      )}

      {status.turn_id ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <CopyButton text={status.turn_id} />
          </TooltipTrigger>
          <TooltipContent>
            Copy the turn id — <code>zwai trace {"<id>"}</code> replays it
          </TooltipContent>
        </Tooltip>
      ) : null}

      {model ? (
        <span className="hidden text-xs text-muted-foreground sm:inline">{model}</span>
      ) : null}

      {!connected ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <Badge variant="danger">
              <WifiOff className="size-3" />
              Reconnecting
            </Badge>
          </TooltipTrigger>
          <TooltipContent>
            Lost the event stream to {meta?.mode === "desktop" ? "the app" : "the server"}.
            It retries on its own and replays anything missed.
          </TooltipContent>
        </Tooltip>
      ) : null}

      <div className="flex items-center" data-no-drag>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onToggleTheme}
          title="Switch theme"
          aria-label="Switch theme"
        >
          {dark ? <Sun /> : <Moon />}
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onTogglePanel}
          aria-label="Toggle side panel"
          title="Toggle side panel (⌘\)"
          className={panelOpen ? "text-foreground" : "text-muted-foreground"}
        >
          <PanelRight />
        </Button>
      </div>
    </header>
  )
}
