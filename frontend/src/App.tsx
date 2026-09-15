import { AlertTriangle, X } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { Composer } from "@/components/app/composer"
import { EmptyState } from "@/components/app/empty-state"
import { Header } from "@/components/app/header"
import { Palette } from "@/components/app/palette"
import { RightPanel, type PanelTab } from "@/components/app/panel"
import { SettingsDialog } from "@/components/app/settings-dialog"
import { Sidebar } from "@/components/app/sidebar"
import { Transcript } from "@/components/app/transcript"
import { Button } from "@/components/ui/button"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { MANAGER_ID } from "@/lib/transcript"
import { useApp } from "@/store/app"

export function App() {
  const store = useApp()
  const {
    meta,
    models,
    threads,
    activeId,
    transcript,
    status,
    turns,
    files,
    workspace,
    loaded,
    connected,
    error,
    theme,
    selectedAgent,
  } = store

  const [panelOpen, setPanelOpen] = useState(true)
  const [panelWidth, setPanelWidth] = useState(352)
  const [panelTab, setPanelTab] = useState<PanelTab>("agents")
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [draft, setDraft] = useState("")
  const [focusSignal, setFocusSignal] = useState(0)
  const booted = useRef(false)

  useEffect(() => {
    // StrictMode mounts twice in development; booting twice would open two
    // event streams for the same conversation.
    if (booted.current) return
    booted.current = true
    void store.boot()
  }, [store])

  const focusComposer = useCallback(() => setFocusSignal((n) => n + 1), [])

  const newThread = useCallback(async () => {
    await store.newThread()
    focusComposer()
  }, [store, focusComposer])

  const toggleTheme = useCallback(() => {
    store.setTheme(isDark(theme) ? "light" : "dark")
  }, [store, theme])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Escape belongs to whatever is on top: a dialog or the palette closes
      // itself, and only with nothing layered over the transcript does Escape
      // mean "stop what you are doing".
      if (e.key === "Escape") {
        if (paletteOpen || settingsOpen || !status.running) return
        e.preventDefault()
        void store.interrupt()
        return
      }
      const mod = e.metaKey || e.ctrlKey
      if (!mod) return
      if (e.key === "k") {
        e.preventDefault()
        setPaletteOpen((open) => !open)
      } else if (e.key === "n") {
        e.preventDefault()
        void newThread()
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
  }, [newThread, paletteOpen, settingsOpen, status.running, store])

  // The elapsed clock ticks locally; the server only says when a turn started,
  // which keeps the event stream free of once-a-second noise.
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

  const thread = threads.find((t) => t.id === activeId)
  const model = useMemo(() => {
    const id = thread?.provider_id || meta?.default_provider
    const info = models.find((m) => m.id === id)
    return info?.label || info?.model
  }, [models, thread?.provider_id, meta?.default_provider])

  // With no conversation open there is nothing to replay, so the guidance is
  // shown immediately instead of a skeleton that would never resolve.
  const showEmptyState =
    !activeId || (loaded && (transcript.agents[MANAGER_ID]?.blocks.length ?? 0) === 0)

  const openAgent = useCallback(
    (id: string) => {
      store.selectAgent(id)
      setPanelTab("agents")
      setPanelOpen(true)
    },
    [store],
  )

  return (
    <TooltipProvider delayDuration={400}>
      <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
        <Sidebar
          threads={threads}
          activeId={activeId}
          runningId={status.running ? activeId : undefined}
          onNew={() => void newThread()}
          onOpen={(id) => void store.openThread(id)}
          onRename={(id, title) => void store.renameThread(id, title)}
          onDelete={(id) => void store.deleteThread(id)}
          onSearch={() => setPaletteOpen(true)}
          onSettings={() => setSettingsOpen(true)}
        />

        <main className="flex min-w-0 flex-1 flex-col">
          <Header
            thread={thread}
            status={status}
            elapsedMs={elapsedMs}
            meta={meta}
            model={model}
            connected={connected || !activeId}
            panelOpen={panelOpen}
            onTogglePanel={() => setPanelOpen((open) => !open)}
            onToggleTheme={toggleTheme}
            dark={isDark(theme)}
          />

          {meta && !meta.configured ? (
            <div className="flex items-center gap-2 border-b border-border bg-running/10 px-4 py-2 text-sm">
              <AlertTriangle className="size-4 shrink-0 text-running" />
              <span className="min-w-0 flex-1">
                No model endpoint is configured yet, so nothing can run.
              </span>
              <Button size="sm" variant="outline" onClick={() => setSettingsOpen(true)}>
                Configure
              </Button>
            </div>
          ) : null}

          {error ? (
            <div className="flex items-start gap-2 border-b border-destructive/40 bg-destructive/10 px-4 py-2 text-sm text-destructive">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <span className="min-w-0 flex-1">{error}</span>
              <Button
                size="icon-sm"
                variant="ghost"
                aria-label="Dismiss"
                onClick={() => store.setError(undefined)}
              >
                <X />
              </Button>
            </div>
          ) : null}

          {showEmptyState ? (
            <EmptyState
              onPick={(text) => {
                setDraft(text)
                focusComposer()
              }}
            />
          ) : (
            <Transcript state={transcript} loaded={loaded} onSelectAgent={openAgent} />
          )}

          <Composer
            running={status.running}
            models={models}
            provider={thread?.provider_id || meta?.default_provider}
            onProviderChange={(id) => {
              if (activeId) void api.patchThread(activeId, { provider_id: id }).then(
                () => store.refreshThreads(),
              )
            }}
            reasoning={thread?.reasoning_effort ?? ""}
            reasoningLevels={meta?.reasoning_levels ?? []}
            onReasoningChange={(level) => {
              if (activeId)
                void api
                  .patchThread(activeId, { reasoning_effort: level })
                  .then(() => store.refreshThreads())
            }}
            onSend={(text) => void store.send(text)}
            onStop={() => void store.interrupt()}
            onUpload={(picked) => store.upload(picked)}
            text={draft}
            onTextChange={setDraft}
            focusSignal={focusSignal}
          />
        </main>

        {panelOpen ? (
          <RightPanel
            tab={panelTab}
            onTabChange={setPanelTab}
            transcript={transcript}
            selectedAgent={selectedAgent}
            onSelectAgent={store.selectAgent}
            files={files}
            workspace={workspace}
            turns={turns}
            meta={meta}
            threadId={activeId}
            onUpload={(picked) => store.upload(picked)}
            onDeleteFile={(path) => void store.removeFile(path)}
            onRefreshFiles={() => void store.refreshFiles()}
            onReveal={(path) => {
              if (activeId) void api.reveal(activeId, path).catch(() => undefined)
            }}
            width={panelWidth}
            onWidthChange={setPanelWidth}
          />
        ) : null}

        <Palette
          open={paletteOpen}
          onOpenChange={setPaletteOpen}
          threads={threads}
          onOpen={(id) => void store.openThread(id)}
          onNew={() => void newThread()}
          onSettings={() => setSettingsOpen(true)}
          onToggleTheme={toggleTheme}
        />

        <SettingsDialog
          open={settingsOpen}
          onOpenChange={setSettingsOpen}
          meta={meta}
          theme={theme}
          onThemeChange={store.setTheme}
          onSaved={() => void store.boot()}
        />
      </div>
    </TooltipProvider>
  )
}

function isDark(theme: string): boolean {
  if (theme === "dark") return true
  if (theme === "light") return false
  return Boolean(window.matchMedia?.("(prefers-color-scheme: dark)").matches)
}
