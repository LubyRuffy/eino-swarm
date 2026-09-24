import { describe, expect, it } from "vitest"

import {
  TIDY_ESTIMATE_CAP,
  emptyTidyReport,
  joinNames,
  normalizeTidyReport,
  tidyEstimateRatio,
  tidyFolded,
} from "./skill-tidy"

describe("skill tidy estimate", () => {
  it("starts empty and never paints a full bar before the tidy finishes", () => {
    expect(tidyEstimateRatio(0, 8)).toBe(0)
    expect(tidyEstimateRatio(1_000, 8)).toBeGreaterThan(0)
    expect(tidyEstimateRatio(1_000, 8)).toBeLessThan(tidyEstimateRatio(10_000, 8))
    expect(tidyEstimateRatio(60 * 60 * 1000, 8)).toBe(TIDY_ESTIMATE_CAP)
    expect(tidyEstimateRatio(10_000, 40)).toBeLessThan(tidyEstimateRatio(10_000, 1))
  })
})

describe("skill tidy report", () => {
  it("treats a missing payload as a tidy catalog of the scanned size", () => {
    const report = normalizeTidyReport(undefined, 4)
    expect(report).toEqual(emptyTidyReport(4))
    expect(tidyFolded(report)).toBe(false)
  })

  it("keeps created, deleted, patched and merged names for the panel to list", () => {
    const report = normalizeTidyReport({
      scanned: 3,
      before: 3,
      after: 2,
      families: 1,
      unchanged: 1,
      created: ["a-procedure"],
      deleted: ["a-procedure-notes", "a-procedure-send"],
      patched: ["a-procedure"],
      merged: [
        {
          keep: "a-procedure",
          dropped: ["a-procedure-notes", "a-procedure-send"],
          created: true,
        },
      ],
      reviewed: true,
    })
    expect(tidyFolded(report)).toBe(true)
    expect(joinNames(report.deleted)).toBe("a-procedure-notes, a-procedure-send")
    expect(report.created).toEqual(["a-procedure"])
    expect(report.patched).toEqual(["a-procedure"])
    expect(report.merged[0]?.keep).toBe("a-procedure")
    expect(report.reviewed).toBe(true)
  })

  it("drops blank names rather than painting empty rows", () => {
    const report = normalizeTidyReport({
      created: ["kept", "  "],
      deleted: [""],
      patched: ["  "],
      merged: [{ keep: "", dropped: ["gone"], created: false }],
    })
    expect(report.created).toEqual(["kept"])
    expect(report.deleted).toEqual([])
    expect(report.patched).toEqual([])
    expect(report.merged).toEqual([])
  })

  it("counts a fold that only created, deleted or patched names as folded", () => {
    expect(
      tidyFolded(
        normalizeTidyReport({
          created: ["kept"],
          deleted: ["chapter"],
          merged: [],
        }),
      ),
    ).toBe(true)
    expect(tidyFolded(normalizeTidyReport({ patched: ["kept"] }))).toBe(true)
    expect(tidyFolded(normalizeTidyReport({ merged: [], reviewed: true }))).toBe(false)
  })
})
