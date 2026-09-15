import { Moon, PanelRight, Sun, WifiOff } from "lucide-react"

import { CopyButton } from "@/components/app/transcript"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import type { Meta, Thread, ThreadStatus } from "@/lib/types"
import { formatDuration } from "@/lib/utils"

/** The title bar. In desktop mode this doubles as the window's drag region,
 *  which is why the buttons sit in a non-draggable island on the right. */
export function Header({
  thread,
  status,
  elapsedMs,
  meta,
  model,
  connected,
  panelOpen,
  onTogglePanel,
  onToggleTheme,
  dark,
}: {
  thread?: Thread
  status: ThreadStatus
  elapsedMs: number
  meta?: Meta
  model?: string
  connected: boolean
  panelOpen: boolean
  onTogglePanel: () => void
  onToggleTheme: () => void
  dark: boolean
}) {
  return (
    <header
      data-drag-region
      className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-4"
    >
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium" data-testid="thread-title">
          {thread?.title || "New conversation"}
        </p>
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
