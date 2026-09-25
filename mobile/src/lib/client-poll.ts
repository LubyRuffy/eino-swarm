import { useEffect, useRef, useState } from "react"

import { linkError, remoteError } from "@/lib/client"
import type { RemoteLink } from "@/lib/link"
import { mergeClientTools } from "@/lib/local-clients"
import { OpClients, type ClientTool } from "@/lib/rpc"

/** Local agent groups are not the inbox. This poll reads the snapshot
 *  beside the link, and a slow reply must not close the PC socket. */
export function useClientPoll(
  link: RemoteLink | null,
  current: { current: RemoteLink | null },
  setEnabled: (on: boolean) => void,
  setTools: (fn: (prev: ClientTool[]) => ClientTool[]) => void,
) {
  useEffect(() => {
    if (!link?.alive()) return
    let stop = false
    const tick = () => {
      if (stop || !link.alive()) return
      void link
        .rpc({ op: OpClients }, { dropOnTimeout: false })
        .then((resp) => {
          const clients = resp.clients
          if (stop || current.current !== link || !resp.ok || !clients) return
          setEnabled(clients.enabled)
          const tools = clients.tools
          if (!clients.pending && tools) setTools((prev) => mergeClientTools(prev, tools, "replace"))
        })
        .catch(() => undefined)
    }
    tick()
    const id = window.setInterval(tick, 2000)
    return () => {
      stop = true
      window.clearInterval(id)
    }
  }, [link, current, setEnabled, setTools])
}

/** A Clients page owns its progress separately from inbox pagination. */
export function useClientPages(
  current: { current: RemoteLink | null },
  setTools: (fn: (prev: ClientTool[]) => ClientTool[]) => void,
  setError: (message: string) => void,
) {
  const [loadingMore, setLoadingMore] = useState("")
  const busy = useRef(false)
  const generation = useRef(0)
  const reset = () => {
    generation.current++
    busy.current = false
    setLoadingMore("")
  }
  const loadMore = async (id: string, next?: string) => {
    const target = current.current
    if (!target?.alive() || !next || busy.current) return
    busy.current = true
    const request = ++generation.current
    setLoadingMore(id)
    try {
      const resp = await target.rpc({ op: OpClients, group: id, before: Number(next) }, { dropOnTimeout: false })
      if (current.current !== target || generation.current !== request) return
      if (!resp.ok) {
        setError(remoteError(resp.error || resp.code || ""))
        return
      }
      const tools = resp.clients?.tools
      if (tools) setTools((prev) => mergeClientTools(prev, tools, "append"))
    } catch (e) {
      if (current.current === target && generation.current === request) setError(linkError(e))
    } finally {
      if (current.current === target && generation.current === request) reset()
    }
  }
  return { loadingMore, loadMore, reset }
}
