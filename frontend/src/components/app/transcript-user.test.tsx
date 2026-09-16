import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { Transcript } from "./transcript"
import type { TranscriptState } from "@/lib/transcript"
import { emptyTranscript } from "@/lib/transcript"

describe("user message actions", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("offers copy and edit under the bubble", () => {
    const at = "2026-09-09T12:20:00.000Z"
    render(
      <Transcript
        state={twoTurns(at)}
        loaded
        onSelectAgent={() => {}}
        onResendUser={() => {}}
      />,
    )
    const row = screen.getByText("alpha").closest("[data-testid=user-message]")
    expect(row).not.toBeNull()
    const actions = row!.parentElement
    expect(actions?.querySelector("[data-testid=user-message-time]")).toHaveAttribute(
      "datetime",
      at,
    )
    expect(screen.getAllByRole("button", { name: "Copy message" })).toHaveLength(2)
    expect(screen.getAllByRole("button", { name: "Edit message" })).toHaveLength(2)
  })

  it("copies the sent text", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    render(
      <Transcript state={twoTurns()} loaded onSelectAgent={() => {}} />,
    )
    fireEvent.click(screen.getAllByRole("button", { name: "Copy message" })[0])
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("alpha"))
  })

  it("edits the bubble in place and resends from that sequence", () => {
    const onResendUser = vi.fn()
    render(
      <Transcript
        state={twoTurns()}
        loaded
        onSelectAgent={() => {}}
        onResendUser={onResendUser}
      />,
    )
    fireEvent.click(screen.getAllByRole("button", { name: "Edit message" })[1])
    const editor = screen.getByTestId("user-message-editor")
    const box = editor.querySelector("textarea")
    expect(box).not.toBeNull()
    expect(box).toHaveValue("beta")
    expect(screen.queryByTestId("composer-input")).toBeNull()
    fireEvent.change(box!, { target: { value: "beta edited" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(onResendUser).toHaveBeenCalledTimes(1)
    expect(onResendUser).toHaveBeenCalledWith("beta edited", 2)
  })

  it("does not offer edit on a pending replacement bubble", () => {
    render(
      <Transcript
        state={{
          ...emptyTranscript(),
          agentOrder: ["manager"],
          agents: {
            manager: {
              id: "manager",
              role: "manager",
              status: "running",
              activity: "",
              blocks: [
                {
                  id: "b1",
                  kind: "user",
                  agentId: "manager",
                  text: "alpha",
                  turnId: "tn_a",
                  seq: 1,
                  at: "",
                },
                {
                  id: "pending-edit",
                  kind: "user",
                  agentId: "manager",
                  text: "edited",
                  turnId: "pending-edit",
                  seq: 0,
                  at: "",
                },
              ],
            },
          },
        }}
        loaded
        onSelectAgent={() => {}}
        onResendUser={() => {}}
      />,
    )
    expect(screen.getAllByRole("button", { name: "Edit message" })).toHaveLength(1)
    expect(screen.getByText("edited")).toBeInTheDocument()
  })

  it("cancels an in-place edit without sending", () => {
    const onResendUser = vi.fn()
    render(
      <Transcript
        state={twoTurns()}
        loaded
        onSelectAgent={() => {}}
        onResendUser={onResendUser}
      />,
    )
    fireEvent.click(screen.getAllByRole("button", { name: "Edit message" })[0])
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onResendUser).not.toHaveBeenCalled()
    expect(screen.queryByTestId("user-message-editor")).toBeNull()
    expect(screen.getByText("alpha")).toBeInTheDocument()
  })

  it("hides copy when the bubble has no text", () => {
    render(
      <Transcript
        state={imageOnlyTurn()}
        loaded
        onSelectAgent={() => {}}
        onResendUser={() => {}}
      />,
    )
    expect(screen.queryByRole("button", { name: "Copy message" })).toBeNull()
    expect(screen.getByRole("button", { name: "Edit message" })).toBeInTheDocument()
  })
})

function imageOnlyTurn(): TranscriptState {
  const at = new Date().toISOString()
  return {
    ...emptyTranscript(),
    agentOrder: ["manager"],
    agents: {
      manager: {
        id: "manager",
        role: "manager",
        status: "done",
        activity: "",
        blocks: [
          {
            id: "b1",
            kind: "user",
            agentId: "manager",
            text: "",
            images: [{ id: "img_ab", name: "clip.png", mime: "image/png" }],
            turnId: "tn_a",
            seq: 1,
            at,
          },
        ],
      },
    },
  }
}

function twoTurns(at = new Date().toISOString()): TranscriptState {
  return {
    ...emptyTranscript(),
    agentOrder: ["manager"],
    agents: {
      manager: {
        id: "manager",
        role: "manager",
        status: "done",
        activity: "",
        blocks: [
          {
            id: "b1",
            kind: "user",
            agentId: "manager",
            text: "alpha",
            turnId: "tn_a",
            seq: 1,
            at,
          },
          {
            id: "b2",
            kind: "user",
            agentId: "manager",
            text: "beta",
            turnId: "tn_b",
            seq: 2,
            at,
          },
        ],
      },
    },
  }
}
