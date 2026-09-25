import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { DesktopUpdateBanner } from "@/components/app/desktop-update"
import {
  AppHeader,
  AppPalette,
  AppSettings,
  ConfiguredBanner,
  ErrorBanner,
  isDark,
  SettingsIdleChrome,
} from "@/components/app/app-chrome"
import { ClientChat } from "@/components/app/client-transcript"
import { Composer } from "@/components/app/composer"
import { DeleteProjectDialog } from "@/components/app/delete-project-dialog"
import { EmptyState } from "@/components/app/empty-state"
import { FindBar, useFindController } from "@/components/app/find-bar"
import { RightPanel, type PanelTab } from "@/components/app/panel"
import { ProjectDialog } from "@/components/app/project-dialog"
import { SelectionMenu } from "@/components/app/selection-menu"
import { ScheduleInbox } from "@/components/app/schedule-inbox"
import { Sidebar } from "@/components/app/sidebar"
import { ToastStack } from "@/components/app/toast-stack"
import { TerminalPanel } from "@/components/app/terminal-panel"
import { Transcript } from "@/components/app/transcript"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { closeClient, useOpenClient } from "@/lib/client-open"
import { toggleContentWidth, toggleTranscriptMode } from "@/lib/appearance"
import { attachExternalLinkHandler } from "@/lib/external-links"
import { findShortcut } from "@/lib/find"
import { appendQuote, type Quote } from "@/lib/quote"
import { liveWorkers } from "@/lib/transcript"
import { terminalShortcut, terminalTarget } from "@/lib/terminal"
import { isWelcomePane, visibleManagerBlockCount } from "@/lib/welcome"
import type { Project, SkillInfo } from "@/lib/types"
import { startSidebarSync } from "@/lib/sidebar-sync"
import { askingThreadIds } from "@/lib/thread-title"
import { toggleLocalePref } from "@/lib/use-t"
import { isMac, readSidebarOpen, writeSidebarOpen } from "@/lib/utils"
import { desktopShell, openLinksNatively } from "@/lib/shell"
import { useApp } from "@/store/app"
import { waitingThreadIds } from "@/store/app-schedule"
import { projectOf, useProjects } from "@/store/projects"
import { useTerminal } from "@/store/terminal"
import { openSettings, useSettingsSheet } from "@/store/settings-sheet"

export function App() {
  const boot = useApp((s) => s.boot)
  const openNative = openLinksNatively(useApp((s) => s.meta?.capabilities?.open_url), desktopShell())
  const booted = useRef(false)
  useEffect(() => {
    // StrictMode double-mount must not open two event streams.
    if (booted.current) return
    booted.current = true
    void boot()
  }, [boot])
  useEffect(
    () =>
      startSidebarSync(async () => {
        const s = useApp.getState()
        await s.syncThreads()
        // The listing tick used to skip waits. An armed chip then kept the
        // first next_run_at until a full reload, so a fired interval looked
        // overdue.
        await s.refreshSchedules({ silent: true })
      }),
    [],
  )
  useEffect(() => {
    // WKWebView loads a clicked http(s) href in this window, target=_blank
    // included. Catch every <a>, not just markdown, so the app is never
    // replaced by a third-party page.
    return attachExternalLinkHandler({
      openNative: openNative ? (url) => void api.openURL(url) : undefined,
    })
  }, [openNative])

  return (
    <TooltipProvider delayDuration={400}>
      <div className="flex h-screen w-screen flex-col overflow-hidden bg-background text-foreground">
        <AppShell />
      </div>
      <ToastStack />
    </TooltipProvider>
  )
}

/** Chrome that must not re-render on every streamed token: sidebar, header,
 *  composer, and the panel subscribe to the slices they actually show.
 *  The composer's usage ring subscribes in a child so a token/usage pulse
 *  cannot rewrite the textarea while a CJK IME is composing. */
