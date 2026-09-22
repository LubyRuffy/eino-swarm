import type { RemoteRequest, RemoteResponse } from "./rpc"
import { putFrames, putName } from "./put-chunks"

type RPC = (req: Omit<RemoteRequest, "v" | "id">) => Promise<RemoteResponse>

let putSeq = 0

export function newPutID(): string {
  putSeq += 1
  return ("p" + putSeq.toString(36) + Date.now().toString(36)).replace(/[^A-Za-z0-9_-]/g, "").slice(0, 64)
}

/** Uploads each file as put frames and returns the ids start/send should name.
 *  An unfinished id is not returned: the caller keeps the draft. */
export async function stageFiles(rpc: RPC, files: File[]): Promise<string[]> {
  const ids: string[] = []
  for (const file of files) {
    const name = putName(file.name, file.type)
    const bytes = new Uint8Array(await file.arrayBuffer())
    const frames = putFrames(newPutID(), name, file.type, bytes)
    if (frames.length === 0) throw new Error("empty")
    const id = frames[0].put_id || ""
    for (const frame of frames) {
      const resp = await rpc(frame)
      if (!resp.ok) throw new Error(resp.error || resp.code || "put")
    }
    ids.push(id)
  }
  return ids
}
