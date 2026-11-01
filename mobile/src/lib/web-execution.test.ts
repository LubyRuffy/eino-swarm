import { beforeEach, expect, it, vi } from "vitest"
import { saveGatewaySession, type GatewaySession } from "./gateway-account"
import { loadPcWebExecution, loadWebExecution, pcWebExecution, savePcWebExecution, saveWebExecution, watchWebAuthority } from "./web-execution"

const session: GatewaySession = { origin: "https://account.invalid", token: "agm_" + "b".repeat(64), user: { id: "owner", username: "owner", approval_status: "approved" }, device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" } }
beforeEach(() => localStorage.clear())

it("never lets phone direct execution override PC settings implicitly", () => {
  for (const account of [null, session]) {
    localStorage.clear()
    if (account) saveGatewaySession(account)
    for (const mode of ["auto", "cloud", "off"] as const) {
      saveWebExecution(mode)
      expect(loadWebExecution()).toBe(mode)
      expect(loadPcWebExecution()).toBe("inherit")
      expect(pcWebExecution()).toBe("")
    }
    savePcWebExecution("local")
    expect(loadWebExecution()).toBe("off")
    expect(pcWebExecution()).toBe("local")
    saveWebExecution("cloud")
    expect(pcWebExecution()).toBe("local")
    savePcWebExecution("inherit")
    expect(pcWebExecution()).toBe("")
  }
})

it.each([null, "auto", "local", "cloud"])('migrates the old shared preference %s independently', (old) => {
  saveGatewaySession(session)
  if (old) localStorage.setItem("zwai.phone.webExecution:" + session.origin + "\x00" + session.user.id, old)
  expect(loadWebExecution()).toBe(old === "local" ? "off" : old === "cloud" ? "cloud" : "auto")
  expect(loadPcWebExecution()).toBe(old === "local" ? "local" : old === "cloud" ? "cloud" : "inherit")
  saveWebExecution("auto")
  expect(loadPcWebExecution()).toBe(old === "local" ? "local" : old === "cloud" ? "cloud" : "inherit")
  savePcWebExecution("inherit")
  expect(loadWebExecution()).toBe("auto")
})

it("isolates both choices by normalized server origin and user, rather than model name or device token", () => {
  saveGatewaySession(session)
  saveWebExecution("off"); savePcWebExecution("local")
  saveGatewaySession({ ...session, token: "agm_" + "c".repeat(64), device: { ...session.device, id: "new-device" } })
  expect(loadWebExecution()).toBe("off"); expect(loadPcWebExecution()).toBe("local")
  for (const other of [{ ...session, origin: "https://other.invalid" }, { ...session, user: { ...session.user, id: "other" } }]) {
    saveGatewaySession(other)
    expect(loadWebExecution()).toBe("auto"); expect(loadPcWebExecution()).toBe("inherit")
  }
  expect(loadWebExecution(null)).toBe("auto"); expect(loadPcWebExecution(null)).toBe("inherit")
})

it("reports successful writes only, and cleans up preference event listeners", () => {
  const listener = vi.fn(), unwatch = watchWebAuthority(listener)
  saveWebExecution("off"); savePcWebExecution("local")
  expect(listener).toHaveBeenCalledTimes(2)
  unwatch(); savePcWebExecution("inherit")
  expect(listener).toHaveBeenCalledTimes(2)
  const storage = vi.spyOn(localStorage, "setItem").mockImplementation(() => { throw new Error("storage full") })
  expect(() => saveWebExecution("cloud")).toThrow("storage full")
  expect(() => savePcWebExecution("cloud")).toThrow("storage full")
  storage.mockRestore()
  expect(loadWebExecution()).toBe("off")
  expect(loadPcWebExecution()).toBe("inherit")
})
