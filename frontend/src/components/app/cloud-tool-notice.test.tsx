import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { CloudToolNotice } from "./cloud-tool-notice"
import type { Block } from "@/lib/transcript"

describe("CloudToolNotice", () => {
  it("keeps a quiet scheduled receipt out of the conversation", () => {
    const block: Block = { id: "cloud-quiet", kind: "notice", agentId: "manager", text: "Cloud tool", turnId: "tn-1", seq: 1, at: "2026-01-01T00:00:00Z", quiet: true, cloudTool: { type: "file_search_call", status: "completed" } }
    render(<CloudToolNotice block={block} />)
    expect(screen.queryByTestId("cloud-tool-notice")).toBeNull()
  })
  it("labels the tool as cloud execution with provider provenance", () => {
    const block: Block = { id: "cloud-1", kind: "notice", agentId: "manager", text: "", turnId: "tn-1", seq: 1, at: "2026-01-01T00:00:00Z", cloudTool: { provider_id: "endpoint-a", type: "file_search_call", status: "completed" } }
    render(<CloudToolNotice block={block} />)
    expect(screen.getByTestId("cloud-tool-notice")).toHaveTextContent("Cloud tool: file_search_call · completed")
    expect(screen.getByTestId("cloud-tool-notice")).toHaveTextContent("endpoint-a")
    expect(screen.queryByRole("button", { name: "Open briefing" })).toBeNull()
  })
})