function AppShell() {
  const [panelOpen, setPanelOpen] = useState(true)
  const [sidebarOpen, setSidebarOpen] = useState(readSidebarOpen)
  const [panelTab, setPanelTab] = useState<PanelTab>("agents")
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [projectDialog, setProjectDialog] = useState<ProjectDialogState>()
  const [doomedProject, setDoomedProject] = useState<Project>()
  const [prefill, setPrefill] = useState("")
  const [prefillToken, setPrefillToken] = useState(0)
  const [focusSignal, setFocusSignal] = useState(0)
  const [quotes, setQuotes] = useState<Quote[]>([])
  const [focusSkill, setFocusSkill] = useState<{
    projectId: string
    name?: string
  }>()
  const find = useFindController()
  const {
    open: findOpen,
    query: findQuery,
    index: findIndex,
    total: findTotal,
    inputRef: findInputRef,
    setQuery: setFindQuery,
    setTotal: setFindTotal,
    openFind,
    close: closeFind,
    next: nextFind,
    selectQuery: selectFindQuery,
  } = find

  const running = useApp((s) => s.status.running)
  const interrupt = useApp((s) => s.interrupt)
  const newThread = useApp((s) => s.newThread)
  const setTheme = useApp((s) => s.setTheme)
  const theme = useApp((s) => s.theme)
  const setLocale = useApp((s) => s.setLocale)
  const setAppearance = useApp((s) => s.setAppearance)
  const selectAgent = useApp((s) => s.selectAgent)
  const activeId = useApp((s) => s.activeId)
  const trafficLights = desktopShell() && isMac()
  const client = useOpenClient()

  const toggleSidebar = useCallback(() => {
    setSidebarOpen((open) => {
      const next = !open
      writeSidebarOpen(next)
      return next
    })
  }, [])

  const focusComposer = useCallback(() => setFocusSignal((n) => n + 1), [])

  const spawnTerminal = useCallback(() => {
    const target = terminalTarget(
      useApp.getState().activeId,
      useProjects.getState().selectedId,
    )
    if (!target) return
    useTerminal.getState().spawn(target)
  }, [])

  const toggleTerminal = useCallback(() => {
    const target = terminalTarget(
      useApp.getState().activeId,
      useProjects.getState().selectedId,
    )
    useTerminal.getState().toggle(target)
  }, [])

  useEffect(() => {
    setQuotes([])
  }, [activeId])

  const addQuote = useCallback(
    (text: string) => {
      setQuotes((prev) => appendQuote(prev, text))
      focusComposer()
    },
    [focusComposer],
  )

  const startThread = useCallback(async () => {
    closeClient()
    await newThread()
    focusComposer()
  }, [newThread, focusComposer])

  const startThreadInProject = useCallback(
    async (project: Project) => {
      // Select first so the folder expands onto the conversation we are
      // about to create. Creating first and then refreshing used to wipe
      // the new row if the listing raced the insert.
      closeClient()
      if (useProjects.getState().selectedId !== project.id) {
        useProjects.getState().select(project.id)
      }
      await newThread(project.id)
      focusComposer()
    },
    [newThread, focusComposer],
  )

  const toggleTheme = useCallback(() => {
    setTheme(isDark(theme) ? "light" : "dark")
  }, [setTheme, theme])

  const toggleLocale = useCallback(() => {
    setLocale(toggleLocalePref(useApp.getState().locale))
  }, [setLocale])

  const toggleWidth = useCallback(() => {
    setAppearance({
      contentWidth: toggleContentWidth(useApp.getState().contentWidth),
    })
  }, [setAppearance])

  const toggleMode = useCallback(() => {
    setAppearance({
      transcriptMode: toggleTranscriptMode(useApp.getState().transcriptMode),
    })
  }, [setAppearance])

  const openAgent = useCallback(
    (id: string) => {
      selectAgent(id)
      setPanelTab("agents")
      setPanelOpen(true)
    },
    [selectAgent],
  )

  const pickIdea = useCallback(
    (text: string) => {
      setPrefill(text)
      setPrefillToken((n) => n + 1)
      focusComposer()
    },
    [focusComposer],
  )

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const dialogOpen =
        paletteOpen ||
        useSettingsSheet.getState().open ||
        Boolean(projectDialog) ||
        Boolean(doomedProject) ||
        useApp.getState().scheduleInboxOpen
      const findAction = findShortcut(e, findOpen)
      if (findAction && !dialogOpen) {
        e.preventDefault()
        if (findAction.action === "open") openFind()
        else if (findAction.action === "select-query") selectFindQuery()
        else if (findAction.action === "next") nextFind(findAction.delta)
        else closeFind()
        return
      }
      if (e.key === "Escape") {
        const target = e.target
        if (
          target instanceof HTMLTextAreaElement &&
          (target.dataset.slashOpen === "true" ||
            target.dataset.goalDraft === "true" ||
            target.dataset.planDraft === "true" ||
            target.dataset.editDraft === "true" ||
            target.dataset.askOther === "true")
        ) {
          return
        }
        if (dialogOpen || !running) return
        e.preventDefault()
        void interrupt()
        return
      }
      const mod = e.metaKey || e.ctrlKey
      if (!mod) return
      if (e.key === "k") {
        e.preventDefault()
        setPaletteOpen((open) => !open)
      } else if (e.key === "n") {
        e.preventDefault()
        void startThread()
      } else if (e.key === "b") {
        e.preventDefault()
        toggleSidebar()
      } else if (e.key === "\\") {
        e.preventDefault()
        setPanelOpen((open) => !open)
      } else if (terminalShortcut(e)) {
        e.preventDefault()
        toggleTerminal()
      } else if (e.key === ",") {
        e.preventDefault()
        openSettings()
      }
    }
    // Capture: ⌘F has to beat the webview's own find bar.
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [
    closeFind,
    doomedProject,
    findOpen,
    interrupt,
    nextFind,
    openFind,
    paletteOpen,
    projectDialog,
    running,
    selectFindQuery,
    startThread,
    toggleSidebar,
    toggleTerminal,
  ])

  return (
    <>
      <SettingsIdleChrome>
      <AppHeader
        panelOpen={panelOpen}
        sidebarOpen={sidebarOpen}
        trafficInset={trafficLights}
        onTogglePanel={() => setPanelOpen((open) => !open)}
        onToggleSidebar={toggleSidebar}
        onOpenTerminal={spawnTerminal}
      />
      <div className="flex min-h-0 min-w-0 flex-1">
        {sidebarOpen ? (
          <AppSidebar
            onNew={() => void startThread()}
            onSearch={() => setPaletteOpen(true)}
            onSettings={openSettings}
            onToggleTheme={toggleTheme}
            onToggleLocale={toggleLocale}
            onNewProject={() => setProjectDialog({ open: true })}
            onNewInProject={(project) => void startThreadInProject(project)}
            onEditProject={(project) => setProjectDialog({ open: true, project })}
            onDeleteProject={setDoomedProject}
            onOpenSkill={(project, skill) => {
              // The list is the directory; the Memory tab is the document.
              // Selecting the project loads its notes so the panel is not
              // still showing whatever conversation happened to be open.
              void useApp.getState().selectProject(project.id)
              useProjects.getState().seeMemory()
              setPanelOpen(true)
              setPanelTab("memory")
              setFocusSkill({ projectId: project.id, name: skill?.name })
            }}
          />
        ) : null}

        <main className="flex min-w-0 flex-1 flex-col">
          <DesktopUpdateBanner />
          <ConfiguredBanner onConfigure={() => openSettings("models")} />
          <ErrorBanner />
          <div
            data-composer-stage=""
            data-testid="composer-stage"
            className="relative min-h-0 min-w-0 flex-1"
          >
            <div className="absolute inset-0 flex min-h-0 min-w-0 flex-col overflow-hidden">
              {client ? (
                <ClientChat id={client.id} />
              ) : (
                <TranscriptPane
                  onSelectAgent={openAgent}
                  onPickIdea={pickIdea}
                  findQuery={findOpen ? findQuery : ""}
                  findIndex={findIndex}
                  onFindCount={setFindTotal}
                />
              )}
            </div>
            <AppComposer
              locked={client != null}
              prefill={prefill}
              prefillToken={prefillToken}
              focusSignal={focusSignal}
              quotes={quotes}
              onQuotesChange={setQuotes}
              onEditProviders={() => openSettings("models")}
            />
            {findOpen ? (
              <FindBar
                query={findQuery}
                index={findIndex}
                total={findTotal}
                inputRef={findInputRef}
                onQuery={setFindQuery}
                onNext={nextFind}
                onClose={closeFind}
              />
            ) : null}
            <ScheduleInbox />
          </div>
          <TerminalPanel onNew={spawnTerminal} />
        </main>

        {panelOpen ? (
          <AppPanel
            tab={panelTab}
            onTabChange={(tab) => {
              if (tab !== "memory") setFocusSkill(undefined)
              setPanelTab(tab)
            }}
            focusSkill={focusSkill}
          />
        ) : null}
      </div>

      <SelectionMenu onAdd={addQuote} />

      <AppPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        onNew={() => void startThread()}
        onSettings={openSettings}
        onToggleTheme={toggleTheme}
        onToggleLocale={toggleLocale}
        onToggleContentWidth={toggleWidth}
        onToggleTranscriptMode={toggleMode}
        onToggleSidebar={toggleSidebar}
        onFind={openFind}
        onOpenTerminal={spawnTerminal}
      />

      <AppProjectDialog
        state={projectDialog}
        onOpenChange={(open) =>
          setProjectDialog((s) => (open ? s : undefined))
        }
      />

      <AppDeleteProject
        project={doomedProject}
        onOpenChange={(open) => {
          if (!open) setDoomedProject(undefined)
        }}
      />
      </SettingsIdleChrome>

      <AppSettings trafficInset={trafficLights} />
    </>
  )
}

