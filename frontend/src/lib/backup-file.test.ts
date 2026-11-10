import { afterEach, describe, expect, it, vi } from "vitest"

import { chooseBackupFile, nativeBackupFileAvailable } from "./backup-file"

type HostWindow = Window & { _wails?: Record<string, unknown> }
const host = window as HostWindow
const send = vi.fn()

function reply(request: { requestId: string }, result: object) {
  window.dispatchEvent(new CustomEvent("zwai:backup-file-result", {
    detail: { requestId: request.requestId, ...result },
  }))
}

function request() {
  return JSON.parse([...send.mock.calls].reverse().find(([message]) => String(message).startsWith("{"))![0])
}

afterEach(() => {
  delete host._wails
  send.mockReset()
})

describe("backup file bridge", () => {
  it("does not promise a native picker in the browser", async () => {
    expect(nativeBackupFileAvailable()).toBe(false)
    await expect(chooseBackupFile("open", { title: "Open", buttonText: "Open" })).rejects.toThrow()
  })

  it("saves through the desktop bridge", async () => {
    host._wails = { invoke: send }
    const choice = chooseBackupFile("save", { title: "Save", buttonText: "Save", filename: "zwai.zwai-backup" })
    const sent = request()
    expect(sent).toMatchObject({ type: "zwai:backup-file", action: "save", filename: "zwai.zwai-backup" })
    reply(sent, { path: "/tmp/zwai.zwai-backup" })
    await expect(choice).resolves.toBe("/tmp/zwai.zwai-backup")
  })
})
