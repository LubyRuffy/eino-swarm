import { writeFile } from "node:fs/promises"
import { expect, test, type Page } from "@playwright/test"

import { reloadOpenConversation } from "./session"

type Patch = { name: string; old_text: string; new_text: string }

/** Historical args belong to the recorded call, even if the skill changes later. */
async function openPatch(page: Page, title: string, patch: Patch, options: { live?: boolean; error?: string } = {}) {
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const base = {
    thread_id: thread.id, turn_id: "skill-patch-turn", agent_id: "manager",
    created_at: "2026-01-01T00:00:00.000Z",
  }
  const events = [
    { ...base, seq: 1, kind: "tool_call", tool_call_id: "skill-patch-call", text: `skill_manage(${JSON.stringify({ action: "patch", ...patch })})` },
    { ...base, seq: 2, kind: "tool_result", tool_call_id: "skill-patch-call", text: JSON.stringify(options.error ? { success: false, error: options.error } : { success: true, name: patch.name }) },
    { ...base, seq: 3, kind: "agent_message", text: "The procedure update was checked." },
    { ...base, seq: 4, kind: "done", text: "The procedure update was checked." },
  ]
  let liveReleased = !options.live, initialHistoryServed = false, logReads = 0, streams = 0
  let release!: () => void
  const liveReady = new Promise<void>((resolve) => { release = resolve })
  await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\?|$)`), async (route) => {
    logReads++
    const history = liveReleased ? events : []
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, events: history, roster: [], has_more: false } })
    initialHistoryServed = true
  })
  if (options.live) {
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
  await expect.poll(() => initialHistoryServed).toBe(true)
  return {
    logReads: () => logReads,
    releaseLive: () => { liveReleased = true; release() },
  }
}

for (const live of [false, true]) {
  test(`skill patch details preserve changed lines through ${live ? "named SSE" : "history"} and reload`, async ({ page }, info) => {
    const patch = {
      name: "evidence-retention",
      old_text: "Preconditions\n  preserve the initial input\nVerification",
      new_text: "Preconditions\n  preserve the complete input\n  retain the saved outcome\nVerification",
    }
    const replay = await openPatch(page, `Skill patch ${live ? "live" : "stored"} ${info.workerIndex}-${info.retry}`, patch, { live })
    let readsBeforeLive = 0
    if (live) {
      await expect.poll(replay.logReads).toBeGreaterThan(0)
      readsBeforeLive = replay.logReads()
      await expect(page.getByTestId("transcript").getByRole("button", { name: /skill_manage/ })).toHaveCount(0)
      replay.releaseLive()
    }
    for (let round = 0; round < 2; round++) {
      const transcript = page.getByTestId("transcript")
      const toggle = transcript.getByRole("button", { name: /skill_manage.*patch.*evidence-retention/ })
      await expect(toggle).toHaveAttribute("aria-expanded", "false")
      await expect(toggle.getByTestId("edit-counts")).toContainText("+2")
      await expect(toggle.getByTestId("edit-counts")).toContainText("−1")
      await expect(transcript.getByTestId("file-diff")).toHaveCount(0)
      await toggle.click()
      const diff = transcript.getByTestId("file-diff")
      await expect(diff).toBeVisible()
      const removed = diff.locator('[data-diff="del"] td:last-child')
      const added = diff.locator('[data-diff="add"] td:last-child')
      await expect(removed).toHaveCount(1)
      await expect(added).toHaveCount(2)
      expect(await removed.textContent()).toBe("  preserve the initial input")
      expect(await added.nth(0).textContent()).toBe("  preserve the complete input")
      expect(await added.nth(1).textContent()).toBe("  retain the saved outcome")
      await expect(diff.locator('[data-diff="eq"] td:last-child')).toHaveText(["Preconditions", "Verification"])
      await expect(transcript.getByTestId("tool-output")).toHaveCount(0)
      if (round === 0) {
        if (live) expect(replay.logReads()).toBe(readsBeforeLive)
        await writeFile(info.outputPath(`skill-patch-${live ? "live" : "history"}.png`), await page.screenshot(), { mode: 0o600 })
      }
      await toggle.click()
      await expect(toggle).toHaveAttribute("aria-expanded", "false")
      await expect(diff).toHaveCount(0)
      if (round === 0) await reloadOpenConversation(page)
    }
  })
}

test("an empty patch replacement shows the removed lines", async ({ page }, info) => {
  await openPatch(page, `Skill deletion patch ${info.workerIndex}-${info.retry}`, {
    name: "obsolete-step", old_text: "  deprecated instruction\n  obsolete verification", new_text: "",
  })
  const transcript = page.getByTestId("transcript")
  const toggle = transcript.getByRole("button", { name: /skill_manage.*patch.*obsolete-step/ })
  await expect(toggle.getByTestId("edit-counts")).toHaveText("−2")
  await toggle.click()
  const diff = transcript.getByTestId("file-diff")
  await expect(diff.locator('[data-diff="add"]')).toHaveCount(0)
  const removed = diff.locator('[data-diff="del"] td:last-child')
  await expect(removed).toHaveCount(2)
  expect(await removed.allTextContents()).toEqual(["  deprecated instruction", "  obsolete verification"])
})

test("a refused skill patch retains its error when the row is expanded", async ({ page }, info) => {
  const error = "old_text was not found in the skill body"
  await openPatch(page, `Refused skill patch ${info.workerIndex}-${info.retry}`, {
    name: "revision-check", old_text: "stale procedure", new_text: "revised procedure",
  }, { error })
  const transcript = page.getByTestId("transcript")
  const toggle = transcript.getByRole("button", { name: /skill_manage.*patch.*revision-check/ })
  await expect(toggle).toHaveAttribute("aria-invalid", "true")
  await expect(toggle).toContainText(error)
  await toggle.click()
  await expect(transcript.getByRole("alert")).toHaveText(error)
  await toggle.click()
  await expect(toggle).toContainText(error)
  await expect(transcript.getByRole("alert")).toHaveCount(0)
})
