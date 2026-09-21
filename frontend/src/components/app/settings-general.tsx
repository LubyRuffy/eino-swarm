import { SelectItem } from "@/components/ui/select"
import {
  normalizeUISettings,
  type Appearance,
  type ContentWidthPref,
  type FontPref,
  type FontSizePref,
} from "@/lib/appearance"
import type { LocalePref } from "@/lib/i18n"
import type { Meta, Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import type { Theme } from "@/store/app"

import {
  SettingsChoice,
  SettingsPage,
  SettingsRow,
  SettingsSection,
  settingsMatch,
} from "./settings-field"

export function GeneralTab({
  theme,
  onThemeChange,
  locale,
  onLocaleChange,
  appearance,
  onAppearanceChange,
  meta,
  settings,
  onChange,
  query = "",
}: {
  theme: Theme
  onThemeChange: (t: Theme) => void
  locale: LocalePref
  onLocaleChange: (l: LocalePref) => void
  appearance: Appearance
  onAppearanceChange: (patch: Partial<Appearance>) => void
  meta?: Meta
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()

  const paint = (patch: Partial<Appearance>) => {
    onAppearanceChange(patch)
    const next = {
      font: patch.font ?? appearance.font,
      fontSize: patch.fontSize ?? appearance.fontSize,
      contentWidth: patch.contentWidth ?? appearance.contentWidth,
    }
    onChange({
      ...settings,
      ui: normalizeUISettings({
        locale,
        font: next.font,
        font_size: next.fontSize,
        content_width: next.contentWidth,
      }),
    })
  }

  return (
    <SettingsPage
      title={t("settings.general.title")}
      description={t("settings.general.desc")}
    >
      <SettingsSection title={t("settings.general.appearance")}>
        <SettingsChoice
          query={query}
          search={[
            t("settings.general.appearance"),
            "theme",
            "light",
            "dark",
            "system",
            "外观",
            "主题",
          ]}
          label={t("settings.general.appearance")}
          hint={t("settings.general.appearanceHint")}
          value={theme}
          onValueChange={(v) => onThemeChange(v as Theme)}
        >
          <SelectItem value="system">
            {t("settings.general.themeSystem")}
          </SelectItem>
          <SelectItem value="light">
            {t("settings.general.themeLight")}
          </SelectItem>
          <SelectItem value="dark">
            {t("settings.general.themeDark")}
          </SelectItem>
        </SettingsChoice>

        <SettingsChoice
          query={query}
          search={[
            t("settings.general.language"),
            "language",
            "locale",
            "english",
            "中文",
            "语言",
          ]}
          label={t("settings.general.language")}
          hint={t("settings.general.languageHint")}
          value={locale}
          onValueChange={(v) => onLocaleChange(v as LocalePref)}
        >
          <SelectItem value="system">
            {t("settings.general.langSystem")}
          </SelectItem>
          <SelectItem value="en">{t("settings.general.langEn")}</SelectItem>
          <SelectItem value="zh">{t("settings.general.langZh")}</SelectItem>
        </SettingsChoice>

        <SettingsChoice
          query={query}
          search={[
            t("settings.general.font"),
            "font",
            "typeface",
            "serif",
            "mono",
            "字体",
          ]}
          label={t("settings.general.font")}
          hint={t("settings.general.fontHint")}
          value={appearance.font}
          onValueChange={(v) => paint({ font: v as FontPref })}
        >
          <SelectItem value="system">
            {t("settings.general.fontSystem")}
          </SelectItem>
          <SelectItem value="serif">
            {t("settings.general.fontSerif")}
          </SelectItem>
          <SelectItem value="mono">{t("settings.general.fontMono")}</SelectItem>
        </SettingsChoice>

        <SettingsChoice
          query={query}
          search={[
            t("settings.general.fontSize"),
            "font size",
            "small",
            "large",
            "字号",
          ]}
          label={t("settings.general.fontSize")}
          hint={t("settings.general.fontSizeHint")}
          value={appearance.fontSize}
          onValueChange={(v) => paint({ fontSize: v as FontSizePref })}
        >
          <SelectItem value="small">
            {t("settings.general.fontSizeSmall")}
          </SelectItem>
          <SelectItem value="medium">
            {t("settings.general.fontSizeMedium")}
          </SelectItem>
          <SelectItem value="large">
            {t("settings.general.fontSizeLarge")}
          </SelectItem>
        </SettingsChoice>

        <SettingsChoice
          query={query}
          search={[
            t("settings.general.contentWidth"),
            "width",
            "full",
            "wide",
            "standard",
            "comfortable",
            "column",
            "铺满",
            "宽屏",
            "标准",
            "宽度",
          ]}
          label={t("settings.general.contentWidth")}
          hint={t("settings.general.contentWidthHint")}
          value={appearance.contentWidth}
          onValueChange={(v) =>
            paint({ contentWidth: v as ContentWidthPref })
          }
        >
          <SelectItem value="comfortable">
            {t("settings.general.contentWidthComfortable")}
          </SelectItem>
          <SelectItem value="full">
            {t("settings.general.contentWidthFull")}
          </SelectItem>
        </SettingsChoice>
      </SettingsSection>

      <SettingsSection title={t("settings.general.logs")}>
        <SettingsChoice
          query={query}
          search={[
            t("settings.general.logLevel"),
            "debug",
            "info",
            "warn",
            "error",
            "日志",
          ]}
          label={t("settings.general.logLevel")}
          hint={t("settings.general.logHint")}
          value={settings.log.level}
          onValueChange={(level) => onChange({ ...settings, log: { level } })}
        >
          {["debug", "info", "warn", "error"].map((l) => (
            <SelectItem key={l} value={l}>
              {l}
            </SelectItem>
          ))}
        </SettingsChoice>
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
          <SettingsRow
            label={t("settings.general.dataDir")}
            hint={
              t("settings.general.version", {
                version: meta?.version ?? "",
                mode: meta?.mode ?? "",
              }) + (meta?.mock ? t("settings.general.mock") : "")
            }
          >
            <code
              className="max-w-[16rem] truncate font-mono text-xs text-muted-foreground"
              title={meta?.data_dir}
            >
              {meta?.data_dir}
            </code>
          </SettingsRow>
        </SettingsSection>
      ) : null}
    </SettingsPage>
  )
}
