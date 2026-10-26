import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { readFileSync } from "node:fs"
import { expect, it, vi } from "vitest"
import { Composer } from "./composer"

for (const command of ["/goal Inspect the assigned operation", "检查 assigned operation /goal", "／goal继续检查", "/plan Inspect the operation"]) {
  for (const action of ["Send", "Steer"]) {
    it(`keeps the quoted ${command} draft before ${action} can become ordinary chat`, async () => {
      const submit = vi.fn()
      const steer = vi.fn()
      const quotes = vi.fn()
      render(<Composer label="Message" sendLabel="Send" steerLabel="Steer" onSubmit={submit} onSteer={steer}
        quotes={["original selected context"]} onQuotesChange={quotes} />)
      fireEvent.change(screen.getByLabelText("Message"), { target: { value: command } })
      fireEvent.click(screen.getByRole("button", { name: action }))
      expect(submit).not.toHaveBeenCalled()
      expect(steer).not.toHaveBeenCalled()
      expect(quotes).not.toHaveBeenCalled()
      expect(screen.getByLabelText("Message")).toHaveValue(command)
      expect(screen.getByTestId("quote-draft")).toHaveValue("original selected context")
      expect(screen.getByRole("alert")).toHaveTextContent(/text only|只接受纯文本/)
    })
  }
}

const fixture = JSON.parse(readFileSync("../internal/slash/testdata/trailing-goal.json", "utf8")) as { text: string; objective?: string }[]
for (const row of fixture) {
  it(`uses the Go command corpus before wrapping quoted input: ${row.text}`, () => {
    const submit = vi.fn()
    render(<Composer label="Message" sendLabel="Send" onSubmit={submit} quotes={["context containing a literal /goal"]} />)
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: row.text } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    if (row.objective !== undefined) {
      expect(submit).not.toHaveBeenCalled()
      expect(screen.getByRole("alert")).toBeVisible()
      expect(screen.getByLabelText("Message")).toHaveValue(row.text)
    } else {
      expect(submit).toHaveBeenCalledOnce()
      expect(submit.mock.calls[0]?.[0]).toContain(row.text.trim())
      expect(screen.queryByRole("alert")).toBeNull()
    }
  })
}

it("removes the refusal only after removing the quote, then sends the complete original goal once", async () => {
  const submit = vi.fn()
  function Draft() {
    const [quotes, setQuotes] = useState(["original context"])
    return <Composer label="Message" sendLabel="Send" onSubmit={submit} quotes={quotes} onQuotesChange={setQuotes} />
  }
  render(<Draft />)
  const input = screen.getByLabelText("Message")
  const command = "/goal Inspect the full assigned operation"
  fireEvent.change(input, { target: { value: command } })
  fireEvent.submit(input.closest("form")!)
  expect(submit).not.toHaveBeenCalled()
  expect(screen.getByRole("alert")).toBeVisible()
  fireEvent.click(screen.getByRole("button", { name: /Remove quote|移除引用/ }))
  expect(screen.queryByRole("alert")).toBeNull()
  expect(input).toHaveValue(command)
  fireEvent.submit(input.closest("form")!)
  await waitFor(() => expect(submit).toHaveBeenCalledOnce())
  expect(submit).toHaveBeenCalledWith(command)
  expect(input).toHaveValue("")
})

it("keeps file bytes and quotations when a command is refused before staging uploads", () => {
  const submit = vi.fn()
  const file = new File([new Uint8Array([0, 255, 10])], "original.bin")
  render(<Composer label="Message" sendLabel="Send" onSubmit={submit} quotes={["original context"]} />)
  fireEvent.change(screen.getByLabelText("Message"), { target: { value: "/goal Inspect" } })
  fireEvent.change(screen.getByTestId("file-input"), { target: { files: [file] } })
  fireEvent.click(screen.getByRole("button", { name: "Send" }))
  expect(submit).not.toHaveBeenCalled()
  expect(screen.getByRole("button", { name: /Remove original.bin|移除original.bin/ })).toBeVisible()
  expect(screen.getByTestId("file-input")).toHaveProperty("files", [file])
})

for (const text of ["Explain the selection", "/goals is a different word", "/goal.md", "https://example.invalid/goal"]) {
  it(`preserves ordinary quoted messages: ${text}`, () => {
    const submit = vi.fn()
    render(<Composer label="Message" sendLabel="Send" onSubmit={submit} quotes={["/goal in the selected source is literal"]} />)
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: text } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(submit).toHaveBeenCalledWith(expect.stringContaining(`<user_request>\n${text}\n</user_request>`))
    expect(screen.queryByRole("alert")).toBeNull()
  })
}
