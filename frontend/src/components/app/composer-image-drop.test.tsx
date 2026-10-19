import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { Attachment } from "@/lib/types"

describe("Composer desktop image input", () => {
  it.each([
    { source: "drop", type: "application/octet-stream" },
    { source: "paste", type: "public.jpeg" },
  ])("sends a $source photo labeled $type as vision bytes, not a workspace path", async ({ source, type }) => {
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: () => "blob:preview" })
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: () => {} })
    const onSend = vi.fn()
    const onUpload = vi.fn(async (): Promise<Attachment[]> => [])
    render(
      <TooltipProvider>
        <Composer
          running={false}
          models={[]}
          provider=""
          model=""
          onModelChange={vi.fn()}
          reasoning=""
          reasoningLevels={[]}
          onReasoningChange={vi.fn()}
          onSend={onSend}
          onStop={vi.fn()}
          onUpload={onUpload}
          focusSignal={0}
        />
      </TooltipProvider>,
    )
    const photo = new File([new Uint8Array([1, 2, 3])], "photo.jpg", { type })
    const transfer = {
      types: ["Files"], files: [photo],
      items: [{ kind: "file", type: photo.type, getAsFile: () => photo }],
    }
    if (source === "drop") {
      fireEvent.drop(screen.getByTestId("composer-drop"), { dataTransfer: transfer })
    } else {
      fireEvent.paste(screen.getByTestId("composer-input"), { clipboardData: transfer })
    }
    expect(screen.getByAltText("photo.jpg")).toHaveAttribute("src", "blob:preview")
    expect(screen.queryByTestId("composer-attachments")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1))
    expect(onUpload).not.toHaveBeenCalled()
    const [, images, opts] = onSend.mock.calls[0]
    expect(images).toEqual([{ name: "photo.jpg", mime: "image/jpeg", data: "AQID" }])
    expect(opts.files).toBeUndefined()
  })
})
