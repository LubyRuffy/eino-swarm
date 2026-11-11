import { expect, test } from "@playwright/test"

import { freshConversation } from "./session"

const OUTPUT_BUDGET_ERROR =
  "the model used its whole output budget before it produced an answer"

test("an output-budget error opens the completion cap", async ({ page }) => {
  // The offline provider does not spend a completion budget. Replay events
  // and turn metadata must identify the same failed human turn; a displayed
  // error alone cannot establish which turn Retry may continue.
  const originalInput = "Run the check again"
  const at = new Date().toISOString()
  const submissions: Record<string, unknown>[] = []
  let sourceSeq: number | undefined
  await page.route(/\/api\/threads\/[^/]+\/turns(?:\?|$)/, async (route) => {
    if (route.request().method() === "POST") {
      submissions.push({ rewind: route.request().postDataJSON() })
      await route.fulfill({ status: 500, json: { error: "Retry must not rewind." } })
      return
    }
    if (route.request().method() !== "GET") { await route.continue(); return }
    const res = await route.fetch()
    if (!res.ok()) { await route.fulfill({ response: res }); return }
    const body = await res.json()
    const rows = Array.isArray(body.turns) ? body.turns : []
    const id = new URL(route.request().url()).pathname.split("/").filter(Boolean)[2]
    const seq = rows.reduce((max: number, turn: { seq?: number }) => Math.max(max, turn.seq ?? 0), 0)
    body.turns = [...rows, {
      id: "turn-budget",
      thread_id: id,
      seq: seq + 1,
      status: "error",
      user_text: originalInput,
      final: "",
      error: OUTPUT_BUDGET_ERROR,
      provider_id: "",
      model: "",
      started_at: at,
      ended_at: at,
      duration_ms: 0,
    }]
    await route.fulfill({ response: res, json: body })
  })
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
      sourceSeq = seq + 1
      events.push(
        {
          thread_id: id,
          turn_id: "turn-budget",
          seq: seq + 1,
          kind: "user_message",
          agent_id: "manager",
          text: originalInput,
          created_at: at,
        },
        {
          thread_id: id,
          turn_id: "turn-budget",
          seq: seq + 2,
          kind: "error",
          agent_id: "manager",
          err: OUTPUT_BUDGET_ERROR,
          text: "",
          created_at: at,
        },
      )
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
  await expect(transcript.getByTestId("transcript-error-retry")).toBeEnabled()
  await transcript.getByRole("button", { name: "Max completion tokens" }).click()

  const dialog = page.getByRole("dialog")
  const cap = dialog.getByLabel("Max completion (tokens)")
  await expect(cap).toBeVisible()
  await expect(cap).toBeFocused()
  await expect(dialog.getByRole("tab", { name: "Swarm" })).toHaveAttribute(
    "aria-selected",
    "true",
  )
  // The modal removes the background from the accessibility tree.
  await expect(transcript.getByTestId("transcript-error-retry")).toBeDisabled()

  await dialog.getByRole("button", { name: "Back to app" }).click()
  await expect(dialog).toBeHidden()
  // Editing the cap is optional. Returning makes the existing Retry usable.
  await expect(transcript.getByTestId("transcript-error-retry")).toBeVisible()
  await expect(transcript.getByRole("button", { name: "Retry" })).toBeEnabled()
  expect(submissions).toEqual([])
  expect(sourceSeq).toBeGreaterThan(0)
  await page.route(/\/api\/threads\/[^/]+\/retry(?:\?|$)/, async (route) => {
    submissions.push(route.request().postDataJSON())
    await route.fulfill({ status: 503, json: { error: "Retry fixture refused the resend." } })
  })
  await transcript.getByRole("button", { name: "Retry" }).click()
  await expect.poll(() => submissions.length).toBe(1)
  expect(submissions).toEqual([{ turn_id: "turn-budget" }])
  await expect(page.getByText("Retry fixture refused the resend.", { exact: true })).toBeVisible()
  await expect(transcript.getByTestId("output-budget-error")).toBeVisible()
  await expect(transcript.getByTestId("user-message-editor")).toHaveCount(0)
})
