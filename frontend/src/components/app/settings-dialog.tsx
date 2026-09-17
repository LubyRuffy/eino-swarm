import {
  ArrowLeft,
  Cpu,
  Library,
  Loader2,
  Palette,
  Search,
  UserRound,
  Workflow,
  Wrench,
} from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"

import { MemorySettings } from "@/components/app/memory-settings"
import { ModelsTab } from "@/components/app/model-settings"
import { GeneralTab } from "@/components/app/settings-general"
import { PersonalityTab } from "@/components/app/settings-personality"
import { SwarmTab } from "@/components/app/settings-swarm"
import { ToolsTab } from "@/components/app/settings-tools"
import { settingsMatch } from "@/components/app/settings-field"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import type { Appearance } from "@/lib/appearance"
import type { LocalePref } from "@/lib/i18n"
import { SettingsPersist } from "@/lib/settings-persist"
import type { Meta, Settings, ToolDescriptor } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT, type Translate } from "@/lib/use-t"
import type { Theme } from "@/store/app"

/** Scrollport for the right-hand page.
 *  `overflow-y-auto` does not replace TabsContent's `overflow-hidden`
 *  (twMerge treats them as different groups), so a 1px outline at the
 *  bottom is clipped. `overflow-auto` does replace it; `pb-1` keeps the
 *  last outline inside the clip edge. */
const settingsScrollTab =
  "thin-scrollbar min-h-0 flex-1 overflow-auto pb-1"

function sections(t: Translate) {
  return [
    {
      id: "general",
      label: t("settings.nav.general"),
      icon: Palette,
      keys: "appearance theme light dark font size serif mono width full wide standard comfortable log level data directory version language locale 语言 中文 english 外观 主题 字体 字号 铺满 宽屏 标准 宽度",
    },
    {
      id: "personality",
      label: t("settings.nav.personality"),
      icon: UserRound,
      keys: "personality preferences tone style voice 人设 个性化 偏好 语气",
    },
    {
      id: "models",
      label: t("settings.nav.models"),
      icon: Cpu,
      keys: "provider endpoint api key base url discover auxiliary title compact context window timeout model 模型 提供商",
    },
    {
      id: "swarm",
      label: t("settings.nav.swarm"),
      icon: Workflow,
      keys: "sub-agent timeout rounds manager pulse coalesce compact context budget goal title concurrent stream 集群",
    },
    {
      id: "tools",
      label: t("settings.nav.tools"),
      icon: Wrench,
      keys: "proxy http https search fetch exec shell 工具 代理",
    },
    {
      id: "memory",
      label: t("settings.nav.memory"),
      icon: Library,
      keys: "remember review notes skills notifications budget 记忆 笔记",
    },
  ] as const
}

type SectionId = ReturnType<typeof sections>[number]["id"]

/** Matches the left rail. The desktop title-bar strip paints the same
 *  column so the sidebar colour runs under the traffic lights. */
const RAIL_WIDTH = "w-56"

/** Empty strip matching native InvisibleTitleBarHeight (h-12). Settings
 *  covers the app header, so without this the lights sit on Back to app. */
function SettingsTitlebar() {
  return (
    <div
      data-drag-region
      data-testid="settings-titlebar"
      className="flex h-12 shrink-0"
    >
      <div
        className={cn(
          RAIL_WIDTH,
          "shrink-0 border-r border-sidebar-border bg-sidebar",
        )}
      />
      <div className="min-w-0 flex-1 bg-background" />
    </div>
  )
}

