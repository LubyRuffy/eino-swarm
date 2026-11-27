import { describe, expect, it } from "vitest"
import { ringState, ringStates, todoFootprint, todosForSubject } from "./analysis"
import type { GraphLink, GraphSubject, GraphTodo } from "./types"

const subject = (id: string, status: GraphSubject["status"] = "lead", found_by_todo?: string): GraphSubject =>
  ({ id, kind: "ip", name: id, attrs: {}, status, version: 1, updated_at: "", found_by_todo })
const todo = (id: string, status: GraphTodo["status"], about?: string): GraphTodo =>
  ({ id, title: id, status, about, position: 0, version: 1 })

describe("ring state", () => {
  // The ring is what tells a person which subjects still need work, so it has
  // to be derivable from the to-do list alone: nothing writes an "analysed" flag.
  it("is unanalysed until a to-do is about the subject", () => {
    expect(ringState(subject("a"), [todo("t", "open", "b")], true)).toBe("unanalyzed")
  })

  it("waits while a to-do is open and flows only while a turn is running", () => {
    const todos = [todo("t1", "doing", "a"), todo("t2", "open", "a")]
    expect(ringState(subject("a"), todos, true)).toBe("doing")
    // A `doing` row left by a turn that is gone is not work in progress.
    expect(ringState(subject("a"), todos, false)).toBe("pending")
    expect(ringState(subject("a"), [todo("t2", "open", "a")], true)).toBe("pending")
  })

  it("is solid once every to-do is settled, including a failed one", () => {
    expect(ringState(subject("a"), [todo("t1", "done", "a"), todo("t2", "failed", "a")], true)).toBe("settled")
  })

  it("reopens when a new to-do appears for an already settled subject", () => {
    const settled = [todo("t1", "done", "a")]
    expect(ringState(subject("a"), settled, false)).toBe("settled")
    expect(ringState(subject("a"), [...settled, todo("t2", "open", "a")], false)).toBe("pending")
  })

  it("greys a retracted subject whatever its to-dos say", () => {
    expect(ringState(subject("a", "retracted"), [todo("t", "doing", "a")], true)).toBe("retracted")
  })

  it("derives a state for every subject", () => {
    const states = ringStates([subject("a"), subject("b")], [todo("t", "done", "a")], true)
    expect([...states]).toEqual([["a", "settled"], ["b", "unanalyzed"]])
  })
})

describe("what a to-do touched", () => {
  const subjects = [subject("a"), subject("b", "lead", "t1"), subject("c", "lead", "t2")]
  const links: GraphLink[] = [
    { id: "l1", from: "a", to: "d", rel: "owns", found_by_todo: "t1" },
    { id: "l2", from: "c", to: "a", rel: "owns", found_by_todo: "t2" },
  ]

  it("is the subject it is about plus what its settlement brought in", () => {
    expect([...todoFootprint(todo("t1", "done", "a"), subjects, links)].sort()).toEqual(["a", "b", "d"])
    expect([...todoFootprint(todo("t3", "open"), subjects, links)]).toEqual([])
  })

  it("lists to-dos that are about a subject or found it", () => {
    const todos = [todo("t1", "done", "a"), todo("t2", "open", "c"), todo("t9", "open", "z")]
    expect(todosForSubject(subjects[1], todos).map((row) => row.id)).toEqual(["t1"])
    expect(todosForSubject(subjects[2], todos).map((row) => row.id)).toEqual(["t2"])
  })
})
