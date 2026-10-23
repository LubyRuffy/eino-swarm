import { expect, it } from "vitest"

import { localizeNotice } from "./i18n"
import { modelRetryNotice } from "./transcript-notices"

it("explains a schedule claim correction without reporting an armed wait", () => {
  const notice = modelRetryNotice('{"attempt":1,"cap":1,"reason":"schedule_claim_unverified"}')
  expect(notice).toBe("Checking the schedule before reporting it as arranged.")
  expect(localizeNotice(notice, "zh")).toBe("正在核对调度记录，确认后再报告已安排。")
})

it("explains collecting existing workers without calling it a model failure", () => {
  const notice = modelRetryNotice('{"attempt":1,"cap":1,"reason":"workers_uncollected","worker_ids":["verifier-1"]}')
  expect(notice).toBe("Collecting delegated work before completing this turn.")
  expect(localizeNotice(notice, "zh")).toBe("正在收取子任务结果，收取后才能完成本回合。")
})
