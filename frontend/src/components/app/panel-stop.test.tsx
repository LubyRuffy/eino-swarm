import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"
import { RightPanel } from "./panel"

function stoppedTranscript() {
  const events = [
    { kind: "user_message", text: "Continue the assigned work." },
    { kind: "spawned", agent_id: "worker-1", role: "worker" },
    { kind: "spawned", agent_id: "worker-2", role: "worker" },
    { kind: "finished", agent_id: "worker-2", text: "Verified result." },
    { kind: "cleanup", text: "Stopped unfinished workers." },
    { kind: "error", err: "interrupted" },
  ].map((event, index) => ({ thread_id: "thread", turn_id: "turn", agent_id: "manager",
    seq: index + 1, created_at: "2026-10-19T01:00:00Z", ...event })) as SwarmEvent[]
  return reduceEvents(emptyTranscript(), events)
}

const props = { tab: "agents" as const, onTabChange: vi.fn(), onSelectAgent: vi.fn(),
  files: [], workspace: "", turns: [], onUpload: async () => {}, onDeleteFile: vi.fn(),
  onRefreshFiles: vi.fn(), onReveal: vi.fn() }

describe("stopped agent roster", () => {
  it.each(["en", "zh"] as const)("separates stopped tasks from successful completion in %s", (locale) => {
    useApp.setState({ locale })
    render(<RightPanel {...props} transcript={stoppedTranscript()} />)
    expect(screen.getByText(locale === "en" ? "Stopped (1)" : "已停止 (1)")).toBeInTheDocument()
    expect(screen.getByText(locale === "en" ? "Done (1)" : "已完成 (1)")).toBeInTheDocument()
    expect(screen.queryByText(/Done \(2\)|已完成 \(2\)/)).not.toBeInTheDocument()
    const stopped = screen.getByRole("button", { name: /worker-1/ })
    expect(stopped.querySelector("[aria-label]")).toHaveAttribute("aria-label", locale === "en" ? "stopped" : "已停止")
    expect(stopped.querySelector(".bg-done, .bg-failed, .animate-breathe, .animate-spin")).toBeNull()
    expect(stopped).toHaveTextContent(locale === "en" ? "stopped" : "已停止")
    expect(screen.getByRole("button", { name: /worker-2/ }).querySelector(".bg-done")).toBeInTheDocument()
  })
})
