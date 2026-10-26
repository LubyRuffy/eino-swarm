import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"

import { startGoalPauseFixture } from "./goal-pause-fixture"
import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"

test("a human send answers once and keeps a capped goal paused until Start", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_GOAL_PAUSE !== "1", "requires the keyless goal pause fixture")
  const fixture = await startGoalPauseFixture()
  let threadId = ""
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "fixture-provider", providers: [{ id: "fixture-provider", label: "Offline goal pause regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false, goal_max_auto_turns: 1 },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await freshConversation(page)
    threadId = (await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id"))!
    expect(threadId).toBeTruthy()
    await composer(page).fill("/goal Verify the required artifact.")
    await composer(page).press("Enter")
    await expect.poll(async () => (await (await request.get(`/api/threads/${threadId}`)).json()).thread.goal_capped).toBe(true)
    await waitForIdle(page)
    await expect(page.getByTestId("goal-banner")).toContainText("Paused")
    await expect(page.getByTestId("goal-start")).toBeVisible()
    const before = (await (await request.get(`/api/threads/${threadId}/turns`)).json()).turns
    expect(before).toHaveLength(2)

    await composer(page).fill("Describe the current state.")
    await composer(page).press("Enter")
    await expect.poll(async () => {
      const { turns } = await (await request.get(`/api/threads/${threadId}/turns`)).json()
      return turns.length >= 3 && turns.at(-1).status !== "running"
    }).toBe(true)
    await waitForIdle(page)
    const afterSend = await (await request.get(`/api/threads/${threadId}`)).json()
    expect(afterSend.thread).toMatchObject({ goal_capped: true })
    expect((await (await request.get(`/api/threads/${threadId}/turns`)).json()).turns).toHaveLength(3)
    await expect(page.getByTestId("goal-banner")).toContainText("Paused")
    await expect(page.getByTestId("goal-start")).toBeVisible()
    await reloadOpenConversation(page)
    await expect(page.getByTestId("goal-banner")).toContainText("Paused")
    await expect(page.getByTestId("goal-start")).toBeVisible()
    await writeFile(info.outputPath("paused-after-human-send.png"), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })

    fixture.hold()
    const resumeRequest = page.waitForRequest((req) => req.method() === "PATCH" && req.url().endsWith(`/api/threads/${threadId}`))
    await page.getByTestId("goal-start").click()
    expect((await resumeRequest).postDataJSON()).toEqual({ goal_resume: true })
    await expect(page.getByTestId("goal-banner")).toContainText("Pursuing")
    await expect(page.getByTestId("goal-start")).toHaveCount(0)
    await writeFile(info.outputPath("pursuing-after-start.png"), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
    fixture.release()
    await expect.poll(async () => {
      const { turns } = await (await request.get(`/api/threads/${threadId}/turns`)).json()
      return turns.length === 5 && turns.at(-1).status === "done"
    }).toBe(true)
    await expect(page.getByTestId("goal-banner")).toContainText("Paused")
    await expect(page.getByTestId("goal-start")).toBeVisible()
    const evidence = await (await request.get(`/api/threads/${threadId}/log`)).json()
    expect(evidence.events.filter((event: { kind: string }) => event.kind === "goal_resumed")).toHaveLength(1)
    expect(fixture.errors).toEqual([])
    await writeFile(info.outputPath("goal-pause-evidence.json"), JSON.stringify({ afterSend, events: evidence.events }, null, 2), { mode: 0o600 })
  } finally {
    if (threadId) await request.post(`/api/threads/${threadId}/interrupt`).catch(() => {})
    await fixture.close()
  }
})
