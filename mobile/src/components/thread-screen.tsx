import { parseSlashSubmit } from "../../../frontend/src/lib/slash-core"
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react"
import { ArrowDown, ChevronLeft, ChevronRight, FileText, GitBranch, Loader2, UsersRound } from "lucide-react"

import type { ComposerPrediction } from "../../../frontend/src/lib/composer-prediction"
import type { InsertMode } from "../../../frontend/src/lib/delivery-mode"
import { AccountButton } from "@/components/account-button"
import { AskCard } from "@/components/ask-card"
import { IterationLimitCard } from "@/components/iteration-limit-card"
import { Composer, type ComposerExtra } from "@/components/composer"
import { DeliveryMenu } from "@/components/delivery-menu"
import { FileBrowser } from "@/components/file-browser"
import { PhoneFileLinkContext } from "@/components/markdown"
import { GoalBanner, ScheduleBanner } from "@/components/status-banners"
import { SelectionActions } from "@/components/selection-actions"
import { ThreadLog } from "@/components/thread-blocks"
import { pairedTurnDestinations, scrollToTranscriptRow, TurnNavigation } from "@/components/turn-navigation"
import { PhoneTaskPanelButton, PhoneTaskPanelSheet, usePhoneTaskPanelView } from "@/components/task-panels"
import { PhoneWorkRecords, phoneGraphText } from "@/components/work-records"
import { Button } from "@/components/ui/button"
import { agentRosterText } from "@/lib/agent-label"
import { installAndroidBack } from "@/lib/android-back"
import { scrollBehavior } from "@/lib/motion"
import { pairedSlashCommands } from "@/lib/composer-commands"
import { t } from "@/lib/i18n"
import { pendingIterationLimit } from "@/lib/iteration-limit"
import { normalizeSelectedText } from "@/lib/quote"
import type { CompactBlock } from "@/lib/transcript"
import { managerBlocks, pendingAsk, phoneAgents } from "@/lib/transcript"
import { hasPhoneHistoryContent, hasPhoneTurnStart } from "@/lib/phone-history"
import { useOlderTurnPreviews } from "@/lib/turn-previews"
import type { FollowupView, HistoryWork, ModelChoice, ThreadDetail } from "@/lib/rpc"
import { cn } from "@/lib/cn"
import type { RemoteLink } from "@/lib/link"

