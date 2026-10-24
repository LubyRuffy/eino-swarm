import { expect, test } from "@playwright/test"
import { readFileSync } from "node:fs"
import { join } from "node:path"

import { composer, freshConversation, reloadOpenConversation, statusBadge, waitForIdle } from "./session"

test("conversation changes notify native location without a paint probe or draft contents", async ({ page }, info) => {
  await page.addInitScript(() => {
    const rows: Array<{ thread?: unknown; turn?: unknown; project?: unknown; draft?: unknown; hasDetail: boolean; hasContent: boolean }> = []
    ;(window as Window & { __locationNotices?: typeof rows }).__locationNotices = rows
    window.addEventListener("zwai:recovery-location", (event) => {
      const value = (window as Window & { __zwaiRecoveryAttribution?: Record<string, unknown> }).__zwaiRecoveryAttribution ?? {}
      rows.push({ thread: value.thread_id, turn: value.turn_id, project: value.project_id, draft: value.draft_key,
        hasDetail: "detail" in event, hasContent: ["text", "quotes", "files"].some((key) => key in value) })
    })
  })
  const notices = () => page.evaluate(() => (window as Window & {
    __locationNotices?: Array<{ thread?: string; draft?: string; hasDetail: boolean; hasContent: boolean }>
  }).__locationNotices ?? [])
  await freshConversation(page)
  const selected = page.locator('[data-testid="thread-row"][aria-current="true"]')
  const first = (await selected.getAttribute("data-id"))!
  await expect.poll(async () => (await notices()).at(-1)?.thread).toBe(first)
  const beforeDraft = (await notices()).length
  await composer(page).fill("A private draft stays with its selected conversation.")
  await expect(page.getByTestId("composer-draft-status")).toContainText("Draft saved on this computer")
  expect(await notices()).toHaveLength(beforeDraft)

  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(selected).not.toHaveAttribute("data-id", first)
  const second = (await selected.getAttribute("data-id"))!
  await expect.poll(async () => (await notices()).at(-1)?.thread).toBe(second)
  const beforeReturn = (await notices()).length
  await page.locator(`[data-testid="thread-row"][data-id="${first}"]`).getByTestId("row-label").click()
  await expect(composer(page)).toHaveValue("A private draft stays with its selected conversation.")
  const returned = (await notices()).slice(beforeReturn)
  expect(returned.length).toBeGreaterThan(0)
  expect(returned.every((row) => row.thread === first && row.draft === `thread:${first}`)).toBe(true)
  expect((await notices()).every((row) => !row.hasDetail && !row.hasContent)).toBe(true)

  // The native launcher supplies this URL. Verify the frontend boot route
  // without a sidebar click; this browser case does not accept native paint.
  await page.goto(`/?shell=desktop&recovery_thread=${encodeURIComponent(first)}&recovery_draft=${encodeURIComponent(`thread:${first}`)}`)
  await expect(selected).toHaveAttribute("data-id", first)
  await expect(composer(page)).toHaveValue("A private draft stays with its selected conversation.")
  await expect.poll(async () => (await notices()).at(-1)?.thread).toBe(first)
  expect((await notices()).every((row) => !row.hasDetail && !row.hasContent)).toBe(true)
  await page.screenshot({ path: info.outputPath("explicit-location-recovery-with-retained-private-draft.png"), animations: "disabled" })
})

