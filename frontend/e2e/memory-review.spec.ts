import { expect, test } from "@playwright/test"

import { composer, waitForIdle } from "./session"
import { capacityRepairCase, startMemoryReviewFixture } from "./memory-review-fixture"

test("rejected reviews retain diagnostics; committed changes expose their full diff live and after reload", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MEMORY_REVIEW !== "1", "requires the keyless loopback memory fixture")
  const fixture = await startMemoryReviewFixture()
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "review-fixture", providers: [{ id: "review-fixture", label: "Offline review regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, auto_review: false, char_limit: 200, entry_max: 200, review_max_iterations: 8 },
    } })).ok()).toBe(true)
    const name = `Review regression ${Date.now()}`
    const { project } = await (await request.post("/api/projects", { data: { name } })).json()
    const otherName = `Other review target ${Date.now()}`
    expect((await request.post("/api/projects", { data: { name: otherName } })).ok()).toBe(true)
    const initial = await (await request.get(`/api/projects/${project.id}/memory`)).json()
    const text = "Saved convention: ".padEnd(200, "r")
    const storedResponse = await request.put(`/api/projects/${project.id}/memory`, { data: { text, rev: initial.memory.memory.rev } })
    expect(storedResponse.ok()).toBe(true)
    const stored = (await storedResponse.json()).memory
    expect(stored.chars).toBe(200)

    await page.goto("/")
    await page.getByTestId("dest-projects").click()
    await page.getByRole("button", { name: `New conversation in ${name}` }).click()
    const thread = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    const accepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${thread}/steer`)
    await composer(page).fill("Keep replies concise.")
    await composer(page).press("Enter")
    const { turn } = await (await accepted).json()
    await waitForIdle(page)
    await page.getByRole("tab", { name: "Memory", exact: true }).click()
    fixture.pauseReview()
    await page.getByRole("button", { name: "Review this conversation now" }).click()
    await expect.poll(() => fixture.calls.some((call) => call.review)).toBe(true)
    // A later turn is already complete when this review's old-turn result lands.
    const nextAccepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${thread}/steer`)
    await composer(page).fill("Verify the current result.")
    await composer(page).press("Enter")
    const nextTurn = (await (await nextAccepted).json()).turn
    await waitForIdle(page)
    await page.getByRole("button", { name: `New conversation in ${otherName}` }).click()
    await page.getByRole("tab", { name: "Memory", exact: true }).click()
    await expect(page.getByTestId("review-status")).toHaveCount(0)
    fixture.resumeReview()
    await expect.poll(async () => {
      const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
      return trace.events.some((event: { kind: string }) => event.kind === "memory_review")
    }).toBe(true)
    // A late completion belongs to the click's conversation and project.
    await expect(page.getByTestId("review-status")).toHaveCount(0)
    const projectButton = page.getByRole("button", { name, exact: true })
    if (await projectButton.getAttribute("aria-expanded") !== "true") await projectButton.click()
    await page.locator(`[data-testid="thread-row"][data-id="${thread}"]`).getByTestId("row-label").click()
    await page.getByRole("tab", { name: "Memory", exact: true }).click()
    const status = page.getByTestId("review-status")
    await expect(status).toContainText("repair round limit reached")
    await expect(status).toContainText("8 attempts")
    await expect(status).toContainText("Evidence reference did not pass validation")
    await expect(status).toContainText("Number not supported by the cited evidence")
    await expect(status).toContainText(`${stored.chars}/${stored.chars}`)
    await expect(status).toContainText("characters over")
    await expect(status).toContainText("Notes were not saved")
    await expect(page.getByLabel("Project notes")).toHaveValue(stored.text)
    await expect(page.getByTestId("memory-notice")).toContainText("repair round limit reached")
    await page.screenshot({ path: info.outputPath("review-failure-memory.png"), animations: "disabled" })
    const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
    const review = JSON.parse(trace.events.find((event: { kind: string }) => event.kind === "memory_review").text)
    expect(review).toMatchObject({ stop_reason: "iteration_limit", attempt_count: 8, changed: false })
    expect(review.refused.map((refusal: { code: string }) => refusal.code)).toContain("capacity")
    expect(review.attempts[0].operations[0].sources.length).toBeGreaterThan(0)
    expect(trace.llm_calls.filter((call: { agent_id: string }) => call.agent_id === "memory-reviewer")).toHaveLength(8)
    await status.getByRole("button", { name: `Trace ${turn.id}` }).click()
    await expect(page.getByRole("tab", { name: "Trace", selected: true })).toBeVisible()
    await page.getByRole("button", { name: "Toggle side panel" }).click()
    await expect(page.getByRole("tab", { name: "Trace", selected: true })).toHaveCount(0)
    await page.getByTestId("memory-notice").getByRole("button", { name: `Trace ${turn.id}` }).click()
    await expect(page.getByRole("tab", { name: "Trace", selected: true })).toBeVisible()
    const log = page.getByTestId("trace-log")
    await expect(log).toBeVisible()
    await log.getByTestId("review-trace-details").click()
    await expect(log).toContainText('"attempt_count": 8')
    await expect(log).toContainText('"sources"')
    await expect(log).toContainText(String(review.attempts[0].operations[0].sources[0]))
    await expect(page.getByRole("tabpanel").getByText(turn.id, { exact: true })).toBeVisible()
    await expect(page.getByRole("tabpanel").getByText(nextTurn.id, { exact: true })).toHaveCount(0)
    await expect(log).not.toContainText("A quote absent from the source.")
    await page.screenshot({ path: info.outputPath("review-failure-trace.png"), animations: "disabled" })
    await info.attach("review-failure-trace", { body: JSON.stringify(trace, null, 2), contentType: "application/json" })

    await page.reload()
    await page.getByTestId("dest-projects").click()
    const reloadedProject = page.getByRole("button", { name, exact: true })
    if (await reloadedProject.getAttribute("aria-expanded") !== "true") await reloadedProject.click()
    await page.locator(`[data-testid="thread-row"][data-id="${thread}"]`).getByTestId("row-label").click()
    await expect(page.getByTestId("memory-notice")).toContainText("repair round limit reached")
    await page.getByRole("tab", { name: "Memory", exact: true }).click()
    fixture.setMode("repair")
    await page.getByRole("button", { name: "Review this conversation now" }).click()
    await expect(status).toContainText("Review finished.")
    await expect(page.getByLabel("Project notes")).toHaveValue("Verify the current result.")

    // Assert the named memory_review stream before reading Trace/history: an
    // observer fetch must not hide a missing live subscription.
    const changedNotice = page.getByTestId("memory-notice").filter({ hasText: "Memory updated: 1 note revised." })
    const detailsButton = changedNotice.getByRole("button", { name: "View details", exact: true })
    await expect(detailsButton).toHaveAttribute("aria-expanded", "false")
    await expect(changedNotice.getByTestId("file-diff")).toHaveCount(0)
    await detailsButton.click()
    await expect(changedNotice.getByRole("button", { name: "Hide details", exact: true })).toHaveAttribute("aria-expanded", "true")
    // The old entry exceeds the legacy 160-character preview. Its complete
    // original text, rather than a guessed/reloaded current note, must survive.
    await expect(changedNotice.locator('[data-diff="del"]')).toContainText(text)
    await expect(changedNotice.locator('[data-diff="add"]')).toContainText("Verify the current result.")
    await changedNotice.scrollIntoViewIfNeeded()
    await page.screenshot({ path: info.outputPath("review-change-details-live.png"), animations: "disabled" })
    await changedNotice.getByRole("button", { name: "Hide details", exact: true }).click()
    await expect(changedNotice.getByTestId("file-diff")).toHaveCount(0)

    const latest = await (await request.get(`/api/trace/${nextTurn.id}`)).json()
    const outcomes = latest.events.filter((event: { kind: string }) => event.kind === "memory_review").map((event: { text: string }) => JSON.parse(event.text))
    expect(outcomes.at(-1)).toMatchObject({ stop_reason: "completed", attempt_count: 2, changed: true, notes: { replace: 1 } })
    expect(outcomes.at(-1).changes).toEqual([expect.objectContaining({ target: "memory", action: "replace", before: text, after: "Verify the current result." })])

    await page.reload()
    await page.getByTestId("dest-projects").click()
    const historyProject = page.getByRole("button", { name, exact: true })
    if (await historyProject.getAttribute("aria-expanded") !== "true") await historyProject.click()
    await page.locator(`[data-testid="thread-row"][data-id="${thread}"]`).getByTestId("row-label").click()
    await expect(detailsButton).toHaveAttribute("aria-expanded", "false")
    await detailsButton.click()
    await expect(changedNotice.locator('[data-diff="del"]')).toContainText(text)
    await expect(changedNotice.locator('[data-diff="add"]')).toContainText("Verify the current result.")
    await changedNotice.scrollIntoViewIfNeeded()
    await page.screenshot({ path: info.outputPath("review-change-details-history.png"), animations: "disabled" })
    expect(fixture.errors).toEqual([])
    expect(fixture.calls.every((call) => !call.authorization)).toBe(true)
    await page.screenshot({ path: info.outputPath("review-repair-success.png"), animations: "disabled" })
  } finally { await fixture.close() }
})

