import { render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import { renderBlock } from "@/components/thread-blocks"
import { setLocale } from "./i18n"
import { applyEvent } from "./transcript"

afterEach(() => setLocale("en"))

it("keeps the worker collection notice across live and historical replay", () => {
  const event = {
    thread_id: "thread", seq: 2, kind: "model_retry", created_at: "2026-01-01T00:00:00Z",
    text: '{"attempt":1,"cap":1,"reason":"workers_uncollected","worker_ids":["verifier-1"]}',
  }
  const live = applyEvent([], event)
  expect(live.map((block) => [block.kind, block.text])).toEqual([
    ["notice", "Collecting delegated work before completing this turn."],
  ])
  expect(applyEvent([], event)).toEqual(live)
  setLocale("zh")
  render(renderBlock(live[0]))
  expect(screen.getByText("正在收取子任务结果，收取后才能完成本回合。")).toBeVisible()
})

it("shows the bounded schedule correction without exposing its JSON", () => {
  const blocks = applyEvent([], {
    thread_id: "thread", seq: 1, kind: "model_retry", created_at: "2026-01-01T00:00:00Z",
    text: '{"attempt":1,"cap":1,"reason":"schedule_claim_unverified"}',
  })
  expect(blocks.map((block) => [block.kind, block.text])).toEqual([
    ["notice", "Checking the schedule before reporting it as arranged."],
  ])
})

it("localizes the schedule correction on the actual phone block", () => {
  setLocale("zh")
  render(renderBlock({ id: "correction", kind: "notice", text: "Checking the schedule before reporting it as arranged." }))
  expect(screen.getByText("正在核对调度记录，确认后再报告已安排。")).toBeVisible()
})
