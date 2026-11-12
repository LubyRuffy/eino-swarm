import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input, Textarea } from "@/components/ui/input"
import { installAndroidBack } from "@/lib/android-back"
import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { OpCatalog, OpProjectCreate, OpProjectGet, OpProjectUpdate, type ModelChoice, type ProjectPatch, type ProjectSettings, type ProjectView } from "@/lib/rpc"

const blank: ProjectSettings = { id: "", name: "", system_prompt: "", workdir: "", memory_enabled: false, nuwa_enabled: false }
const modelKey = (provider?: string, model?: string) => provider && model ? JSON.stringify([provider, model]) : ""

// A mounted editor belongs to exactly one link. Its parent keys it by PC/link,
// and unmounting invalidates pending reads/writes before another PC can render.
export function ProjectManager({ link, projects, onSaved, onClose }: {
  link: RemoteLink
  projects: ProjectView[]
  onSaved: (project: ProjectView) => void
  onClose: () => void
}) {
  const [rows, setRows] = useState(projects)
  const [draft, setDraft] = useState<ProjectSettings>()
  const [original, setOriginal] = useState<ProjectSettings>()
  const [selected, setSelected] = useState<string>()
  const [models, setModels] = useState<ModelChoice[]>([])
  const [memory, setMemory] = useState(false)
  const [catalogReady, setCatalogReady] = useState(false)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const [saved, setSaved] = useState(false)
  const [unsupported, setUnsupported] = useState(false)
  const generation = useRef(0)
  const mounted = useRef(true)
  const close = useRef(onClose)
  close.current = onClose
  const dialog = useRef<HTMLElement>(null)

  useEffect(() => {
    mounted.current = true
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    dialog.current?.querySelector<HTMLButtonElement>("button")?.focus()
    const removeBack = installAndroidBack(() => { close.current(); return true })
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.stopPropagation(); close.current() }
      if (event.key !== "Tab") return
      const controls = Array.from(dialog.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled)') ?? [])
      const first = controls[0], last = controls.at(-1)
      if (!dialog.current?.contains(document.activeElement)) { event.preventDefault(); (event.shiftKey ? last : first)?.focus() }
      else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }
    window.addEventListener("keydown", key)
    return () => { mounted.current = false; generation.current++; removeBack(); window.removeEventListener("keydown", key); if (previous?.isConnected) previous.focus() }
  }, [link])

  async function edit(id: string) {
    const gen = ++generation.current
    setSelected(id); setDraft(undefined); setOriginal(undefined); setError(""); setSaved(false); setLoading(true); setCatalogReady(false); setUnsupported(false)
    try {
      const [catalog, response] = await Promise.all([
        link.rpc({ op: OpCatalog }, { dropOnTimeout: false }),
        id ? link.rpc({ op: OpProjectGet, project_id: id }, { dropOnTimeout: false }) : Promise.resolve(undefined),
      ])
      if (!mounted.current || generation.current !== gen) return
      if (response && (!response.ok || !response.project)) {
        setUnsupported(response.code === "unknown_op")
        setError(response.code === "unknown_op" ? t("projects.unsupported") : response.error || t("projects.failed")); return
      }
      if (!catalog.ok || typeof catalog.memory_available !== "boolean") {
        setUnsupported(catalog.ok)
        setError(t(catalog.ok ? "projects.unsupported" : "projects.catalogFailed")); return
      }
      setModels(catalog.models ?? []); setMemory(catalog.memory_available); setCatalogReady(true)
      const next = response?.project ?? { ...blank, memory_enabled: catalog.memory_available }
      setDraft({ ...next }); setOriginal({ ...next })
    } catch { if (mounted.current && generation.current === gen) setError(t("projects.failed")) }
    finally { if (mounted.current && generation.current === gen) setLoading(false) }
  }

  function change(patch: ProjectPatch) { setDraft((value) => value ? { ...value, ...patch } : value); setError("") }
  async function save() {
    if (!draft || !original || saving) return
    const gen = generation.current
    const patch: ProjectPatch = {}
    for (const key of ["name", "system_prompt", "workdir", "memory_enabled", "nuwa_enabled"] as const) {
      if (!draft.id || draft[key] !== original[key]) Object.assign(patch, { [key]: key === "name" ? draft.name.trim() : draft[key] })
    }
    const fields = ["nuwa_planner_provider_id", "nuwa_planner_model", "nuwa_executor_provider_id", "nuwa_executor_model"] as const
    if (fields.every((key) => draft[key]) && fields.some((key) => draft[key] !== original[key])) {
      for (const key of fields) patch[key] = draft[key]
    }
    if (!Object.keys(patch).length) return
    setSaving(true); setError("")
    try {
      const r = await link.rpc({ op: draft.id ? OpProjectUpdate : OpProjectCreate, ...(draft.id ? { project_id: draft.id } : {}), project_patch: patch }, { dropOnTimeout: false })
      if (!mounted.current || generation.current !== gen) return
      if (!r.ok || !r.projects?.[0]) {
        setUnsupported(r.code === "unknown_op")
        setError(r.code === "unknown_op" ? t("projects.unsupported") : r.error || t("projects.failed")); return
      }
      const row = r.projects[0]
      setRows((prev) => prev.some((p) => p.id === row.id) ? prev.map((p) => p.id === row.id ? row : p) : [...prev, row])
      onSaved(row); setSelected(undefined); setDraft(undefined); setSaved(true)
    } catch { if (mounted.current && generation.current === gen) setError(t("projects.failed")) }
    finally { if (mounted.current && generation.current === gen) setSaving(false) }
  }
  const busy = loading || saving
  const pickModel = (role: "planner" | "executor", value: string) => {
    if (!value) return
    const [provider, model] = JSON.parse(value) as [string, string]
    change({ [`nuwa_${role}_provider_id`]: provider, [`nuwa_${role}_model`]: model })
  }
  const toggleNuwa = (enabled: boolean) => {
    const patch: ProjectPatch = { nuwa_enabled: enabled }
    if (enabled && !draft?.nuwa_planner_provider_id && models.length) {
      const planner = models.find((m) => m.default) ?? models[0]
      const executor = models.find((m) => modelKey(m.provider_id, m.model) !== modelKey(planner.provider_id, planner.model)) ?? planner
      Object.assign(patch, { nuwa_planner_provider_id: planner.provider_id, nuwa_planner_model: planner.model, nuwa_executor_provider_id: executor.provider_id, nuwa_executor_model: executor.model })
    }
    change(patch)
  }
  const dirty = draft && original && JSON.stringify(draft) !== JSON.stringify(original)
  return createPortal(<div className="fixed inset-0 z-50 flex flex-col justify-end">
    <div className="absolute inset-0 bg-foreground/40" onClick={onClose} />
    <section ref={dialog} role="dialog" aria-modal="true" aria-labelledby="project-manager-title" className="relative flex max-h-[90%] min-w-0 flex-col rounded-t-2xl border border-border bg-background pb-[env(safe-area-inset-bottom)]">
      <header className="flex shrink-0 items-center justify-between px-4 py-2">
        <h2 id="project-manager-title" className="text-lg font-semibold">{t("projects.manage")}</h2>
        <Button type="button" variant="ghost" aria-label={t("home.close")} onClick={onClose}><X className="size-5" aria-hidden /></Button>
      </header>
      <div className="flex min-h-0 flex-col gap-4 overflow-y-auto px-4 pb-5">
        {selected === undefined ? <>
          <Button type="button" onClick={() => void edit("")}>{t("projects.new")}</Button>
          {rows.length ? rows.map((p) => <Button key={p.id} type="button" variant="outline" className="shrink-0 justify-start truncate" onClick={() => void edit(p.id)}>{p.name}</Button>) : <p>{t("projects.empty")}</p>}
          {saved ? <p role="status">{t("projects.saved")}</p> : null}
        </> : <>
          <Button type="button" variant="ghost" disabled={saving} onClick={() => { generation.current++; setSelected(undefined); setDraft(undefined); setError(""); setLoading(false) }}>{t("projects.back")}</Button>
          {loading ? <p role="status">{t("projects.loading")}</p> : null}
          {draft ? <form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); void save() }}>
            <fieldset disabled={busy || unsupported} className="flex min-w-0 flex-col gap-4">
              <label className="flex flex-col gap-1 text-sm">{t("projects.name")}<Input value={draft.name} onChange={(e) => change({ name: e.target.value })} /></label>
              <label className="flex flex-col gap-1 text-sm">{t("projects.instruction")}<Textarea rows={5} value={draft.system_prompt} onChange={(e) => change({ system_prompt: e.target.value })} /></label>
              <label className="flex flex-col gap-1 text-sm">{t("projects.workdir")}<Input autoCapitalize="none" autoCorrect="off" spellCheck={false} value={draft.workdir} onChange={(e) => change({ workdir: e.target.value })} /></label>
              <p className="break-all text-xs text-muted-foreground">{t("projects.workdirHint")}{draft.resolved_workdir ? ` (${draft.resolved_workdir})` : ""}</p>
              <label className="flex items-center justify-between gap-3 text-sm">{t("projects.memory")}<input type="checkbox" checked={draft.memory_enabled && memory} disabled={!memory} onChange={(e) => change({ memory_enabled: e.target.checked })} /></label>
              {!memory ? <p className="text-xs text-muted-foreground">{t("projects.memoryOff")}</p> : null}
              <label className="flex items-center justify-between gap-3 text-sm">{t("projects.nuwa")}<input type="checkbox" checked={draft.nuwa_enabled} onChange={(e) => toggleNuwa(e.target.checked)} /></label>
              {draft.nuwa_enabled ? <>
                {(["planner", "executor"] as const).map((role) => {
                  const value = modelKey(draft[`nuwa_${role}_provider_id`], draft[`nuwa_${role}_model`])
                  return <label key={role} className="flex min-w-0 flex-col gap-1 text-sm">{t(role === "planner" ? "projects.planner" : "projects.executor")}
                    <select className="h-10 w-full min-w-0 rounded-md border border-input bg-background px-2 text-base" value={value} onChange={(e) => pickModel(role, e.target.value)}>
                      <option value="" disabled>{t("projects.defaultModels")}</option>
                      {value && !models.some((m) => modelKey(m.provider_id, m.model) === value) ? <option value={value}>{draft[`nuwa_${role}_model`]} · {t("projects.unavailable")}</option> : null}
                      {models.map((m) => <option key={modelKey(m.provider_id, m.model)} value={modelKey(m.provider_id, m.model)}>{m.provider_label || m.provider_id} · {m.label || m.model}</option>)}
                    </select>
                  </label>
                })}
                <p className="text-xs text-muted-foreground">{t("projects.modelsHint")}</p>
              </> : null}
            </fieldset>
            <Button type="submit" disabled={busy || unsupported || !catalogReady || !draft.name.trim() || Boolean(draft.id && !dirty)}>{t(saving ? "projects.saving" : draft.id ? "projects.save" : "projects.create")}</Button>
          </form> : null}
          {error ? <p role="alert" className="break-words text-sm text-destructive">{error}</p> : null}
          {!draft && error && !unsupported ? <Button type="button" disabled={busy} onClick={() => void edit(selected)}>{t("scan.retry")}</Button> : null}
        </>}
      </div>
    </section>
  </div>, document.body)
}
