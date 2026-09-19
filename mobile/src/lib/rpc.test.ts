import { describe, expect, it } from "vitest"

import {
  decodeResponse,
  encodeRequest,
  OpList,
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
})
