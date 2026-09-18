import { cloneElement, isValidElement, type ReactNode } from "react"
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"

import type { ChartSpec } from "@/lib/chart-spec"

import { ChartPending } from "./chart-pending"
import { CHART_SERIES_PAINT, TranscriptChart } from "./transcript-chart"

vi.mock("recharts", async (importOriginal) => {
  const actual = await importOriginal<typeof import("recharts")>()
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children?: ReactNode }) => (
      <div data-testid="chart-surface">
        {isValidElement(children)
          ? cloneElement(children, { width: 400, height: 224 } as never)
          : children}
      </div>
    ),
  }
})

const spec = (over: Partial<ChartSpec> = {}): ChartSpec => ({
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
  ...over,
})

describe("TranscriptChart", () => {
  it("names the figure after the spec title", () => {
    render(<TranscriptChart spec={spec()} />)
    const figure = screen.getByTestId("transcript-chart")
    expect(figure).toHaveAttribute("aria-label", "Counts")
    expect(figure.querySelector("figcaption")?.textContent).toBe("Counts")
    // A fragment around the axes used to vanish; Recharts only sees direct children.
    expect(figure.querySelector(".recharts-xAxis")).not.toBeNull()
    expect(figure.querySelector(".recharts-yAxis")).not.toBeNull()
  })

  it("keeps the data table behind a tab so the plot is the first view", async () => {
    const user = userEvent.setup()
    render(<TranscriptChart spec={spec()} />)
    const figure = screen.getByTestId("transcript-chart")
    expect(screen.getByRole("tab", { name: "Chart" })).toHaveAttribute(
      "data-state",
      "active",
    )
    expect(screen.getByRole("tab", { name: "Table" })).toHaveAttribute(
      "data-state",
      "inactive",
    )
    expect(figure.querySelector("svg")).not.toBeNull()
    expect(screen.queryByRole("table")).not.toBeInTheDocument()

    await user.click(screen.getByRole("tab", { name: "Table" }))
    expect(screen.getByRole("table", { name: "Counts" })).toBeVisible()
    expect(screen.getByRole("cell", { name: "a" })).toBeVisible()
    expect(screen.getByRole("cell", { name: "3" })).toBeVisible()
    expect(figure.querySelector("svg")).toBeNull()

    await user.click(screen.getByRole("tab", { name: "Chart" }))
    expect(figure.querySelector("svg")).not.toBeNull()
    expect(screen.queryByRole("table")).not.toBeInTheDocument()
  })

  it("paints with theme tokens so a light/dark switch restyles the same figure", () => {
    expect(CHART_SERIES_PAINT).toEqual([
      "hsl(var(--chart-1))",
      "hsl(var(--chart-2))",
      "hsl(var(--chart-3))",
      "hsl(var(--chart-4))",
      "hsl(var(--chart-5))",
    ])
    render(<TranscriptChart spec={spec()} />)
    const svg = screen.getByTestId("transcript-chart").querySelector("svg")?.innerHTML ?? ""
    expect(svg).toMatch(/var\(--muted-foreground\)/)
    expect(svg).toMatch(/var\(--border\)/)
    expect(svg).not.toMatch(/#[0-9a-fA-F]{3,8}/)
    expect(svg).not.toMatch(/rgb\(/)
  })

  it("falls back to a generic name when the spec has no title", () => {
    render(<TranscriptChart spec={spec({ title: "" })} />)
    expect(screen.getByTestId("transcript-chart")).toHaveAttribute(
      "aria-label",
      "Chart",
    )
    expect(
      screen.getByTestId("transcript-chart").querySelector("figcaption"),
    ).toBeNull()
  })

  it("renders each mark type without throwing", () => {
    for (const type of ["bar", "line", "area", "pie"] as const) {
      const { unmount } = render(
        <TranscriptChart spec={spec({ type, y: type === "pie" ? ["n"] : ["n"] })} />,
      )
      expect(screen.getByTestId("transcript-chart")).toBeInTheDocument()
      unmount()
    }
  })
})

describe("ChartPending", () => {
  it("is a placeholder a live fence can sit in until the JSON closes", () => {
    render(<ChartPending />)
    expect(screen.getByTestId("transcript-chart-pending")).toBeInTheDocument()
    expect(screen.getByText("Drawing chart")).toBeInTheDocument()
  })
})
