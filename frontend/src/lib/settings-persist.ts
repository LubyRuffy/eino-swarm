import { normalizeUISettings } from "./appearance"
import type { Settings, SettingsPatch, ProviderSettingsWrite, RemoteSettings } from "./types"

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
    "server", "tools", "personality", "log", "remote", "search", "clients", "speech",
  ] as const) {
    if (!same(next[key], baseline[key])) Object.assign(patch, { [key]: next[key] })
  }
  if (!same(next.memory, baseline.memory)) {
    const { auto_dream, ...rest } = next.memory
    patch.memory = rest
    if (auto_dream !== undefined && auto_dream !== baseline.memory.auto_dream) {
      patch.memory.auto_dream = auto_dream
    }
  }
  if (!same(next.ui, normalizeUISettings(baseline.ui))) patch.ui = next.ui
  if (!same(next.swarm, baseline.swarm)) {
    const { agent_timeout_seconds, auto_title, composer_predictions, graph, graph_auto_record, selection_translation,
      schedule_answer_check, schedule_answer_model, ...rest } = next.swarm
    patch.swarm = { ...rest }
    if (agent_timeout_seconds !== baseline.swarm.agent_timeout_seconds) {
      patch.swarm.agent_timeout_seconds = agent_timeout_seconds
    }
    if (auto_title !== baseline.swarm.auto_title) patch.swarm.auto_title = auto_title
    if (composer_predictions !== undefined && composer_predictions !== baseline.swarm.composer_predictions) {
      patch.swarm.composer_predictions = composer_predictions
    }
    if (graph_auto_record !== undefined && graph_auto_record !== baseline.swarm.graph_auto_record) {
      patch.swarm.graph_auto_record = graph_auto_record
    }
    if (selection_translation !== undefined && selection_translation !== baseline.swarm.selection_translation) {
      patch.swarm.selection_translation = selection_translation
    }
    if (graph !== undefined && graph !== baseline.swarm.graph) {
      patch.swarm.graph = graph
    }
    if (schedule_answer_check !== undefined && schedule_answer_check !== baseline.swarm.schedule_answer_check) {
      patch.swarm.schedule_answer_check = schedule_answer_check
    }
    if (schedule_answer_model !== undefined && schedule_answer_model !== baseline.swarm.schedule_answer_model) {
      patch.swarm.schedule_answer_model = schedule_answer_model
    }
  }
  if (!same(next.models, baseline.models)) {
    patch.models = {
      default: next.models.default,
      // Sent only when this draft changed it, so a save from a stale sheet
      // cannot undo a choice made elsewhere.
      ...(next.models.default_source !== baseline.models.default_source
        && next.models.default_source !== undefined
        ? { default_source: next.models.default_source } : {}),
      ...(next.models.account_model !== baseline.models.account_model
        ? { account_model: next.models.account_model ?? "" } : {}),
      ...(next.models.account_presentation !== baseline.models.account_presentation
        && next.models.account_presentation !== undefined
        ? { account_presentation: next.models.account_presentation } : {}),
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
      bindings: next.models.bindings,
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

  // An explicit device-login action saves these fields on the server. The
  // dialog drains writes and pauses edits first; unrelated draft fields remain
  // dirty and the confirmed account mode must never be sent back as a PUT.
  acceptRemote(remote: RemoteSettings) {
    if (this.baseline) this.baseline.remote = structuredClone(remote)
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
