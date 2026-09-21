import { AlertTriangle, X } from "lucide-react"
import { type ReactNode, useCallback } from "react"
import { useShallow } from "zustand/react/shallow"

import { Header } from "@/components/app/header"
import { Palette } from "@/components/app/palette"
import { SettingsDialog } from "@/components/app/settings-dialog"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { activeWake } from "@/store/app-schedule"
import { projectOf, useProjects } from "@/store/projects"
import { useSettingsSheet } from "@/store/settings-sheet"
import { useTerminal } from "@/store/terminal"

/** Settings is a full-page sheet. Keeping `open` off AppShell's state is
 *  the difference between painting the conversation and not: a setState
 *  there used to re-parse a 10k-block transcript on the same click. */
export function SettingsIdleChrome({ children }: { children: ReactNode }) {
  const open = useSettingsSheet((s) => s.open)
  return (
    <div
      className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden"
      hidden={open}
      inert={open || undefined}
    >
      {children}
    </div>
  )
}

export function isDark(theme: string): boolean {
  if (theme === "dark") return true
  if (theme === "light") return false
  return Boolean(window.matchMedia?.("(prefers-color-scheme: dark)").matches)
}

export function AppHeader({
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
}: {
  panelOpen: boolean
  sidebarOpen: boolean
  trafficInset: boolean
  onTogglePanel: () => void
  onToggleSidebar: () => void
  onToggleTheme: () => void
  onToggleLocale: () => void
  onToggleContentWidth: () => void
  onToggleTranscriptMode: () => void
  onOpenTerminal: () => void
}) {
  const threads = useApp((s) => s.threads)
  const activeId = useApp((s) => s.activeId)
  const status = useApp((s) => s.status)
  const schedules = useApp((s) => s.schedules)
  const meta = useApp((s) => s.meta)
  const connected = useApp((s) => s.connected)
  const theme = useApp((s) => s.theme)
  const contentWidth = useApp((s) => s.contentWidth)
  const transcriptMode = useApp((s) => s.transcriptMode)
  const projects = useProjects((s) => s.projects)
  const selectedId = useProjects((s) => s.selectedId)
  const terminalOpen = useTerminal((s) => s.open)
  const thread = threads.find((t) => t.id === activeId)
  return (
    <Header
      thread={thread}
      project={projectOf(projects, thread?.project_id)}
      status={status}
      waiting={Boolean(status.waiting) || Boolean(activeWake(schedules, activeId))}
      meta={meta}
      connected={connected || !activeId}
      panelOpen={panelOpen}
      sidebarOpen={sidebarOpen}
      trafficInset={trafficInset}
      onTogglePanel={onTogglePanel}
      onToggleSidebar={onToggleSidebar}
      onToggleTheme={onToggleTheme}
      onToggleLocale={onToggleLocale}
      onToggleContentWidth={onToggleContentWidth}
      onToggleTranscriptMode={onToggleTranscriptMode}
      onOpenTerminal={onOpenTerminal}
      contentWidth={contentWidth}
      transcriptMode={transcriptMode}
      dark={isDark(theme)}
      terminalOpen={terminalOpen}
      terminalEnabled={Boolean(activeId || selectedId)}
    />
  )
}

export function ConfiguredBanner({ onConfigure }: { onConfigure: () => void }) {
  const t = useT()
  const meta = useApp((s) => s.meta)
  if (!meta || meta.configured) return null
  return (
    <div className="flex items-center gap-2 border-b border-border bg-running/10 px-4 py-2 text-sm">
      <AlertTriangle className="size-4 shrink-0 text-running" />
      <span className="min-w-0 flex-1">
        {t("banner.unconfigured")}
      </span>
      <Button size="sm" variant="outline" onClick={onConfigure}>
        {t("banner.configure")}
      </Button>
    </div>
  )
}

export function ErrorBanner() {
  const t = useT()
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
        aria-label={t("banner.dismiss")}
        onClick={() => setError(undefined)}
      >
        <X />
      </Button>
    </div>
  )
}

export function AppPalette({
  open,
  onOpenChange,
  onNew,
  onSettings,
  onToggleTheme,
  onToggleLocale,
  onToggleContentWidth,
  onToggleTranscriptMode,
  onToggleSidebar,
  onFind,
  onOpenTerminal,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onNew: () => void
  onSettings: () => void
  onToggleTheme: () => void
  onToggleLocale: () => void
  onToggleContentWidth: () => void
  onToggleTranscriptMode: () => void
  onToggleSidebar: () => void
  onFind: () => void
  onOpenTerminal: () => void
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
      onToggleLocale={onToggleLocale}
      onToggleContentWidth={onToggleContentWidth}
      onToggleTranscriptMode={onToggleTranscriptMode}
      onToggleSidebar={onToggleSidebar}
      onFind={onFind}
      onOpenTerminal={onOpenTerminal}
    />
  )
}

export function AppSettings({
  trafficInset,
}: {
  trafficInset: boolean
}) {
  const open = useSettingsSheet((s) => s.open)
  const section = useSettingsSheet((s) => s.section)
  const onOpenChange = useCallback((next: boolean) => {
    useSettingsSheet.setState(next ? { open: true } : { open: false, section: "general" })
  }, [])
  const meta = useApp((s) => s.meta)
  const theme = useApp((s) => s.theme)
  const setTheme = useApp((s) => s.setTheme)
  const locale = useApp((s) => s.locale)
  const setLocale = useApp((s) => s.setLocale)
  // A fresh object every snapshot is React 19 error 185: the window
  // never paints. Shallow keeps the same fields without a new identity.
  const appearance = useApp(
    useShallow((s) => ({
      font: s.font,
      uiFontSize: s.uiFontSize,
      contentFont: s.contentFont,
      fontSize: s.fontSize,
      codeFont: s.codeFont,
      codeFontSize: s.codeFontSize,
      contentWidth: s.contentWidth,
      transcriptMode: s.transcriptMode,
      palette: s.palette,
    })),
  )
  const setAppearance = useApp((s) => s.setAppearance)
  const refreshAfterSettings = useCallback(() => {
    // boot() would reopen threads[0] and yank the conversation that is
    // sitting under this sheet. Meta + models is what Settings changed.
    void Promise.all([api.meta(), api.models()]).then(([meta, listed]) => {
      useApp.setState({ meta, models: listed.models })
    })
  }, [])
  return (
    <SettingsDialog
      open={open}
      onOpenChange={onOpenChange}
      meta={meta}
      theme={theme}
      onThemeChange={setTheme}
      locale={locale}
      onLocaleChange={setLocale}
      appearance={appearance}
      onAppearanceChange={setAppearance}
      onSaved={refreshAfterSettings}
      trafficInset={trafficInset}
      initialSection={section}
    />
  )
}