export function ThreadScreen({
  detail,
  prediction,
  onReadVisibility,
  fileLink,
  panelOwnerKey = "",
  workRecordsGeneration,
  blocks,
  historyWork,
  historyBefore,
  hasMore,
  loadingOlder,
  caughtUp = true,
  onBack,
  onOpenAccount,
  onOlder,
  followups = [],
  onSend,
  onInsert,
  onSteerFollowup,
  onDropFollowup,
  composerPending,
  restoring,
  models,
  reasoningLevels,
  catalogBusy,
  onRefreshModels,
  refreshBusy,
  refreshError,
  hideModel,
  onTune,
  onStop,
  onAnswer,
  onAnswerStructured,
  onContinue,
  onRunNow,
  onCancelWait,
  onResumeGoal,
}: {
  prediction?: ComposerPrediction
  detail: ThreadDetail
  onReadVisibility?: (visible: boolean) => void
  fileLink?: RemoteLink
  panelOwnerKey?: string
  workRecordsGeneration?: number
  blocks: CompactBlock[]
  historyWork?: HistoryWork[]
  historyBefore?: number
  hasMore?: boolean
  loadingOlder?: boolean
  caughtUp?: boolean
  onBack: () => void
  onOpenAccount?: () => void
  onOlder?: (turnId?: string) => Promise<number | undefined> | void
  followups?: FollowupView[]
  onSend: (text: string, extra?: ComposerExtra) => void
  onInsert?: (mode: InsertMode, turnId: string, text: string, extra?: ComposerExtra) => void | Promise<void>
  onSteerFollowup?: (id: string, mode?: InsertMode, turnId?: string) => void
  onDropFollowup?: (id: string) => void
  composerPending?: boolean
  restoring?: boolean
  models?: ModelChoice[]
  reasoningLevels?: string[]
  catalogBusy?: boolean
  onRefreshModels?: () => void | Promise<void>
  refreshBusy?: boolean
  refreshError?: string
  hideModel?: boolean
  onTune?: (next: { providerId: string; model: string; reasoning: string }) => void
  onStop: () => void
  onAnswer: (text: string) => void | Promise<void>
  onAnswerStructured: (
    callId: string,
    answers: Record<string, { answers: string[] }>,
  ) => void | Promise<void>
  onContinue?: (proceed: boolean) => Promise<void>
  onRunNow?: () => void
  onCancelWait?: () => void
  onResumeGoal?: () => void
}) {
  const manager = managerBlocks(blocks)
  const agents = phoneAgents(blocks)
  const activeAgents = agents.filter((agent) => agent.status === "running")
  const finishedAgents = agents.filter((agent) => agent.status === "done" || agent.status === "failed")
  const stoppedAgents = agents.filter((agent) => agent.status === "cancelled")
  const agentGroups = [
    { id: "active", label: t("thread.agent.activeGroup", { n: activeAgents.length }), agents: activeAgents },
    { id: "finished", label: t("thread.agent.finishedGroup", { n: finishedAgents.length }), agents: finishedAgents },
    { id: "stopped", label: t("thread.agent.stoppedGroup", { n: stoppedAgents.length }), agents: stoppedAgents },
  ]
  const [agentPage, setAgentPage] = useState("chat")
  const [linkedFile, setLinkedFile] = useState<{ path: string; link: RemoteLink; threadId: string; owner: string }>()
  const fileTrigger = useRef<HTMLElement | null>(null)
  const quickPath = linkedFile && linkedFile.link === fileLink && linkedFile.threadId === detail.id && linkedFile.owner === panelOwnerKey
    ? linkedFile.path : undefined
  const showingRoster = agentPage === "agents"
  const taskPanels = usePhoneTaskPanelView(fileLink, detail.id, panelOwnerKey, Boolean(detail.running))
  const [workView, setWorkView] = useState<{ owner: string; link: RemoteLink }>()
  const workOwner = panelOwnerKey && workRecordsGeneration !== undefined ? `${panelOwnerKey}\0${detail.id}` : ""
  const workOpen = Boolean(fileLink && workOwner && workView?.owner === workOwner && workView.link === fileLink)
  const closeWork = useCallback(() => setWorkView(undefined), [])
  useEffect(() => { if (!workOpen) closeWork() }, [closeWork, workOpen])
  const turnId = detail.running?.turn_id
  const destinations = pairedTurnDestinations(detail.turns ?? [], manager, turnId)
  const suppliedPreviews = new Set(detail.turns?.filter((turn) => turn.user_text).map((turn) => turn.id))
  const olderPreviews = useOlderTurnPreviews(
    caughtUp && hasMore && destinations.length >= 3 ? fileLink : undefined,
    detail.id, historyBefore, destinations.filter((turn) => !suppliedPreviews.has(turn.id)),
  )
  useLayoutEffect(() => {
    onReadVisibility?.(agentPage === "chat" && !taskPanels.open && !workOpen && !quickPath)
  }, [agentPage, onReadVisibility, taskPanels.open, workOpen, quickPath])
  const selectedAgent = agents.find((agent) => agent.id === agentPage)
  const ask = pendingAsk(manager)
  const opening = !caughtUp && blocks.length === 0
  const asking = Boolean(detail.running?.ask_user || ask?.pending)
  const canSelectTranscript = !asking && !quickPath && (agentPage === "chat" || Boolean(selectedAgent))
  const running = Boolean(detail.running)
  const iterationLimit = pendingIterationLimit(manager, detail.running?.turn_id)
  const waiting = Boolean(detail.waiting && !running)
  const scroller = useRef<HTMLDivElement>(null)
  const quoteSource = useRef<HTMLDivElement>(null)
  const [selectedText, setSelectedText] = useState("")
  const [quoteActionPosition, setQuoteActionPosition] = useState<{ top: number; left: number } | null>(null)
  const [quoteActionDocked, setQuoteActionDocked] = useState(false)
  const [quotes, setQuotes] = useState<string[]>([])
  const stick = useRef(true)
  const pinHeight = useRef<number | null>(null)
  const [historyJump, setHistoryJump] = useState<{ thread: string; turnId?: string; seq?: number }>()
  const [atTail, setAtTail] = useState(false)
  const [behind, setBehind] = useState(false)

  const resetSelectionAction = useCallback(() => {
    setSelectedText("")
    setQuoteActionPosition(null)
    setQuoteActionDocked(false)
  }, [])

  const openLinkedFile = useCallback((path: string, trigger: HTMLElement) => {
    if (!fileLink) return
    fileTrigger.current = trigger
    resetSelectionAction()
    setLinkedFile({ path, link: fileLink, threadId: detail.id, owner: panelOwnerKey })
  }, [fileLink, detail.id, panelOwnerKey, resetSelectionAction])
  const closeLinkedFile = useCallback(() => setLinkedFile(undefined), [])

  useLayoutEffect(() => {
    if (quickPath) return
    const trigger = fileTrigger.current
    fileTrigger.current = null
    if (trigger?.isConnected) trigger.focus({ preventScroll: true })
  }, [quickPath])

  useEffect(() => closeLinkedFile(), [fileLink, detail.id, panelOwnerKey, closeLinkedFile])

  useEffect(() => {
    setAgentPage("chat")
    resetSelectionAction()
  }, [detail.id, resetSelectionAction])

  const changePage = (page: string) => {
    resetSelectionAction()
    pinHeight.current = null
    stick.current = true
    setBehind(false)
    setAgentPage(page)
  }

  useEffect(() => {
    // The task panel sheet handles Back for its own dialogs before closing.
    if (taskPanels.open || quickPath || agentPage === "chat" || agentPage === "files") return
    return installAndroidBack(() => {
      changePage(agentPage === "agents" ? "chat" : "agents")
      return true
    })
  }, [agentPage, taskPanels.open, quickPath])

  useEffect(() => {
    if (!canSelectTranscript) return
    const inMessage = (node: Node) => {
      const element = node instanceof Element ? node : node.parentElement
      return element?.closest("[data-quote-text]")
    }
    const read = () => {
      const selection = window.getSelection()
      if (!selection || selection.isCollapsed || selection.rangeCount === 0) return
      const range = selection.getRangeAt(0)
      const inActions = (node: Node) => (node instanceof Element ? node : node.parentElement)?.closest("[data-phone-quote-action]")
      // The translated result remains selectable for the system Copy action.
      if (inActions(range.startContainer) && inActions(range.endContainer)) return
      const source = quoteSource.current
      if (!source?.contains(range.startContainer) || !source.contains(range.endContainer) ||
        !inMessage(range.startContainer) || !inMessage(range.endContainer)) {
        resetSelectionAction()
        return
      }
      setSelectedText(normalizeSelectedText(selection.toString()))
      const viewport = scroller.current?.getBoundingClientRect()
      const fragments = typeof range.getClientRects === "function" ? range.getClientRects() : null
      const last = fragments?.item(fragments.length - 1)
      const rect = last ?? (typeof range.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null)
      if (viewport && rect && viewport.width > 0 && viewport.height > 0) {
        // The system menu can sit below a middle selection. Near the bottom
        // it sits above; use that remaining space before reserving a new row.
        const gap = viewport.bottom - rect.bottom >= 126 ? 64 : 8
        const below = rect.bottom - viewport.top + gap
        if (below + 54 > viewport.height - 8) {
          // A native menu can span the full width above a bottom selection.
          // Give the quote action its own row instead of guessing its width.
          setQuoteActionDocked(true)
          setQuoteActionPosition(null)
        } else {
          setQuoteActionDocked(false)
          setQuoteActionPosition({
            top: below,
            left: viewport.width / 2,
          })
        }
      }
      stick.current = false
    }
    const dismissOnOutsideClick = (event: MouseEvent) => {
      const target = event.target instanceof Element ? event.target : null
      if (target?.closest("[data-phone-quote-action]")) return
      const selection = window.getSelection()
      const range = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : undefined
      const rects = range?.getClientRects?.()
      if (target && quoteSource.current?.contains(target) && rects && Array.from(rects).some((rect) =>
        event.clientX >= rect.left && event.clientX <= rect.right && event.clientY >= rect.top && event.clientY <= rect.bottom)) return
      if (range && quoteSource.current?.contains(range.startContainer) && quoteSource.current.contains(range.endContainer) &&
        inMessage(range.startContainer) && inMessage(range.endContainer)) selection?.removeAllRanges()
      resetSelectionAction()
    }
    document.addEventListener("selectionchange", read)
    document.addEventListener("click", dismissOnOutsideClick, true)
    return () => {
      document.removeEventListener("selectionchange", read)
      document.removeEventListener("click", dismissOnOutsideClick, true)
    }
  }, [canSelectTranscript, resetSelectionAction])

  useLayoutEffect(() => {
    if (!quoteActionDocked) return
    const selection = window.getSelection()
    const viewport = scroller.current
    if (!selection?.rangeCount || !viewport) return
    const overflow = selection.getRangeAt(0).getBoundingClientRect().bottom - viewport.getBoundingClientRect().bottom + 8
    if (overflow > 0) viewport.scrollTop += overflow
  }, [quoteActionDocked, selectedText])

  const loadOlder = () => {
    if (!onOlder || loadingOlder || !hasMore || !caughtUp) return
    // The reader is already at the top, asking for what is above. Pinning
    // the old offset hides that page and leaves the last turn on screen.
    if (!showingRoster) {
      pinHeight.current = 0
      stick.current = false
    }
    void Promise.resolve(onOlder()).then((seq) => {
      if (seq) setHistoryJump({ thread: detail.id, seq })
    })
  }

  const pullY = useRef<number | null>(null)

  useLayoutEffect(() => {
    const el = scroller.current
    if (showingRoster || !el || !caughtUp) return
    el.style.overflowAnchor = "none"
    if (loadingOlder) return
    if (pinHeight.current != null) {
      // Stay at the top. A follow-up request here never leaves 加载中:
      // the new rows keep the scroller under the 48px tripwire.
      el.scrollTop = 0
      pinHeight.current = null
      return
    }
    if (stick.current) {
      el.scrollTop = el.scrollHeight
      if (behind) setBehind(false)
    }
    if (!atTail) setAtTail(true)
  }, [caughtUp, blocks, loadingOlder, hasMore, atTail, behind, agentPage, showingRoster])

  useLayoutEffect(() => {
    if (!historyJump || loadingOlder) return
    setHistoryJump(undefined)
    const el = scroller.current
    if (historyJump.thread !== detail.id || agentPage !== "chat" || !el) return
    const row = historyJump.turnId ? turnRow(el, historyJump.turnId)
      : el.querySelector<HTMLElement>(`[data-phone-event-seq="${historyJump.seq}"]`)
    scrollToTranscriptRow(el, row)
  }, [agentPage, detail.id, historyJump, loadingOlder])

  const jumpToTurn = (id: string) => {
    const el = scroller.current
    stick.current = false
    if ((!hasMore || hasPhoneTurnStart(manager, id, true)) && el && scrollToTranscriptRow(el, turnRow(el, id))) return
    if (onOlder && hasMore && !loadingOlder) {
      void Promise.resolve(onOlder(id)).then(() => setHistoryJump({ thread: detail.id, turnId: id }))
    }
  }

  const toLatest = () => {
    const el = scroller.current
    if (!el) return
    stick.current = true
    setBehind(false)
    el.scrollTo({ top: el.scrollHeight, behavior: scrollBehavior() })
  }

  const onPullStart = (y: number) => {
    pullY.current = y
  }
  const onPullMove = (y: number) => {
    const start = pullY.current
    const el = scroller.current
    if (start == null || !el || !hasMore) return
    if (el.scrollTop <= 0 && y - start > 48) {
      pullY.current = null
      loadOlder()
    }
  }

  if (workOpen && fileLink && workRecordsGeneration !== undefined) {
    return <>
      <PhoneWorkRecords key={workOwner} link={fileLink} threadId={detail.id} generation={workRecordsGeneration} running={running}
        covered={taskPanels.open} onBack={closeWork} />
      <PhoneTaskPanelSheet view={taskPanels} />
    </>
  }

  if (agentPage === "files" && fileLink) {
    return <><div className="flex min-h-0 flex-1 flex-col" inert={taskPanels.open} aria-hidden={taskPanels.open}>
      <FileBrowser key={`${detail.id}:${panelOwnerKey ?? ""}`} link={fileLink} threadId={detail.id} running={running} covered={taskPanels.open} onClose={() => changePage("chat")}
        status={<GoalBanner detail={detail} running={running} />} />
    </div><PhoneTaskPanelSheet view={taskPanels} /></>
  }

  return (
    <><div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
    <main inert={taskPanels.open || Boolean(quickPath)} aria-hidden={taskPanels.open || Boolean(quickPath)}
      className={cn("mx-auto flex h-full min-w-0 w-full max-w-lg flex-col justify-end overflow-hidden bg-background", quickPath && "invisible")}>
      <header className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-1">
        <Button
          variant="ghost"
          className="size-10 shrink-0 px-0"
          onClick={() => agentPage === "chat" ? onBack() : changePage(agentPage === "agents" ? "chat" : "agents")}
          aria-label={t("thread.back")}
        >
          <ChevronLeft className="size-5" />
        </Button>
        <div className="flex min-w-0 flex-1 items-center gap-2">
          {running || waiting ? (
            <span
              className="size-1.5 shrink-0 rounded-full bg-[hsl(var(--running))] motion-safe:animate-pulse"
              aria-hidden
            />
          ) : null}
          <h1 className="min-w-0 truncate text-sm font-medium">
            {selectedAgent?.role || (agentPage === "agents" ? t("thread.agents") : detail.title)}
          </h1>
          {waiting ? (
            <span
              data-testid="thread-status"
              className="shrink-0 text-xs text-[hsl(var(--running))]"
            >
              {t("thread.waiting")}
            </span>
          ) : null}
        </div>
        <PhoneTaskPanelButton view={taskPanels} />
        {agentPage === "chat" && workOwner && fileLink ? (
          <Button type="button" variant="ghost" className="size-10 shrink-0 px-0"
            aria-label={phoneGraphText("graph.tab")} onClick={() => setWorkView({ owner: workOwner, link: fileLink })}>
            <GitBranch className="size-5" aria-hidden />
          </Button>
        ) : null}
        {agentPage === "chat" && onOpenAccount ? <AccountButton onClick={onOpenAccount} /> : null}
      </header>
      <GoalBanner
        detail={detail}
        running={running}
        waiting={agentPage === "chat" && waiting}
        disabled={restoring}
        onResume={agentPage === "chat" ? onResumeGoal : undefined}
      />
      {agentPage === "chat" ? <ScheduleBanner
        detail={detail}
        running={running}
        disabled={restoring}
        onRunNow={onRunNow}
        onCancel={onCancelWait}
      /> : null}
      {agentPage === "chat" && detail.plan_on ? (
        <p className="px-4 text-[11px] text-[hsl(var(--running))]">{t("thread.plan")}</p>
      ) : null}

      {agentPage === "chat" && caughtUp ? (
        <TurnNavigation turns={destinations.map((turn) => ({
          ...turn, label: turn.label || olderPreviews?.get(turn.id) || "",
        }))}
          scroller={scroller} runningTurnId={turnId} pending={loadingOlder} onJump={jumpToTurn} />
      ) : null}

      {hasMore ? (
        <div className="flex shrink-0 justify-center border-b border-border">
          <Button
            type="button"
            variant="ghost"
            className="h-8 text-xs text-muted-foreground"
            disabled={loadingOlder || !caughtUp}
            onClick={loadOlder}
          >
            {loadingOlder || !caughtUp ? (
              <span className="inline-flex items-center gap-1.5">
                <Loader2 className="size-3.5 motion-safe:animate-spin motion-reduce:animate-none" aria-hidden />
                {t("thread.loading")}
              </span>
            ) : (
              t("thread.earlier")
            )}
          </Button>
        </div>
      ) : null}

      {opening ? (
        <div
          role="status"
          aria-busy="true"
          className="flex min-h-0 flex-1 flex-col justify-end gap-3 px-4 py-6"
        >
          <span className="sr-only">{t("thread.loading")}</span>
          <div className="h-4 w-2/3 animate-pulse rounded-full bg-muted motion-reduce:animate-none" />
          <div className="h-4 w-1/2 animate-pulse rounded-full bg-muted motion-reduce:animate-none" />
          <div className="h-16 animate-pulse rounded-2xl bg-muted motion-reduce:animate-none" />
        </div>
      ) : (
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {/* Android WebView will not scroll a flex-column <ol>. Bounded
            overflow box; Earlier sits above so paging is not a dead drag. */}
        <div
          key={showingRoster ? "roster" : "log"}
          ref={scroller}
          data-testid="transcript"
          className={cn(
            "min-h-0 min-w-0 w-full flex-1 overflow-x-hidden overflow-y-scroll overscroll-y-contain touch-pan-y px-3 py-2 [-webkit-overflow-scrolling:touch]",
            !showingRoster && !atTail && "invisible",
          )}
          onScroll={(e) => {
            if (showingRoster) return
            const el = e.currentTarget
            const gap = el.scrollHeight - el.scrollTop - el.clientHeight
            stick.current = gap < 48
            // The button is for a reader who scrolled away, not for the two
            // pixels of slack a streaming answer leaves behind.
            const away = gap > 240
            if (away !== behind) setBehind(away)
          }}
          onTouchStart={(e) => onPullStart(e.touches[0]?.clientY ?? 0)}
          onTouchMove={(e) => onPullMove(e.touches[0]?.clientY ?? 0)}
          onTouchEnd={() => {
            pullY.current = null
          }}
        >
          {/* Bottom-aligned: a short conversation sits above the composer
              instead of floating under a screen of blank. */}
          <div className={cn("flex min-h-full min-w-0 w-full flex-col gap-1.5",
            showingRoster ? "justify-start" : "justify-end")}>
            <div ref={quoteSource} data-quote-source="">
              {agentPage === "chat" && caughtUp && hasMore && !hasPhoneHistoryContent(manager) ? (
                <p role="status" className="px-3 py-4 text-center text-sm text-muted-foreground">
                  {t("thread.hiddenHistory")}
                </p>
              ) : null}
              {showingRoster ? (
                <div className="flex flex-col gap-1" data-testid="agent-roster">
                  {agentGroups.filter((group) => group.agents.length > 0).map((group) => (
                    <section key={group.id} className="flex flex-col gap-1">
                      <h2 className="px-2 py-1 text-xs font-medium text-muted-foreground">{group.label}</h2>
                      {group.agents.map((agent) => (
                        <button key={agent.id} type="button" onClick={() => changePage(agent.id)}
                          className="flex min-w-0 items-center gap-2 rounded-lg border border-border px-3 py-2 text-left text-sm">
                          <span className="flex min-w-0 flex-1 flex-col">
                            <span className="truncate">{agentRosterText(agent.role, agent.id)}</span>
                            {agent.activity ? <span className="truncate text-xs text-muted-foreground">{agent.activity}</span> : null}
                          </span>
                          <span className="shrink-0 text-xs text-muted-foreground">{t(`thread.agent.${agent.status}`)}</span>
                          <ChevronRight className="size-4 shrink-0" aria-hidden />
                        </button>
                      ))}
                    </section>
                  ))}
                </div>
              ) : (
                <PhoneFileLinkContext.Provider value={fileLink ? openLinkedFile : undefined}>
                <ThreadLog key={`${panelOwnerKey}:${agentPage}`} blocks={selectedAgent ? selectedAgent.blocks : manager}
                  allowInputEdit={agentPage === "chat"}
                  historyWork={historyWork}
                  historyAgent={selectedAgent?.id}
                  imageOwner={fileLink ? { link: fileLink, threadId: detail.id } : undefined}
                  running={selectedAgent ? selectedAgent.status === "running" : running}
                  onContinue={agentPage === "chat" && detail.running?.awaiting_continue !== false ? onContinue : undefined}
                  activeTurnId={detail.running?.turn_id} continueDisabled={composerPending || restoring} />
                </PhoneFileLinkContext.Provider>
              )}
            </div>
            {agentPage === "chat" && detail.running?.awaiting_continue && !iterationLimit ? (
              <IterationLimitCard onContinue={onContinue} disabled={composerPending || restoring} />
            ) : null}
            {agentPage === "chat" && ask?.pending && (ask.questions?.length ?? 0) > 0 ? (
              <AskCard
                key={ask.callId}
                questions={ask.questions!}
                disabled={composerPending || restoring}
                onSubmit={(answers) => onAnswerStructured(ask.callId || "", answers)}
              />
            ) : null}
          </div>
        </div>
        {canSelectTranscript && selectedText ? (
          <div
            className={cn(
              "z-20 w-[calc(100%-1.5rem)] max-w-sm rounded-xl border border-foreground/20 bg-card text-card-foreground shadow-xl",
              quoteActionDocked ? "mx-3 mb-2 shrink-0 self-center" : "absolute -translate-x-1/2",
              !quoteActionDocked && !quoteActionPosition && "bottom-3 left-1/2",
            )}
            data-phone-quote-action=""
            style={!quoteActionDocked ? quoteActionPosition ?? undefined : undefined}
            onMouseDown={(event) => {
              if ((event.target as Element).closest("button")) event.preventDefault()
            }}
          >
            <SelectionActions key={`${detail.id}:${selectedText}`} text={selectedText} threadId={detail.id} link={fileLink} providerID={detail.provider_id}
              onExpand={() => { setQuoteActionDocked(true); setQuoteActionPosition(null) }}
              onDismiss={() => { resetSelectionAction(); window.getSelection()?.removeAllRanges() }}
              onAdd={() => {
                setQuotes((current) => [...current, selectedText])
                window.getSelection()?.removeAllRanges()
                if (selectedAgent) changePage("chat")
                else resetSelectionAction()
              }} />
          </div>
        ) : null}
        {!showingRoster && behind && atTail ? (
          <button
            type="button"
            data-testid="to-latest"
            aria-label={t("thread.toLatest")}
            className={cn(
              "absolute bottom-3 left-1/2 flex size-9 -translate-x-1/2 items-center justify-center",
              "rounded-full border border-border bg-card text-foreground shadow-md",
              "transition-opacity active:bg-accent",
            )}
            onClick={toLatest}
          >
            <ArrowDown className="size-4" aria-hidden />
          </button>
        ) : null}
      </div>
      )}

      {agentPage === "chat" && followups.length > 0 ? (
        <QueueTray
          items={followups}
          onSteer={onSteerFollowup}
          onDrop={onDropFollowup}
          turnId={turnId}
          disabled={restoring || asking || (running && !turnId)}
        />
      ) : null}
      {agentPage === "chat" ? <Composer
        above={fileLink || agents.length > 0 ? (
          <div data-testid="thread-composer-links" className="mb-1.5 flex min-w-0 flex-wrap justify-center gap-2">
            {agents.length > 0 ? (
              <button type="button" className={composerLinkClass} onClick={() => changePage("agents")}>
                <UsersRound className="size-3.5" aria-hidden />{t("thread.agents")} ({activeAgents.length})
              </button>
            ) : null}
            {fileLink ? (
              <button type="button" className={composerLinkClass} onClick={() => changePage("files")}>
                <FileText className="size-3.5" aria-hidden />{t("thread.files")}
              </button>
            ) : null}
          </div>
        ) : undefined}
        prediction={!running && !asking && !waiting && caughtUp ? prediction : undefined}
        quotes={asking ? [] : quotes}
        onQuotesChange={setQuotes}
        // The box and the button cannot share a name, or a screen reader
        // announces two "Answer" controls and a test cannot pick either.
        label={asking ? t("thread.answerBox") : t("thread.message")}
        sendLabel={asking ? t("thread.answer") : running ? t("thread.followUp") : t("thread.send")}
        onInsert={turnId && !asking && onInsert ? (mode, text, extra) => onInsert(mode, turnId, text, extra) : undefined}
        stopLabel={t("thread.stop")}
        onStop={running || restoring ? onStop : undefined}
        disabled={restoring}
        pending={composerPending}
        models={models}
        reasoningLevels={reasoningLevels}
        providerId={detail.provider_id}
        model={detail.model}
        reasoning={detail.reasoning ?? ""}
        catalogBusy={catalogBusy}
        onRefreshModels={onRefreshModels}
        refreshBusy={refreshBusy}
        refreshError={refreshError}
        hideModel={hideModel}
        slashCommands={asking ? undefined : pairedSlashCommands}
        onTune={onTune}
        onSubmit={asking ? (text, extra) => {
          if (parseSlashSubmit(text)?.id !== "image") return onAnswer(text)
          return extra ? onSend(text, extra) : onSend(text)
        } : onSend}
      /> : null}
    </main>
    {quickPath && fileLink ? <div className="absolute inset-0" inert={taskPanels.open} aria-hidden={taskPanels.open}>
      <FileBrowser key={quickPath} link={fileLink} threadId={detail.id} quickPath={quickPath} onClose={closeLinkedFile} covered={taskPanels.open} />
    </div> : null}
    </div><PhoneTaskPanelSheet view={taskPanels} /></>
  )
}

