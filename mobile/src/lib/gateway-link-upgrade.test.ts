import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { bytesToB64url } from "./bytes"
import { fingerprint } from "./crypto"
import { clearGatewaySession, saveGatewaySession, type GatewayHost, type GatewaySession } from "./gateway-account"
import { accountHostLink, beginAnonymousPairing, completeAccountHostLink, gatewayUpgradePending, upgradeAnonymousGatewayLinks } from "./gateway-link-upgrade"
import { encodeOffer } from "./offer"
import { loadActiveFingerprint, loadLastThreadId, loadSavedLinks, removeLink, saveActiveFingerprint, saveLastThreadId, saveLink, type SavedLink } from "./store"

vi.mock("@capacitor/browser", () => ({ Browser: { open: vi.fn() } }))
vi.mock("@capacitor/core", () => ({ Capacitor: { getPlatform: () => "ios", isNativePlatform: () => false }, registerPlugin: () => ({}) }))
const origin = "https://gateway.example.test"
const pub = new Uint8Array(32).fill(11)
const hostPub = bytesToB64url(pub)
const session: GatewaySession = { origin, token: "agm_" + "c".repeat(64), user: { id: "u1", username: "user", approval_status: "approved" }, device: { id: "m1", expires_at: "2099-01-01T00:00:00Z" } }
const host: GatewayHost = { id: "pc1", name: "PC", hostname: "pc", version: "1", enabled: true, online: true, owner_user_id: "u1", host_pub: hostPub }
const anonymous: SavedLink = { hubURL: origin, hostPub, fingerprint: fingerprint(pub), ticket: "anonymous", sessionID: "old", label: "My PC" }
const ok = () => new Response(JSON.stringify({ data: { binding_id: "b1", ticket: "d".repeat(64), session_id: "e".repeat(32), host_pub: hostPub } }))
beforeEach(() => { localStorage.clear(); clearGatewaySession(); saveGatewaySession(session); saveLink(anonymous) })
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); clearGatewaySession() })

it("preserves PC order, active choice, label and last thread while migrating inactive saved PCs", async () => {
  const other = { ...anonymous, fingerprint: "other", hostPub: "other", hubURL: "https://other.example.test" }
  saveLastThreadId("thread-pc")
  saveLink(other)
  saveLastThreadId("thread-other")
  vi.stubGlobal("fetch", vi.fn(async () => ok()))
  await upgradeAnonymousGatewayLinks(session, [host], () => true)
  expect(loadSavedLinks().map((row) => row.fingerprint)).toEqual([anonymous.fingerprint, "other"])
  expect(loadSavedLinks()[0]).toMatchObject({ gatewayOwnerID: "u1", label: "My PC" })
  expect(loadActiveFingerprint()).toBe("other")
  expect(loadLastThreadId()).toBe("thread-other")
  saveActiveFingerprint(anonymous.fingerprint)
  expect(loadLastThreadId()).toBe("thread-pc")
})

it.each([
  ["another account", { ...host, owner_user_id: "u2" }, anonymous],
  ["missing owner", { ...host, owner_user_id: undefined }, anonymous],
  ["old Gateway", { ...host, host_pub: undefined }, anonymous],
  ["different full key", { ...host, host_pub: bytesToB64url(new Uint8Array(32).fill(12)) }, anonymous],
  ["offline", { ...host, online: false }, anonymous],
  ["different Hub", host, { ...anonymous, hubURL: "https://other.example.test" }],
  ["different base path", host, { ...anonymous, hubURL: origin + "/tenant" }],
  ["partial managed metadata", host, { ...anonymous, gatewayOrigin: origin }],
] as const)("does not automatically merge %s", async (_name, candidate, saved) => {
  saveLink(saved)
  const fetcher = vi.fn()
  vi.stubGlobal("fetch", fetcher)
  await upgradeAnonymousGatewayLinks(session, [candidate], () => true)
  expect(fetcher).not.toHaveBeenCalled()
  expect(loadSavedLinks()[0].gatewayOwnerID).toBeUndefined()
})

it("rejects ambiguous full public keys and never connects an unsaved account PC", async () => {
  const fetcher = vi.fn()
  vi.stubGlobal("fetch", fetcher)
  await upgradeAnonymousGatewayLinks(session, [host, { ...host, id: "duplicate" }], () => true)
  removeLink(anonymous.fingerprint)
  await upgradeAnonymousGatewayLinks(session, [host], () => true)
  expect(fetcher).not.toHaveBeenCalled()
})

