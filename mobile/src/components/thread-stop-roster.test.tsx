import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { setLocale } from "@/lib/i18n"
import { applyEvent, type CompactBlock } from "@/lib/transcript"
import type { RemoteEvent } from "@/lib/rpc"
import { ThreadScreen } from "./thread-screen"

describe("phone stopped worker roster", () => {
  it.each([
    { locale: "en" as const, open: "Agents (0)", stopped: "Stopped (1)", finished: "Finished (2)", state: "stopped" },
    { locale: "zh" as const, open: "子 Agent (0)", stopped: "已停止 (1)", finished: "已结束 (2)", state: "已停止" },
  ])("separates stopped workers from real completed and failed work in $locale", (language) => {
    setLocale(language.locale)
    const events = [
      { kind: "spawned", agent_id: "reader-1", role: "reader" },
      { kind: "spawned", agent_id: "done-2", role: "completed" },
      { kind: "finished", agent_id: "done-2", text: "Verified result." },
      { kind: "spawned", agent_id: "failed-3", role: "failed" },
      { kind: "finished", agent_id: "failed-3", err: "provider unavailable" },
      { kind: "cleanup" },
      { kind: "finished", agent_id: "reader-1", err: "swarm: agent reader-1 cancelled: context canceled" },
    ].map((row, index) => ({ thread_id: "thread", seq: index + 1, text: "", created_at: "", ...row }) as RemoteEvent)
    const blocks = events.reduce<CompactBlock[]>(applyEvent, [])
    render(<ThreadScreen detail={{ id: "thread", title: "Work" }} blocks={blocks}
      onBack={vi.fn()} onSend={vi.fn()} onSteer={vi.fn()} onStop={vi.fn()}
      onAnswer={vi.fn()} onAnswerStructured={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: language.open }))
    const stopped = screen.getByRole("heading", { name: language.stopped }).closest("section")!
    expect(within(stopped).getAllByRole("button")).toHaveLength(1)
    const reader = within(stopped).getByRole("button", { name: new RegExp(`reader-1.*${language.state}`) })
    expect(reader).toBeVisible()
    expect(screen.getByRole("heading", { name: language.finished })).toBeVisible()
    expect(screen.getByTestId("agent-roster").querySelector(".text-destructive")).toBeNull()
    fireEvent.click(reader)
    const diagnostic = screen.getByText("swarm: agent reader-1 cancelled: context canceled")
    expect(diagnostic).toBeVisible()
    expect(diagnostic).not.toHaveClass("text-destructive")
  })
})
