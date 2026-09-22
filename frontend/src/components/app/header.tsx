import {
  Code2,
  FoldHorizontal,
  Moon,
  PanelLeft,
  PanelRight,
  SquareTerminal,
  Sun,
  UnfoldHorizontal,
  WifiOff,
} from "lucide-react"
import { useEffect, useState } from "react"

import { CopyButton } from "@/components/app/transcript"
import { AskMark } from "@/components/app/ask-mark"
import { WaitMark } from "@/components/app/wait-mark"
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
import type { ContentWidthPref, TranscriptModePref } from "@/lib/appearance"
import { chromeTypeClass } from "@/lib/chrome-type"
import { desktopShell, uniqueSurfaces } from "@/lib/shell"
import type { Meta, Project, Thread, ThreadStatus } from "@/lib/types"
import { cn, formatDuration } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

/** The window title bar. It spans the full width: traffic lights + sidebar
 *  toggle on the left (Codex/Cursor), the conversation on the rest. In desktop
 *  mode it is also the drag region; a double-click zooms like Finder. */
export function Header({
  thread,
  project,
  status,
  waiting,
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
  onToggleTranscriptMode,
  onOpenTerminal,
  contentWidth,
  transcriptMode,
  dark,
  terminalOpen,
  terminalEnabled,
}: {
  thread?: Thread
  /** The project the open conversation belongs to, when it has one: its name
   *  prefixes the title on this one line. That is the only clue on screen that
   *  tools are pointed somewhere else. */
  project?: Project
  status: ThreadStatus
  /** True while an active thread wake is parked. Still not `running`. */
  waiting?: boolean
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
  onToggleTranscriptMode: () => void
  onOpenTerminal: () => void
  contentWidth: ContentWidthPref
  transcriptMode: TranscriptModePref
  dark: boolean
  terminalOpen: boolean
  terminalEnabled: boolean
}) {
  const t = useT()
  const scheduled = useApp((s) => s.scheduleInboxOpen)
  // The elapsed clock ticks here, not in App: a once-a-second setState in
  // the shell used to re-parse every markdown block in the conversation.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!status.running || scheduled) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [status.running, scheduled])
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
  const developer = transcriptMode === "developer"
  const modeLabel = developer
    ? t("header.switchToUser")
    : t("header.switchToDeveloper")
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
          {!scheduled && project ? (
            <>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span
                    className={cn(
                      chromeTypeClass,
                      "max-w-48 shrink-0 truncate text-muted-foreground",
                    )}
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
              <span className={cn(chromeTypeClass, "shrink-0 text-muted-foreground")}>·</span>
            </>
          ) : null}
          <p
            className={cn(chromeTypeClass, "min-w-0 truncate")}
            data-testid="thread-title"
          >
            {scheduled
              ? t("schedule.inboxTitle")
              : thread?.title || t("header.newConversation")}
          </p>
        </div>

        {scheduled ? null : status.running ? (
          <Badge
            variant={status.awaiting_answer ? "ask" : "warning"}
            data-testid="status-badge"
          >
            {status.awaiting_answer ? (
              <AskMark />
            ) : (
              <span className="size-1.5 animate-breathe rounded-full bg-running" />
            )}
            {/* A running clock counts in seconds; "Working · 0ms" reads like a
                bug even when it is the truth. Missing started_at used to
                clamp to 1s forever — that is a lie, not a clock. */}
            {status.awaiting_answer
              ? t("header.yourTurn")
              : status.awaiting_continue
                ? t("header.waiting")
                : status.compressing
                  ? t("header.compressing")
                  : t("header.working")}
            {status.started_at
              ? ` · ${formatDuration(Math.max(elapsedMs, 1000))}`
              : null}
          </Badge>
        ) : waiting ? (
          <Badge variant="warning" data-testid="status-badge">
            <WaitMark />
            {t("header.waiting")}
          </Badge>
        ) : (
          <Badge variant="outline" data-testid="status-badge">
            {t("header.idle")}
          </Badge>
        )}

        {(meta?.clients?.length ?? 0) >= 2 ? (
          <Badge variant="outline" data-testid="shared-clients">
            {t("header.shared", {
              where: uniqueSurfaces(meta?.clients)
                .map((surface) =>
                  surface === "desktop" || surface === "web" || surface === "tui"
                    ? t(`surface.${surface}`)
                    : surface,
                )
                .join(" · "),
            })}
          </Badge>
        ) : null}

        {!scheduled && status.turn_id ? (
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
              {desktopShell()
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
            className={cn(chromeTypeClass, "px-1.5")}
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
            onClick={onToggleTranscriptMode}
            title={modeLabel}
            aria-label={modeLabel}
            aria-pressed={developer}
            className={developer ? "text-foreground" : "text-muted-foreground"}
          >
            <Code2 />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onOpenTerminal}
            disabled={!terminalEnabled}
            aria-label={t("header.terminal")}
            title={
              terminalEnabled
                ? `${t("header.terminal")} (⌘J)`
                : t("header.terminalDisabled")
            }
            aria-pressed={terminalOpen}
            className={terminalOpen ? "text-foreground" : "text-muted-foreground"}
          >
            <SquareTerminal />
          </Button>
          {scheduled ? null : (
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
          )}
        </div>
      </div>
    </header>
  )
}
