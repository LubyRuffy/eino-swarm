import { describe, expect, it } from "vitest"

import { closeIncompleteMarkdown } from "./stream-markdown"

describe("closeIncompleteMarkdown", () => {
  it("leaves a complete heading and paragraph alone", () => {
    const src = "## Result\n\nstill writing"
    expect(closeIncompleteMarkdown(src)).toBe(src)
  })

  it("closes trailing bold so it renders instead of flashing asterisks", () => {
    expect(closeIncompleteMarkdown("see **alpha")).toBe("see **alpha**")
  })

  it("does not double-close already matched bold", () => {
    expect(closeIncompleteMarkdown("see **alpha** now")).toBe("see **alpha** now")
  })

  it("closes trailing inline code and strikethrough", () => {
    expect(closeIncompleteMarkdown("run `ls")).toBe("run `ls`")
    expect(closeIncompleteMarkdown("drop ~~old")).toBe("drop ~~old~~")
  })

  it("closes nested bold then strike in reverse order", () => {
    expect(closeIncompleteMarkdown("**keep ~~this")).toBe("**keep ~~this~~**")
  })

  it("closes *** as a single marker", () => {
    expect(closeIncompleteMarkdown("***both")).toBe("***both***")
  })

  it("does not close a marker that has no content yet", () => {
    expect(closeIncompleteMarkdown("wait **")).toBe("wait **")
    expect(closeIncompleteMarkdown("`")).toBe("`")
  })

  it("leaves an unclosed fence alone so the rest stays a code block", () => {
    const src = "intro\n\n```js\nconst x = 1"
    expect(closeIncompleteMarkdown(src)).toBe(src)
  })

  it("does not rewrite ** inside a closed fence", () => {
    const src = "```\n**not bold**\n```\n\nafter"
    expect(closeIncompleteMarkdown(src)).toBe(src)
  })

  it("still closes bold after a closed fence", () => {
    expect(closeIncompleteMarkdown("```\ncode\n```\n\n**after")).toBe(
      "```\ncode\n```\n\n**after**",
    )
  })

  it("ignores escaped markers", () => {
    expect(closeIncompleteMarkdown("a \\*\\* b")).toBe("a \\*\\* b")
  })

  it("does not treat a single asterisk as italic — lists and multiply would lie", () => {
    expect(closeIncompleteMarkdown("* item")).toBe("* item")
    expect(closeIncompleteMarkdown("3 * 4")).toBe("3 * 4")
  })
})
