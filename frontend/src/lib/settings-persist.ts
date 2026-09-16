import type { Settings } from "./types"

/** Wait after the last edit before rewriting config.yaml. Toggles still
 *  feel instant; a URL or a number does not write per keystroke. */
export const SETTINGS_SAVE_DEBOUNCE_MS = 400

export function persistPayload(
  settings: Settings,
  locale?: string,
): Partial<Settings> {
  return {
    ...settings,
    ui: { locale: locale || settings.ui?.locale || "system" },
  }
}

/** Debounced PUT of the Settings document. The sheet calls schedule on
 *  every edit and flush when leaving, so Back to app does not drop the
 *  last change. Locale is applied at write time: a language switch while
 *  a URL is still debounce-pending must not restore the old pin. */
export class SettingsPersist {
  private timer: ReturnType<typeof setTimeout> | undefined
  private pendingSettings: Settings | undefined
  private pendingLocale: string | undefined
  private chain: Promise<void> = Promise.resolve()

  constructor(
    private readonly write: (patch: Partial<Settings>) => Promise<unknown>,
    private readonly debounceMs: number = SETTINGS_SAVE_DEBOUNCE_MS,
  ) {}

  schedule(settings: Settings, locale?: string) {
    this.pendingSettings = settings
    if (locale !== undefined) this.pendingLocale = locale
    if (this.timer !== undefined) clearTimeout(this.timer)
    this.timer = setTimeout(() => {
      this.timer = undefined
      void this.flush().catch(() => undefined)
    }, this.debounceMs)
  }

  setLocale(locale: string) {
    this.pendingLocale = locale
  }

  flush(): Promise<void> {
    if (this.timer !== undefined) {
      clearTimeout(this.timer)
      this.timer = undefined
    }
    const settings = this.pendingSettings
    this.pendingSettings = undefined
    if (!settings) return this.chain
    const body = persistPayload(settings, this.pendingLocale)
    this.chain = this.chain.then(
      () => this.write(body).then(() => undefined),
      () => this.write(body).then(() => undefined),
    )
    return this.chain
  }

  dispose() {
    if (this.timer !== undefined) clearTimeout(this.timer)
    this.timer = undefined
    this.pendingSettings = undefined
  }
}
