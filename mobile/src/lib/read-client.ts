import { OpClientRead, type ClientView, type RemoteResponse } from "@/lib/rpc"

type Reader = {
  alive: () => boolean
  rpc: (req: { op: string; task_id: string }) => Promise<RemoteResponse>
}

/** Read-only body of one local agent task. A dead link returns nothing. */
export async function readClientTask(link: Reader | null, id: string): Promise<ClientView | null> {
  if (!link?.alive()) return null
  const resp = await link.rpc({ op: OpClientRead, task_id: id })
  return resp.client_view ?? null
}
