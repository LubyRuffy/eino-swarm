import { expect, test, type Page } from "@playwright/test"

import { freshConversation, reloadOpenConversation } from "./session"

const workerID = "verifier-1"
const timeoutError = `swarm: agent ${workerID} exceeded timeout 2m0s (1 consecutive watchdog failures): context deadline exceeded`

// Replay fixtures isolate the presentation contract; worker-timeout.spec.ts
// separately obtains the same error from the real offline worker watchdog.
async function failureHistory(page: Page, error: string, parent = false) {
  await page.route(/\/api\/threads\/[^/]+\/log(?:\?|$)/, async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    const base = {
      thread_id: new URL(route.request().url()).pathname.split("/")[3],
      turn_id: "failure-turn", agent_id: "manager", created_at: new Date().toISOString(),
    }
    const events = [
      { kind: "user_message", text: "Perform the assigned verification." },
      { kind: "spawned", agent_id: workerID, role: "verifier" },
      ...Array.from({ length: 60 }, (_, index) => ({
        kind: "agent_message", agent_id: workerID,
        text: `Verification progress ${index + 1}. Checked the assigned inputs and recorded the findings.`,
      })),
      ...(parent
        ? [{ kind: "error", err: error }]
        : [{ kind: "finished", agent_id: workerID, err: error }, { kind: "done" }]),
    ]
    body.events = events.map((event, index) => ({ ...base, seq: index + 1, ...event }))
    body.has_more = false
    await route.fulfill({ response, json: body })
  })
}

async function openFailure(page: Page) {
  await page.getByRole("tab", { name: "Agents", exact: true }).click()
  const row = page.getByTestId("agent-roster").locator(`[data-agent-id="${workerID}"]`)
  await expect(row).toContainText("View failure reason")
  await row.click()
  return page.getByTestId("agent-failure")
}

test("worker failure stays visible above a long log and opens the exact timeout setting", async ({ page, request }, info) => {
  const { settings: initial } = await (await request.get("/api/settings")).json()
  const previousTimeout = initial.swarm.agent_timeout_seconds as number
  const nextTimeout = previousTimeout + 30
  try {
    await failureHistory(page, timeoutError)
    await freshConversation(page)
    const failure = await openFailure(page)
    await expect(failure).toContainText(timeoutError)
    const log = page.getByTestId("agent-scroller")
    await expect.poll(() => log.evaluate((element) => element.scrollHeight > element.clientHeight)).toBe(true)
    await log.evaluate((element) => { element.scrollTop = element.scrollHeight })
    await expect(failure).toBeInViewport()
    expect(await log.evaluate((element) => element.contains(document.querySelector('[data-testid="agent-failure"]')))).toBe(false)
    await page.screenshot({ path: info.outputPath("worker-failure-detail.png") })

    await failure.getByRole("button", { name: "Sub-agent timeout", exact: true }).click()
    const settings = page.getByRole("dialog")
    const timeout = settings.getByLabel("Sub-agent timeout (seconds)", { exact: true })
    await expect(timeout).toBeFocused()
    await timeout.fill(String(nextTimeout))
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.swarm.agent_timeout_seconds).toBe(nextTimeout)
    await page.screenshot({ path: info.outputPath("worker-timeout-setting.png") })
    await settings.getByRole("button", { name: "Back to app" }).click()
    await expect(failure).toContainText(timeoutError)

    await reloadOpenConversation(page)
    await expect(await openFailure(page)).toContainText(timeoutError)
    await page.getByTestId("agent-failure").getByRole("button", { name: "Sub-agent timeout", exact: true }).click()
    await expect(page.getByRole("dialog").getByLabel("Sub-agent timeout (seconds)", { exact: true })).toHaveValue(String(nextTimeout))
  } finally {
    expect((await request.put("/api/settings", { data: { swarm: initial.swarm } })).ok()).toBe(true)
  }
})

test("an ambiguous provider deadline shows the cause without a watchdog repair link", async ({ page }) => {
  await failureHistory(page, "provider request: context deadline exceeded")
  await freshConversation(page)
  const failure = await openFailure(page)
  await expect(failure).toContainText("provider request: context deadline exceeded")
  await expect(failure.getByRole("button")).toHaveCount(0)
})

test("workers stopped by a failed turn retain the parent cause after reload", async ({ page }) => {
  await failureHistory(page, "the model service is unavailable", true)
  await freshConversation(page)
  for (let replay = 0; replay < 2; replay++) {
    const failure = await openFailure(page)
    await expect(failure).toContainText("Stopped because this turn failed")
    await expect(failure).toContainText("the model service is unavailable")
    await expect(failure.getByRole("button")).toHaveCount(0)
    if (replay === 0) await reloadOpenConversation(page)
  }
})