function AppDeleteProject({
  project,
  onOpenChange,
}: {
  project?: Project
  onOpenChange: (open: boolean) => void
}) {
  const remove = useProjects((s) => s.remove)
  const refreshThreads = useApp((s) => s.refreshThreads)
  return (
    <DeleteProjectDialog
      project={project}
      onOpenChange={onOpenChange}
      onConfirm={(p) => {
        void remove(p.id).then(() => refreshThreads())
      }}
    />
  )
}

interface ProjectDialogState {
  open: boolean
  project?: Project
}

function AppProjectDialog({
  state,
  onOpenChange,
}: {
  state?: ProjectDialogState
  onOpenChange: (open: boolean) => void
}) {
  const create = useProjects((s) => s.create)
  const update = useProjects((s) => s.update)
  const memoryAvailable = useApp((s) => s.meta?.capabilities?.memory ?? false)
  const selectProject = useApp((s) => s.selectProject)
  const project = state?.project
  return (
    <ProjectDialog
      open={Boolean(state?.open)}
      project={project}
      memoryAvailable={memoryAvailable}
      onOpenChange={onOpenChange}
      onSave={async (patch) => {
        if (project) return update(project.id, patch)
        const created = await create(patch)
        // A project nobody is looking at is a project nobody uses: select it
        // so the next conversation lands in it.
        await selectProject(created.id)
        return created
      }}
    />
  )
}