test("a live recovery SSE reaches Full log without a history or Trace fetch", async ({ page, request }, info) => {
  let eventConnections = 0
  page.on("request", (req) => { if (/\/api\/threads\/[^/]+\/events(?:\?|$)/.test(req.url())) eventConnections++ })
  await freshConversation(page)
  const threadId = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id"))!
  const accepted = page.waitForResponse((response) => response.request().method() === "POST"
    && new URL(response.url()).pathname === `/api/threads/${threadId}/steer`)
  await composer(page).fill("Perform the assigned offline verification.")
  await composer(page).press("Enter")
  const response = await accepted
  expect(response.status()).toBe(202)
  const { turn: { id: turnId } } = await response.json()
  await waitForIdle(page)
  await page.getByRole("tab", { name: "Trace", exact: true }).click()
  await page.getByTestId("trace-log-toggle").click()
  const log = page.getByTestId("trace-log")
  let forbiddenReads = 0
  for (const path of [/\/api\/trace\//, /\/api\/threads\/[^/]+\/log(?:\?|$)/]) {
    await page.route(path, async (route) => { forbiddenReads++; await route.abort() })
  }
  const priorConnections = eventConnections
  expect(priorConnections).toBeGreaterThan(0)
  const meta = await (await request.get("/api/meta")).json()
  // This is the private server created by Playwright, never a user's engine.
  const { pid: enginePID } = JSON.parse(readFileSync(join(meta.data_dir, "engine.json"), "utf8"))
  const sessionId = "live-recovery-" + threadId
  const posted = await request.post("/api/desktop/recovery", { data: {
    session_id: sessionId, generation: 1, attempt: 1, shell_pid: process.pid, engine_pid: enginePID,
    thread_id: threadId, turn_id: turnId, phase: "recovered", at: new Date().toISOString(),
  } })
  expect(posted.ok()).toBe(true)
  expect(await posted.json()).toEqual({ persisted: true })
  await expect(log).toContainText(sessionId, { timeout: 5000 })
  const diagnostic = log.locator("li").filter({ hasText: "desktop_recovery" })
  await expect(diagnostic).toHaveCount(1)
  await diagnostic.getByTestId("review-trace-details").click()
  await expect(diagnostic.locator("pre")).toContainText(`"turn_id": "${turnId}"`)
  await expect(statusBadge(page)).toHaveText("Idle")
  await expect(page.getByTestId("trace-status")).toHaveText("done")
  await expect(page.getByTestId("transcript")).not.toContainText(sessionId)
  await page.getByRole("tab", { name: /^Agents(?: \d+)?$/ }).click()
  await expect(page.getByTestId("agent-roster").locator('[data-agent-id="desktop"]')).toHaveCount(0)
  expect(forbiddenReads).toBe(0)
  expect(eventConnections).toBe(priorConnections)
  await page.getByRole("tab", { name: "Trace", exact: true }).click()
  await page.getByTestId("trace-log-toggle").click()
  await diagnostic.getByTestId("review-trace-details").click()
  await expect(diagnostic.locator("pre")).toContainText(sessionId)
  await page.screenshot({ path: info.outputPath("live-recovery-without-history-fetch.png"), animations: "disabled" })
})

test("interface recovery stays in Full log without becoming a running worker or chat message", async ({ page }, info) => {
  // The real HTTP tests cover durable recording; this replay fixture drives
  // the browser's normal cold-history/Trace route with that wire event.
  await page.route(/\/api\/threads\/[^/]+\/log(?:\?|$)/, async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    const base = { thread_id: new URL(route.request().url()).pathname.split("/")[3], turn_id: "recovery-turn", agent_id: "manager", created_at: new Date().toISOString() }
    body.events = [
      { ...base, seq: 1, kind: "user_message", text: "Perform the assigned verification." },
      { ...base, seq: 2, kind: "done", text: "Verification completed." },
      { ...base, seq: 3, kind: "desktop_recovery", agent_id: "desktop", text: '{"session_id":"recovery-test-session","phase":"recovered"}' },
    ]
    body.has_more = false
    await route.fulfill({ response, json: body })
  })
  await freshConversation(page)
  for (let replay = 0; replay < 2; replay++) {
    await expect(statusBadge(page)).toHaveText("Idle")
    await expect(page.getByTestId("transcript")).not.toContainText("recovery-test-session")
    await page.getByRole("tab", { name: "Agents", exact: true }).click()
    await expect(page.getByTestId("agent-roster").locator('[data-agent-id="desktop"]')).toHaveCount(0)
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await expect(page.getByTestId("trace-status")).toHaveText("done")
    await page.getByTestId("trace-log-toggle").click()
    const log = page.getByTestId("trace-log")
    await expect(log).toContainText("desktop_recovery")
    await log.getByTestId("review-trace-details").click()
    await expect(log.locator("pre")).toContainText('"seq": 3')
    await expect(log.locator("pre")).toContainText("recovery-test-session")
    if (replay === 0) await reloadOpenConversation(page)
  }
  await page.screenshot({ path: info.outputPath("recovery-full-log-without-worker.png"), animations: "disabled" })
})
