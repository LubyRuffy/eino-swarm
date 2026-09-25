import { useEffect } from "react"

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