function AppSidebar({
  onNew,
  onSearch,
  onSettings,
  onToggleTheme,
  onToggleLocale,
  onNewProject,
  onNewInProject,
  onEditProject,
  onDeleteProject,
  onOpenSkill,
}: {
  onNew: () => void
  onSearch: () => void
  onSettings: () => void
  onToggleTheme: () => void
  onToggleLocale: () => void
  onNewProject: () => void
  onNewInProject: (project: Project) => void
  onEditProject: (project: Project) => void
  onDeleteProject: (project: Project) => void
  onOpenSkill: (project: Project, skill?: SkillInfo) => void
}) {
  const threads = useApp((s) => s.threads)
  const activeId = useApp((s) => s.activeId)
  const running = useApp((s) => s.status.running)
  const awaitingAnswer = useApp((s) => Boolean(s.status.awaiting_answer))
  const overlayRunningId = threads.find((t) => t.running && t.id !== activeId)?.id
  const schedules = useApp((s) => s.schedules)
  const waitingIds = useMemo(
    () => waitingThreadIds(schedules, threads),
    [schedules, threads],
  )
  const askingIds = useMemo(
    () => askingThreadIds(threads, activeId, awaitingAnswer),
    [threads, activeId, awaitingAnswer],
  )
  const openThread = useApp((s) => s.openThread)
  const renameThread = useApp((s) => s.renameThread)
  const deleteThread = useApp((s) => s.deleteThread)
  const reorderThreads = useApp((s) => s.reorderThreads)
  const selectProject = useApp((s) => s.selectProject)
  const projects = useProjects((s) => s.projects)
  const selectedProjectId = useProjects((s) => s.selectedId)
  const reorderProjects = useProjects((s) => s.reorder)
  const pinThread = useApp((s) => s.pinThread)
  return (
    <Sidebar
      threads={threads}
      activeId={activeId}
      runningId={running ? activeId : overlayRunningId}
      waitingIds={waitingIds}
      askingIds={askingIds}
      onNew={onNew}
      onOpen={(id) => {
        closeClient()
        void openThread(id)
      }}
      onRename={(id, title) => void renameThread(id, title)}
      onDelete={(id) => void deleteThread(id)}
      onReorder={(ids) => void reorderThreads(ids)}
      onSearch={onSearch}
      onSettings={onSettings}
      onToggleTheme={onToggleTheme}
      onToggleLocale={onToggleLocale}
      projects={projects}
      selectedProjectId={selectedProjectId}
      onSelectProject={(id) => void selectProject(id)}
      onNewProject={onNewProject}
      onNewInProject={onNewInProject}
      onEditProject={onEditProject}
      onDeleteProject={onDeleteProject}
      onOpenSkill={onOpenSkill}
      onReorderProjects={(ids) => void reorderProjects(ids)}
      onPin={(id, pinned) => void pinThread(id, pinned)}
    />
  )
}

