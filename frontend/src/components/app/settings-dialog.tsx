import {
  ArrowLeft,
  Bot,
  Cpu,
  Library,
  Loader2,
  Palette,
  Search,
  Smartphone,
  UserRound,
  Workflow,
  Wrench,
} from "lucide-react"
import { useEffect, useMemo, useRef, useState } from "react"

import { MemorySettings } from "@/components/app/memory-settings"
import { ModelsTab } from "@/components/app/model-settings"
import { GeneralTab } from "@/components/app/settings-general"
import { PersonalityTab } from "@/components/app/settings-personality"
import { ClientsTab } from "@/components/app/settings-clients"
import { RemoteTab } from "@/components/app/settings-remote"
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
import { api } from "@/lib/api"
import { normalizeAppearance, type Appearance } from "@/lib/appearance"
import { chromeTypeClass } from "@/lib/chrome-type"
import type { LocalePref } from "@/lib/i18n"
import { SettingsPersist } from "@/lib/settings-persist"
import type { Meta, Settings, ToolDescriptor } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT, type Translate } from "@/lib/use-t"
import type { Theme } from "@/store/app"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

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
      keys: "appearance theme light dark palette zwai fofa font size serif mono width full wide standard comfortable transcript user developer compact 语言 中文 english 外观 主题 配色 字体 字号 UI 正文 代码 铺满 宽屏 标准 宽度 用户 开发 精简",
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
      keys: "provider endpoint api key base url discover auxiliary title compact context window timeout model embedding semantic search 模型 提供商 嵌入 语义 搜索",
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
    {
      id: "remote",
      label: t("settings.nav.remote"),
      icon: Smartphone,
      keys: "phone remote pair qr hub token pairing scan awake sleep model device 手机 扫码 远程 配对 唤醒 休眠 型号",
    },
    {
      id: "clients",
      label: t("settings.nav.clients"),
      icon: Bot,
      keys: "clients claude codex cursor local agent tasks 客户端 本地",
    },
  ] as const
}

export type SettingsSectionId = ReturnType<typeof sections>[number]["id"]

/** Sidebar, ⌘, and ⌘K open here. Model picker / unconfigured banner pass
 *  `models` because that is the page they were already talking about. */
