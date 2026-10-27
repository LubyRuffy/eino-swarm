import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { setLocale } from "@/lib/i18n"
import type { CompactBlock } from "@/lib/transcript"

import { renderBlock, ThreadLog } from "./thread-blocks"

describe("phone reasoning disclosure", () => {
  it("keeps reasoning bodies closed after opening a live or replay work group", () => {
    setLocale("en")
    for (const running of [false, true]) {
      const { unmount } = render(<ThreadLog running={running} blocks={[
        { id: "thought-a", kind: "reasoning", text: "Initial analysis", streaming: running },
        { id: "read-a", kind: "tool", toolName: "read", text: "Read result" },
        { id: "thought-b", kind: "reasoning", text: "Additional analysis", streaming: running },
      ]} />)
      const group = screen.getByTestId("work-fold")
      expect(group).toHaveAttribute("aria-expanded", "false")
      fireEvent.click(group)
      expect(group).toHaveAttribute("aria-expanded", "true")
      expect(screen.queryAllByTestId("phone-thought")).toHaveLength(0)
      const thoughts = screen.getAllByTestId("phone-thought-toggle")
      expect(thoughts).toHaveLength(2)
      thoughts.forEach((thought) => expect(thought).toHaveAttribute("aria-expanded", "false"))
      unmount()
    }
  })

  it("reveals the complete original reasoning only through its own toggle", () => {
    setLocale("en")
    const text = "First observation.\nA separate condition remains relevant."
    render(<>{renderBlock({ id: "reasoning", kind: "reasoning", text })}</>)
    expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
    const toggle = screen.getByTestId("phone-thought-toggle")
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByTestId("phone-thought").textContent).toBe(text)
    fireEvent.click(toggle)
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
  })

  it("preserves explicit open and closed choices through live updates and completion", () => {
    setLocale("en")
    for (const open of [false, true]) {
      const blocks = (text: string, streaming: boolean): CompactBlock[] => [
        { id: "same-thought", kind: "reasoning", text, streaming },
      ]
      const { rerender, unmount } = render(<ThreadLog running blocks={blocks("First observation", true)} />)
      fireEvent.click(screen.getByTestId("work-fold"))
      fireEvent.click(screen.getByTestId("phone-thought-toggle"))
      if (!open) fireEvent.click(screen.getByTestId("phone-thought-toggle"))

      rerender(<ThreadLog running blocks={blocks("First observation plus another condition", true)} />)
      expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", String(open))
      expect(screen.queryAllByTestId("phone-thought")).toHaveLength(open ? 1 : 0)
      if (open) expect(screen.getByTestId("phone-thought")).toHaveTextContent("plus another condition")

      rerender(<ThreadLog blocks={blocks("Complete reasoning retained after completion", false)} />)
      expect(screen.getByTestId("work-fold")).toHaveAttribute("aria-expanded", "true")
      expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", String(open))
      expect(screen.queryAllByTestId("phone-thought")).toHaveLength(open ? 1 : 0)
      if (open) expect(screen.getByTestId("phone-thought").textContent).toBe("Complete reasoning retained after completion")
      unmount()
    }
  })

  it("keeps different reasoning IDs independent when a new thought joins the group", () => {
    setLocale("en")
    const initial: CompactBlock[] = [
      { id: "first", kind: "reasoning", text: "First analysis" },
      { id: "second", kind: "reasoning", text: "Second analysis" },
    ]
    const { rerender } = render(<ThreadLog blocks={initial} />)
    fireEvent.click(screen.getByTestId("work-fold"))
    const thoughts = screen.getAllByTestId("phone-thought-toggle")
    thoughts.forEach((thought) => expect(thought).toHaveAttribute("aria-expanded", "false"))
    fireEvent.click(thoughts[0])
    fireEvent.click(thoughts[1])
    fireEvent.click(thoughts[0])
    expect(screen.getAllByTestId("phone-thought")).toHaveLength(1)
    expect(screen.getByTestId("phone-thought")).toHaveTextContent("Second analysis")

    rerender(<ThreadLog blocks={[
      ...initial,
      { id: "third", kind: "reasoning", text: "New analysis", streaming: true },
    ]} running />)
    const updated = screen.getAllByTestId("phone-thought-toggle")
    expect(updated).toHaveLength(3)
    expect(updated[0]).toHaveAttribute("aria-expanded", "false")
    expect(updated[1]).toHaveAttribute("aria-expanded", "true")
    expect(updated[2]).toHaveAttribute("aria-expanded", "false")
    expect(screen.getAllByTestId("phone-thought")).toHaveLength(1)
    expect(screen.getByTestId("phone-thought")).toHaveTextContent("Second analysis")
  })

  it("keeps a single thought closed without folding answers or changing tool disclosure", () => {
    setLocale("en")
    render(<>
      <ThreadLog blocks={[
        { id: "single", kind: "reasoning", text: "Private process detail" },
        { id: "answer", kind: "answer", text: "The final answer stays visible." },
      ]} />
      <div data-testid="independent-tool">
        {renderBlock({ id: "tool", kind: "tool", toolName: "read", text: "Tool body" })}
      </div>
    </>)
    expect(screen.getByText("The final answer stays visible.")).toBeVisible()
    expect(screen.getAllByTestId("work-fold")).toHaveLength(1)
    const tool = within(screen.getByTestId("independent-tool"))
    const toolToggle = tool.getByRole("button")
    expect(toolToggle).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(screen.getByTestId("work-fold"))
    expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
    expect(screen.getByText("The final answer stays visible.")).toBeVisible()
    expect(toolToggle).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(toolToggle)
    expect(toolToggle).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByTestId("phone-thought-toggle")).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByTestId("phone-thought")).not.toBeInTheDocument()
  })
})
