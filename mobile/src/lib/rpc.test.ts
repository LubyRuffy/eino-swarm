import { describe, expect, it } from "vitest"

import {
  decodeResponse,
  encodeRequest,
  OpEvent,
  OpList,
  OpLog,
  OpUnwatch,
  OpWatch,
  PROTOCOL_V,
  slimListBytes,
  type RemoteResponse,
} from "./rpc"

describe("slim rpc", () => {
  it("keeps the default list payload small and free of project secrets", () => {
    const req = encodeRequest({ v: PROTOCOL_V, id: "1", op: OpList })
    expect(JSON.parse(new TextDecoder().decode(req))).toMatchObject({ op: "list" })
    const resp: RemoteResponse = {
      v: 1,
      id: "1",
      ok: true,
      path: "relay",
      session_id: "aa".repeat(16),
      projects: [{ id: "p1", name: "work" }],
      threads: Array.from({ length: 5 }, (_, i) => ({
        id: "t" + i,
        title: "thread " + i,
        running: false,
        last_active_at: "2026-01-01T00:00:00Z",
        summary: "x".repeat(40),
      })),
      running: [],
    }
    const raw = JSON.stringify(resp)
    expect(raw).not.toMatch(/system_prompt/)
    expect(raw).not.toMatch(/token_delta/)
    expect(slimListBytes(resp)).toBeLessThanOrEqual(16 * 1024)
    const decoded = decodeResponse(new TextEncoder().encode(raw))
    expect(decoded.threads).toHaveLength(5)
  })

  it("encodes watch with since and decodes an event push", () => {
    const req = encodeRequest({
      v: PROTOCOL_V,
      id: "w",
      op: OpWatch,
      thread_id: "t1",
      since: 12,
    })
    expect(JSON.parse(new TextDecoder().decode(req))).toMatchObject({
      op: OpWatch,
      thread_id: "t1",
      since: 12,
    })
    const stop = encodeRequest({ v: PROTOCOL_V, id: "u", op: OpUnwatch, thread_id: "t1" })
    expect(JSON.parse(new TextDecoder().decode(stop)).op).toBe(OpUnwatch)
    const decoded = decodeResponse(
      new TextEncoder().encode(
        JSON.stringify({
          v: 1,
          id: "",
          ok: true,
          op: OpEvent,
          thread_id: "t1",
          seq: 3,
          event: {
            thread_id: "t1",
            seq: 3,
            kind: "delta",
            text: "hi",
            created_at: "2026-01-01T00:00:00Z",
          },
        }),
      ),
    )
    expect(decoded.op).toBe(OpEvent)
    expect(decoded.event?.seq).toBe(3)
    const older = encodeRequest({
      v: PROTOCOL_V,
      id: "l",
      op: OpLog,
      thread_id: "t1",
      before: 4,
    })
    expect(JSON.parse(new TextDecoder().decode(older))).toMatchObject({
      op: OpLog,
      before: 4,
    })
  })
})
