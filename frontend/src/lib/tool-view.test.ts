import { describe, expect, it } from "vitest"

import { execCommand, summariseToolCall, toolRowSummary, viewTool } from "./tool-view"

describe("summariseToolCall", () => {
  it("shows the command, not the JSON envelope", () => {
    expect(summariseToolCall("exec", `{"command":"echo hi","cwd":"."}`)).toBe("echo hi")
    expect(summariseToolCall("exec", `{"command":"echo hi"}`)).not.toMatch(/[{"]/)
    expect(summariseToolCall("exec", `{"cwd":"."}`)).toBe("")
  })

  it("shows the search query, not the JSON envelope", () => {
    expect(summariseToolCall("web_search", `{"query":"alpha terms"}`)).toBe("alpha terms")
    expect(summariseToolCall("web_search", `{"query":"alpha terms"}`)).not.toContain("query")
  })

  it("joins a pattern with its path when both are present", () => {
    expect(summariseToolCall("grep", `{"pattern":"TODO","path":"src"}`)).toBe("TODO · src")
    expect(summariseToolCall("wait_agents", `{"timeout_s":60,"agent_ids":["a-1"]}`)).toBe("")
  })

  it("falls back to flattened text when args are not JSON", () => {
    expect(summariseToolCall("exec", "echo hi")).toBe("echo hi")
  })

  it("flattens a multiline command for the one-line summary", () => {
    expect(summariseToolCall("exec", JSON.stringify({ command: "echo hi\n&& ls" }))).toBe(
      "echo hi && ls",
    )
  })
})

describe("execCommand", () => {
  it("keeps newlines so expand can show the whole invocation", () => {
    expect(execCommand(JSON.stringify({ command: "cat <<END\nline\nEND", cwd: "." }))).toBe(
      "cat <<END\nline\nEND",
    )
  })

  it("is empty when there is no command", () => {
    expect(execCommand(`{"cwd":"."}`)).toBe("")
    expect(execCommand("")).toBe("")
  })
})

describe("viewTool", () => {
  it("surfaces a failed command in red-ready fields, not as pretty JSON", () => {
    const raw = JSON.stringify({
      exit_code: 127,
      stdout: "",
      stderr: "not found",
      failed: true,
      error: "exit status 127",
      full_command: "nope",
    })
    const view = viewTool("exec", `{"command":"nope"}`, raw)
    expect(view.summary).toBe("nope")
    expect(view.failed).toBe(true)
    expect(view.error).toBe("exit status 127")
    expect(view.body).toBe("not found")
    expect(view.body).not.toContain("full_command")
    expect(view.body).not.toContain("{")
  })

  it("shows stdout for a successful command", () => {
    const raw = JSON.stringify({ exit_code: 0, stdout: "ok\nline", stderr: "", failed: false })
    const view = viewTool("exec", `{"command":"echo ok"}`, raw)
    expect(view.failed).toBe(false)
    expect(view.body).toBe("ok\nline")
  })

  it("lists search hits instead of dumping JSON", () => {
    const raw = JSON.stringify({
      message: "ok",
      results: [
        { title: "One", url: "https://example.invalid/one", summary: "first hit" },
        { title: "Two", url: "https://example.invalid/two" },
      ],
    })
    const view = viewTool("web_search", `{"query":"alpha terms"}`, raw)
    expect(view.hits).toHaveLength(2)
    expect(view.hits?.[0]).toEqual({
      title: "One",
      url: "https://example.invalid/one",
      snippet: "first hit",
    })
    expect(view.body).toBe("")
  })

  it("treats an error: prefix as a failed call", () => {
    const view = viewTool("web_fetch", `{"url":"https://example.invalid"}`, "error: timed out")
    expect(view.failed).toBe(true)
    expect(view.error).toBe("timed out")
  })

  it("shows a memory write as the action and the note, not the JSON", () => {
    expect(summariseToolCall("memory", `{"action":"add","content":"User prefers terse replies"}`)).toBe(
      "add · User prefers terse replies",
    )
    const view = viewTool(
      "memory",
      `{"action":"add","content":"User prefers terse replies"}`,
      JSON.stringify({ success: true, changed: true, usage: "24/2200", entries: ["User prefers terse replies"] }),
    )
    expect(view.failed).toBe(false)
    expect(view.body).toContain("User prefers terse replies")
    expect(view.body).toContain("24/2200")
  })

  it("flags a refused memory write and lists what is already stored", () => {
    const view = viewTool(
      "memory",
      `{"action":"add","content":"another"}`,
      JSON.stringify({
        success: false,
        error: "memory is at 30/30 characters",
        current_entries: ["the one that is already there"],
      }),
    )
    expect(view.failed).toBe(true)
    expect(view.error).toContain("30/30")
    expect(view.body).toBe("the one that is already there")
  })

  it("shows a missing skill once, and lists the ones that exist", () => {
    const view = viewTool(
      "skill_view",
      `{"name":"absent"}`,
      JSON.stringify({
        success: false,
        error: 'no skill named "absent". skill_view only opens skills recorded in this project\'s memory',
        available: ["present"],
      }),
    )
    expect(view.failed).toBe(true)
    expect(view.error).toContain("absent")
    expect(view.body).toBe("present")
    expect(view.body).not.toContain("no skill named")
  })

  it("does not repeat a missing-skill error when nothing is recorded", () => {
    const view = viewTool(
      "skill_view",
      `{"name":"absent"}`,
      JSON.stringify({
        success: false,
        error: 'no skill named "absent". skill_view only opens skills recorded in this project\'s memory',
        available: [],
      }),
    )
    expect(view.failed).toBe(true)
    expect(view.error).toContain("absent")
    expect(view.body).toBe("")
  })
})

describe("toolRowSummary", () => {
  it("puts a memory refusal on the collapsed row, not just the note that did not land", () => {
    const view = viewTool(
      "memory",
      `{"action":"replace","content":"a longer status"}`,
      JSON.stringify({
        success: false,
        error: "memory is at 2200/2200 characters; this write exceeds the limit by 40. Do not retry the same write.",
        current_entries: ["the one that is already there"],
      }),
    )
    const row = toolRowSummary(view)
    expect(row).toContain("replace")
    expect(row).toContain("Do not retry")
    expect(row).toContain("2200/2200")
  })

  it("leaves a successful row as the action and the note", () => {
    const view = viewTool(
      "memory",
      `{"action":"add","content":"a durable fact"}`,
      JSON.stringify({ success: true, changed: true, usage: "14/2200", entries: ["a durable fact"] }),
    )
    expect(toolRowSummary(view)).toBe("add · a durable fact")
  })
})

describe("file change view", () => {
  it("puts plus/minus counts on an edit path, not the JSON envelope", () => {
    const view = viewTool(
      "edit",
      JSON.stringify({
        file_path: "pkg/alpha.go",
        search_block: "return 0",
        replace_block: "return 1",
      }),
      "ok: replaced block in pkg/alpha.go",
    )
    expect(view.summary).toContain("pkg/alpha.go")
    expect(view.summary).toMatch(/\+1/)
    expect(view.summary).toMatch(/−1/)
    expect(view.summary).not.toContain("search_block")
    expect(view.summary).not.toContain("{")
    expect(toolRowSummary(view)).toContain("+1")
    expect(view.diff?.added).toBe(1)
    expect(view.body).toBe("")
  })

  it("puts an added-line count on a write path, not the status sentence", () => {
    const view = viewTool(
      "write",
      JSON.stringify({
        file_path: "pkg/alpha.go",
        content: "package alpha\nfunc Alpha() {}\n",
      }),
      "Updated file pkg/alpha.go",
    )
    expect(view.summary).toContain("pkg/alpha.go")
    expect(view.summary).toMatch(/\+2/)
    expect(view.summary).not.toContain("content")
    expect(view.summary).not.toContain("{")
    expect(toolRowSummary(view)).toContain("+2")
    expect(view.diff?.added).toBe(2)
    expect(view.body).toBe("")
  })

  it("does not advertise +0 on an empty write, but still attaches the hunk", () => {
    const view = viewTool(
      "write",
      JSON.stringify({ file_path: "pkg/alpha.go", content: "" }),
      "Updated file pkg/alpha.go",
    )
    expect(view.summary).toBe("pkg/alpha.go")
    expect(view.summary).not.toMatch(/\+0/)
    expect(view.diff?.added).toBe(0)
    expect(view.diff?.hunks[0].lines).toEqual([])
  })

  it("attaches the hunk while the call is still pending", () => {
    const view = viewTool(
      "edit",
      JSON.stringify({
        file_path: "pkg/alpha.go",
        search_block: "return 0",
        replace_block: "return 1",
      }),
    )
    expect(view.diff?.added).toBe(1)
    expect(view.body).toBe("")
  })
})
