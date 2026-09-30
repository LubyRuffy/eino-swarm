import { expect, test } from "@playwright/test"

import { freshConversation } from "./session"

const OUTPUT_BUDGET_ERROR =
  "the model used its whole output budget before it produced an answer"

test("an output-budget error opens the completion cap", async ({ page }) => {
  // The offline provider does not spend a completion budget. The stored
  // sentence is what the desktop matches, so the log the UI paints is that
  // sentence and nothing else about the script changes.
  await page.route(/\/api\/threads\/[^/]+\/log(?:\?|$)/, async (route) => {
    const url = new URL(route.request().url())
    const res = await route.fetch()
    if (!res.ok()) {
      await route.fulfill({ response: res })
      return
    }
    const body = await res.json()
    if (!url.searchParams.get("before")) {
      const events = Array.isArray(body.events) ? body.events : []
      const id = url.pathname.split("/").filter(Boolean)[2]
      const seq = events.reduce(
        (max: number, ev: { seq?: number }) => Math.max(max, ev.seq ?? 0),
        0,
      )
      events.push({
        thread_id: id,
        turn_id: "turn-budget",
        seq: seq + 1,
        kind: "error",
        agent_id: "manager",
        err: OUTPUT_BUDGET_ERROR,
        text: "",
        created_at: new Date().toISOString(),
      })
      body.events = events
    }
    await route.fulfill({
      status: res.status(),
      contentType: "application/json",
      body: JSON.stringify(body),
    })
  })

  await freshConversation(page)
  const transcript = page.getByTestId("transcript")
  await expect(transcript.getByTestId("output-budget-error")).toBeVisible()
  await expect(transcript.getByText(OUTPUT_BUDGET_ERROR)).toBeVisible()
  await transcript.getByRole("button", { name: "Max completion tokens" }).click()

  const dialog = page.getByRole("dialog")
  const cap = dialog.getByLabel("Max completion (tokens)")
  await expect(cap).toBeVisible()
  await expect(cap).toBeFocused()
  await expect(dialog.getByRole("tab", { name: "Swarm" })).toHaveAttribute(
    "aria-selected",
    "true",
  )
})
