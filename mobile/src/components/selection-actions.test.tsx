import { publishPairedCapabilities, publishPairedSelectionTranslation } from "@/lib/paired-capabilities"
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { defaultSpeechSettings } from "../../../frontend/src/lib/speech-types"
import { setLocale } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { SelectionActions } from "./selection-actions"

const mocks = vi.hoisted(() => ({ play: vi.fn(), stop: vi.fn(), cancel: vi.fn(), speech: { outputReady: true, snapshot: { limits: { chunk_chars: 300 } } }, playback: { key: "", state: "idle" } }))
vi.mock("@/lib/phone-speech", () => ({ usePhoneSpeech: () => mocks.speech, cancelPhoneSpeech: mocks.cancel, synthesizePhoneSpeech: vi.fn() }))
vi.mock("../../../frontend/src/lib/speech-media", () => ({ speechPlayback: { subscribe: () => () => {}, getSnapshot: () => mocks.playback, play: mocks.play, stop: mocks.stop } }))
const props = () => ({ text: "Keep two hosts.", threadId: "t1", onAdd: vi.fn(), onDismiss: vi.fn(), onExpand: vi.fn() })
const link = (rpc = vi.fn().mockResolvedValue({ ok: true, translation: "保留两台主机。" })) => {
  const target = { alive: () => true, rpc } as unknown as RemoteLink
  publishPairedCapabilities(target, { mode: "local", configurable: true, ready: true, scope: "fixture" })
  return target
}
beforeEach(() => {
  vi.clearAllMocks()
  setLocale("zh")
  mocks.speech = { outputReady: true, snapshot: { limits: defaultSpeechSettings().limits } }
  mocks.playback = { key: "", state: "idle" }
  mocks.play.mockResolvedValue(undefined)
})

it("reads only the selected passage and stops owned playback on leaving", () => {
  const p = props()
  const view = render(<SelectionActions {...p} />)
  fireEvent.click(screen.getByRole("button", { name: "朗读" }))
  expect(mocks.cancel).toHaveBeenCalledOnce()
  expect(mocks.play).toHaveBeenCalledWith("phone-selection:t1", p.text, expect.objectContaining({ chunkChars: defaultSpeechSettings().limits.chunk_chars, synthesize: expect.any(Function) }))
  expect(p.onAdd).not.toHaveBeenCalled()
  mocks.playback = { key: "phone-selection:t1", state: "loading" }
  view.rerender(<SelectionActions {...p} />)
  fireEvent.click(screen.getByRole("button", { name: "停止朗读" }))
  expect(mocks.stop).toHaveBeenCalledOnce()
  view.unmount()
  expect(mocks.stop).toHaveBeenCalledTimes(2)
})

it("shows reading failures and hides reading for disabled speech or fenced code", async () => {
  const p = props()
  mocks.play.mockRejectedValue(new Error("failed"))
  const view = render(<SelectionActions {...p} />)
  fireEvent.click(screen.getByRole("button", { name: "朗读" }))
  expect(await screen.findByRole("alert")).toBeVisible()
  mocks.speech.outputReady = false
  view.rerender(<SelectionActions {...p} />)
  expect(screen.queryByRole("button", { name: "朗读" })).toBeNull()
  mocks.speech.outputReady = true
  view.rerender(<SelectionActions {...p} text={'```sh\nls\n```'} />)
  expect(screen.queryByRole("button", { name: "朗读" })).toBeNull()
})

it("keeps translations in the menu and suppresses duplicate requests while loading", async () => {
  let resolve!: (value: unknown) => void
  const rpc = vi.fn(() => new Promise((done) => { resolve = done }))
  const p = props()
  render(<SelectionActions {...p} link={link(rpc)} />)
  const button = screen.getByRole("button", { name: "翻译" })
  fireEvent.click(button)
  expect(button).toBeDisabled()
  fireEvent.click(button)
  expect(rpc).toHaveBeenCalledOnce()
  expect(screen.getByText("正在翻译…")).toBeVisible()
  await act(async () => resolve({ ok: true, translation: "保留两台主机。" }))
  expect(screen.getByRole("region", { name: "译文" })).toHaveTextContent("保留两台主机。")
  expect(p.onAdd).not.toHaveBeenCalled()
  expect(p.onExpand).toHaveBeenCalledOnce()
})

