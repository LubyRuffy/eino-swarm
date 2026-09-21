import { SelectItem } from "@/components/ui/select"
import {
  appearanceToUI,
  normalizeAppearance,
  type Appearance,
  type CodeFontSizePref,
  type ContentWidthPref,
  type FacePref,
  type FontPref,
  type FontSizePref,
  type FontSizeToken,
  type TranscriptModePref,
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
  SettingsTwinChoice,
  settingsMatch,
} from "./settings-field"
import { AppearanceTheme } from "./theme-cards"

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
    const next = normalizeAppearance({
      font: patch.font ?? appearance.font,
      ui_font_size: patch.uiFontSize ?? appearance.uiFontSize,
      content_font: patch.contentFont ?? appearance.contentFont,
      font_size: patch.fontSize ?? appearance.fontSize,
      code_font: patch.codeFont ?? appearance.codeFont,
      code_font_size: patch.codeFontSize ?? appearance.codeFontSize,
      content_width: patch.contentWidth ?? appearance.contentWidth,
      transcript_mode: patch.transcriptMode ?? appearance.transcriptMode,
      palette: patch.palette ?? appearance.palette,
    })
    onChange({
      ...settings,
      ui: appearanceToUI(next, locale),
    })
  }

  const faceItems = (includeUi: boolean) => (
    <>
      {includeUi ? (
        <SelectItem value="ui">{t("settings.general.fontSameUI")}</SelectItem>
      ) : null}
      <SelectItem value="system">{t("settings.general.fontSystem")}</SelectItem>
      <SelectItem value="serif">{t("settings.general.fontSerif")}</SelectItem>
      <SelectItem value="mono">{t("settings.general.fontMono")}</SelectItem>
    </>
  )

  const sizeItems = (
    <>
      <SelectItem value="small">{t("settings.general.fontSizeSmall")}</SelectItem>
      <SelectItem value="medium">
        {t("settings.general.fontSizeMedium")}
      </SelectItem>
      <SelectItem value="large">{t("settings.general.fontSizeLarge")}</SelectItem>
    </>
  )

  return (
    <SettingsPage
      title={t("settings.general.title")}
      description={t("settings.general.desc")}
    >
      <SettingsSection title={t("settings.general.appearance")}>
        <AppearanceTheme
          query={query}
          theme={theme}
          onThemeChange={onThemeChange}
          palette={appearance.palette}
          onPaletteChange={(palette) => paint({ palette })}
        />

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

        <SettingsTwinChoice
          query={query}
          search={[
            t("settings.general.uiFont"),
            "ui font",
            "chrome",
            "serif",
            "mono",
            "字号",
            "字体",
          ]}
          label={t("settings.general.uiFont")}
          hint={t("settings.general.uiFontHint")}
          left={{
            value: appearance.font,
            onValueChange: (v) => paint({ font: v as FontPref }),
            ariaLabel: t("settings.general.uiFont"),
            children: faceItems(false),
          }}
          right={{
            value: appearance.uiFontSize,
            onValueChange: (v) => paint({ uiFontSize: v as FontSizeToken }),
            ariaLabel: t("settings.general.uiFontSize"),
            children: sizeItems,
          }}
        />

        <SettingsTwinChoice
          query={query}
          search={[
            t("settings.general.contentFont"),
            "content font",
            "conversation",
            "字号",
            "正文",
          ]}
          label={t("settings.general.contentFont")}
          hint={t("settings.general.contentFontHint")}
          left={{
            value: appearance.contentFont,
            onValueChange: (v) => paint({ contentFont: v as FacePref }),
            ariaLabel: t("settings.general.contentFont"),
            children: faceItems(true),
          }}
          right={{
            value: appearance.fontSize,
            onValueChange: (v) => paint({ fontSize: v as FontSizePref }),
            ariaLabel: t("settings.general.contentFontSize"),
            children: (
              <>
                <SelectItem value="ui">
                  {t("settings.general.fontSizeSameUI")}
                </SelectItem>
                {sizeItems}
              </>
            ),
          }}
        />

        <SettingsTwinChoice
          query={query}
          search={[
            t("settings.general.codeFont"),
            "code font",
            "mono",
            "代码",
            "字号",
          ]}
          label={t("settings.general.codeFont")}
          hint={t("settings.general.codeFontHint")}
          left={{
            value: appearance.codeFont,
            onValueChange: (v) => paint({ codeFont: v as FacePref }),
            ariaLabel: t("settings.general.codeFont"),
            children: faceItems(true),
          }}
          right={{
            value: appearance.codeFontSize,
            onValueChange: (v) =>
              paint({ codeFontSize: v as CodeFontSizePref }),
            ariaLabel: t("settings.general.codeFontSize"),
            children: (
              <>
                <SelectItem value="ui">
                  {t("settings.general.fontSizeSameUI")}
                </SelectItem>
                <SelectItem value="content">
                  {t("settings.general.fontSizeSameContent")}
                </SelectItem>
                {sizeItems}
              </>
            ),
          }}
        />

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

        <SettingsChoice
          query={query}
          search={[
            t("settings.general.transcriptMode"),
            "transcript",
            "user",
            "developer",
            "verbose",
            "compact",
            "tools",
            "thinking",
            "用户",
            "开发",
            "精简",
            "工具",
            "思考",
          ]}
          label={t("settings.general.transcriptMode")}
          hint={t("settings.general.transcriptModeHint")}
          value={appearance.transcriptMode}
          onValueChange={(v) =>
            paint({ transcriptMode: v as TranscriptModePref })
          }
        >
          <SelectItem value="user">
            {t("settings.general.transcriptUser")}
          </SelectItem>
          <SelectItem value="developer">
            {t("settings.general.transcriptDeveloper")}
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