it("shares one server issuance between simultaneous refreshes and explicit Connect", async () => {
  let release!: (response: Response) => void
  const fetcher = vi.fn(() => new Promise<Response>((resolve) => { release = resolve }))
  vi.stubGlobal("fetch", fetcher)
  const first = upgradeAnonymousGatewayLinks(session, [host], () => true)
  const second = upgradeAnonymousGatewayLinks(session, [host], () => true)
  const selected = accountHostLink(session, host)
  await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
  release(ok())
  const saved = await selected
  saveLink(saved)
  completeAccountHostLink(session, host.id, saved)
  await Promise.all([first, second])
  expect(fetcher).toHaveBeenCalledTimes(1)
  expect(loadSavedLinks()).toEqual([expect.objectContaining({ ticket: "d".repeat(64), gatewayOwnerID: "u1" })])
})

it("retains a rotated result after storage failure and retries saving without rotating again", async () => {
  const fetcher = vi.fn(async () => ok())
  vi.stubGlobal("fetch", fetcher)
  const write = localStorage.setItem.bind(localStorage)
  const storage = vi.spyOn(localStorage, "setItem").mockImplementation((key, value) => {
    if (key === "zwai.remote.links") throw new DOMException("quota", "QuotaExceededError")
    write(key, value)
  })
  await expect(upgradeAnonymousGatewayLinks(session, [host], () => true)).rejects.toThrow("quota")
  expect(loadSavedLinks()[0].ticket).toBe("anonymous")
  expect(gatewayUpgradePending(anonymous)).toBe(true)
  storage.mockRestore()
  await upgradeAnonymousGatewayLinks(session, [host], () => true)
  expect(fetcher).toHaveBeenCalledTimes(1)
  expect(loadSavedLinks()[0].ticket).toBe("d".repeat(64))
  expect(gatewayUpgradePending(anonymous)).toBe(false)
})

it("preserves the anonymous record and reports a failed Connect request", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", { status: 503 })))
  await expect(upgradeAnonymousGatewayLinks(session, [host], () => true)).rejects.toThrow()
  expect(loadSavedLinks()).toEqual([anonymous])
  expect(gatewayUpgradePending(anonymous)).toBe(false)
})

it.each(["forget", "rescan", "logout", "new-account", "generation"])("does not revive an anonymous record after %s while Connect is pending", async (action) => {
  let release!: (response: Response) => void
  vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((resolve) => { release = resolve })))
  let active = true
  const migrating = upgradeAnonymousGatewayLinks(session, [host], () => active)
  const result = migrating.catch((error: unknown) => error)
  await vi.waitFor(() => expect(release).toBeTypeOf("function"))
  if (action === "forget") removeLink(anonymous.fingerprint)
  if (action === "rescan") saveLink({ ...anonymous, ticket: "rescanned", sessionID: "rescanned" })
  if (action === "logout") clearGatewaySession()
  if (action === "new-account") saveGatewaySession({ ...session, token: "agm_" + "a".repeat(64), user: { ...session.user, id: "u2" } })
  if (action === "generation") active = false
  release(ok())
  await result
  expect(loadSavedLinks().every((row) => !row.gatewayOwnerID)).toBe(true)
  if (action === "forget") expect(loadSavedLinks()).toEqual([])
  if (action === "rescan") expect(loadSavedLinks()[0].ticket).toBe("rescanned")
})

it("keeps a shared in-flight gate after forgetting the old record", async () => {
  let release!: (response: Response) => void
  const fetcher = vi.fn(() => new Promise<Response>((resolve) => { release = resolve }))
  vi.stubGlobal("fetch", fetcher)
  const migrating = upgradeAnonymousGatewayLinks(session, [host], () => true)
  await vi.waitFor(() => expect(release).toBeTypeOf("function"))
  removeLink(anonymous.fingerprint)
  const manual = accountHostLink(session, host)
  release(ok())
  const saved = await manual
  saveLink(saved)
  completeAccountHostLink(session, host.id, saved)
  await migrating
  expect(fetcher).toHaveBeenCalledTimes(1)
  expect(loadSavedLinks()[0].ticket).toBe(saved.ticket)
})

it("excludes simultaneous QR redemption and account rotation for the same full identity", async () => {
  const uri = encodeOffer({ hubURL: origin, code: "pair", hostPub: pub, lan: [] })
  const release = beginAnonymousPairing(uri)
  const fetcher = vi.fn(async () => ok())
  vi.stubGlobal("fetch", fetcher)
  await expect(upgradeAnonymousGatewayLinks(session, [host], () => true)).rejects.toThrow("pc_pairing_busy")
  expect(fetcher).not.toHaveBeenCalled()
  release()
  const next = await accountHostLink(session, host)
  expect(() => beginAnonymousPairing(uri)).toThrow("pc_upgrade_pending")
  saveLink(next)
  completeAccountHostLink(session, host.id, next)
  beginAnonymousPairing(uri)()
})
