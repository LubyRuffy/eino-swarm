import type { ComposerExtra } from "@/components/composer"

import type { RemoteRequest, RemoteResponse } from "./rpc"
import { stageFiles } from "./stage-files"

type RPC = (req: Omit<RemoteRequest, "v" | "id">) => Promise<RemoteResponse>

/** Upload whatever the box is holding, then hand the turn to the PC.
 *  The PC decides whether that is a follow-up, a steer, or a new turn. */
export async function sendComposed(
  rpc: RPC,
  req: Omit<RemoteRequest, "v" | "id">,
  extra?: ComposerExtra,
): Promise<RemoteResponse> {
  const files = extra ? [...extra.images, ...extra.files] : []
  const puts = files.length ? await stageFiles(rpc, files) : []
  return rpc({
    ...req,
    puts: puts.length ? puts : undefined,
    provider_id: extra?.providerId || undefined,
    model: extra?.model || undefined,
    reasoning: extra ? extra.reasoning : undefined,
  })
}