it("cancels the exact translation request and ignores late results after closing", async () => {
  let resolve!: (value: unknown) => void
  const rpc = vi.fn((request: { op: string }) => request.op === "cancel_translation" ? Promise.resolve({ ok: true }) : new Promise((done) => { resolve = done }))
  const view = render(<SelectionActions {...props()} link={link(rpc)} />)
  fireEvent.click(screen.getByRole("button", { name: "翻译" }))
  const [{ id }, options] = rpc.mock.calls[0] as unknown as [{ id: string }, { signal: AbortSignal }]
  view.unmount()
  expect(options.signal.aborted).toBe(true)
  expect(rpc).toHaveBeenLastCalledWith({ op: "cancel_translation", call_id: id }, expect.anything())
  await act(async () => resolve({ ok: true, translation: "stale" }))
  expect(screen.queryByText("stale")).toBeNull()
})

it.each([
  [{ ok: false, code: "unknown_op" }, "请更新电脑端以使用选区翻译。"],
  [{ ok: false, code: "selection_translation_too_long" }, "选中文字过长，请缩短后重试。"],
  [{ ok: true, translation: " " }, "翻译失败，请重试。"],
])("shows a recoverable translation error: %o", async (response, message) => {
  render(<SelectionActions {...props()} link={link(vi.fn().mockResolvedValue(response))} />)
  fireEvent.click(screen.getByRole("button", { name: "翻译" }))
  expect(await screen.findByRole("alert")).toHaveTextContent(message)
  expect(screen.getByRole("button", { name: "翻译" })).toBeEnabled()
})

it("cancels timed-out translation and permits explicit retry", async () => {
  const rpc = vi.fn().mockRejectedValue(new Error("rpc timeout"))
  render(<SelectionActions {...props()} link={link(rpc)} />)
  fireEvent.click(screen.getByRole("button", { name: "翻译" }))
  expect(await screen.findByRole("alert")).toHaveTextContent("翻译失败")
  await waitFor(() => expect(rpc).toHaveBeenCalledTimes(2))
  expect(rpc.mock.calls[1][0].op).toBe("cancel_translation")
})

it("offers translation only for English in Chinese UI with a PC and preserves quote and close actions", () => {
  const p = props()
  const remote = link()
  const view = render(<SelectionActions {...p} link={remote} />)
  expect(screen.getByRole("button", { name: "翻译" })).toBeVisible()
  fireEvent.click(screen.getByRole("button", { name: "加入对话" }))
  fireEvent.click(screen.getByRole("button", { name: "关闭选中文字操作" }))
  expect(p.onAdd).toHaveBeenCalledOnce()
  expect(p.onDismiss).toHaveBeenCalledOnce()
  view.rerender(<SelectionActions {...p} text="中文" link={remote} />)
  expect(screen.queryByRole("button", { name: "翻译" })).toBeNull()
  view.rerender(<SelectionActions {...p} />)
  expect(screen.queryByRole("button", { name: "翻译" })).toBeNull()
  setLocale("en")
  view.rerender(<SelectionActions {...p} link={remote} />)
  expect(screen.queryByRole("button", { name: "Translate" })).toBeNull()
})

it("dismisses and stops owned work when backgrounded", () => {
  const p = props()
  render(<SelectionActions {...p} />)
  Object.defineProperty(document, "hidden", { configurable: true, value: true })
  fireEvent(document, new Event("visibilitychange"))
  expect(p.onDismiss).toHaveBeenCalledOnce()
  Object.defineProperty(document, "hidden", { configurable: true, value: false })
})

it("removes and cancels selection translation when the paired PC switches it off", async () => {
  let resolve!: (value: unknown) => void
  const rpc = vi.fn((request: { op: string }) => request.op === "cancel_translation"
    ? Promise.resolve({ ok: true }) : new Promise(done => { resolve = done }))
  const target = link(rpc)
  render(<SelectionActions {...props()} link={target} />)
  fireEvent.click(screen.getByRole("button", { name: "翻译" }))
  act(() => publishPairedSelectionTranslation(target, false))
  expect(screen.queryByRole("button", { name: "翻译" })).toBeNull()
  expect(rpc).toHaveBeenLastCalledWith(expect.objectContaining({ op: "cancel_translation" }), expect.anything())
  await act(async () => resolve({ ok: true, translation: "stale" }))
  expect(screen.queryByRole("region", { name: "译文" })).toBeNull()
  act(() => publishPairedSelectionTranslation(target, true))
  expect(screen.getByRole("button", { name: "翻译" })).toBeVisible()
})
