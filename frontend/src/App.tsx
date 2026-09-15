import { AlertTriangle, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { Composer } from "@/components/app/composer"
import { DeleteProjectDialog } from "@/components/app/delete-project-dialog"
import { EmptyState } from "@/components/app/empty-state"
import { Header } from "@/components/app/header"
import { Palette } from "@/components/app/palette"
import { RightPanel, type PanelTab } from "@/components/app/panel"
import { ProjectDialog } from "@/components/app/project-dialog"
import { SettingsDialog } from "@/components/app/settings-dialog"
import { Sidebar } from "@/components/app/sidebar"
import { Transcript } from "@/components/app/transcript"
import { Button } from "@/components/ui/button"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { MANAGER_ID } from "@/lib/transcript"
import type { Project } from "@/lib/types"
import { isMac, readSidebarOpen, writeSidebarOpen } from "@/lib/utils"
import { useApp } from "@/store/app"
import { projectOf, useProjects } from "@/store/projects"

export function App() {
  const boot = useApp((s) => s.boot)
  const booted = useRef(false)
  useEffect(() => {
    // StrictMode mounts twice in development; booting twice would open two
    // event streams for the same conversation.
    if (booted.current) return
    booted.current = true
    void boot()
  }, [boot])

  return (
    <TooltipProvider delayDuration={400}>
      <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
        <AppShell />
      </div>
    </TooltipProvider>
  )
}

/** Chrome that must not re-render on every streamed token: sidebar, header,
 *  composer, and the panel subscribe to the slices they actually show. */
function AppShell() {
  const [panelOpen, setPanelOpen] = useState(true)
  const [sidebarOpen, setSidebarOpen] = useState(readSidebarOpen)
  const [panelTab, setPanelTab] = useState<PanelTab>("agents")
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [projectDialog, setProjectDialog] = useState<ProjectDialogState>()
  const [doomedProject, setDoomedProject] = useState<Project>()
  const [prefill, setPrefill] = useState("")
  const [prefillToken, setPrefillToken] = useState(0)
  const [focusSignal, setFocusSignal] = useState(0)

  const running = useApp((s) => s.status.running)
  const interrupt = useApp((s) => s.interrupt)
  const newThread = useApp((s) => s.newThread)
  const setTheme = useApp((s) => s.setTheme)
  const theme = useApp((s) => s.theme)
  const selectAgent = useApp((s) => s.selectAgent)
  const mode = useApp((s) => s.meta?.mode)
  const trafficLights = mode === "desktop" && isMac()

  const toggleSidebar = useCallback(() => {
    setSidebarOpen((open) => {
      const next = !open
      writeSidebarOpen(next)
      return next
    })
  }, [])

  const focusComposer = useCallback(() => setFocusSignal((n) => n + 1), [])

  const startThread = useCallback(async () => {
    await newThread()
    focusComposer()
  }, [newThread, focusComposer])

  const toggleTheme = useCallback(() => {
    setTheme(isDark(theme) ? "light" : "dark")
  }, [setTheme, theme])

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
      if (e.key === "Escape") {
        if (paletteOpen || settingsOpen || !running) return
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
      } else if (e.key === ",") {
        e.preventDefault()
        setSettingsOpen(true)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [interrupt, paletteOpen, running, settingsOpen, startThread, toggleSidebar])

  return (
    <>
      {sidebarOpen ? (
        <AppSidebar
          onNew={() => void startThread()}
          onSearch={() => setPaletteOpen(true)}
          onSettings={() => setSettingsOpen(true)}
          onCollapse={toggleSidebar}
          onNewProject={() => setProjectDialog({ open: true })}
          onEditProject={(project) => setProjectDialog({ open: true, project })}
          onDeleteProject={setDoomedProject}
        />
      ) : null}

      <main className="flex min-w-0 flex-1 flex-col">
        <AppHeader
          panelOpen={panelOpen}
          sidebarOpen={sidebarOpen}
          trafficInset={!sidebarOpen && trafficLights}
          onTogglePanel={() => setPanelOpen((open) => !open)}
          onToggleSidebar={toggleSidebar}
          onToggleTheme={toggleTheme}
        />
        <ConfiguredBanner onConfigure={() => setSettingsOpen(true)} />
        <ErrorBanner />
        <TranscriptPane onSelectAgent={openAgent} onPickIdea={pickIdea} />
        <AppComposer
          prefill={prefill}
          prefillToken={prefillToken}
          focusSignal={focusSignal}
        />
      </main>

      {panelOpen ? (
        <AppPanel tab={panelTab} onTabChange={setPanelTab} />
      ) : null}

      <AppPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        onNew={() => void startThread()}
        onSettings={() => setSettingsOpen(true)}
        onToggleTheme={toggleTheme}
        onToggleSidebar={toggleSidebar}
      />

      <AppSettings open={settingsOpen} onOpenChange={setSettingsOpen} />

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
  onCollapse,
  onNewProject,
  onEditProject,
  onDeleteProject,
}: {
  onNew: () => void
  onSearch: () => void
  onSettings: () => void
  onCollapse: () => void
  onNewProject: () => void
  onEditProject: (project: Project) => void
  onDeleteProject: (project: Project) => void
}) {
  const threads = useApp((s) => s.threads)
  const activeId = useApp((s) => s.activeId)
  const running = useApp((s) => s.status.running)
  const openThread = useApp((s) => s.openThread)
  const renameThread = useApp((s) => s.renameThread)
  const deleteThread = useApp((s) => s.deleteThread)
  const selectProject = useApp((s) => s.selectProject)
  const mode = useApp((s) => s.meta?.mode)
  const projects = useProjects((s) => s.projects)
  const selectedProjectId = useProjects((s) => s.selectedId)
  return (
    <Sidebar
      threads={threads}
      activeId={activeId}
      runningId={running ? activeId : undefined}
      trafficInset={mode === "desktop" && isMac()}
      onNew={onNew}
      onOpen={(id) => void openThread(id)}
      onRename={(id, title) => void renameThread(id, title)}
      onDelete={(id) => void deleteThread(id)}
      onSearch={onSearch}
      onSettings={onSettings}
      onCollapse={onCollapse}
      projects={projects}
      selectedProjectId={selectedProjectId}
      onSelectProject={(id) => void selectProject(id)}
      onNewProject={onNewProject}
      onEditProject={onEditProject}
      onDeleteProject={onDeleteProject}
    />
  )
}

function AppHeader({
  panelOpen,
  sidebarOpen,
  trafficInset,
  onTogglePanel,
  onToggleSidebar,
  onToggleTheme,
}: {
  panelOpen: boolean
  sidebarOpen: boolean
  trafficInset: boolean
  onTogglePanel: () => void
  onToggleSidebar: () => void
  onToggleTheme: () => void
}) {
  const threads = useApp((s) => s.threads)
  const activeId = useApp((s) => s.activeId)
  const status = useApp((s) => s.status)
  const meta = useApp((s) => s.meta)
  const models = useApp((s) => s.models)
  const connected = useApp((s) => s.connected)
  const theme = useApp((s) => s.theme)
  const projects = useProjects((s) => s.projects)
  const thread = threads.find((t) => t.id === activeId)
  const model = useMemo(() => {
    const id = thread?.provider_id || meta?.default_provider
    const info = models.find((m) => m.id === id)
    return info?.label || info?.model
  }, [models, thread?.provider_id, meta?.default_provider])
  return (
    <Header
      thread={thread}
      project={projectOf(projects, thread?.project_id)}
      status={status}
      meta={meta}
      model={model}
      connected={connected || !activeId}
      panelOpen={panelOpen}
      sidebarOpen={sidebarOpen}
      trafficInset={trafficInset}
      onTogglePanel={onTogglePanel}
      onToggleSidebar={onToggleSidebar}
      onToggleTheme={onToggleTheme}
      dark={isDark(theme)}
    />
  )
}

function ConfiguredBanner({ onConfigure }: { onConfigure: () => void }) {
  const meta = useApp((s) => s.meta)
  if (!meta || meta.configured) return null
  return (
    <div className="flex items-center gap-2 border-b border-border bg-running/10 px-4 py-2 text-sm">
      <AlertTriangle className="size-4 shrink-0 text-running" />
      <span className="min-w-0 flex-1">
        No model endpoint is configured yet, so nothing can run.
      </span>
      <Button size="sm" variant="outline" onClick={onConfigure}>
        Configure
      </Button>
    </div>
  )
}

function ErrorBanner() {
  const error = useApp((s) => s.error)
  const setError = useApp((s) => s.setError)
  if (!error) return null
  return (
    <div className="flex items-start gap-2 border-b border-destructive/40 bg-destructive/10 px-4 py-2 text-sm text-destructive">
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <span className="min-w-0 flex-1">{error}</span>
      <Button
        size="icon-sm"
        variant="ghost"
        aria-label="Dismiss"
        onClick={() => setError(undefined)}
      >
        <X />
      </Button>
    </div>
  )
}

function TranscriptPane({
  onSelectAgent,
  onPickIdea,
}: {
  onSelectAgent: (id: string) => void
  onPickIdea: (text: string) => void
}) {
  const activeId = useApp((s) => s.activeId)
  const loaded = useApp((s) => s.loaded)
  const transcript = useApp((s) => s.transcript)
  const showEmptyState =
    !activeId || (loaded && (transcript.agents[MANAGER_ID]?.blocks.length ?? 0) === 0)
  if (showEmptyState) {
    return <EmptyState onPick={onPickIdea} />
  }
  return <Transcript state={transcript} loaded={loaded} onSelectAgent={onSelectAgent} />
}

function AppComposer({
  prefill,
  prefillToken,
  focusSignal,
}: {
  prefill: string
  prefillToken: number
  focusSignal: number
}) {
  const running = useApp((s) => s.status.running)
  const models = useApp((s) => s.models)
  const threads = useApp((s) => s.threads)
  const activeId = useApp((s) => s.activeId)
  const meta = useApp((s) => s.meta)
  const send = useApp((s) => s.send)
  const interrupt = useApp((s) => s.interrupt)
  const upload = useApp((s) => s.upload)
  const refreshThreads = useApp((s) => s.refreshThreads)
  const thread = threads.find((t) => t.id === activeId)
  return (
    <Composer
      running={running}
      models={models}
      provider={thread?.provider_id || meta?.default_provider}
      onProviderChange={(id) => {
        if (activeId) void api.patchThread(activeId, { provider_id: id }).then(() => refreshThreads())
      }}
      reasoning={thread?.reasoning_effort ?? ""}
      reasoningLevels={meta?.reasoning_levels ?? []}
      onReasoningChange={(level) => {
        if (activeId)
          void api.patchThread(activeId, { reasoning_effort: level }).then(() => refreshThreads())
      }}
      onSend={(text) => void send(text)}
      onStop={() => void interrupt()}
      onUpload={(picked) => upload(picked)}
      prefill={prefill}
      prefillToken={prefillToken}
      focusSignal={focusSignal}
    />
  )
}

function AppPanel({
  tab,
  onTabChange,
}: {
  tab: PanelTab
  onTabChange: (tab: PanelTab) => void
}) {
  const transcript = useApp((s) => s.transcript)
  const selectedAgent = useApp((s) => s.selectedAgent)
  const selectAgent = useApp((s) => s.selectAgent)
  const files = useApp((s) => s.files)
  const workspace = useApp((s) => s.workspace)
  const turns = useApp((s) => s.turns)
  const meta = useApp((s) => s.meta)
  const activeId = useApp((s) => s.activeId)
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
  const project = projectOf(
    projects,
    threads.find((t) => t.id === activeId)?.project_id,
  )
  return (
    <RightPanel
      tab={tab}
      onTabChange={onTabChange}
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
              memory,
              loading: memoryLoading,
              onSave: saveMemory,
              onDeleteSkill: (name) => void removeSkill(name),
              onRefresh: () => void loadMemory(project.id),
              onReview: () => void reviewNow(),
            }
          : undefined
      }
      onUpload={(picked) => upload(picked)}
      onDeleteFile={(path) => void removeFile(path)}
      onRefreshFiles={() => void refreshFiles()}
      onReveal={(path) => {
        if (activeId) void api.reveal(activeId, path).catch(() => undefined)
      }}
    />
  )
}

