import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"

import { AccountModelsSync } from "@/components/app/account-models-sync"
import { EngineStartupBanner } from "@/components/app/engine-startup"
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
import { CompletionReader } from "@/components/app/completion-reader"
import { Composer } from "@/components/app/composer"
import { displayedModel, rememberedCatalogModel } from "@/lib/models"
import { DeleteProjectDialog } from "@/components/app/delete-project-dialog"
import { EmptyState } from "@/components/app/empty-state"
import { ExternalSkillsDialog } from "@/components/app/external-skills-dialog"
import { FindBar, useFindController } from "@/components/app/find-bar"
import type { PanelTab } from "@/components/app/panel"
import { AppPanel } from "@/components/app/app-panel"
import { useRegisteredInspectorAutoOpen } from "@/components/app/inspector-panels"
import { ProjectDialog } from "@/components/app/project-dialog"
import { SelectionMenu } from "@/components/app/selection-menu"
import { SideChat } from "@/components/app/side-chat"
import { ScheduleDetail } from "@/components/app/schedule-inbox"
import { useVisibleDest } from "@/components/app/dest-rail"
import { Sidebar } from "@/components/app/sidebar"
import { ToastStack } from "@/components/app/toast-stack"
import { TerminalPanel } from "@/components/app/terminal-panel"
import { WelcomeGate } from "@/components/app/welcome-gate"
import { Transcript } from "@/components/app/transcript"
import { WorkspaceFilePreview } from "@/components/app/workspace-file-preview"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { toggleContentWidth, toggleTranscriptMode } from "@/lib/appearance"
import { attachExternalLinkHandler } from "@/lib/external-links"
import { findShortcut } from "@/lib/find"
import { appendQuote } from "@/lib/quote"
import { composerDraftKey, composerDraftSession } from "@/lib/composer-draft"
import { installComposerRecoveryAttribution } from "@/lib/composer-recovery"
import { liveWorkers } from "@/lib/transcript"
import { terminalShortcut, terminalTarget } from "@/lib/terminal"
import { showLoginGate } from "@/lib/account-models"
import { isWelcomePane, visibleManagerBlockCount } from "@/lib/welcome"
import type { Project, SkillInfo } from "@/lib/types"
import { startSidebarSync } from "@/lib/sidebar-sync"
import { observeSystemTheme } from "@/lib/system-theme"
import { askingThreadIds } from "@/lib/thread-title"
import { useSideChatScope } from "@/lib/use-side-chat"
import { toggleLocalePref, useT } from "@/lib/use-t"
import { isMac, readSidebarOpen, writeSidebarOpen } from "@/lib/utils"
import { desktopShell, openLinksNatively } from "@/lib/shell"
import { applyTheme, useApp } from "@/store/app"
import { waitingThreadIds } from "@/store/app-schedule"
import { useSideChat } from "@/store/app-side-chat"
import { useTraceFocus } from "@/store/trace-focus"
import { useProjects } from "@/store/projects"
import { useTerminal } from "@/store/terminal"
import { openSettings, useCoveredTranscript, useSettingsSheet } from "@/store/settings-sheet"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

