import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { LocalePref } from "@/lib/i18n"
import type { Meta, Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import type { Theme } from "@/store/app"

import {
  SettingsPage,
  SettingsSection,
  settingsMatch,
} from "./settings-field"

export function GeneralTab({
  theme,
  onThemeChange,
  locale,
  onLocaleChange,
  meta,
  settings,
  onChange,
  query = "",
}: {
  theme: Theme
  onThemeChange: (t: Theme) => void
  locale: LocalePref
  onLocaleChange: (l: LocalePref) => void
  meta?: Meta
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  return (
    <SettingsPage
      title={t("settings.general.title")}
      description={t("settings.general.desc")}
    >
      <SettingsSection title={t("settings.general.appearance")}>
        {settingsMatch(
          query,
          t("settings.general.appearance"),
          "theme",
          "light",
          "dark",
          "system",
          "外观",
          "主题",
        ) ? (
          <div
            className="flex items-start justify-between gap-6 px-4 py-3.5"
            data-settings-row=""
          >
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{t("settings.general.appearance")}</p>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {t("settings.general.appearanceHint")}
              </p>
            </div>
            <div className="w-56 shrink-0">
              <Select
                value={theme}
                onValueChange={(v) => onThemeChange(v as Theme)}
              >
                <SelectTrigger
                  className="h-9 text-sm"
                  aria-label={t("settings.general.appearance")}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="system">
                    {t("settings.general.themeSystem")}
                  </SelectItem>
                  <SelectItem value="light">
                    {t("settings.general.themeLight")}
                  </SelectItem>
                  <SelectItem value="dark">
                    {t("settings.general.themeDark")}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        ) : null}

        {settingsMatch(
          query,
          t("settings.general.language"),
          "language",
          "locale",
          "english",
          "中文",
          "语言",
        ) ? (
          <div
            className="flex items-start justify-between gap-6 px-4 py-3.5"
            data-settings-row=""
          >
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{t("settings.general.language")}</p>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {t("settings.general.languageHint")}
              </p>
            </div>
            <div className="w-56 shrink-0">
              <Select
                value={locale}
                onValueChange={(v) => onLocaleChange(v as LocalePref)}
              >
                <SelectTrigger
                  className="h-9 text-sm"
                  aria-label={t("settings.general.language")}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="system">
                    {t("settings.general.langSystem")}
                  </SelectItem>
                  <SelectItem value="en">
                    {t("settings.general.langEn")}
                  </SelectItem>
                  <SelectItem value="zh">
                    {t("settings.general.langZh")}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        ) : null}
      </SettingsSection>

      <SettingsSection title={t("settings.general.logs")}>
        {settingsMatch(query, t("settings.general.logLevel"), "debug", "info", "warn", "error", "日志") ? (
          <div
            className="flex items-start justify-between gap-6 px-4 py-3.5"
            data-settings-row=""
          >
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{t("settings.general.logLevel")}</p>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {t("settings.general.logHint")}
              </p>
            </div>
            <div className="w-56 shrink-0">
              <Select
                value={settings.log.level}
                onValueChange={(level) =>
                  onChange({ ...settings, log: { level } })
                }
              >
                <SelectTrigger
                  className="h-9 text-sm"
                  aria-label={t("settings.general.logLevel")}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {["debug", "info", "warn", "error"].map((l) => (
                    <SelectItem key={l} value={l}>
                      {l}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        ) : null}
      </SettingsSection>

      {settingsMatch(
        query,
        t("settings.general.dataDir"),
        "Data directory",
        "version",
        "版本",
        meta?.data_dir,
        meta?.version,
        meta?.mode,
      ) ? (
        <SettingsSection title={t("settings.general.install")}>
          <div
            className="flex flex-col gap-1 px-4 py-3.5 text-sm"
            data-settings-row=""
          >
            <p>
              {t("settings.general.dataDir")}{" "}
              <code className="font-mono text-xs text-muted-foreground">
                {meta?.data_dir}
              </code>
            </p>
            <p className="text-xs text-muted-foreground">
              {t("settings.general.version", {
                version: meta?.version ?? "",
                mode: meta?.mode ?? "",
              })}
              {meta?.mock ? t("settings.general.mock") : ""}
            </p>
          </div>
        </SettingsSection>
      ) : null}
    </SettingsPage>
  )
}
