import { fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { applyGoalDetail } from "@/lib/goal"
import { setLocale } from "@/lib/i18n"
import type { RemoteEvent, ThreadDetail } from "@/lib/rpc"
import { applyEvent, type CompactBlock } from "@/lib/transcript"
import { ThreadScreen } from "./thread-screen"

afterEach(() => {
  vi.restoreAllMocks()
  setLocale("en")
})

for (const locale of ["en", "zh"] as const) {
  it.each(["goal_capped", "goal_idle"] as const)(`${locale}: retains the pause cause without a resume action (%s)`, (pause) => {
    setLocale(locale)
    render(<ThreadScreen detail={{ id: "readonly", title: "Read only", goal: "Verify the artifact.", [pause]: true }}
      blocks={[]} onBack={vi.fn()} onSend={vi.fn()} onSteer={vi.fn()} onStop={vi.fn()}
      onAnswer={vi.fn()} onAnswerStructured={vi.fn()} />)
    const banner = screen.getByTestId("goal-banner")
    expect(banner).toHaveTextContent(locale === "zh" ? "自动执行已暂停" : "Auto-run paused")
    expect(within(banner).getByTestId("goal-reason")).toHaveTextContent(
      pause === "goal_capped"
        ? locale === "zh" ? "自动续跑已达到本次上限。" : "Automatic continuation reached its current limit."
        : locale === "zh" ? "上一轮自动续跑没有实质进展。" : "The last automatic continuation made no progress.",
    )
    expect(within(banner).queryByRole("button")).not.toBeInTheDocument()
    expect(banner).not.toHaveTextContent(locale === "zh" ? "点「开始目标」" : "Start goal resumes")
  })

  for (const pause of ["goal_blocked", "goal_capped", "goal_idle"] as const) {
    it(`${locale}: identifies automatic pursuit as paused after a manual answer (${pause})`, () => {
      setLocale(locale)
      vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-01-02T02:03:04Z"))
      const resume = vi.fn()
      let detail: ThreadDetail = {
        id: "paused-goal", title: "Goal with a later manual reply",
        goal: "Finish the required verification.", goal_on: true,
        goal_started_at: "2026-01-01T00:00:00Z",
      }
      let blocks: CompactBlock[] = []
      // A terminal manual turn is distinct from the durable goal's explicit
      // resume/completion events. Preserve that real reducer boundary.
      const events = [
        { kind: pause, text: JSON.stringify({ reason: "Required evidence is unavailable." }) },
        { kind: "user_message", text: "Describe the current result." },
        { kind: "agent_message", agent_id: "manager", text: "The requested summary is ready." },
        { kind: "done", agent_id: "manager", text: "The requested summary is ready." },
      ]
      for (const [index, item] of events.entries()) {
        const event: RemoteEvent = {
          thread_id: detail.id, seq: index + 1,
          created_at: "2026-01-02T02:03:04Z", ...item,
        }
        detail = applyGoalDetail(detail, event)
        blocks = applyEvent(blocks, event)
      }
      const handlers = {
        onBack: vi.fn(), onSend: vi.fn(), onSteer: vi.fn(), onStop: vi.fn(),
        onAnswer: vi.fn(), onAnswerStructured: vi.fn(), onResumeGoal: resume,
      }
      const view = render(<ThreadScreen detail={detail} blocks={blocks} {...handlers} />)
      const banner = screen.getByTestId("goal-banner")
      expect(banner).toHaveTextContent(locale === "zh" ? "自动执行已暂停" : "Auto-run paused")
      expect(banner).toHaveTextContent(locale === "zh" ? "目标设立至今" : "Goal age:")
      expect(banner).toHaveTextContent("1d 2h 3m 4s")
      expect(banner).toHaveTextContent(locale === "zh" ? "自动执行暂停原因：" : "Auto-run pause reason:")
      expect(screen.getByTestId("transcript")).toHaveTextContent("The requested summary is ready.")
      expect(resume).not.toHaveBeenCalled()
      expect(detail.goal_complete).not.toBe(true)
      fireEvent.click(within(banner).getByRole("button", { name: locale === "zh" ? "开始目标" : "Start goal" }))
      expect(resume).toHaveBeenCalledOnce()

      detail = applyGoalDetail(detail, { kind: "goal_complete" })
      view.rerender(<ThreadScreen detail={detail} blocks={blocks} {...handlers} />)
      expect(screen.queryByTestId("goal-banner")).not.toBeInTheDocument()
      expect(resume).toHaveBeenCalledOnce()
    })
  }
}