function TranscriptPane({
  onSelectAgent,
  onPickIdea,
  findQuery,
  findIndex,
  onFindCount,
}: {
  onSelectAgent: (id: string) => void
  onPickIdea: (text: string) => void
  findQuery: string
  findIndex: number
  onFindCount: (total: number) => void
}) {
  const activeId = useApp((s) => s.activeId)
  const loaded = useApp((s) => s.loaded)
  const transcript = useApp((s) => s.transcript)
  const running = useApp((s) => s.status.running || s.transcript.running)
  const historyHasMore = useApp((s) => s.historyHasMore)
  const send = useApp((s) => s.send)
  const resendUser = useCallback(
    (text: string, seq: number) => {
      void send(text, undefined, { fromEventSeq: seq })
    },
    [send],
  )
  const showEmptyState = isWelcomePane({
    activeId,
    loaded,
    visibleManagerBlocks: visibleManagerBlockCount(transcript),
    running,
    workerCount: liveWorkers(transcript),
    historyHasMore,
  })
  useEffect(() => {
    if (showEmptyState) onFindCount(0)
  }, [showEmptyState, onFindCount])
  if (showEmptyState) {
    return <EmptyState onPick={onPickIdea} />
  }
  return (
    <Transcript
      key={activeId}
      state={transcript}
      loaded={loaded}
      onSelectAgent={onSelectAgent}
      onResendUser={resendUser}
      findQuery={findQuery}
      findIndex={findIndex}
      onFindCount={onFindCount}
    />
  )
}