export function App() {
  useEffect(installComposerRecoveryAttribution, [])
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

/** Subscribes to the inbox on its own. AppShell must not: a click here
 *  used to re-parse the conversation that stays mounted underneath. */
function ShellColumns({ children }: { children: ReactNode }) {
  const scheduled = useApp((s) => s.scheduleInboxOpen)
  return (
    <div
      className="flex min-h-0 min-w-0 flex-1 overflow-hidden"
      data-testid={scheduled ? "schedule-page" : undefined}
      aria-labelledby={scheduled ? "schedule-page-title" : undefined}
    >
      {children}
    </div>
  )
}

function TaskBody({ chat }: { chat: ReactNode }) {
  const pane = useVisibleDest()
  if (pane === "scheduled") return <ScheduleDetail />
  return <><CompletionReader />{chat}</>
}

/** Chrome that must not re-render on every streamed token: sidebar, header,
 *  composer, and the panel subscribe to the slices they actually show.
 *  The composer's usage ring subscribes in a child so a token/usage pulse
 *  cannot rewrite the textarea while a CJK IME is composing. */
function AppShell() {
  const t = useT()
  const loginWall = useApp((s) => showLoginGate(s.meta))
  const [panelOpen, setPanelOpen] = useState(true)
  const [sidebarOpen, setSidebarOpen] = useState(readSidebarOpen)
  const [panelTab, setPanelTab] = useState<PanelTab>("agents")
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [projectDialog, setProjectDialog] = useState<ProjectDialogState>()
  const projectLoad = useRef<AbortController | null>(null)
  useEffect(() => () => projectLoad.current?.abort(), [])
  const openProjectDialog = async (project?: Project) => {
    projectLoad.current?.abort()
    useToasts.getState().dismiss("project:load")
    if (!project) {
      setProjectDialog({ open: true })
      return
    }
    // The sidebar snapshot can predate edits by an agent or another window.
    const load = new AbortController()
    projectLoad.current = load
    try {
      const fresh = await api.project(project.id, load.signal)
      if (!load.signal.aborted) setProjectDialog({ open: true, project: fresh })
    } catch (error) {
      if (!load.signal.aborted) {
        toastError(errorMessage(error), { id: "project:load", title: t("settings.loadFailed") })
      }
    } finally {
      if (projectLoad.current === load) projectLoad.current = null
    }
  }
  const [doomedProject, setDoomedProject] = useState<Project>()
  const [externalSkillsProject, setExternalSkillsProject] = useState<Project>()
  const [prefill, setPrefill] = useState("")
  const [prefillToken, setPrefillToken] = useState(0)
  const [focusSignal, setFocusSignal] = useState(0)
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

  const newThread = useApp((s) => s.newThread)
  const setTheme = useApp((s) => s.setTheme)
  const theme = useApp((s) => s.theme)
  useEffect(() => observeSystemTheme(theme, applyTheme), [theme])
  const setLocale = useApp((s) => s.setLocale)
  const setAppearance = useApp((s) => s.setAppearance)
  const selectAgent = useApp((s) => s.selectAgent)
  const activeId = useApp((s) => s.activeId)
  const showInspector = useCallback((tab: PanelTab) => {
    useSideChat.getState().close()
    setPanelOpen(true)
    setPanelTab(tab)
  }, [])
  useRegisteredInspectorAutoOpen(activeId, showInspector)
  const traceRequest = useTraceFocus((s) => s.request)
  useEffect(() => useTraceFocus.getState().clear(), [activeId])
  useEffect(() => {
    if (useTraceFocus.getState().turnId) showInspector("trace")
  }, [traceRequest, showInspector])
  const trafficLights = desktopShell() && isMac()
  const side = useSideChatScope(activeId)

  const togglePanel = useCallback(() => {
    const chat = useSideChat.getState()
    if (chat.session) chat.close()
    setPanelOpen((open) => Boolean(chat.session) || !open)
  }, [])

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

  const addQuote = useCallback(
    (text: string) => {
      const draft = currentComposerDraft()
      void draft.load().then(() => draft.update({ quotes: appendQuote(draft.getSnapshot().draft.quotes, text) })).catch(() => undefined)
      focusComposer()
    },
    [focusComposer],
  )

  const startThread = useCallback(async () => {
    await newThread()
    focusComposer()
  }, [newThread, focusComposer])

  const startThreadInProject = useCallback(
    async (project: Project) => {
      // Select first so the folder expands onto the conversation we are
      // about to create. Creating first and then refreshing used to wipe
      // the new row if the listing raced the insert.
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
      showInspector("agents")
    },
    [selectAgent, showInspector],
  )

  const pickIdea = useCallback(
    (text: string) => {
      setPrefill(text)
      setPrefillToken((n) => n + 1)
      focusComposer()
    },
    [focusComposer],
  )

  const editQueuedFollowup = useCallback(
    (id: string, text: string) => {
      const threadId = useApp.getState().activeId
      const draft = currentComposerDraft()
      void (async () => {
        try {
          await draft.importQueuedDraft(text, () => useApp.getState().activeId === threadId ? useApp.getState().deleteFollowup(id) : Promise.resolve(false))
          focusComposer()
        } catch (error) { useApp.getState().setError(error instanceof Error ? error.message : String(error)) }
      })()
    },
    [focusComposer],
  )

  const editUnreadSteer = useCallback(
    (text: string, seq: number) => {
      const threadId = useApp.getState().activeId
      const draft = currentComposerDraft()
      void (async () => {
        try {
          await draft.importQueuedDraft(text, () => useApp.getState().activeId === threadId ? useApp.getState().retractSteer(seq) : Promise.resolve(false))
          focusComposer()
        } catch (error) { useApp.getState().setError(error instanceof Error ? error.message : String(error)) }
      })()
    },
    [focusComposer],
  )

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // A local dialog or the focused side chat owns Escape.
      if (
        e.key === "Escape" &&
        (document.querySelector('[role="dialog"][data-state="open"]') ||
          (e.target instanceof Element && e.target.closest("#side-chat")))
      ) return
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
      const mod = e.metaKey || e.ctrlKey
      if (!mod) return
      if (showLoginGate(useApp.getState().meta) && e.key !== ",") return
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
        togglePanel()
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
    nextFind,
    openFind,
    paletteOpen,
    projectDialog,
    selectFindQuery,
    startThread,
    togglePanel,
    toggleSidebar,
    toggleTerminal,
  ])

  return (
    <WorkspaceFilePreview>
    <>
      <SettingsIdleChrome>
      <AppHeader
        panelOpen={panelOpen && !side.shown}
        sidebarOpen={sidebarOpen}
        trafficInset={trafficLights}
        onTogglePanel={togglePanel}
        onToggleSidebar={toggleSidebar}
        onOpenTerminal={spawnTerminal}
        onNew={() => void startThread()}
        onSearch={() => setPaletteOpen(true)}
        sideChat={side.toggle}
      />
      <ShellColumns>
        <AppSidebar
            listOpen={sidebarOpen}
            onNew={() => void startThread()}
            onSettings={openSettings}
            onToggleTheme={toggleTheme}
            onToggleLocale={toggleLocale}
            onNewProject={() => void openProjectDialog()}
            onNewInProject={(project) => void startThreadInProject(project)}
            onHistoryReport={(project) => {
              if (useApp.getState().creatingThread) return
              useProjects.getState().select(project.id)
              void (async () => {
                const id = await newThread(project.id, undefined, t("projects.historyTitle"))
                if (id) await useApp.getState().send(t("projects.historyPrompt"), undefined, { threadId: id, historyReport: true })
              })()
            }}
            onEditProject={(project) => void openProjectDialog(project)}
            onDeleteProject={(project) => {
              projectLoad.current?.abort()
              setDoomedProject(project)
            }}
            onOpenExternalSkills={setExternalSkillsProject}
            onOpenSkill={(project, skill) => {
              // The list is the directory; the Memory tab is the document.
              // Selecting the project loads its notes so the panel is not
              // still showing whatever conversation happened to be open.
              void useApp.getState().selectProject(project.id)
              useProjects.getState().seeMemory()
              showInspector("memory")
              setFocusSkill({ projectId: project.id, name: skill?.name })
            }}
          />

        <div className="flex min-w-0 flex-1">
        <main className="relative flex min-w-0 flex-1 flex-col">
          <EngineStartupBanner />
          <DesktopUpdateBanner />
          <ConfiguredBanner onConfigure={() => openSettings("models")} />
          <AccountModelsSync />
          <WelcomeGate />
          <ErrorBanner />
          <TaskBody
            chat={
          <div
            data-composer-stage=""
            data-testid="composer-stage"
            className="relative min-h-0 min-w-0 flex-1"
            inert={loginWall || undefined}
          >
            <div className="absolute inset-0 flex min-h-0 min-w-0 flex-col overflow-hidden">
                <TranscriptPane
                  onSelectAgent={openAgent}
                  onPickIdea={pickIdea}
                  onEditQueuedSteer={editUnreadSteer}
                  findQuery={findOpen ? findQuery : ""}
                  findIndex={findIndex}
                  onFindCount={setFindTotal}
                />
            </div>
            <AppComposer
              prefill={prefill}
              prefillToken={prefillToken}
              focusSignal={focusSignal}
              onEditFollowup={editQueuedFollowup}
              onEditProviders={() => openSettings("models")}
              onOpenSideChat={side.fromComposer}
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
          </div>
            }
          />
          <TerminalPanel onNew={spawnTerminal} />
        </main>

          {side.shown && side.owner ? (
            <SideChat key={side.owner} threadId={side.owner} onClose={side.close} />
          ) : null}
        </div>

        {panelOpen && !side.shown ? (
            <AppPanel
              tab={panelTab}
              onTabChange={(tab) => {
                if (tab !== "memory") setFocusSkill(undefined)
                setPanelTab(tab)
              }}
              focusSkill={focusSkill}
            />
        ) : null}
      </ShellColumns>

      <SelectionMenu key={`${activeId ?? ""}:${side.owner ?? ""}`} threadId={activeId ?? undefined} onAdd={addQuote} onAsk={side.fromQuote} />

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

      {externalSkillsProject ? (
        <ExternalSkillsDialog
          key={externalSkillsProject.id}
          project={externalSkillsProject}
          onClose={() => setExternalSkillsProject(undefined)}
        />
      ) : null}

      <AppDeleteProject
        project={doomedProject}
        onOpenChange={(open) => {
          if (!open) setDoomedProject(undefined)
        }}
      />
      </SettingsIdleChrome>

      <AppSettings trafficInset={trafficLights} />
    </>
    </WorkspaceFilePreview>
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
  listOpen,
  onNew,
  onSettings,
  onToggleTheme,
  onToggleLocale,
  onNewProject,
  onNewInProject,
  onHistoryReport,
  onEditProject,
  onDeleteProject,
  onOpenSkill,
  onOpenExternalSkills,
}: {
  listOpen: boolean
  onNew: () => void
  onSettings: () => void
  onToggleTheme: () => void
  onToggleLocale: () => void
  onNewProject: () => void
  onNewInProject: (project: Project) => void
  onHistoryReport?: (project: Project) => void
  onEditProject: (project: Project) => void
  onDeleteProject: (project: Project) => void
  onOpenSkill: (project: Project, skill?: SkillInfo) => void
  onOpenExternalSkills: (project: Project) => void
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
      listOpen={listOpen}
      threads={threads}
      activeId={activeId}
      runningId={running ? activeId : overlayRunningId}
      waitingIds={waitingIds}
      askingIds={askingIds}
      onNew={onNew}
      onOpen={(id) => {
        void openThread(id)
      }}
      onRename={(id, title) => void renameThread(id, title)}
      onDelete={(id) => void deleteThread(id)}
      onReorder={(ids) => void reorderThreads(ids)}
      onSettings={onSettings}
      onToggleTheme={onToggleTheme}
      onToggleLocale={onToggleLocale}
      projects={projects}
      selectedProjectId={selectedProjectId}
      onSelectProject={(id) => void selectProject(id)}
      onNewProject={onNewProject}
      onNewInProject={onNewInProject}
      onHistoryReport={onHistoryReport}
      onEditProject={onEditProject}
      onDeleteProject={onDeleteProject}
      onOpenSkill={onOpenSkill}
      onOpenExternalSkills={onOpenExternalSkills}
      onReorderProjects={(ids) => void reorderProjects(ids)}
      onPin={(id, pinned) => void pinThread(id, pinned)}
    />
  )
}

