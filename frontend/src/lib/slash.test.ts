import { describe, expect, it } from "vitest"

import {
  commandNeedsArgument,
  compactHint,
  contextHint,
  filterSlashCommands,
  nextSlashIndex,
  parseSlashSubmit,
  slashDraft,
  SLASH_COMMANDS,
  withSlashHints,
} from "./slash"

describe("slashDraft", () => {
  it("opens only when the box starts with a slash and has no space yet", () => {
    expect(slashDraft("")).toBeNull()
    expect(slashDraft("hello")).toBeNull()
    expect(slashDraft("/")).toEqual({ query: "" })
    expect(slashDraft("/go")).toEqual({ query: "go" })
    expect(slashDraft("/goal")).toEqual({ query: "goal" })
    expect(slashDraft("/goal ")).toBeNull()
    expect(slashDraft(" /goal")).toBeNull()
  })

  it("closes once a known command already has its argument, even without a space", () => {
    expect(slashDraft("/goalkeep")).toEqual({ query: "goalkeep" })
    expect(slashDraft("/goal持续推进")).toBeNull()
    expect(slashDraft("／goal持续推进")).toBeNull()
  })
})

describe("filterSlashCommands", () => {
  it("lists every command for a bare slash", () => {
    const names = filterSlashCommands("").map((c) => c.name)
    expect(names).toEqual(["goal", "compact"])
  })

  it("filters by prefix without requiring a particular sample task", () => {
    expect(filterSlashCommands("g").map((c) => c.id)).toEqual(["goal"])
    expect(filterSlashCommands("comp").map((c) => c.id)).toEqual(["compact"])
    expect(filterSlashCommands("nope")).toEqual([])
  })
})

describe("parseSlashSubmit", () => {
  it("reads a command name and optional argument", () => {
    expect(parseSlashSubmit("/compact")).toEqual({ id: "compact", arg: "" })
    expect(parseSlashSubmit("  /compact  ")).toEqual({ id: "compact", arg: "" })
    expect(parseSlashSubmit("/goal")).toEqual({ id: "goal", arg: "" })
    expect(parseSlashSubmit("/goal keep going")).toEqual({
      id: "goal",
      arg: "keep going",
    })
    expect(parseSlashSubmit("/GOAL Keep going")).toEqual({
      id: "goal",
      arg: "Keep going",
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
  it("only goal waits for more text", () => {
    expect(commandNeedsArgument("goal")).toBe(true)
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
