import { useCallback, useEffect, useRef, useState } from "react"
import { t } from "./i18n"
import type { RemoteLink } from "./link"
import { OpCatalog, OpRefreshCatalog, type ModelChoice } from "./rpc"

// Discovery owns a 15s host budget; allow the response to cross the relay
// without letting this optional operation drop an active conversation.
const refreshTimeoutMs = 30_000

export function useModelCatalog(link: RemoteLink | null) {
  const [models, setModels] = useState<ModelChoice[]>([])
  const [levels, setLevels] = useState<string[]>([])
  const [catalogBusy, setCatalogBusy] = useState(false)
  const [refreshBusy, setRefreshBusy] = useState(false)
  const [refreshError, setRefreshError] = useState("")
  const active = useRef(link)
  const generation = useRef(0)
  const inFlight = useRef<object | null>(null)
  active.current = link

  useEffect(() => {
    const gen = ++generation.current
    inFlight.current = null
    setModels([])
    setLevels([])
    setRefreshError("")
    setRefreshBusy(false)
    setCatalogBusy(Boolean(link?.alive()))
    if (link?.alive()) {
      const current = () => active.current === link && generation.current === gen
      void link.rpc({ op: OpCatalog }).then((r) => {
        if (!current() || !r.ok) return
        setModels(r.models ?? [])
        setLevels(r.reasoning_levels ?? [])
      }).catch(() => undefined).finally(() => {
        if (current()) setCatalogBusy(false)
      })
    }
    return () => { generation.current += 1 }
  }, [link])

  const refreshModels = useCallback(async () => {
    const target = active.current
    if (!target?.alive() || inFlight.current) return
    const job = {}
    inFlight.current = job
    const gen = ++generation.current
    const current = () => active.current === target && generation.current === gen
    setCatalogBusy(false)
    setRefreshBusy(true)
    setRefreshError("")
    try {
      const r = await target.rpc({ op: OpRefreshCatalog }, {
        dropOnTimeout: false, timeoutMs: refreshTimeoutMs,
      })
      if (!current()) return
      if (!r.ok) {
        setRefreshError(t(r.code === "unknown_op" ? "composer.refreshUnsupported" : "composer.refreshFailed"))
        return
      }
      setModels(r.models ?? [])
      setLevels(r.reasoning_levels ?? [])
    } catch {
      if (current()) setRefreshError(t("composer.refreshFailed"))
    } finally {
      if (inFlight.current === job) inFlight.current = null
      if (current()) setRefreshBusy(false)
    }
  }, [])

  return { models, levels, catalogBusy, refreshBusy, refreshError, refreshModels }
}
