import { describe, expect, it } from "vitest"

import {
  canOpenTerminal,
  MAX_TERMINALS,
  terminalShortcut,
  terminalSocketURL,
  terminalTabLabel,
  terminalTarget,
  terminalQuote,
  terminalSelectionRect,
} from "./terminal"
import { formatQuotedMessage, parseQuotedMessage } from "./quote"

describe("terminal quotations", () => {
  it("retains the actual terminal source through the existing send format", () => {
    const text = terminalQuote(" command\r\noutput ", {
      id: "term_2", threadId: "th_source", cwd: "/tmp/work \n", shell: "/bin/shell",
    })
    expect(text).toContain('Source: integrated terminal\n\ncommand\noutput\n\nSession: "term_2"')
    expect(text).toContain('Conversation: "th_source"')
    expect(text).toContain('Initial directory: "/tmp/work \\n"')
    expect(text).toContain('Shell: "/bin/shell"')
    expect(parseQuotedMessage(formatQuotedMessage([text], "Investigate"))).toEqual({
      quotes: [text], body: "Investigate",
    })
  })

  it("does not invent a command, current directory or unknown shell", () => {
    const text = terminalQuote("failure", { id: "term_1", projectId: "pj_source" })
    expect(text).toContain('Project: "pj_source"')
    expect(text).not.toMatch(/Command:|Current directory:|Initial directory:|Shell:/)
    expect(terminalQuote(" \r\n ", { id: "term_1" })).toBe("")
  })

  it("anchors the menu to visible terminal cells including wrapped selections", () => {
    const bounds = { top: 100, left: 20, width: 800, height: 240 }
    expect(terminalSelectionRect({ start: { x: 2, y: 10 }, end: { x: 8, y: 10 } }, 10, 80, 12, bounds))
      .toEqual({ top: 100, bottom: 120, left: 40, width: 60, height: 20 })
    expect(terminalSelectionRect({ start: { x: 2, y: 9 }, end: { x: 0, y: 13 } }, 10, 80, 12, bounds))
      .toEqual({ top: 100, bottom: 160, left: 20, width: 800, height: 60 })
    expect(terminalSelectionRect({ start: { x: 2, y: 10 }, end: { x: 0, y: 11 } }, 10, 80, 12, bounds))
      .toEqual({ top: 100, bottom: 120, left: 40, width: 780, height: 20 })
    expect(terminalSelectionRect({ start: { x: 0, y: 0 }, end: { x: 5, y: 1 } }, 10, 80, 12, bounds)).toBeNull()
    expect(terminalSelectionRect({ start: { x: 0, y: 30 }, end: { x: 5, y: 31 } }, 10, 80, 12, bounds)).toBeNull()
  })
})

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
