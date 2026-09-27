import { OpClientRead, type ClientView, type RemoteResponse } from "@/lib/rpc"

type Reader = {
  alive: () => boolean
  rpc: (req: { op: string; task_id: string; before?: number }) => Promise<RemoteResponse>
}

/** Read-only body of one local agent task. before is the earlier-page cursor.
 *  A dead link returns nothing. */
export async function readClientTask(
  link: Reader | null,
  id: string,
  before?: number,
): Promise<ClientView | null> {
  if (!link?.alive()) return null
  const resp = await link.rpc({
    op: OpClientRead,
    task_id: id,
    before: before && before > 0 ? before : undefined,
  })
  return resp.client_view ?? null
}