const composerLinkClass = "flex min-h-9 items-center gap-1.5 rounded-full border border-border bg-card px-3 text-xs text-muted-foreground active:bg-accent"

function turnRow(scroller: HTMLElement, turnId: string): HTMLElement | null {
  const rows = Array.from(scroller.querySelectorAll<HTMLElement>("[data-phone-turn]"))
  return rows.find((row) => row.dataset.phoneTurn === turnId && "phonePrompt" in row.dataset)
    ?? rows.find((row) => row.dataset.phoneTurn === turnId)
    ?? null
}

function QueueTray({
  items,
  onSteer,
  onDrop,
  turnId,
  disabled,
}: {
  items: FollowupView[]
  onSteer?: (id: string, mode?: InsertMode, turnId?: string) => void
  onDrop?: (id: string) => void
  turnId?: string
  disabled?: boolean
}) {
  return (
    <div data-testid="followup-queue" className="border-t border-border px-3 py-2">
      <div className="mb-1 flex items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">{t("queue.waiting")}</p>
      </div>
      <ul className="flex flex-col gap-1">
        {items.map((item) => {
          const text = item.text || t("delivery.attachments", { n: item.attachments ?? 0 })
          return (
          <li key={item.id} className="flex items-center gap-2">
            <p className="min-w-0 flex-1 truncate text-sm">{text}</p>
            <Button
              variant="ghost"
              className="h-8 shrink-0 px-2"
              disabled={disabled}
              onClick={() => turnId ? onSteer?.(item.id, "steer", turnId) : onSteer?.(item.id)}
            >
              {turnId ? t("delivery.steer") : t("delivery.sendNow")}
            </Button>
            {turnId ? <DeliveryMenu disabled={disabled} queuedText={text} onSelect={(mode) => onSteer?.(item.id, mode, turnId)} /> : null}
            <Button
              variant="ghost"
              className="h-8 shrink-0 px-2 text-muted-foreground"
              aria-label={t("queue.remove")}
              onClick={() => onDrop?.(item.id)}
            >
              {t("queue.remove")}
            </Button>
          </li>
          )
        })}
      </ul>
    </div>
  )
}