function TranscriptPane({
  onSelectAgent,
  onPickIdea,
  onEditQueuedSteer,
  findQuery,
  findIndex,
  onFindCount,
}: {
  onSelectAgent: (id: string) => void
  onPickIdea: (text: string) => void
  onEditQueuedSteer: (text: string, seq: number) => void
  findQuery: string
  findIndex: number
  onFindCount: (total: number) => void
}) {
  const activeId = useApp((s) => s.activeId)
  const loaded = useApp((s) => s.loaded)
  const transcript = useCoveredTranscript()
  const running = useApp((s) => s.status.running || s.transcript.running)
  const historyHasMore = useApp((s) => s.historyHasMore)
  const send = useApp((s) => s.send)
  const continueFailed = useApp((s) => s.continueFailed)
  const resendUser = useCallback(
    (text: string, seq: number) => {
      return send(text, undefined, { fromEventSeq: seq })
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
      onContinueFailed={continueFailed}
      onEditQueuedSteer={onEditQueuedSteer}
      findQuery={findQuery}
      findIndex={findIndex}
      onFindCount={onFindCount}
    />
  )
}

function currentComposerDraft() {
  return composerDraftSession(composerDraftKey(useApp.getState().activeId, useProjects.getState().selectedId))
}

function AppComposer({
  prefill,
  prefillToken,
  focusSignal,
  onEditFollowup,
  onEditProviders,
  onOpenSideChat,
}: {
  prefill: string
  prefillToken: number
  focusSignal: number
  onEditFollowup: (id: string, text: string) => void
  onEditProviders: () => void
  onOpenSideChat: (question: string) => boolean
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
  const activeTurnId = useApp((s) => s.status.running ? s.status.turn_id : undefined)
  const waitingForAnswer = useApp((s) => s.status.awaiting_answer)
  const deleteFollowup = useApp((s) => s.deleteFollowup)
  const clearFollowups = useApp((s) => s.clearFollowups)
  const upload = useApp((s) => s.upload)
  const refreshThreads = useApp((s) => s.refreshThreads)
  const refreshCatalogs = useApp((s) => s.refreshCatalogs)
  const newThread = useApp((s) => s.newThread)
  const creatingThread = useApp((s) => s.creatingThread)
  const projectId = useProjects((s) => s.selectedId)
  const pinnedCatalog = useRef("")
  useEffect(() => {
    if (!thread) return
    const remember = rememberedCatalogModel(
      displayedModel(models, thread.provider_id || meta?.default_provider, thread.model),
      thread.model,
    )
    if (!remember) return
    const key = `${thread.id}\t${remember.providerId}\t${remember.model}`
    if (pinnedCatalog.current === key) return
    pinnedCatalog.current = key
    void api.patchThread(thread.id, { provider_id: remember.providerId, model: remember.model }).then(() => refreshThreads())
  }, [thread, models, meta?.default_provider, refreshThreads])
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
      draftKey={composerDraftKey(activeId, projectId)}
      onResolveThread={async () => activeId ?? await newThread(projectId, composerDraftKey(activeId, projectId))}
      running={running}
      compressing={compressing}
      models={models}
      provider={thread?.provider_id || meta?.default_provider}
      model={thread?.model}
      onModelChange={(providerId, name) => {
        void (async () => {
          const id = activeId ?? (await newThread(projectId, composerDraftKey(activeId, projectId)))
          if (!id) return
          await api.patchThread(id, { provider_id: providerId, model: name })
          await refreshThreads()
        })()
      }}
      reasoning={thread?.reasoning_effort ?? ""}
      reasoningLevels={meta?.reasoning_levels ?? []}
      onReasoningChange={(level) => {
        void (async () => {
          const id = activeId ?? await newThread(projectId, composerDraftKey(activeId, projectId))
          if (!id) return
          await api.patchThread(id, { reasoning_effort: level })
          await refreshThreads()
        })().catch((err: unknown) => useApp.getState().setError(String(err)))
      }}
      onSend={(text, images, opts) => send(text, images, opts)}
      onStop={() => void interrupt()}
      onUpload={upload}
      onRefreshModels={() => refreshCatalogs()}
      onEditProviders={onEditProviders}
      onOpenSideChat={onOpenSideChat}
      restoring={meta?.startup === "restoring"}
      canStopWhileRestoring={Boolean(activeId)}
      disabled={creatingThread}
      prefill={prefill}
      prefillToken={prefillToken}
      focusSignal={focusSignal}
      followups={followups}
      activeTurnId={activeTurnId}
      waitingForAnswer={waitingForAnswer}
      onSteerFollowup={(id, mode) => void steerFollowup(id, mode, activeTurnId)}
      onDeleteFollowup={(id) => void deleteFollowup(id)}
      onEditFollowup={(item) => onEditFollowup(item.id, item.text)}
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
      onSetGoal={(text, id) => setGoal(text, id)}
      onClearGoal={() => setGoal("")}
      onEditGoal={(text) => editGoal(text)}
      onResumeGoal={() => void resumeGoal()}
      onCompact={() => void compactThread()}
      planMode={thread?.plan_mode}
      planMarkdown={thread?.plan_markdown}
      awaitingAnswer={awaitingAnswer}
      onSetPlan={(text, id) => setPlan(text, id)}
      onSavePlan={(text) => void savePlan(text)}
      onImplementPlan={() => void implementPlan()}
      onLeavePlan={() => void leavePlan()}
    />
  )
}

