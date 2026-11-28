import { act, fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"

import { installAndroidBack } from "@/lib/android-back"
import { setLocale } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { formatQuotedMessage } from "@/lib/quote"
import type { RemoteResponse } from "@/lib/rpc"
import { HistoryInputEditor } from "./history-input-editor"

it("resends the original quotation once and closes only this editor on Back", async () => {
  setLocale("en")
  let reply: (response: RemoteResponse) => void = () => {}
  const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValueOnce({
    v: 1, id: "read", ok: true,
    event: { kind: "user_message", seq: 7, thread_id: "thread", created_at: "", text: formatQuotedMessage(["Original evidence"], "Old request") },
  }).mockImplementationOnce(() => new Promise((resolve) => { reply = resolve }))
  const returnFromThread = vi.fn(() => true)
  const remove = installAndroidBack(returnFromThread)
  try {
    const close = vi.fn()
    const view = render(<HistoryInputEditor owner={{
      link: { rpc, path: "local", alive: () => true, close: vi.fn(), announceDevice: vi.fn() },
      threadId: "thread",
    }} seq={7} onClose={close} />)
    const input = await screen.findByRole("textbox", { name: "Edit message" })
    expect(input).toHaveValue("Old request")
    expect(screen.getByText("Selected text: Original evidence")).toBeVisible()
    expect(window.__zwaiAndroidBack?.()).toBe(true)
    expect(close).toHaveBeenCalledTimes(1)
    expect(returnFromThread).not.toHaveBeenCalled()
    close.mockClear()

    fireEvent.change(input, { target: { value: "New request" } })
    const form = screen.getByRole("form", { name: "Edit message" })
    fireEvent.submit(form)
    fireEvent.submit(form)
    expect(window.__zwaiAndroidBack?.()).toBe(true)
    expect(close).not.toHaveBeenCalled()
    expect(rpc).toHaveBeenCalledTimes(2)
    expect(rpc.mock.calls[1][0]).toMatchObject({
      op: "edit_input", thread_id: "thread", from_event_seq: 7,
      replacement: formatQuotedMessage(["Original evidence"], "New request"),
    })
    await act(async () => { reply({ v: 1, id: "edit", ok: true }) })
    expect(close).toHaveBeenCalledTimes(1)
    view.unmount()
    expect(window.__zwaiAndroidBack?.()).toBe(true)
    expect(returnFromThread).toHaveBeenCalledTimes(1)
  } finally {
    remove()
  }
})
