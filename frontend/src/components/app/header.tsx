import {
  FoldHorizontal,
  Moon,
  PanelLeft,
  PanelRight,
  Sun,
  UnfoldHorizontal,
  WifiOff,
} from "lucide-react"
import { useEffect, useState } from "react"

import { CopyButton } from "@/components/app/transcript"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  SIDEBAR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_VAR,
} from "@/lib/sidebar-width"
import type { ContentWidthPref } from "@/lib/appearance"
import type { Meta, Project, Thread, ThreadStatus } from "@/lib/types"
import { cn, formatDuration } from "@/lib/utils"
import { useT } from "@/lib/use-t"

/** The window title bar. It spans the full width: traffic lights + sidebar
 *  toggle on the left (Codex/Cursor), the conversation on the rest. In desktop
 *  mode it is also the drag region; a double-click zooms like Finder. */
export function Header({
  thread,
  project,
  status,
  meta,
  connected,
  panelOpen,
  sidebarOpen,
  trafficInset,
  onTogglePanel,
  onToggleSidebar,
  onToggleTheme,
  onToggleLocale,
  onToggleContentWidth,
  contentWidth,
  dark,
}: {
  thread?: Thread
  /** The project the open conversation belongs to, when it has one: its name
   *  prefixes the title on this one line. That is the only clue on screen that
   *  tools are pointed somewhere else. */
  project?: Project
  status: ThreadStatus
  meta?: Meta
  connected: boolean
  panelOpen: boolean
  sidebarOpen: boolean
  /** Desktop macOS: the native traffic lights sit on this bar. */
  trafficInset?: boolean
  onTogglePanel: () => void
  onToggleSidebar: () => void
  onToggleTheme: () => void
  onToggleLocale: () => void
  onToggleContentWidth: () => void
  contentWidth: ContentWidthPref
  dark: boolean
}) {
  const t = useT()
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
  const listLabel = sidebarOpen
    ? t("header.hideConversations")
    : t("header.showConversations")
  const wide = contentWidth === "full"
  const widthLabel = wide
    ? t("header.switchToStandard")
    : t("header.switchToWide")
  return (
    <header
      data-drag-region
      className="flex h-12 shrink-0 items-center border-b border-border bg-background"
    >
      <div
        data-testid="titlebar-leading"
        className={cn(
          "flex h-full shrink-0 items-center",
          trafficInset ? "pl-traffic" : "pl-3",
        )}
        style={
          sidebarOpen
            ? {
                width: `var(${SIDEBAR_WIDTH_VAR}, ${SIDEBAR_WIDTH_DEFAULT}px)`,
              }
            : undefined
        }
      >
        <Button
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          onClick={onToggleSidebar}
          aria-label={listLabel}
          title={`${listLabel} (⌘B)`}
        >
          <PanelLeft />
        </Button>
      </div>
      <div className="flex min-w-0 flex-1 items-center gap-2 pr-4">
        {/* One line: a stacked project name under the title was two rows in
            a 48px bar. The model name lives on the composer; repeating it here
            ate the same row. */}
        <div className="flex min-w-0 flex-1 items-center gap-1.5">
          {project ? (
            <>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span
                    className="max-w-48 shrink-0 truncate text-sm text-muted-foreground"
                    data-testid="thread-project"
                  >
                    {project.name}
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  {t("header.workingIn", {
                    path: project.workdir || project.memory_dir,
                  })}
                </TooltipContent>
              </Tooltip>
              <span className="shrink-0 text-sm text-muted-foreground">·</span>
            </>
          ) : null}
          <p
            className="min-w-0 truncate text-sm font-medium"
            data-testid="thread-title"
          >
            {thread?.title || t("header.newConversation")}
          </p>
        </div>

        {status.running ? (
          <Badge variant="warning" data-testid="status-badge">
            <span className="size-1.5 animate-breathe rounded-full bg-running" />
            {/* A running clock counts in seconds; "Working · 0ms" reads like a
                bug even when it is the truth. Missing started_at used to
                clamp to 1s forever — that is a lie, not a clock. */}
            {status.awaiting_continue
              ? t("header.waiting")
              : t("header.working")}
            {status.started_at
              ? ` · ${formatDuration(Math.max(elapsedMs, 1000))}`
              : null}
          </Badge>
        ) : (
          <Badge variant="outline" data-testid="status-badge">
            {t("header.idle")}
          </Badge>
        )}

        {status.turn_id ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <CopyButton text={status.turn_id} />
            </TooltipTrigger>
            <TooltipContent>
              {t("header.copyTurn")}
            </TooltipContent>
          </Tooltip>
        ) : null}

        {!connected ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge variant="danger">
                <WifiOff className="size-3" />
                {t("header.reconnecting")}
              </Badge>
            </TooltipTrigger>
            <TooltipContent>
              {meta?.mode === "desktop"
                ? t("header.lostStreamDesktop")
                : t("header.lostStreamWeb")}
            </TooltipContent>
          </Tooltip>
        ) : null}

        <div className="flex items-center" data-no-drag>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onToggleTheme}
            title={t("header.switchTheme")}
            aria-label={t("header.switchTheme")}
          >
            {dark ? <Sun /> : <Moon />}
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onToggleLocale}
            title={t("header.switchLanguage")}
            aria-label={t("header.switchLanguage")}
            className="px-1.5 text-[11px] font-medium"
          >
            {t("header.languageMark")}
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onToggleContentWidth}
            title={widthLabel}
            aria-label={widthLabel}
            aria-pressed={wide}
            className={wide ? "text-foreground" : "text-muted-foreground"}
          >
            {wide ? <FoldHorizontal /> : <UnfoldHorizontal />}
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onTogglePanel}
            aria-label={t("header.togglePanel")}
            title={`${t("header.togglePanel")} (⌘\\)`}
            className={panelOpen ? "text-foreground" : "text-muted-foreground"}
          >
            <PanelRight />
          </Button>
        </div>
      </div>
    </header>
  )
}
