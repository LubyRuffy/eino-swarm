import { describe, expect, it } from "vitest"

import {
  commandNeedsArgument,
  compactHint,
  contextHint,
  filterSlashCommands,
  nextSlashIndex,
  normalizeSlashPrefix,
  parseSlashSubmit,
  slashDraft,
  SLASH_COMMANDS,
  withSlashHints,
} from "./slash"

describe("slashDraft", () => {
  it("opens on a slash token at the caret, including after existing text", () => {
    expect(slashDraft("")).toBeNull()
    expect(slashDraft("hello")).toBeNull()
    expect(slashDraft("/")).toEqual({ query: "", start: 0, end: 1 })
    expect(slashDraft("/go")).toEqual({ query: "go", start: 0, end: 3 })
    expect(slashDraft("/goal")).toEqual({ query: "goal", start: 0, end: 5 })
    expect(slashDraft("/goal ")).toBeNull()
    expect(slashDraft("hello /")).toEqual({ query: "", start: 6, end: 7 })
    expect(slashDraft("hello /go")).toEqual({ query: "go", start: 6, end: 9 })
    expect(slashDraft(" /goal")).toEqual({ query: "goal", start: 1, end: 6 })
    expect(slashDraft("字/")).toEqual({ query: "", start: 1, end: 2 })
    expect(slashDraft("foo/bar")).toBeNull()
    expect(slashDraft("https://")).toBeNull()
  })

  it("opens from the IME punctuation the Slash key emits in CJK mode", () => {
    expect(slashDraft("、")).toEqual({ query: "", start: 0, end: 1 })
    expect(slashDraft("、go")).toEqual({ query: "go", start: 0, end: 3 })
    expect(slashDraft("／")).toEqual({ query: "", start: 0, end: 1 })
    expect(slashDraft("字、")).toEqual({ query: "", start: 1, end: 2 })
  })

  it("closes once a known command already has its argument, even without a space", () => {
    expect(slashDraft("/goalkeep")).toEqual({ query: "goalkeep", start: 0, end: 9 })
    expect(slashDraft("/goal持续推进")).toBeNull()
    expect(slashDraft("／goal持续推进")).toBeNull()
    expect(slashDraft("、goal持续推进")).toBeNull()
    expect(slashDraft("hello /goal持续推进")).toBeNull()
  })
})

describe("normalizeSlashPrefix", () => {
  it("rewrites IME slash runes so the box shows the catalog prefix", () => {
    expect(normalizeSlashPrefix("")).toBe("")
    expect(normalizeSlashPrefix("/go")).toBe("/go")
    expect(normalizeSlashPrefix("、")).toBe("/")
    expect(normalizeSlashPrefix("、go")).toBe("/go")
    expect(normalizeSlashPrefix("／goal")).toBe("/goal")
    expect(normalizeSlashPrefix("hello")).toBe("hello")
    expect(normalizeSlashPrefix("hello 、")).toBe("hello /")
    expect(normalizeSlashPrefix("字、")).toBe("字/")
  })
})

describe("filterSlashCommands", () => {
  it("lists every command for a bare slash", () => {
    const names = filterSlashCommands("").map((c) => c.name)
    expect(names).toEqual(["goal", "plan", "compact"])
  })

  it("filters by prefix without requiring a particular sample task", () => {
    expect(filterSlashCommands("goa").map((c) => c.id)).toEqual(["goal"])
    expect(filterSlashCommands("pla").map((c) => c.id)).toEqual(["plan"])
    expect(filterSlashCommands("comp").map((c) => c.id)).toEqual(["compact"])
    expect(filterSlashCommands("nope")).toEqual([])
  })
})

describe("parseSlashSubmit", () => {
  it("reads a command name and optional argument", () => {
    expect(parseSlashSubmit("/compact")).toEqual({ id: "compact", arg: "" })
    expect(parseSlashSubmit("  /compact  ")).toEqual({ id: "compact", arg: "" })
    expect(parseSlashSubmit("/goal")).toEqual({ id: "goal", arg: "" })
    expect(parseSlashSubmit("/plan")).toEqual({ id: "plan", arg: "" })
    expect(parseSlashSubmit("/plan keep going")).toEqual({
      id: "plan",
      arg: "keep going",
    })
    expect(parseSlashSubmit("/goal keep going")).toEqual({
      id: "goal",
      arg: "keep going",
    })
    expect(parseSlashSubmit("/GOAL Keep going")).toEqual({
      id: "goal",
      arg: "Keep going",
    })
    expect(parseSlashSubmit("/plan inspect then change")).toEqual({
      id: "plan",
      arg: "inspect then change",
    })
    expect(parseSlashSubmit("/nope")).toBeNull()
    expect(parseSlashSubmit("goal")).toBeNull()
  })

  it("still reads the command when the objective is glued on without a space", () => {
    expect(parseSlashSubmit("/goal持续推进")).toEqual({
      id: "goal",
      arg: "持续推进",
    })
    expect(parseSlashSubmit("/GOAL持续推进")).toEqual({
      id: "goal",
      arg: "持续推进",
    })
    expect(parseSlashSubmit("／goal keep going")).toEqual({
      id: "goal",
      arg: "keep going",
    })
    expect(parseSlashSubmit("、goal keep going")).toEqual({
      id: "goal",
      arg: "keep going",
    })
    expect(parseSlashSubmit("hello /goal keep going")).toEqual({
      id: "goal",
      arg: "keep going",
    })
    expect(parseSlashSubmit("see foo/bar")).toBeNull()
    expect(parseSlashSubmit("/goals")).toBeNull()
    expect(parseSlashSubmit("/compacted")).toBeNull()
  })
})

describe("contextHint", () => {
  it("is a percentage of the configured budget", () => {
    expect(contextHint(40_000, 80_000)).toBe("50% full")
    expect(contextHint(0, 80_000)).toBe("0% full")
    expect(contextHint(100, 0)).toBeUndefined()
  })
})

describe("withSlashHints", () => {
  it("attaches a live hint without inventing commands", () => {
    const [compact] = withSlashHints(SLASH_COMMANDS, {
      compact: "12% full",
    }).filter((c) => c.id === "compact")
    expect(compact.hint).toBe("12% full")
    expect(SLASH_COMMANDS.find((c) => c.id === "compact")?.hint).toBeUndefined()
  })
})

describe("compactHint", () => {
  it("prefers the token window, then the char budget", () => {
    expect(compactHint(40, 80, 1, 100)).toBe("50% full")
    expect(compactHint(0, 0, 20, 80)).toBe("25% full")
    expect(compactHint(0, 0, 0, 0)).toBeUndefined()
    expect(compactHint(0, 80, 0, 100)).toBeUndefined()
    expect(compactHint(0, 0, 0, 80)).toBeUndefined()
  })
})

describe("commandNeedsArgument", () => {
  it("goal and plan wait for more text", () => {
    expect(commandNeedsArgument("goal")).toBe(true)
    expect(commandNeedsArgument("plan")).toBe(true)
    expect(commandNeedsArgument("compact")).toBe(false)
  })
})

describe("nextSlashIndex", () => {
  it("clamps to the visible rows", () => {
    expect(nextSlashIndex(0, 2, 1)).toBe(1)
    expect(nextSlashIndex(1, 2, 1)).toBe(1)
    expect(nextSlashIndex(0, 2, -1)).toBe(0)
    expect(nextSlashIndex(3, 0, 1)).toBe(0)
  })
})