function AppComposer({
  prefill,
  prefillToken,
  focusSignal,
  quotes,
  onQuotesChange,
  onEditProviders,
  locked,
}: {
  prefill: string
  prefillToken: number
  focusSignal: number
  quotes: Quote[]
  onQuotesChange: (quotes: Quote[]) => void
  onEditProviders: () => void
  locked?: boolean
}) {
  // Do not select `usage` here. That pulse is one object per model call and
  // would re-render the controlled textarea while a CJK IME is composing.
  const running = useApp((s) => s.status.running)
  const compressing = useApp((s) => Boolean(s.status.compressing))
  const models = useApp((s) => s.models)
  const thread = useApp((s) => s.threads.find((row) => row.id === s.activeId))
  const activeId = useApp((s) => s.activeId)
  const meta = useApp((s) => s.meta)
  const send = useApp((s) => s.send)
  const interrupt = useApp((s) => s.interrupt)
  const followups = useApp((s) => s.followups)
  const steerFollowup = useApp((s) => s.steerFollowup)
  const deleteFollowup = useApp((s) => s.deleteFollowup)
  const requeueFollowup = useApp((s) => s.requeueFollowup)
  const clearFollowups = useApp((s) => s.clearFollowups)
  const upload = useApp((s) => s.upload)
  const refreshThreads = useApp((s) => s.refreshThreads)
  const refreshCatalogs = useApp((s) => s.refreshCatalogs)
  const newThread = useApp((s) => s.newThread)
  const setGoal = useApp((s) => s.setGoal)
  const editGoal = useApp((s) => s.editGoal)
  const resumeGoal = useApp((s) => s.resumeGoal)
  const compactThread = useApp((s) => s.compactThread)
  const setPlan = useApp((s) => s.setPlan)
  const savePlan = useApp((s) => s.savePlan)
  const implementPlan = useApp((s) => s.implementPlan)
  const leavePlan = useApp((s) => s.leavePlan)
  const awaitingAnswer = useApp((s) => Boolean(s.status.awaiting_answer))
  return (
    <Composer
      running={running}
      compressing={compressing}
      models={models}
      provider={thread?.provider_id || meta?.default_provider}
      model={thread?.model}
      onModelChange={(providerId, name) => {
        void (async () => {
          const id = activeId ?? (await newThread())
          if (!id) return
          await api.patchThread(id, { provider_id: providerId, model: name })
          await refreshThreads()
        })()
      }}
      reasoning={thread?.reasoning_effort ?? ""}
      reasoningLevels={meta?.reasoning_levels ?? []}
      onReasoningChange={(level) => {
        if (activeId)
          void api.patchThread(activeId, { reasoning_effort: level }).then(() => refreshThreads())
      }}
      onSend={
        locked ? () => undefined : (text, images, opts) => void send(text, images, opts)
      }
      onStop={() => void interrupt()}
      onUpload={upload}
      onRefreshModels={() => refreshCatalogs()}
      onEditProviders={onEditProviders}
      disabled={locked}
      prefill={prefill}
      prefillToken={prefillToken}
      focusSignal={focusSignal}
      quotes={quotes}
      onQuotesChange={onQuotesChange}
      followups={followups}
      onSteerFollowup={(id) => void steerFollowup(id)}
      onDeleteFollowup={(id) => void deleteFollowup(id)}
      onRequeueFollowup={(id, text) => void requeueFollowup(id, text)}
      onClearFollowups={() => void clearFollowups()}
      goal={thread?.goal}
      goalComplete={thread?.goal_complete}
      goalBlocked={thread?.goal_blocked}
      goalBlockReason={thread?.goal_block_reason}
      goalCapped={thread?.goal_capped}
      goalIdle={thread?.goal_idle}
      goalStartedAt={thread?.goal_started_at}
      contextChars={thread?.context_chars}
      contextBudget={thread?.context_budget ?? meta?.swarm.context_char_budget}
      onSetGoal={(text) => void setGoal(text)}
      onClearGoal={() => void setGoal("")}
      onEditGoal={(text) => void editGoal(text)}
      onResumeGoal={() => void resumeGoal()}
      onCompact={() => void compactThread()}
      planMode={thread?.plan_mode}
      planMarkdown={thread?.plan_markdown}
      awaitingAnswer={awaitingAnswer}
      onSetPlan={(text) => void setPlan(text)}
      onSavePlan={(text) => void savePlan(text)}
      onImplementPlan={() => void implementPlan()}
      onLeavePlan={() => void leavePlan()}
    />
  )
}