test("one complete refusal automatically compacts related notes without dropping constraints or unrelated memory", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_MEMORY_REVIEW !== "1", "requires the keyless loopback memory fixture")
  const fixture = await startMemoryReviewFixture()
  fixture.setMode("compact")
  try {
    const { related, unrelated, request: requirement, compacted } = capacityRepairCase
    const initialText = [related, unrelated].join("\n\n")
    const expectedText = [compacted, unrelated].join("\n\n")
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "review-fixture", providers: [{ id: "review-fixture", label: "Offline review regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false },
      memory: { ...settings.memory, enabled: true, auto_review: true, char_limit: initialText.length, entry_max: initialText.length, review_max_iterations: 2 },
    } })).ok()).toBe(true)
    const name = `Capacity repair ${Date.now()}`
    const { project } = await (await request.post("/api/projects", { data: { name } })).json()
    const initial = await (await request.get(`/api/projects/${project.id}/memory`)).json()
    const storedResponse = await request.put(`/api/projects/${project.id}/memory`, { data: { text: initialText, rev: initial.memory.memory.rev } })
    expect(storedResponse.ok()).toBe(true)
    const stored = (await storedResponse.json()).memory
    expect(stored.chars).toBe(initialText.length)

    await page.goto("/")
    await page.getByTestId("dest-projects").click()
    await page.getByRole("button", { name: `New conversation in ${name}` }).click()
    const thread = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    expect(thread).toBeTruthy()
    const accepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${thread}/steer`)
    await composer(page).fill(requirement)
    await composer(page).press("Enter")
    const { turn } = await (await accepted).json()
    await waitForIdle(page)

    // Observe the actual named memory_review delivery before a Trace or
    // history fetch can conceal a missing live subscription.
    const changedNotice = page.getByTestId("memory-notice").filter({ hasText: "Memory updated: 1 note revised." })
    await expect(changedNotice).toBeVisible()
    expect(fixture.repairReceipts).toHaveLength(1)
    expect(fixture.calls.filter((call) => call.review)).toHaveLength(2)
    await page.getByRole("tab", { name: /^Memory(?: new)?$/ }).click()
    await expect(page.getByLabel("Project notes")).toHaveValue(expectedText)
    await changedNotice.getByRole("button", { name: "View details", exact: true }).click()
    await expect(changedNotice.locator('[data-diff="del"]')).toContainText(related)
    await expect(changedNotice.locator('[data-diff="add"]')).toContainText(compacted)
    await page.screenshot({ path: info.outputPath("capacity-repair-live.png"), animations: "disabled" })

    const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
    const outcome = JSON.parse(trace.events.find((event: { kind: string }) => event.kind === "memory_review").text)
    expect(outcome).toMatchObject({ stop_reason: "completed", attempt_count: 2, changed: true, notes: { replace: 1 } })
    expect(outcome.attempts.map((attempt: { status: string }) => attempt.status)).toEqual(["refused", "accepted"])
    expect(outcome.refused.filter((refusal: { attempt: number }) => refusal.attempt === 1).map((refusal: { code: string }) => refusal.code).sort()).toEqual(["capacity", "evidence", "numeric"])
    expect(trace.llm_calls.filter((call: { agent_id: string }) => call.agent_id === "memory-reviewer")).toHaveLength(2)
    expect(outcome.changes).toEqual([expect.objectContaining({ target: "memory", action: "replace", before: related, after: compacted })])
    const durable = JSON.stringify(outcome)
    for (const privateField of ["An absent quotation.", "91MiB", "invalid_evidence", "unsupported_numbers", "current_entries", "max_content_chars", "proposed_usage"]) expect(durable).not.toContain(privateField)
    const saved = (await (await request.get(`/api/projects/${project.id}/memory`)).json()).memory.memory
    expect(saved.entries).toEqual([compacted, unrelated])
    expect(saved.chars).toBeLessThanOrEqual(stored.chars)

    await page.reload()
    await page.getByTestId("dest-projects").click()
    const projectButton = page.getByRole("button", { name, exact: true })
    if (await projectButton.getAttribute("aria-expanded") !== "true") await projectButton.click()
    await page.locator(`[data-testid="thread-row"][data-id="${thread}"]`).getByTestId("row-label").click()
    await expect(changedNotice).toBeVisible()
    await page.getByRole("tab", { name: /^Memory(?: new)?$/ }).click()
    await expect(page.getByLabel("Project notes")).toHaveValue(expectedText)
    expect(fixture.errors).toEqual([])
    expect(fixture.calls.every((call) => !call.authorization)).toBe(true)
    await page.screenshot({ path: info.outputPath("capacity-repair-reload.png"), animations: "disabled" })
  } finally { await fixture.close() }
})
