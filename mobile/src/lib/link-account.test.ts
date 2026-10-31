import { beforeEach, expect, it, vi } from "vitest"
import { openSaved } from "./client"
import { saveGatewaySession } from "./gateway-account"
import { openLink } from "./link"
import { saveLink, type SavedLink } from "./store"

vi.mock("./client", () => ({ openSaved: vi.fn(async () => ({ alive: () => true })) }))
vi.mock("./mock-link", () => ({ wantsMock: () => false, mockLink: vi.fn() }))

const origin = "https://ai.fofa.info:2440"
const saved: SavedLink = { hubURL: origin, ticket: "private-ticket", hostPub: "public-key", fingerprint: "pc-a", sessionID: "session", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "h1", gatewayMobileID: "m1" }

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
  saveGatewaySession({ origin, token: "agm_" + "a".repeat(64), user: { id: "u1", username: "user", approval_status: "approved" }, device: { id: "m1", expires_at: "2099-01-01T00:00:00Z" } })
  saveLink(saved)
})

it("ordinary reconnects reuse their existing Pairlink ticket and never issue a new binding", async () => {
  const fetcher = vi.fn()
  vi.stubGlobal("fetch", fetcher)
  try {
    for (let attempt = 0; attempt < 3; attempt++) await openLink(saved)
    expect(openSaved).toHaveBeenCalledTimes(3)
    for (const [link] of vi.mocked(openSaved).mock.calls) expect(link).toEqual(saved)
    expect(fetcher).not.toHaveBeenCalled()
    vi.mocked(openSaved).mockRejectedValueOnce(new Error("binding_revoked"))
    await expect(openLink(saved)).rejects.toThrow("binding_revoked")
    expect(fetcher).not.toHaveBeenCalled()
  } finally { vi.unstubAllGlobals() }
})

it("another Mobile session cannot open the saved socket", async () => {
  await expect(openLink({ ...saved, gatewayMobileID: "another-session" })).rejects.toThrow("session_expired")
  expect(openSaved).not.toHaveBeenCalled()
})
