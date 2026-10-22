import { normalizeUISettings } from "./appearance"
import type { Settings, SettingsPatch, ProviderSettingsWrite } from "./types"

/** Wait after the last edit before rewriting config.yaml. Toggles still
 *  feel instant; a URL or a number does not write per keystroke. */
export const SETTINGS_SAVE_DEBOUNCE_MS = 400

export function persistPayload(
  settings: Settings,
  locale?: string,
  baseline?: Settings,
): SettingsPatch {
  const next = { ...settings, ui: normalizeUISettings(settings.ui, locale) }
  if (!baseline) return next
  const patch: SettingsPatch = {}
  const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b)
  for (const key of [
    "server", "tools", "memory", "personality", "log", "remote", "search", "clients",
  ] as const) {
    if (!same(next[key], baseline[key])) Object.assign(patch, { [key]: next[key] })
  }
  if (!same(next.ui, normalizeUISettings(baseline.ui))) patch.ui = next.ui
  if (!same(next.swarm, baseline.swarm)) {
    const { agent_timeout_seconds, ...rest } = next.swarm
    patch.swarm = { ...rest }
    if (agent_timeout_seconds !== baseline.swarm.agent_timeout_seconds) {
      patch.swarm.agent_timeout_seconds = agent_timeout_seconds
    }
  }
  if (!same(next.models, baseline.models)) {
    patch.models = {
      default: next.models.default,
      providers: next.models.providers.map((provider): ProviderSettingsWrite => {
        const previous = baseline.models.providers.find((p) => p.id === provider.id)
        const row: ProviderSettingsWrite = { ...provider }
        if (previous) {
          if (row.timeout_seconds === previous.timeout_seconds) delete row.timeout_seconds
          if (row.first_byte_timeout_seconds === previous.first_byte_timeout_seconds) {
            delete row.first_byte_timeout_seconds
          }
        }
        return row
      }),
    }
  }
  return patch
}

/** Debounced edits against the last successfully submitted local draft.
 *  The sheet schedules every edit and flushes when leaving, so Back to app
 *  does not drop the
 *  last change. Locale is applied at write time: a language switch while
 *  a URL is still debounce-pending must not restore the old pin. */
export class SettingsPersist {
  private timer: ReturnType<typeof setTimeout> | undefined
  private pendingSettings: Settings | undefined
  private pendingLocale: string | undefined
  private chain: Promise<void> = Promise.resolve()
  private baseline: Settings | undefined

  constructor(
    private readonly write: (patch: SettingsPatch) => Promise<unknown>,
    private readonly debounceMs: number = SETTINGS_SAVE_DEBOUNCE_MS,
    baseline?: Settings,
  ) {
    if (baseline) this.seed(baseline)
  }

  // The dialog waits for queued writes and a fresh GET before seeding on open.
  seed(settings: Settings) {
    this.baseline = structuredClone(settings)
  }

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
    const settings = this.pendingSettings && structuredClone(this.pendingSettings)
    this.pendingSettings = undefined
    if (!settings) return this.chain
    const send = async () => {
      if (!this.baseline) return
      // A queued A→B→A must compare A after B finishes, not when A was queued.
      const submitted = {
        ...settings,
        ui: normalizeUISettings(settings.ui, this.pendingLocale),
      }
      const body = persistPayload(submitted, undefined, this.baseline)
      if (!Object.keys(body).length) return
      await this.write(body)
      // A server reply may include a concurrent phone edit absent from this UI.
      // Compare later edits with what this local draft actually submitted.
      this.baseline = submitted
    }
    this.chain = this.chain.then(send, send)
    return this.chain
  }

  dispose() {
    if (this.timer !== undefined) clearTimeout(this.timer)
    this.timer = undefined
    this.pendingSettings = undefined
  }
}
