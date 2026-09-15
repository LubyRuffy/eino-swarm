import { describe, expect, it } from "vitest"

import { summariseToolCall, viewTool } from "./tool-view"

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
})
