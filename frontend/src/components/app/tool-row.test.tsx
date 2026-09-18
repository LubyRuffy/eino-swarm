import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { ToolRow } from "./tool-row"
import type { Block } from "@/lib/transcript"

function toolBlock(partial: Partial<NonNullable<Block["tool"]>>): Block {
  return {
    id: "b1",
    kind: "tool",
    agentId: "manager",
    text: "exec",
    tool: {
      callId: "c1",
      name: "exec",
      args: `{"command":"printf x"}`,
      pending: false,
      ...partial,
    },
    turnId: "t1",
    seq: 1,
    at: new Date().toISOString(),
  }
}

describe("ToolRow", () => {
  it("opens a pending call so live output is visible", () => {
    render(
      <ToolRow
        block={toolBlock({
          pending: true,
          result: JSON.stringify({ stdout: "chunk-one", stderr: "" }),
        })}
      />,
    )
    expect(screen.getByTestId("tool-output").textContent).toBe("chunk-one")
  })

  it("keeps a finished call collapsed until the user opens it", () => {
    render(
      <ToolRow
        block={toolBlock({
          result: JSON.stringify({
            exit_code: 0,
            stdout: "done",
            stderr: "",
            failed: false,
          }),
        })}
      />,
    )
    expect(screen.queryByTestId("tool-output")).not.toBeInTheDocument()
  })
})
