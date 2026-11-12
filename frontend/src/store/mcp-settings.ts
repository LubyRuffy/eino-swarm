import { useCallback, useEffect, useRef, useState } from "react"
import { api } from "@/lib/api"
import { assignMCPServerID, createMCPDraft, MCPDraftError, mcpDraftInput, type MCPDraft, type MCPServerInput, type MCPServerView, type MCPToolCatalog } from "@/lib/mcp-settings"
import { useT } from "@/lib/use-t"
import { errorMessage, toastError } from "./toasts"

export interface MCPConnectionState {
  catalog?: MCPToolCatalog
  error?: string
}

export function useMCPSettings() {
  const t = useT()
  const [servers, setServers] = useState<MCPServerView[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState("")
  const [draft, setDraft] = useState<MCPDraft>()
  const [editing, setEditing] = useState<string>()
  const [pendingDelete, setPendingDelete] = useState<MCPServerView>()
  const [busy, setBusy] = useState("")
  const [formError, setFormError] = useState("")
  const [connections, setConnections] = useState<Record<string, MCPConnectionState>>({})
  const [expandedTools, setExpandedTools] = useState<Record<string, boolean>>({})
  const alive = useRef(true)
  const busyRef = useRef(false)
  const requests = useRef(new Set<AbortController>())

  const load = useCallback(async () => {
    const controller = new AbortController()
    requests.current.add(controller)
    setLoading(true)
    setLoadError("")
    try {
      const loaded = await api.mcpServers(controller.signal)
      if (!controller.signal.aborted) setServers(loaded)
    } catch (error) {
      if (!controller.signal.aborted) {
        setLoadError(errorMessage(error))
        toastError(errorMessage(error), { title: t("mcp.loadFailed") })
      }
    } finally {
      requests.current.delete(controller)
      if (!controller.signal.aborted) setLoading(false)
    }
  }, [t])

  useEffect(() => {
    alive.current = true
    void load()
    return () => {
      alive.current = false
      requests.current.forEach((controller) => controller.abort())
      requests.current.clear()
    }
  }, [load])

  const edit = (server?: MCPServerView) => {
    if (busyRef.current) return
    setEditing(server?.id)
    setDraft(createMCPDraft(server))
    setFormError("")
  }

  const cancel = () => { setDraft(undefined); setFormError("") }

  const save = async () => {
    if (!draft || busyRef.current) return
    let input: MCPServerInput
    try {
      input = mcpDraftInput(draft)
    } catch (error) {
      setFormError(t(error instanceof MCPDraftError && error.field === "args" ? "mcp.invalidArgs" : "mcp.invalidSecrets"))
      return
    }
    if (!input.name.trim() || !(input.transport === "stdio" ? input.command.trim() : input.url.trim())) {
      setFormError(t("mcp.required"))
      return
    }
    if (editing === undefined) {
      try {
        const id = assignMCPServerID(input.id, servers.map((server) => server.id))
        input = { ...input, id }
        if (draft.id !== id) setDraft({ ...draft, id })
      } catch {
        setFormError(t("mcp.saveFailed"))
        return
      }
    }
    busyRef.current = true
    setBusy("save")
    setFormError("")
    try {
      const saved = await api.saveMCPServer(input, editing === undefined)
      if (!alive.current) return
      setServers((current) => [...current.filter((server) => server.id !== saved.id), saved])
      setConnections((current) => {
        const next = { ...current }
        delete next[saved.id]
        return next
      })
      cancel()
    } catch (error) {
      if (alive.current) {
        setFormError(errorMessage(error))
        toastError(errorMessage(error), { title: t("mcp.saveFailed") })
      }
    } finally {
      busyRef.current = false
      if (alive.current) setBusy("")
    }
  }

  const remove = async (id: string) => {
    if (busyRef.current) return
    busyRef.current = true
    setBusy(`delete:${id}`)
    try {
      await api.deleteMCPServer(id)
      if (!alive.current) return
      setServers((current) => current.filter((server) => server.id !== id))
      setConnections((current) => {
        const next = { ...current }
        delete next[id]
        return next
      })
      if (editing === id) cancel()
    } catch (error) {
      if (alive.current) toastError(errorMessage(error), { title: t("mcp.deleteFailed") })
    } finally {
      busyRef.current = false
      if (alive.current) setBusy("")
    }
  }

  const inspect = async (id: string) => {
    if (busyRef.current) return
    busyRef.current = true
    setBusy(`inspect:${id}`)
    const controller = new AbortController()
    requests.current.add(controller)
    setConnections((current) => ({ ...current, [id]: {} }))
    try {
      const catalog = await api.inspectMCPServer(id, controller.signal)
      if (!controller.signal.aborted) setConnections((current) => ({ ...current, [id]: { catalog } }))
    } catch (error) {
      if (!controller.signal.aborted) {
        setConnections((current) => ({ ...current, [id]: { error: errorMessage(error) } }))
        toastError(errorMessage(error), { title: t("mcp.connectionFailed") })
      }
    } finally {
      requests.current.delete(controller)
      busyRef.current = false
      if (alive.current) setBusy("")
    }
  }

  return { servers, loading, loadError, load, draft, setDraft, editing, edit, cancel, save,
    pendingDelete, setPendingDelete, remove, busy, formError, connections, inspect, expandedTools,
    setToolExpanded: (key: string, open: boolean) => setExpandedTools((current) => ({ ...current, [key]: open })) }
}
