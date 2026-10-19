import { afterEach, describe, expect, it, vi } from "vitest"

import { chooseDirectory, nativeDirectoryAvailable } from "./workdir"

type HostWindow = Window & { _wails?: Record<string, unknown> }
const host = window as HostWindow
const send = vi.fn()

function reply(request: { requestId: string }, result: object) {
  window.dispatchEvent(new CustomEvent("zwai:directory-result", {
    detail: { requestId: request.requestId, ...result },
  }))
}

function request() {
  return JSON.parse([...send.mock.calls].reverse().find(([message]) => message.startsWith("{"))![0])
}

afterEach(() => {
  delete host._wails
  send.mockReset()
  vi.restoreAllMocks()
  document.body.innerHTML = ""
})

describe("native working directories", () => {
  it("does not promise a native picker in a browser or from a shell query alone", async () => {
    expect(nativeDirectoryAvailable()).toBe(false)
    await expect(chooseDirectory({ title: "Select", buttonText: "Use", directory: "" })).rejects.toThrow()
  })

  it("chooses a directory through the native bridge and ignores other requests", async () => {
    host._wails = { invoke: send }
    expect(nativeDirectoryAvailable()).toBe(true)
    const choice = chooseDirectory({ title: "Select", buttonText: "Use", directory: "/old" })
    const sent = request()
    expect(sent).toMatchObject({ type: "zwai:directory", action: "choose", title: "Select", directory: "/old", buttonText: "Use" })
    reply({ requestId: "unrelated" }, { path: "/wrong" })
    reply(sent, { path: "/目录 with spaces" })
    await expect(choice).resolves.toBe("/目录 with spaces")
  })

  it("keeps cancellation empty and reports native errors", async () => {
    host._wails = { invoke: send }
    const cancelled = chooseDirectory({ title: "Select", buttonText: "Use", directory: "" })
    reply(request(), { path: "" })
    await expect(cancelled).resolves.toBe("")
    const failed = chooseDirectory({ title: "Select", buttonText: "Use", directory: "" })
    reply(request(), { error: "not a directory", code: "workdir" })
    await expect(failed).rejects.toThrow("not a directory")
  })

  it("rejects a bridge failure without leaving a response listener", async () => {
    host._wails = { invoke: () => { throw new Error("bridge closed") } }
    await expect(chooseDirectory({ title: "Select", buttonText: "Use", directory: "" })).rejects.toThrow("bridge closed")
  })

  it("rejects malformed native responses instead of filling a false path", async () => {
    host._wails = { invoke: send }
    const choice = chooseDirectory({ title: "Select", buttonText: "Use", directory: "" })
    window.dispatchEvent(new CustomEvent("zwai:directory-result", { detail: null }))
    reply(request(), { path: ["/wrong"] })
    await expect(choice).rejects.toThrow("Invalid directory selection response")
  })
})
