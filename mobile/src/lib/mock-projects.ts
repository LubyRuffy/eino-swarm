import type { ProjectSettings, RemoteRequest, RemoteResponse } from "./rpc"

export class MockProjects {
  private rows: ProjectSettings[] = [
    { id: "p-platform", name: "Platform", system_prompt: "", workdir: "", memory_enabled: true, nuwa_enabled: false },
    { id: "p-notes", name: "Field notes", system_prompt: "", workdir: "", memory_enabled: true, nuwa_enabled: false },
  ]
  list() { return this.rows.map(({ id, name }) => ({ id, name })) }
  handle(req: Omit<RemoteRequest, "v" | "id"> & { id?: string }): RemoteResponse {
    const base = { v: 1, id: req.id ?? "", ok: true, memory_available: true }
    const scenario = new URLSearchParams(location.search).get("projects")
    if (scenario === "legacy") return { ...base, ok: false, code: "unknown_op" }
    const found = this.rows.find((p) => p.id === req.project_id)
    if (req.op === "project_get") return found ? { ...base, project: { ...found } } : { ...base, ok: false, code: "not_found" }
    if (scenario === "save-fail") return { ...base, ok: false, error: "Could not save project" }
    const patch = req.project_patch
    if (!patch || !patch.name?.trim() && req.op === "project_create") return { ...base, ok: false, code: "bad_request" }
    if (req.op === "project_create") {
      const row = { id: `p-${this.rows.length}`, name: "", system_prompt: "", workdir: "", memory_enabled: true, nuwa_enabled: false, ...patch }
      this.rows.push(row)
      return { ...base, projects: [{ id: row.id, name: row.name }] }
    }
    if (!found) return { ...base, ok: false, code: "not_found" }
    Object.assign(found, patch)
    return { ...base, projects: [{ id: found.id, name: found.name }] }
  }
}