export function SettingsDialog({
  open,
  onOpenChange,
  meta,
  theme,
  onThemeChange,
  onSaved,
  trafficInset,
  locale,
  onLocaleChange,
  appearance,
  onAppearanceChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  meta?: Meta
  theme: Theme
  onThemeChange: (t: Theme) => void
  onSaved: () => void
  /** Desktop macOS: reserve the native title bar so Back to app is below
   *  the traffic lights, the way Codex does. */
  trafficInset?: boolean
  locale: LocalePref
  onLocaleChange: (l: LocalePref) => void
  appearance: Appearance
  onAppearanceChange: (patch: Partial<Appearance>) => void
}) {
  const t = useT()
  const nav = useMemo(() => sections(t), [t.locale])
  const [settings, setSettings] = useState<Settings>()
  const [catalog, setCatalog] = useState<ToolDescriptor[]>([])
  const [error, setError] = useState<string>()
  const [query, setQuery] = useState("")
  const [section, setSection] = useState<SectionId>("models")

  const onSavedRef = useRef(onSaved)
  onSavedRef.current = onSaved
  const localeRef = useRef(locale)
  localeRef.current = locale
  const setErrorRef = useRef(setError)
  setErrorRef.current = setError
  const persistRef = useRef<SettingsPersist | null>(null)
  if (!persistRef.current) {
    persistRef.current = new SettingsPersist(async (patch) => {
      try {
        await api.saveSettings(patch)
        onSavedRef.current()
      } catch (e) {
        setErrorRef.current(e instanceof Error ? e.message : String(e))
        throw e
      }
    })
  }

  useEffect(() => {
    if (!open) return
    setError(undefined)
    setQuery("")
    setSection("models")
    void Promise.all([api.settings(), api.tools()])
      .then(([s, tools]) => {
        setSettings(s)
        setCatalog(tools.catalog)
      })
      .catch((e) => setError(String(e instanceof Error ? e.message : e)))
  }, [open])

  useEffect(() => {
    persistRef.current?.setLocale(locale)
  }, [locale])

  useEffect(() => {
    return () => persistRef.current?.dispose()
  }, [])

  const apply = (next: Settings) => {
    setSettings(next)
    setError(undefined)
    persistRef.current?.schedule(next, localeRef.current)
  }

  const leave = async () => {
    try {
      await persistRef.current?.flush()
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  const visibleNav = nav.filter((s) =>
    settingsMatch(query, s.label, s.keys),
  )

  useEffect(() => {
    if (!query.trim()) return
    const ids = nav
      .filter((s) => settingsMatch(query, s.label, s.keys))
      .map((s) => s.id)
    if (!ids.includes(section) && ids[0]) setSection(ids[0])
  }, [query, section, nav])

  return (
    <Dialog
      // Full-page sheet, not a popup. modal's hideOthers walks every node
      // under the conversation; a long transcript made opening Settings
      // stall for seconds before the spinner even meant anything.
      modal={false}
      open={open}
      onOpenChange={(next) => {
        if (next) onOpenChange(true)
        else void leave()
      }}
    >
      <DialogContent
        hideClose
        onPointerDownOutside={(e) => e.preventDefault()}
        className="inset-0 left-0 top-0 flex h-dvh max-h-dvh w-full max-w-none translate-x-0 translate-y-0 flex-col gap-0 overflow-hidden rounded-none border-0 p-0 shadow-none sm:rounded-none data-[state=open]:zoom-in-100"
      >
        {trafficInset ? <SettingsTitlebar /> : null}
        {/* A form, so the API key field belongs to one and browsers stop
            warning about a stray password input. Enter must not close the
            sheet — there is no Save; edits already write themselves. */}
        <form
          className="flex min-h-0 flex-1 overflow-hidden"
          onSubmit={(e) => e.preventDefault()}
        >
          <Tabs
            orientation="vertical"
            value={section}
            onValueChange={(v) => setSection(v as SectionId)}
            className="flex min-h-0 flex-1"
          >
            <aside
              className={cn(
                RAIL_WIDTH,
                "flex shrink-0 flex-col gap-3 border-r border-sidebar-border bg-sidebar py-4 text-sidebar-foreground",
              )}
            >
              <div className="flex flex-col gap-3 px-3">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-8 justify-start gap-2 px-2 text-muted-foreground"
                  onClick={() => void leave()}
                >
                  <ArrowLeft />
                  {t("settings.back")}
                </Button>
                <DialogTitle className="px-2 text-lg font-semibold">
                  {t("settings.title")}
                </DialogTitle>
                <div className="relative">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") e.preventDefault()
                    }}
                    aria-label={t("settings.search")}
                    placeholder={t("settings.searchPlaceholder")}
                    spellCheck={false}
                    className="h-8 bg-background pl-8 text-xs shadow-none"
                  />
                </div>
              </div>

              <TabsList className="mt-1 flex h-auto w-full flex-col items-stretch gap-0.5 bg-transparent px-2 py-0">
                {visibleNav.map((s) => (
                  <TabsTrigger
                    key={s.id}
                    value={s.id}
                    className="h-9 w-full justify-start gap-2 rounded-lg px-2.5 text-sm font-medium shadow-none data-[state=active]:bg-accent data-[state=active]:text-accent-foreground data-[state=active]:shadow-none"
                  >
                      <s.icon className="size-4" aria-hidden />
                    {s.label}
                  </TabsTrigger>
                ))}
              </TabsList>

              <DialogDescription className="mt-auto px-4 pb-3 text-[11px] leading-snug text-muted-foreground">
                {t("settings.storedIn", {
                  path: meta?.data_dir ?? t("settings.storedFallback"),
                })}
              </DialogDescription>
            </aside>

            <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-background">
              {error ? (
                <p className="shrink-0 px-8 py-3 text-sm text-destructive">
                  {error}
                </p>
              ) : null}

              {!settings ? (
                <div className="flex flex-1 items-center justify-center text-muted-foreground">
                  <Loader2 className="size-5 animate-spin" />
                </div>
              ) : visibleNav.length === 0 ? (
                <p className="px-8 py-12 text-sm text-muted-foreground">
                  {t("settings.noMatch")}
                </p>
              ) : (
                <>
                  <TabsContent value="personality" className={settingsScrollTab}>
                    <PersonalityTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  </TabsContent>
                  <TabsContent value="models" className={settingsScrollTab}>
                    <ModelsTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  </TabsContent>
                  <TabsContent value="swarm" className={settingsScrollTab}>
                    <SwarmTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  </TabsContent>
                  <TabsContent value="tools" className={settingsScrollTab}>
                    <ToolsTab
                      settings={settings}
                      catalog={catalog}
                      onChange={apply}
                      query={query}
                    />
                  </TabsContent>
                  <TabsContent value="memory" className={settingsScrollTab}>
                    <MemorySettings
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  </TabsContent>
                  <TabsContent value="general" className={settingsScrollTab}>
                    <GeneralTab
                      theme={theme}
                      onThemeChange={onThemeChange}
                      locale={locale}
                      onLocaleChange={onLocaleChange}
                      appearance={appearance}
                      onAppearanceChange={onAppearanceChange}
                      meta={meta}
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  </TabsContent>
                </>
              )}
            </div>
          </Tabs>
        </form>
      </DialogContent>
    </Dialog>
  )
}