export const SETTINGS_HOME_SECTION: SettingsSectionId = "general"

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
  initialSection = SETTINGS_HOME_SECTION,
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
  initialSection?: SettingsSectionId
}) {
  const t = useT()
  const tRef = useRef(t)
  tRef.current = t
  const nav = useMemo(() => sections(t), [t.locale])
  const [settings, setSettings] = useState<Settings>()
  const [catalog, setCatalog] = useState<ToolDescriptor[]>([])
  const [query, setQuery] = useState("")
  const [section, setSection] = useState<SettingsSectionId>(initialSection)

  const onSavedRef = useRef(onSaved)
  onSavedRef.current = onSaved
  const localeRef = useRef(locale)
  localeRef.current = locale
  const persistRef = useRef<SettingsPersist | null>(null)
  if (!persistRef.current) {
    persistRef.current = new SettingsPersist(async (patch) => {
      try {
        await api.saveSettings(patch)
        useToasts.getState().dismiss("settings:save")
        onSavedRef.current()
      } catch (e) {
        toastError(errorMessage(e), {
          id: "settings:save",
          title: tRef.current("settings.saveFailed"),
        })
        throw e
      }
    })
  }

  useEffect(() => {
    if (!open) return
    useToasts.getState().dismiss("settings:save")
    useToasts.getState().dismiss("settings:load")
    setQuery("")
    setSection(initialSection)
    void Promise.all([api.settings(), api.tools()])
      .then(([s, tools]) => {
        setSettings(s)
        setCatalog(tools.catalog)
      })
      .catch((e) =>
        toastError(errorMessage(e), {
          id: "settings:load",
          title: tRef.current("settings.loadFailed"),
        }),
      )
  }, [open, initialSection])

  useEffect(() => {
    persistRef.current?.setLocale(locale)
  }, [locale])

  useEffect(() => {
    return () => persistRef.current?.dispose()
  }, [])

  const apply = (next: Settings) => {
    setSettings(next)
    useToasts.getState().dismiss("settings:save")
    persistRef.current?.schedule(next, localeRef.current)
  }

  const leave = async () => {
    try {
      await persistRef.current?.flush()
      onOpenChange(false)
    } catch (e) {
      toastError(errorMessage(e), {
        id: "settings:save",
        title: t("settings.saveFailed"),
      })
    }
  }

  const visibleNav = nav.filter((s) =>
    settingsMatch(query, s.label, s.keys),
  )
  // Radix used to fire an empty value when the panes were not TabsContent.
  // An unknown id paints a blank sheet with no rail highlight.
  const page: SettingsSectionId =
    visibleNav.find((s) => s.id === section)?.id ??
    visibleNav[0]?.id ??
    SETTINGS_HOME_SECTION

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
          <div className="flex min-h-0 flex-1">
            <aside
              className={cn(
                RAIL_WIDTH,
                "flex shrink-0 flex-col gap-2 border-r border-sidebar-border bg-sidebar py-3 text-sidebar-foreground",
              )}
            >
              <DialogTitle className="sr-only">
                {t("settings.title")}
              </DialogTitle>
              <div className="flex flex-col gap-2 px-3">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className={cn(
                    chromeTypeClass,
                    "h-7 justify-start gap-1.5 px-2 text-muted-foreground",
                  )}
                  onClick={() => void leave()}
                >
                  <ArrowLeft />
                  {t("settings.back")}
                </Button>
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
                    className={cn(
                      chromeTypeClass,
                      "h-8 border-transparent bg-sidebar-accent pl-8 shadow-none placeholder:text-muted-foreground",
                    )}
                  />
                </div>
              </div>

              <div
                role="tablist"
                aria-orientation="vertical"
                className="flex h-auto w-full flex-col items-stretch gap-0.5 px-2"
              >
                {visibleNav.map((s) => {
                  const on = s.id === page
                  return (
                    <button
                      key={s.id}
                      type="button"
                      role="tab"
                      aria-selected={on}
                      data-state={on ? "active" : "inactive"}
                      className={cn(
                        chromeTypeClass,
                        "inline-flex h-8 w-full items-center justify-start gap-2 rounded-md px-2 shadow-none",
                        on
                          ? "bg-sidebar-accent text-foreground"
                          : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-foreground",
                      )}
                      onClick={() => setSection(s.id)}
                    >
                      <s.icon
                        className="size-4 text-muted-foreground"
                        aria-hidden
                      />
                      {s.label}
                    </button>
                  )
                })}
              </div>

              <DialogDescription
                className={cn(
                  chromeTypeClass,
                  "mt-auto px-3 pb-2 text-muted-foreground",
                )}
              >
                {t("settings.storedIn", {
                  path: meta?.data_dir ?? t("settings.storedFallback"),
                })}
              </DialogDescription>
            </aside>

            <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-background">
              {!settings ? (
                <div className="flex flex-1 items-center justify-center text-muted-foreground">
                  <Loader2 className="size-5 animate-spin" />
                </div>
              ) : visibleNav.length === 0 ? (
                <p className={cn(chromeTypeClass, "px-8 py-12 text-muted-foreground")}>
                  {t("settings.noMatch")}
                </p>
              ) : (
                <div
                  role="tabpanel"
                  data-state="active"
                  className={settingsScrollTab}
                >
                  {page === "general" ? (
                    <GeneralTab
                      theme={theme}
                      onThemeChange={onThemeChange}
                      locale={locale}
                      onLocaleChange={onLocaleChange}
                      appearance={normalizeAppearance(appearance)}
                      onAppearanceChange={onAppearanceChange}
                      meta={meta}
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                  {page === "personality" ? (
                    <PersonalityTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                  {page === "models" ? (
                    <ModelsTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                  {page === "swarm" ? (
                    <SwarmTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                  {page === "tools" ? (
                    <ToolsTab
                      settings={settings}
                      catalog={catalog}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                  {page === "memory" ? (
                    <MemorySettings
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                  {page === "remote" ? (
                    <RemoteTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                  {page === "clients" ? (
                    <ClientsTab
                      settings={settings}
                      onChange={apply}
                      query={query}
                    />
                  ) : null}
                </div>
              )}
            </div>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