function AppPalette({
  open,
  onOpenChange,
  onNew,
  onSettings,
  onToggleTheme,
  onToggleSidebar,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onNew: () => void
  onSettings: () => void
  onToggleTheme: () => void
  onToggleSidebar: () => void
}) {
  const threads = useApp((s) => s.threads)
  const openThread = useApp((s) => s.openThread)
  return (
    <Palette
      open={open}
      onOpenChange={onOpenChange}
      threads={threads}
      onOpen={(id) => void openThread(id)}
      onNew={onNew}
      onSettings={onSettings}
      onToggleTheme={onToggleTheme}
      onToggleSidebar={onToggleSidebar}
    />
  )
}

function AppSettings({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const meta = useApp((s) => s.meta)
  const theme = useApp((s) => s.theme)
  const setTheme = useApp((s) => s.setTheme)
  const boot = useApp((s) => s.boot)
  return (
    <SettingsDialog
      open={open}
      onOpenChange={onOpenChange}
      meta={meta}
      theme={theme}
      onThemeChange={setTheme}
      onSaved={() => void boot()}
    />
  )
}

function isDark(theme: string): boolean {
  if (theme === "dark") return true
  if (theme === "light") return false
  return Boolean(window.matchMedia?.("(prefers-color-scheme: dark)").matches)
}
