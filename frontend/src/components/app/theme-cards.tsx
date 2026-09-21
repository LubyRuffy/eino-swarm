import { SelectItem } from "@/components/ui/select"
import type { PalettePref } from "@/lib/appearance"
import { chromeTypeClass } from "@/lib/chrome-type"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import type { Theme } from "@/store/app"

import {
  SettingsChoice,
  SettingsCopy,
  settingsMatch,
} from "./settings-field"

const THEME_MODES: Theme[] = ["system", "light", "dark"]

function SwatchFace() {
  return (
    <div className="theme-swatch-face">
      <div className="theme-swatch-page">
        <span className="theme-swatch-bar theme-swatch-bar-accent" />
        <span className="theme-swatch-bar theme-swatch-bar-mid" />
        <span className="theme-swatch-bar theme-swatch-bar-short" />
      </div>
    </div>
  )
}

function ThemeSwatch({
  mode,
  palette,
}: {
  mode: Theme
  palette: PalettePref
}) {
  if (mode === "system") {
    return (
      <div className="theme-swatch relative">
        <div className="absolute inset-0" data-swatch={`${palette}-light`}>
          <SwatchFace />
        </div>
        <div
          className="theme-swatch-split absolute inset-0"
          data-swatch={`${palette}-dark`}
        >
          <SwatchFace />
        </div>
      </div>
    )
  }
  return (
    <div className="theme-swatch" data-swatch={`${palette}-${mode}`}>
      <SwatchFace />
    </div>
  )
}

function modeLabel(
  mode: Theme,
  t: ReturnType<typeof useT>,
): { caption: string; name: string } {
  switch (mode) {
    case "light":
      return {
        caption: t("settings.general.themeLight"),
        name: t("settings.general.themeLight"),
      }
    case "dark":
      return {
        caption: t("settings.general.themeDark"),
        name: t("settings.general.themeDark"),
      }
    default:
      return {
        caption: t("settings.general.themeSystemCard"),
        name: t("settings.general.themeSystem"),
      }
  }
}

export function AppearanceTheme({
  theme,
  onThemeChange,
  palette,
  onPaletteChange,
  query = "",
}: {
  theme: Theme
  onThemeChange: (t: Theme) => void
  palette: PalettePref
  onPaletteChange: (p: PalettePref) => void
  query?: string
}) {
  const t = useT()
  const themeVisible = settingsMatch(
    query,
    t("settings.general.theme"),
    t("settings.general.themeHint"),
    t("settings.general.themeSystem"),
    t("settings.general.themeLight"),
    t("settings.general.themeDark"),
    "theme",
    "light",
    "dark",
    "system",
    "外观",
    "主题",
  )

  return (
    <>
      {themeVisible ? (
        <div className="px-4 py-3" data-settings-row="">
          <SettingsCopy
            label={t("settings.general.theme")}
            hint={t("settings.general.themeHint")}
          />
          <div
            role="radiogroup"
            aria-label={t("settings.general.theme")}
            className="mt-3 grid grid-cols-3 gap-3"
          >
            {THEME_MODES.map((mode) => {
              const { caption, name } = modeLabel(mode, t)
              const selected = theme === mode
              return (
                <button
                  key={mode}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  aria-label={name}
                  onClick={() => onThemeChange(mode)}
                  className={cn(
                    "flex flex-col gap-2 rounded-xl text-left outline-none",
                    "focus-visible:ring-2 focus-visible:ring-ring",
                  )}
                >
                  <div
                    className={cn(
                      selected
                        ? "ring-2 ring-foreground ring-offset-2 ring-offset-background"
                        : "ring-1 ring-border",
                      "rounded-[calc(var(--radius)+4px)]",
                    )}
                  >
                    <ThemeSwatch mode={mode} palette={palette} />
                  </div>
                  <span
                    className={cn(
                      chromeTypeClass,
                      "text-center",
                      selected ? "text-foreground" : "text-muted-foreground",
                    )}
                  >
                    {caption}
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      ) : null}

      <SettingsChoice
        query={query}
        search={[
          t("settings.general.palette"),
          "palette",
          "theme",
          "zwai",
          "fofa",
          "配色",
          "主题",
        ]}
        label={t("settings.general.palette")}
        hint={t("settings.general.paletteHint")}
        value={palette}
        onValueChange={(v) => onPaletteChange(v as PalettePref)}
      >
        <SelectItem value="zwai">
          {t("settings.general.paletteZWAI")}
        </SelectItem>
        <SelectItem value="fofa">
          {t("settings.general.paletteFOFA")}
        </SelectItem>
      </SettingsChoice>
    </>
  )
}
