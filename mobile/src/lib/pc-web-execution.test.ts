import { beforeEach, expect, it, vi } from "vitest"
import { pcWebSelection } from "./pc-web-execution"
import { saveGatewaySession, type GatewaySession } from "./gateway-account"
import { savePcWebExecution } from "./web-execution"
import type { RemoteLink } from "./link"
import { OpMeta } from "./rpc"

const session: GatewaySession = { origin: "https://gateway.invalid", token: "agm_" + "c".repeat(64), user: { id: "owner", username: "owner", approval_status: "approved" }, device: { id: "device", expires_at: "2099-01-01T00:00:00Z" } }
beforeEach(() => localStorage.clear())
function link(supported: boolean): RemoteLink {
  return { path: "relay", alive: () => true, close: vi.fn(), announceDevice: vi.fn(), rpc: vi.fn(async () => ({ v: 1, id: "meta", ok: true, web_execution_supported: supported })) }
}

it("keeps PC configuration when the phone signs in without sending a credential", async () => {
  saveGatewaySession(session)
  const pc = link(true)
  expect(await pcWebSelection(pc)).toEqual({ web_execution: "" })
  expect(pc.rpc).toHaveBeenCalledWith({ op: OpMeta }, { dropOnTimeout: false, timeoutMs: 5000 })
  expect(JSON.stringify((pc.rpc as ReturnType<typeof vi.fn>).mock.calls)).not.toContain(session.token)
  savePcWebExecution("local")
  expect(await pcWebSelection(pc)).toEqual({ web_execution: "local" })
  expect(pc.rpc).toHaveBeenCalledTimes(1)
})

it("clears an old per-conversation preference when anonymous automatic follows the PC", async () => {
  expect(await pcWebSelection(link(true))).toEqual({ web_execution: "" })
})

it("preserves legacy anonymous use and rejects ignored explicit cloud/local selection", async () => {
  const pc = link(false)
  expect(await pcWebSelection(pc)).toEqual({})
  saveGatewaySession(session)
  expect(await pcWebSelection(pc)).toEqual({})
  savePcWebExecution("cloud")
  await expect(pcWebSelection(pc)).rejects.toThrow("web_execution_unsupported")
  savePcWebExecution("local")
  await expect(pcWebSelection(pc)).rejects.toThrow("web_execution_unsupported")
})

it("does not swallow a metadata transport failure or dispatch a conversation mutation", async () => {
  const pc = link(false)
  pc.rpc = vi.fn(async () => { throw new Error("offline") })
  await expect(pcWebSelection(pc)).rejects.toThrow("offline")
  expect(pc.rpc).toHaveBeenCalledTimes(1)
})

it("does not swallow an explicit metadata refusal", async () => {
  const pc = link(false)
  pc.rpc = vi.fn(async () => ({ v: 1, id: "metadata", ok: false, code: "restoring" }))
  await expect(pcWebSelection(pc)).rejects.toThrow("restoring")
  expect(pc.rpc).toHaveBeenCalledTimes(1)
})
