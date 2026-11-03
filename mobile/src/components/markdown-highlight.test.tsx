import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { loadHighlighter } from "../../../frontend/src/lib/highlighter"
import { PhoneMarkdown } from "./markdown"

describe("phone fenced source", () => {
  // C++ is a large grammar. Load it before the assertions so a busy test
  // run does not spend the case timeout on the first compile.
  beforeAll(async () => {
    await loadHighlighter("cpp")
  }, 20000)

  afterEach(() => vi.unstubAllGlobals())

  it.each(["cpp", "c++", "CXX"])("colours a %s fence", async (lang) => {
    const source = 'const char* title = "value"; // note'
    render(<PhoneMarkdown text={`\`\`\`${lang}\n${source}\n\`\`\``} />)
    const code = screen.getByTestId("markdown-code").querySelector("code")!
    expect(code.textContent).toBe(source)
    await waitFor(() => expect(code.querySelector("[style*='--shiki-light']")).toBeTruthy())
    expect(code.textContent).toContain("const")
    expect(code.textContent).toContain('"value"')
    expect(code.textContent).toContain("// note")
    expect(code.querySelector("div")).toBeNull()
  })

  it.each(["", "unknown", "text"])("keeps an unrecognised %s fence plain", async (lang) => {
    const source = "const int count = 2;"
    render(<PhoneMarkdown text={`\`\`\`${lang}\n${source}\n\`\`\``} />)
    const code = screen.getByTestId("markdown-code").querySelector("code")!
    expect(code.textContent).toBe(source)
    await waitFor(() => expect(screen.getByRole("button", { name: "Copy code" })).toBeEnabled())
    expect(code.querySelector("[class*='--shiki']")).toBeNull()
  })

  it("copies the original whitespace and markup without copying token or header text", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    const source = '\tconst char* title = "<div>汉字</div>"; // keep  \n\nreturn 0;  \n'
    render(<PhoneMarkdown text={`\`\`\`cpp\n${source}\n\`\`\``} />)
    const code = screen.getByTestId("markdown-code").querySelector("code")!
    expect(code.textContent).toBe(source)
    expect(code.querySelector("div")).toBeNull()
    await waitFor(() => expect(code.textContent).toContain("汉字"))
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(source))
  })

  it("keeps the fence and its copy state while a stream appends and then closes it", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    const first = "const int count = 1;"
    const { rerender } = render(<PhoneMarkdown text={`\`\`\`cpp\n${first}`} />)
    const block = screen.getByTestId("markdown-code")
    const code = block.querySelector("code")!
    const button = screen.getByRole("button", { name: "Copy code" })
    await waitFor(() => expect(code.querySelector("[style*='--shiki']")).toBeTruthy())
    fireEvent.click(button)
    await waitFor(() => expect(button).toHaveAttribute("title", "Copied"))

    const final = first + "\nreturn count;"
    rerender(<PhoneMarkdown text={`\`\`\`cpp\n${final}`} />)
    expect(screen.getByTestId("markdown-code")).toBe(block)
    expect(block.querySelector("code")).toBe(code)
    expect(code.textContent).toBe(final)
    expect(button).toHaveAttribute("title", "Copied")

    rerender(<PhoneMarkdown text={`\`\`\`cpp\n${final}\n\`\`\`\n\nFollowing text.`} />)
    expect(block.querySelector("code")).toBe(code)
    expect(code.textContent).toBe(final)
    expect(screen.getByRole("button", { name: "Copy code" })).toBe(button)
    expect(screen.getByText("Following text.")).toBeInTheDocument()
    fireEvent.click(button)
    await waitFor(() => expect(writeText).toHaveBeenLastCalledWith(final))
  })

  it("colours the packaged fence tokens the native probe looks up", async () => {
    const source = [
      "// Offline sample",
      'const char* title = "signal";',
      "int count = 7;",
      "if (count > 0) return count;",
      'const char* detail = "a deliberately long line stays inside a horizontally scrollable code panel";',
    ].join("\n")
    render(<PhoneMarkdown text={`\`\`\`cpp\n${source}\n\`\`\``} />)
    const code = screen.getByTestId("markdown-code").querySelector("code")!
    expect(code.textContent).toBe(source)
    await waitFor(() => expect(code.querySelector("[style*='--shiki-light']")).toBeTruthy())
    const styled = [...code.querySelectorAll("span")].filter((node) =>
      node.getAttribute("style")?.includes("--shiki-light"),
    )
    const styleOf = (pred: (text: string) => boolean) =>
      styled.find((node) => pred(node.textContent ?? ""))?.getAttribute("style") ?? ""
    const keyword = styleOf((text) => text === "const")
    const string = styleOf((text) => text.includes("signal"))
    const comment = styleOf((text) => text.includes("Offline"))
    const operator = styleOf((text) => text === ">" || text === "return")
    expect(keyword).toContain("--shiki-light")
    expect(string).toContain("--shiki-light")
    expect(comment).toContain("--shiki-light")
    expect(operator).toContain("--shiki-light")
    expect(keyword).not.toBe(string)
    expect(keyword).not.toBe(comment)
  })

  it("leaves inline source uncoloured", () => {
    render(<PhoneMarkdown text="Use `const int count = 2;` inline." />)
    const code = document.querySelector("code")!
    expect(code.textContent).toBe("const int count = 2;")
    expect(code.querySelector('[class*="text-syntax-"]')).toBeNull()
    expect(screen.queryByTestId("markdown-code")).not.toBeInTheDocument()
  })
})
