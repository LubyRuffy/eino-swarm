import { describe, expect, it } from "vitest"

import {
  canOpenTerminal,
  MAX_TERMINALS,
  terminalShortcut,
  terminalSocketURL,
  terminalTabLabel,
  terminalTarget,
} from "./terminal"

describe("terminalTarget", () => {
  it("prefers the open conversation over a selected project", () => {
    expect(terminalTarget("th_1", "pj_1")).toEqual({ threadId: "th_1" })
    expect(terminalTarget(undefined, "pj_1")).toEqual({ projectId: "pj_1" })
    expect(terminalTarget()).toBeUndefined()
  })
})

describe("canOpenTerminal", () => {
  it("needs a conversation or a project", () => {
    expect(canOpenTerminal()).toBe(false)
    expect(canOpenTerminal({})).toBe(false)
    expect(canOpenTerminal({ threadId: "th_1" })).toBe(true)
    expect(canOpenTerminal({ projectId: "pj_1" })).toBe(true)
  })
})

describe("terminalSocketURL", () => {
  const loc = { protocol: "http:", host: "127.0.0.1:8787" }

  it("names the conversation, not a path", () => {
    const url = terminalSocketURL({ threadId: "th_1" }, 80, 24, loc)
    expect(url).toBe(
      "ws://127.0.0.1:8787/api/threads/th_1/terminal?cols=80&rows=24",
    )
    expect(url).not.toMatch(/cwd=/)
    expect(url).not.toMatch(/workdir=/)
  })

  it("falls back to the project when there is no conversation", () => {
    expect(terminalSocketURL({ projectId: "pj_1" }, 40, 12, loc)).toBe(
      "ws://127.0.0.1:8787/api/projects/pj_1/terminal?cols=40&rows=12",
    )
  })

  it("uses wss on https", () => {
    expect(
      terminalSocketURL({ threadId: "th_1" }, 80, 24, {
        protocol: "https:",
        host: "127.0.0.1:443",
      }),
    ).toMatch(/^wss:\/\//)
  })

  it("does not let the client pick a working directory", () => {
    expect(() => terminalSocketURL({}, 80, 24, loc)).toThrow(/conversation or a project/)
  })
})

describe("terminalTabLabel", () => {
  it("uses the last path segment so two repos are distinguishable", () => {
    expect(terminalTabLabel("/Users/me/src/zwai", "Terminal 1")).toBe("zwai")
    expect(terminalTabLabel("/tmp/ws/", "Terminal 2")).toBe("ws")
    expect(terminalTabLabel(undefined, "Terminal 1")).toBe("Terminal 1")
  })
})

describe("terminalShortcut", () => {
  it("is ⌘J without shift, matching Codex", () => {
    expect(
      terminalShortcut({
        key: "j",
        metaKey: true,
        ctrlKey: false,
        shiftKey: false,
        altKey: false,
      }),
    ).toBe(true)
    expect(
      terminalShortcut({
        key: "j",
        metaKey: true,
        ctrlKey: false,
        shiftKey: true,
        altKey: false,
      }),
    ).toBe(false)
    expect(MAX_TERMINALS).toBe(8)
  })
})