function AppPanel({
  tab,
  onTabChange,
  focusSkill,
}: {
  tab: PanelTab
  onTabChange: (tab: PanelTab) => void
  focusSkill?: { projectId: string; name?: string }
}) {
  const transcript = useApp((s) => s.transcript)
  const selectedAgent = useApp((s) => s.selectedAgent)
  const selectAgent = useApp((s) => s.selectAgent)
  const files = useApp((s) => s.files)
  const workspace = useApp((s) => s.workspace)
  const turns = useApp((s) => s.turns)
  const meta = useApp((s) => s.meta)
  const usage = useApp((s) => s.usage)
  const activeId = useApp((s) => s.activeId)
  const scheduled = useApp((s) => s.scheduleInboxOpen)
  const upload = useApp((s) => s.upload)
  const removeFile = useApp((s) => s.removeFile)
  const refreshFiles = useApp((s) => s.refreshFiles)
  const threads = useApp((s) => s.threads)
  const reviewNow = useApp((s) => s.reviewNow)
  const projects = useProjects((s) => s.projects)
  const memory = useProjects((s) => s.memory)
  const memoryLoading = useProjects((s) => s.memoryLoading)
  const loadMemory = useProjects((s) => s.loadMemory)
  const saveMemory = useProjects((s) => s.saveMemory)
  const removeSkill = useProjects((s) => s.removeSkill)
  const tidySkills = useProjects((s) => s.tidySkills)
  const clearTidy = useProjects((s) => s.clearTidy)
  const tidying = useProjects((s) => s.tidying)
  const tidyReport = useProjects((s) => s.tidyReport)
  const tidyError = useProjects((s) => s.tidyError)
  const tidyLive = useProjects((s) => s.tidyLive)
  const memoryUnread = useProjects((s) => s.memoryUnread)
  const seeMemory = useProjects((s) => s.seeMemory)
  const memoryProjectId = useProjects((s) => s.memoryProjectId)
  const reviewing = useProjects((s) => s.reviewing)
  const reviewHint = useProjects((s) => s.reviewHint)
  const conversationProject = projectOf(
    projects,
    threads.find((t) => t.id === activeId)?.project_id,
  )
  // A click on a sidebar skill is a request to look at that project's
  // memory, even when the open conversation belongs to another one — or
  // to none. The conversation's project is the default the rest of the
  // time, so merely filtering the list does not swap the notes under you.
  const project =
    (focusSkill ? projectOf(projects, focusSkill.projectId) : undefined) ??
    conversationProject
  const memoryForProject =
    project && memoryProjectId === project.id ? memory : undefined
  const clientOpen = useOpenClient() != null
  // Scheduled is a page, not a conversation. A foreign session uses the same
  // column and is not this thread's agents, files, or trace.
  if (scheduled || clientOpen) return null
  return (
    <RightPanel
      tab={tab}
      onTabChange={(next) => {
        if (next === "memory") seeMemory()
        onTabChange(next)
      }}
      transcript={transcript}
      selectedAgent={selectedAgent}
      onSelectAgent={selectAgent}
      files={files}
      workspace={workspace}
      turns={turns}
      meta={meta}
      threadId={activeId}
      memory={
        project
          ? {
              project,
              memory: memoryForProject,
              loading: memoryLoading || memoryProjectId !== project.id,
              onSave: saveMemory,
              onDeleteSkill: (name) => void removeSkill(name),
              onRefresh: () => void loadMemory(project.id),
              onReview: () => void reviewNow(),
              onTidySkills: () => void tidySkills(),
              reviewing,
              reviewHint,
              tidying,
              tidyReport,
              tidyError,
              tidyLive,
              onDismissTidy: clearTidy,
              unread: memoryUnread,
              onSeen: seeMemory,
              focusSkill: focusSkill?.name,
            }
          : undefined
      }
      onUpload={async (picked) => {
        await upload(picked)
      }}
      onDeleteFile={(path) => void removeFile(path)}
      onRefreshFiles={() => void refreshFiles()}
      onReveal={(path) => {
        if (activeId) void api.reveal(activeId, path).catch(() => undefined)
      }}
      usage={usage}
    />
  )
}

