import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { composerDraftSession, type ComposerDraftRecord } from "@/lib/composer-draft"
import { useApp } from "@/store/app"

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

for (const command of ["goal", "plan"] as const) {
  for (const extra of ["file", "image", "quote"] as const) {
    it(`keeps the complete ${extra} draft when /${command} can acknowledge only its text`, async () => {
      const key = `thread:slash-${command}-${extra}`
      const locale = extra === "image" ? "zh" : "en"
      useApp.setState({ locale })
      vi.stubGlobal("URL", Object.assign(class extends URL {}, {
        createObjectURL: vi.fn(() => "blob:selected-image"), revokeObjectURL: vi.fn(),
      }))
      const bytes = new Blob([new Uint8Array([0, 255, 32])], { type: extra === "image" ? "image/png" : "application/octet-stream" })
      const original: ComposerDraftRecord = {
        key, revision: 1, text: `/${command} Inspect the assigned operation.`, state: "editing",
        quotes: extra === "quote" ? [{ id: "source", text: "selected context" }] : [],
        files: extra === "quote" ? [] : [{ id: "original", name: extra === "image" ? "photo.png" : "bytes.bin",
          mime: bytes.type, kind: extra, size: bytes.size, last_modified: 123 }],
      }
      let stored = structuredClone(original)
      vi.spyOn(api, "composerDraft").mockImplementation(async () => structuredClone(stored))
      vi.spyOn(api, "composerDraftFile").mockResolvedValue(bytes)
      vi.spyOn(api, "saveComposerDraft").mockImplementation(async (draft) => {
        stored = { ...structuredClone(draft), revision: stored.revision + 1 }
        return structuredClone(stored)
      })
      const clear = vi.spyOn(api, "deleteComposerDraft").mockImplementation(async () => {
        stored = { ...stored, revision: stored.revision + 1, text: "", quotes: [], files: [], state: "editing" }
        return structuredClone(stored)
      })
      const action = vi.fn(async () => true)
      const upload = vi.fn(async () => [])
      const send = vi.fn(async () => true)
      const view = render(<TooltipProvider><Composer running={false} models={[]} draftKey={key}
        onModelChange={vi.fn()} reasoningLevels={[]} onReasoningChange={vi.fn()}
        onSend={send} onSetGoal={action} onSetPlan={action} onStop={vi.fn()} onUpload={upload}
        focusSignal={0} /></TooltipProvider>)
      const input = screen.getByTestId("composer-input")
      await waitFor(() => expect(input).toBeEnabled())
      await act(async () => {
        fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
        await new Promise((done) => setTimeout(done, 0))
      })
      expect({ action_calls: action.mock.calls.length, upload_calls: upload.mock.calls.length,
        send_calls: send.mock.calls.length, clear_calls: clear.mock.calls.length, draft: stored }).toEqual({
        action_calls: 0, upload_calls: 0, send_calls: 0, clear_calls: 0, draft: original,
      })
      expect(input).toHaveValue(original.text)
      expect(screen.getByRole("alert")).toHaveTextContent(locale === "zh" ? "草稿已保留" : "Your draft was kept")
      expect(composerDraftSession(key).getSnapshot().draft).toMatchObject(original)
      view.unmount()
    })
  }
}
