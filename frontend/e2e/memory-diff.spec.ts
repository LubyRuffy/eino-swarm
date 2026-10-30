import { writeFile } from "node:fs/promises"
import { expect, test, type Page } from "@playwright/test"

import { reloadOpenConversation } from "./session"

type MemoryArgs = { action: "add" | "replace" | "remove"; content?: string; old_text?: string }
type MemoryResult = {
  success: boolean
  changed?: boolean
  before?: string
  after?: string
  error?: string
  current_entries?: string[]
}

const unrelated = "A different durable preference must stay out of the change preview."

/** Replay belongs to the recorded receipt, never to today's memory file. */
async function openMemory(page: Page, title: string, args: MemoryArgs, result: MemoryResult, live = false) {
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const base = {
    thread_id: thread.id, turn_id: "memory-diff-turn", agent_id: "manager",
    created_at: "2026-01-01T00:00:00.000Z",
  }
  const events = [
    { ...base, seq: 1, kind: "tool_call", tool_call_id: "memory-diff-call", text: `memory(${JSON.stringify(args)})` },
    { ...base, seq: 2, kind: "tool_result", tool_call_id: "memory-diff-call", text: JSON.stringify({ usage: "137/2200", entries: [unrelated, result.after ?? args.content ?? ""], ...result }) },
    { ...base, seq: 3, kind: "agent_message", text: "The memory operation was checked." },
    { ...base, seq: 4, kind: "done", text: "The memory operation was checked." },
  ]
  let liveReleased = !live, historyServed = false, logReads = 0, streams = 0
  let release!: () => void
  const liveReady = new Promise<void>((resolve) => { release = resolve })
  await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\?|$)`), async (route) => {
    logReads++
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, events: liveReleased ? events : [], roster: [], has_more: false } })
    historyServed = true
  })
  if (live) {
    await page.route(new RegExp(`/api/threads/${thread.id}/events(?:\\?|$)`), async (route) => {
      const first = ++streams === 1
      if (first) await liveReady
      const body = "retry: 3600000\n\n" + (first ? events.map((event) =>
        `id: ${event.seq}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`,
      ).join("") : "")
      await route.fulfill({ contentType: "text/event-stream", body })
    })
  }
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
  await expect.poll(() => historyServed).toBe(true)
  return {
    logReads: () => logReads,
    releaseLive: () => { liveReleased = true; release() },
  }
}

for (const live of [false, true]) {
  test(`memory replace shows its complete saved entry through ${live ? "named SSE" : "history"} and reload`, async ({ page }, info) => {
    const before = "Preferred delivery\n  retain the original evidence\nVerification remains required."
    const after = "Preferred delivery\n  retain the complete evidence\n  record the observed result\nVerification remains required."
    const replay = await openMemory(page, `Memory replace ${live ? "live" : "stored"} ${info.workerIndex}-${info.retry}`, {
      action: "replace", old_text: "original evidence", content: after,
    }, { success: true, changed: true, before, after }, live)
    let readsBeforeLive = 0
    if (live) {
      readsBeforeLive = replay.logReads()
      await expect(page.getByTestId("transcript").getByRole("button", { name: /^memory/ })).toHaveCount(0)
      replay.releaseLive()
    }
    for (let round = 0; round < 2; round++) {
      const transcript = page.getByTestId("transcript")
      const toggle = transcript.getByRole("button", { name: /^memory.*replace/ })
      await expect(toggle).toHaveAttribute("aria-expanded", "false")
      await expect(toggle.getByTestId("edit-counts")).toContainText("+2")
      await expect(toggle.getByTestId("edit-counts")).toContainText("−1")
      await expect(transcript.getByTestId("file-diff")).toHaveCount(0)
      await toggle.click()
      const diff = transcript.getByTestId("file-diff")
      await expect(diff).toBeVisible()
      await expect(diff).toContainText("MEMORY.md")
      expect(await diff.locator('[data-diff="del"] td:last-child').allTextContents()).toEqual(["  retain the original evidence"])
      expect(await diff.locator('[data-diff="add"] td:last-child').allTextContents()).toEqual(["  retain the complete evidence", "  record the observed result"])
      await expect(diff.locator('[data-diff="eq"] td:last-child')).toHaveText(["Preferred delivery", "Verification remains required."])
      await expect(transcript.getByTestId("tool-output")).toHaveCount(0)
      await expect(transcript).not.toContainText(unrelated)
      await expect(transcript).not.toContainText("137/2200")
      await expect(transcript).not.toContainText("Request preview")
      if (round === 0) {
        if (live) expect(replay.logReads()).toBe(readsBeforeLive)
        await writeFile(info.outputPath(`memory-replace-${live ? "live" : "history"}.png`), await page.screenshot(), { mode: 0o600 })
      }
      await toggle.click()
      await expect(toggle).toHaveAttribute("aria-expanded", "false")
      await expect(diff).toHaveCount(0)
      if (round === 0) await reloadOpenConversation(page)
    }
  })
}

for (const action of ["add", "remove"] as const) {
  test(`memory ${action} paints only the affected saved lines`, async ({ page }, info) => {
    const entry = "Durable preference\n  preserve the verification context"
    await openMemory(page, `Memory ${action} ${info.workerIndex}-${info.retry}`, action === "add"
      ? { action, content: entry }
      : { action, old_text: "verification context" }, {
      success: true, changed: true, before: action === "remove" ? entry : "", after: action === "add" ? entry : "",
    })
    const transcript = page.getByTestId("transcript")
    const toggle = transcript.getByRole("button", { name: new RegExp(`^memory.*${action}`) })
    await expect(toggle).toHaveAttribute("aria-expanded", "false")
    await expect(toggle.getByTestId("edit-counts")).toHaveText(action === "add" ? "+2" : "−2")
    await toggle.click()
    const diff = transcript.getByTestId("file-diff")
    const op = action === "add" ? "add" : "del"
    await expect(diff.locator(`[data-diff="${op}"] td:last-child`)).toHaveText(entry.split("\n"))
    expect(await diff.locator(`[data-diff="${op}"] td:last-child`).nth(1).textContent()).toBe("  preserve the verification context")
    await expect(diff.locator(`[data-diff="${op === "add" ? "del" : "add"}"]`)).toHaveCount(0)
    await expect(transcript.getByTestId("tool-output")).toHaveCount(0)
    await expect(transcript).not.toContainText(unrelated)
  })
}

test("a legacy memory replacement labels its matching-text preview", async ({ page }, info) => {
  await openMemory(page, `Legacy memory ${info.workerIndex}-${info.retry}`, {
    action: "replace", old_text: "old matching text", content: "replacement preference",
  }, { success: true, changed: true })
  const transcript = page.getByTestId("transcript")
  await transcript.getByRole("button", { name: /^memory.*replace/ }).click()
  await expect(transcript.getByText("Request preview; the complete previous entry was not recorded.", { exact: true })).toBeVisible()
  const diff = transcript.getByTestId("file-diff")
  await expect(diff.locator('[data-diff="del"] td:last-child')).toHaveText("old matching text")
  await expect(diff.locator('[data-diff="add"] td:last-child')).toHaveText("replacement preference")
  await expect(transcript.getByTestId("tool-output")).toHaveCount(0)
  await expect(transcript).not.toContainText(unrelated)
})

test("a refused memory replacement retains its error above the requested change", async ({ page }, info) => {
  const error = "No existing note matches old_text."
  await openMemory(page, `Refused memory ${info.workerIndex}-${info.retry}`, {
    action: "replace", old_text: "missing preference", content: "replacement preference",
  }, { success: false, error, current_entries: [unrelated] })
  const transcript = page.getByTestId("transcript")
  const toggle = transcript.getByRole("button", { name: /^memory.*replace/ })
  await expect(toggle).toHaveAttribute("aria-invalid", "true")
  await expect(toggle).toContainText(error)
  await toggle.click()
  await expect(transcript.getByRole("alert")).toHaveText(error)
  await expect(transcript.getByText("Requested change", { exact: true })).toBeVisible()
  await expect(transcript.getByTestId("file-diff").locator('[data-diff="del"] td:last-child')).toHaveText("missing preference")
  await expect(transcript.getByTestId("file-diff").locator('[data-diff="add"] td:last-child')).toHaveText("replacement preference")
  await toggle.click()
  await expect(toggle).toContainText(error)
  await expect(transcript.getByRole("alert")).toHaveCount(0)
})

test("adding a duplicate memory does not invent another addition", async ({ page }, info) => {
  await openMemory(page, `Duplicate memory ${info.workerIndex}-${info.retry}`, {
    action: "add", content: "existing preference",
  }, { success: true, changed: false })
  const transcript = page.getByTestId("transcript")
  const toggle = transcript.getByRole("button", { name: /^memory.*add/ })
  await expect(toggle).toHaveAttribute("aria-expanded", "false")
  await expect(toggle.getByTestId("edit-counts")).toHaveCount(0)
  await toggle.click()
  await expect(transcript.getByText("No memory change.", { exact: true })).toBeVisible()
  await expect(transcript.getByTestId("file-diff")).toHaveCount(0)
  await expect(transcript.getByTestId("tool-output")).toHaveCount(0)
  await expect(transcript).not.toContainText(unrelated)
})
