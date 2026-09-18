import { describe, expect, it } from "vitest"

import {
  CHART_MAX_ROWS,
  CHART_MAX_SERIES,
  parseChartSpec,
} from "./chart-spec"

describe("parseChartSpec", () => {
  it("accepts the canonical row-oriented body", () => {
    const got = parseChartSpec(`{
      "type": "bar",
      "title": "Counts",
      "unit": "n",
      "x": "item",
      "y": "n",
      "data": [{"item": "a", "n": 1}, {"item": "b", "n": 3}]
    }`)
    expect(got).toEqual({
      ok: true,
      spec: {
        type: "bar",
        title: "Counts",
        unit: "n",
        x: "item",
        y: ["n"],
        stacked: false,
        data: [
          { item: "a", n: 1 },
          { item: "b", n: 3 },
        ],
      },
    })
  })

  it("accepts several y fields and a line type", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "line",
        x: "t",
        y: ["left", "right"],
        data: [
          { t: "1", left: 1, right: 2 },
          { t: "2", left: 3, right: 4 },
        ],
      }),
    )
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(got.spec.type).toBe("line")
    expect(got.spec.y).toEqual(["left", "right"])
  })

  it("treats labels plus values as rows so a parallel-array dump still paints", () => {
    const got = parseChartSpec(
      JSON.stringify({
        kind: "pie",
        name: "Share",
        labels: ["a", "b", "c"],
        values: [10, 20, 70],
      }),
    )
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(got.spec.type).toBe("pie")
    expect(got.spec.title).toBe("Share")
    expect(got.spec.x).toBe("label")
    expect(got.spec.y).toEqual(["value"])
    expect(got.spec.data).toEqual([
      { label: "a", value: 10 },
      { label: "b", value: 20 },
      { label: "c", value: 70 },
    ])
  })

  it("treats a values object as labelled quantities", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "bar",
        values: { a: 2, b: 5 },
      }),
    )
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(got.spec.data).toEqual([
      { label: "a", value: 2 },
      { label: "b", value: 5 },
    ])
  })

  it("infers x and y from the first row when they were omitted", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "area",
        data: [
          { k: "p", v: 1 },
          { k: "q", v: 2 },
        ],
      }),
    )
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(got.spec.x).toBe("k")
    expect(got.spec.y).toEqual(["v"])
  })

  it("coerces numeric strings and keeps a stacked flag", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "bar",
        stacked: true,
        x: "k",
        y: ["a", "b"],
        data: [
          { k: "p", a: "1", b: "2" },
          { k: "q", a: "3", b: "4" },
        ],
      }),
    )
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(got.spec.stacked).toBe(true)
    expect(got.spec.data[0]).toEqual({ k: "p", a: 1, b: 2 })
  })

  it("caps rows and series so a dump cannot freeze the transcript", () => {
    const y = Array.from({ length: CHART_MAX_SERIES + 3 }, (_, i) => `s${i}`)
    const data = Array.from({ length: CHART_MAX_ROWS + 5 }, (_, i) => {
      const row: Record<string, string | number> = { k: String(i) }
      for (const key of y) row[key] = i
      return row
    })
    const got = parseChartSpec(JSON.stringify({ type: "bar", x: "k", y, data }))
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(got.spec.y).toHaveLength(CHART_MAX_SERIES)
    expect(got.spec.data).toHaveLength(CHART_MAX_ROWS)
  })

  it("drops a row that is missing a plotted number instead of charting a hole", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "bar",
        x: "k",
        y: "n",
        data: [
          { k: "a", n: 1 },
          { k: "b" },
          { k: "c", n: 3 },
        ],
      }),
    )
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(got.spec.data).toEqual([
      { k: "a", n: 1 },
      { k: "c", n: 3 },
    ])
  })

  it("rejects a single row: that is a number, not a comparison", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "bar",
        x: "k",
        y: "n",
        data: [{ k: "a", n: 1 }],
      }),
    )
    expect(got).toEqual({ ok: false, incomplete: false })
  })

  it("rejects an unknown type rather than guessing a mark", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "radar",
        x: "k",
        y: "n",
        data: [
          { k: "a", n: 1 },
          { k: "b", n: 2 },
        ],
      }),
    )
    expect(got.ok).toBe(false)
    if (got.ok) return
    expect(got.incomplete).toBe(false)
  })

  it("marks a truncated object as incomplete so a live fence can wait", () => {
    expect(parseChartSpec(`{"type":"bar","x":"k"`)).toEqual({
      ok: false,
      incomplete: true,
    })
    expect(parseChartSpec("{")).toEqual({ ok: false, incomplete: true })
    expect(parseChartSpec("")).toEqual({ ok: false, incomplete: true })
  })

  it("does not treat junk as a half-written chart", () => {
    expect(parseChartSpec("not json")).toEqual({ ok: false, incomplete: false })
    expect(parseChartSpec("[1, 2, 3]")).toEqual({ ok: false, incomplete: false })
  })

  it("does not invent a series that was never in the body", () => {
    const got = parseChartSpec(
      JSON.stringify({
        type: "bar",
        x: "k",
        y: "n",
        data: [
          { k: "a", n: 1 },
          { k: "b", n: 2 },
        ],
      }),
    )
    expect(got.ok).toBe(true)
    if (!got.ok) return
    expect(JSON.stringify(got.spec)).not.toMatch(/revenue|month|sales/i)
  })
})
