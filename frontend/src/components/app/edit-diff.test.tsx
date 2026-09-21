import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { EditCountMarks, EditDiffView } from "./edit-diff"
import type { EditDiff } from "@/lib/edit-diff"

function diff(partial: Partial<EditDiff> & Pick<EditDiff, "hunks">): EditDiff {
  return { path: "pkg/alpha.go", added: 0, removed: 0, ...partial }
}

describe("EditCountMarks", () => {
  it("stays the row colour until hover, then splits add from delete", () => {
    render(<EditCountMarks added={8} removed={2} />)
    expect(screen.getByText("+8")).toHaveClass("group-hover:text-diff-add")
    expect(screen.getByText("+8")).toHaveClass("group-focus-within:text-diff-add")
    expect(screen.getByText("+8")).not.toHaveClass("text-diff-add")
    expect(screen.getByText("−2")).toHaveClass("group-hover:text-diff-del")
    expect(screen.getByText("−2")).toHaveClass("group-focus-within:text-diff-del")
    expect(screen.getByText("−2")).not.toHaveClass("text-diff-del")
  })

  it("omits a zero side and the whole mark when nothing changed", () => {
    const { rerender, container } = render(<EditCountMarks added={2} removed={0} />)
    expect(screen.getByText("+2")).toBeInTheDocument()
    expect(screen.queryByText(/^−/)).toBeNull()
    rerender(<EditCountMarks added={0} removed={1} />)
    expect(screen.queryByText(/^\+/)).toBeNull()
    expect(screen.getByText("−1")).toBeInTheDocument()
    rerender(<EditCountMarks added={0} removed={0} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe("EditDiffView header", () => {
  it("keeps plus/minus counts off the path so a long name cannot eat them", () => {
    render(
      <EditDiffView
        diff={diff({
          added: 1,
          removed: 1,
          hunks: [{ lines: [{ op: "del", text: "return 0" }, { op: "add", text: "return 1" }] }],
        })}
      />,
    )
    const header = screen.getByTestId("file-diff").querySelector("p")
    expect(header).toHaveTextContent("pkg/alpha.go")
    expect(screen.getByTestId("edit-counts")).toHaveTextContent("+1")
    expect(screen.getByTestId("edit-counts")).toHaveTextContent("−1")
  })
})
