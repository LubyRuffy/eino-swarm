import { readFileSync } from "node:fs"
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { sendPhoneTurn } from "@/lib/phone-turn"
import type { RemoteLink } from "@/lib/link"
import { OpPut, OpSend, OpSteer, type RemoteRequest, type RemoteResponse } from "@/lib/rpc"
import { emptyView } from "@/lib/session"

function readableFile(bytes: Uint8Array, name: string, type: string) {
  const file = new File([new Uint8Array(bytes)], name, { type })
  // JSDOM lacks File.arrayBuffer; use its real FileReader so the test still
  // reads the selected File's bytes rather than substituting canned chunks.
  Object.defineProperty(file, "arrayBuffer", { value: () => new Promise<ArrayBuffer>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.onerror = () => reject(reader.error)
    reader.readAsArrayBuffer(file)
  }) })
  return file
}

for (const op of [OpSend, OpSteer]) {
  it(`retains the goal draft and original file/image bytes after PC ${op} refuses mixed input`, async () => {
    const imageBytes = new Uint8Array(readFileSync("../internal/slash/testdata/goal-attachment.png"))
    const fileBytes = new Uint8Array([0, 255, 32, 13, 10])
    const files = [readableFile(imageBytes, "original.png", "image/png"), readableFile(fileBytes, "original.bin", "application/octet-stream")]
    const uploaded: number[][] = []
    const submitted: string[] = []
    const rpc = vi.fn(async (req: Omit<RemoteRequest, "v" | "id">): Promise<RemoteResponse> => {
      if (req.op === OpPut) {
        uploaded.push(Array.from(atob(req.data ?? ""), (ch) => ch.charCodeAt(0)))
        return { v: 1, id: "put-receipt", ok: true, put: { id: req.put_id ?? "", ready: true } }
      }
      submitted.push(req.text ?? "")
      return submitted.length === 1
        ? { v: 1, id: "refusal", ok: false, error: "engine: /goal accepts text only; remove attachments or send them as an ordinary message" }
        : { v: 1, id: "accepted", ok: true }
    })
    const commit = vi.fn()
    const setError = vi.fn()
    const deps = { link: () => ({ alive: () => true, rpc }) as unknown as RemoteLink,
      view: emptyView, commit, setError, setPending: vi.fn(), fail: vi.fn() }
    render(<Composer label="Message" sendLabel="Send" onSubmit={(text, extra) => sendPhoneTurn(deps, op, "completed-goal-thread", text, extra)} />)
    const input = screen.getByLabelText("Message")
    const command = "Inspect the assigned operation /goal"
    fireEvent.change(input, { target: { value: command } })
    fireEvent.change(screen.getByTestId("file-input"), { target: { files } })
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Send" })) })
    await waitFor(() => expect(input).toHaveValue(command))
    expect(screen.getByRole("button", { name: "Remove original.png" })).toBeVisible()
    expect(screen.getByRole("button", { name: "Remove original.bin" })).toBeVisible()
    expect(commit).not.toHaveBeenCalled()
    expect(setError).toHaveBeenCalledWith(expect.stringContaining("text only"))
    fireEvent.change(input, { target: { value: "Inspect the assigned operation." } })
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Send" })) })
    await waitFor(() => expect(commit).toHaveBeenCalledTimes(1))
    expect(input).toHaveValue("")
    expect(uploaded).toEqual([Array.from(imageBytes), Array.from(fileBytes), Array.from(imageBytes), Array.from(fileBytes)])
    expect(submitted).toEqual([command, "Inspect the assigned operation."])
    expect(screen.queryByRole("button", { name: "Remove original.png" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Remove original.bin" })).toBeNull()
  })
}
