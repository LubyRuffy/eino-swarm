import { useCallback, useEffect, useRef, useState } from "react"
import { t } from "./i18n"
import type { RemoteLink } from "./link"
import { OpGetTimeouts, OpSetTimeouts, type TimeoutSettings, type TimeoutSettingsPatch } from "./rpc"

export const MAX_TIMEOUT_SECONDS = 9_223_372_036

type Draft = { agent: string; providers: { id: string; idle: string; first: string }[] }
export function positiveSeconds(text: string): number | null {
  const n = Number(text)
  return /^\d+$/.test(text) && Number.isSafeInteger(n) && n > 0 && n <= MAX_TIMEOUT_SECONDS ? n : null
}
function draftOf(value: TimeoutSettings): Draft {
  return { agent: String(value.agent_timeout_seconds), providers: value.providers.map((p) => ({ id: p.id, idle: String(p.timeout_seconds), first: String(p.first_byte_timeout_seconds) })) }
}
function validSettings(value: TimeoutSettings | undefined): value is TimeoutSettings {
  const positive = (n: number) => Number.isSafeInteger(n) && n > 0 && n <= MAX_TIMEOUT_SECONDS
  return Boolean(value && positive(value.agent_timeout_seconds) && Array.isArray(value.providers) && value.providers.every((p) =>
    typeof p.id === "string" && p.id && typeof p.label === "string" && positive(p.timeout_seconds) && positive(p.first_byte_timeout_seconds) && positive(p.effective_first_byte_timeout_seconds),
  ) && new Set(value.providers.map((p) => p.id)).size === value.providers.length)
}
function changedPatch(value: TimeoutSettings, draft: Draft): TimeoutSettingsPatch {
  const patch: TimeoutSettingsPatch = {}
  const agent = positiveSeconds(draft.agent)!
  if (agent !== value.agent_timeout_seconds) patch.agent_timeout_seconds = agent
  const providers: NonNullable<TimeoutSettingsPatch["providers"]> = []
  for (const row of draft.providers) {
    const old = value.providers.find((p) => p.id === row.id)!
    const next: NonNullable<TimeoutSettingsPatch["providers"]>[number] = { id: row.id }
    if (positiveSeconds(row.idle) !== old.timeout_seconds) next.timeout_seconds = positiveSeconds(row.idle)!
    if (positiveSeconds(row.first) !== old.first_byte_timeout_seconds) next.first_byte_timeout_seconds = positiveSeconds(row.first)!
    if (Object.keys(next).length > 1) providers.push(next)
  }
  if (providers.length) patch.providers = providers
  return patch
}

export function usePCTimeouts(link: RemoteLink | null) {
  const [settings, setSettings] = useState<TimeoutSettings | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const [unsupported, setUnsupported] = useState(false)
  const [saved, setSaved] = useState(false)
  const active = useRef(link)
  const generation = useRef(0)
  const loadedLink = useRef<RemoteLink | null>(null)
  const job = useRef<object | null>(null)
  active.current = link

  const reload = useCallback(async () => {
    const target = active.current
    if (job.current) return
    if (!target?.alive()) { setLoading(false); setError(t("pcTimeouts.disconnected")); return }
    const token = {}; job.current = token
    const gen = ++generation.current
    const current = () => active.current === target && generation.current === gen
    setLoading(true); setError(""); setUnsupported(false); setSaved(false)
    try {
      const r = await target.rpc({ op: OpGetTimeouts }, { dropOnTimeout: false })
      if (!current()) return
      if (!r.ok || !validSettings(r.timeout_settings)) {
        setUnsupported(r.code === "unknown_op")
        setError(t(r.code === "unknown_op" ? "pcTimeouts.unsupported" : "pcTimeouts.loadFailed"))
        return
      }
      loadedLink.current = target
      setSettings(r.timeout_settings); setDraft(draftOf(r.timeout_settings))
    } catch {
      if (current()) setError(t("pcTimeouts.loadFailed"))
    } finally {
      if (job.current === token) job.current = null
      if (current()) setLoading(false)
    }
  }, [])

  useEffect(() => {
    generation.current++; job.current = null; loadedLink.current = null
    setSettings(null); setDraft(null); setSaving(false); setError(""); setUnsupported(false); setSaved(false)
    void reload()
    return () => { generation.current++; job.current = null }
  }, [link, reload])

  const invalid = draft !== null && (positiveSeconds(draft.agent) === null || draft.providers.some((p) => positiveSeconds(p.idle) === null || positiveSeconds(p.first) === null))
  const patch = settings && draft && !invalid ? changedPatch(settings, draft) : null
  const changed = patch !== null && Object.keys(patch).length > 0
  const save = async () => {
    const target = active.current
    if (job.current || !patch || !changed || unsupported || loadedLink.current !== target) return
    if (!target?.alive()) { setError(t("pcTimeouts.disconnected")); return }
    const token = {}; job.current = token
    const gen = ++generation.current
    const current = () => active.current === target && generation.current === gen
    setSaving(true); setError(""); setSaved(false)
    try {
      const r = await target.rpc({ op: OpSetTimeouts, timeout_settings: patch }, { dropOnTimeout: false })
      if (!current()) return
      if (!r.ok || !validSettings(r.timeout_settings)) {
        setUnsupported(r.code === "unknown_op")
        setError(t(r.code === "unknown_op" ? "pcTimeouts.unsupported" : r.code === "bad_request" ? "pcTimeouts.rejected" : r.code === "save_failed" ? "pcTimeouts.saveFailed" : "pcTimeouts.saveUnconfirmed"))
        return
      }
      loadedLink.current = target
      setSettings(r.timeout_settings); setDraft(draftOf(r.timeout_settings)); setSaved(true)
    } catch {
      if (current()) setError(t("pcTimeouts.saveUnconfirmed"))
    } finally {
      if (job.current === token) job.current = null
      if (current()) setSaving(false)
    }
  }
  const editAgent = (agent: string) => {
    if (job.current) return
    setDraft((old) => old ? { ...old, agent } : old); setSaved(false); setError("")
  }
  const editProvider = (id: string, field: "idle" | "first", text: string) => {
    if (job.current) return
    setDraft((old) => old ? { ...old, providers: old.providers.map((p) => p.id === id ? { ...p, [field]: text } : p) } : old)
    setSaved(false); setError("")
  }
  return { settings, draft, loading, saving, error, unsupported, saved, invalid, changed, reload, save, editAgent, editProvider }
}
