import { describe, expect, it } from "vitest"

import { foldClientEntries, splitOpeningRequest } from "./client-fold"

const user = { role: "user", text: "the request" }
const thought = { role: "thinking", text: "checked the path" }
const tool = { role: "tool", text: "read" }
const tool2 = { role: "tool", text: "grep" }
const answer = { role: "assistant", text: "done" }

describe("foldClientEntries", () => {
  it("folds adjacent thinking and tools and keeps the reply outside", () => {
    const rows = foldClientEntries([user, thought, tool, tool2, answer], "user")
    expect(rows.map((r) => r.type)).toEqual(["entry", "work", "entry"])
    expect(rows[1]).toMatchObject({
      type: "work",
      entries: [thought, tool, tool2],
    })
  })

  it("splits the fold when a reply sits between tool runs", () => {
    const rows = foldClientEntries([user, tool, answer, tool2], "user")
    expect(rows.filter((r) => r.type === "work")).toHaveLength(2)
  })

  it("leaves every row visible in developer view", () => {
    const rows = foldClientEntries([thought, tool], "developer")
    expect(rows.every((r) => r.type === "entry")).toBe(true)
  })
})

describe("splitOpeningRequest", () => {
  it("keeps the opening user lines out of the scroll", () => {
    const later = { role: "user", text: "a follow-up" }
    const split = splitOpeningRequest([user, thought, tool, later, answer])
    expect(split.opening).toEqual([user])
    expect(split.rest.map((e) => e.text)).toEqual([
      thought.text,
      tool.text,
      later.text,
      answer.text,
    ])
  })
})
