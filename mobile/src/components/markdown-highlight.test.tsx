import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PhoneMarkdown } from "./markdown"

describe("phone fenced source", () => {
  afterEach(() => vi.unstubAllGlobals())

  it.each(["cpp", "c++", "CXX"])("colours a %s fence through the existing C dialect", (lang) => {
    const source = 'const char* title = "value"; // note'
    render(<PhoneMarkdown text={`\`\`\`${lang}\n${source}\n\`\`\``} />)
    const code = screen.getByTestId("markdown-code").querySelector("code")!
    expect(code.textContent).toBe(source)
    expect(code.querySelector(".text-syntax-keyword")).toHaveTextContent("const")
    expect(code.querySelector(".text-syntax-string")).toHaveTextContent('"value"')
    expect(code.querySelector(".text-syntax-comment")).toHaveTextContent("// note")
  })

  it.each(["", "unknown", "text"])("keeps an unrecognised %s fence plain", (lang) => {
    const source = "const int count = 2;"
    render(<PhoneMarkdown text={`\`\`\`${lang}\n${source}\n\`\`\``} />)
    const code = screen.getByTestId("markdown-code").querySelector("code")!
    expect(code.textContent).toBe(source)
    expect(code.querySelector('[class*="text-syntax-"]')).toBeNull()
  })

  it("copies the original whitespace and markup without copying token or header text", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    const source = '\tconst char* title = "<div>汉字</div>"; // keep  \n\nreturn 0;  \n'
    render(<PhoneMarkdown text={`\`\`\`cpp\n${source}\n\`\`\``} />)
    const code = screen.getByTestId("markdown-code").querySelector("code")!
    expect(code.textContent).toBe(source)
    expect(code.querySelector("div")).toBeNull()
    expect(code.querySelector(".text-syntax-string")).toHaveTextContent("汉字")
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
    expect(code.querySelector(".text-syntax-keyword")).toBeTruthy()
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

  it("leaves inline source uncoloured", () => {
    render(<PhoneMarkdown text="Use `const int count = 2;` inline." />)
    const code = document.querySelector("code")!
    expect(code.textContent).toBe("const int count = 2;")
    expect(code.querySelector('[class*="text-syntax-"]')).toBeNull()
    expect(screen.queryByTestId("markdown-code")).not.toBeInTheDocument()
  })
})
