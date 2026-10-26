import { expect, it } from "vitest"

import { localizeNotice } from "./i18n"
import { goalCappedNotice, modelRetryNotice } from "./transcript-notices"

it("explains a schedule claim correction without reporting an armed wait", () => {
  const notice = modelRetryNotice('{"attempt":1,"cap":1,"reason":"schedule_claim_unverified"}')
  expect(notice).toBe("The previous answer contained an unverified schedule claim. One correction was requested.")
  expect(localizeNotice(notice, "zh")).toBe("上一份终稿的调度声明无法核对，已请求一次纠正。")
})

it("names a terminal schedule refusal without suggesting verification is still running", () => {
  const error = "schedule_claim_unverified: the claimed future check could not be verified; no unverified answer was published (no_matching_schedule)"
  expect(localizeNotice(error, "zh")).toBe("本回合的调度声明无法核对，终稿未发布（schedule_claim_unverified / no_matching_schedule）。")
  expect(localizeNotice(error, "en")).toBe("This turn's schedule claim could not be verified. No final answer was published (schedule_claim_unverified / no_matching_schedule).")
  expect(localizeNotice(`${error} extra`, "zh")).toBe(`${error} extra`)
})

it("distinguishes the consecutive schedule refusal limit from an external blocker", () => {
  const notice = goalCappedNotice('{"reason":"schedule_claim_unverified","code":"no_matching_schedule","attempt":3,"cap":3}')
  expect(notice).toBe("Stopped auto-continuing: consecutive schedule claims could not be verified. Press Start on the goal to keep going.")
  expect(localizeNotice(notice, "zh")).toBe("已停止自动续跑：连续多次调度声明无法核对。点「开始目标」继续。")
})

it("explains collecting existing workers without calling it a model failure", () => {
  const notice = modelRetryNotice('{"attempt":1,"cap":1,"reason":"workers_uncollected","worker_ids":["verifier-1"]}')
  expect(notice).toBe("Collecting delegated work before completing this turn.")
  expect(localizeNotice(notice, "zh")).toBe("正在收取子任务结果，收取后才能完成本回合。")
})
