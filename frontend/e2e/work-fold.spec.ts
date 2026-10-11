import { expect, test, type Page } from "@playwright/test"

/** User-view folding lives here so conversation.spec.ts stays under 1000. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("composer-input")).toBeVisible()
}

test("collapses adjacent thinking and tools behind one row", async ({ page }) => {
  await page.route(/\/api\/threads\/[^/]+\/log(?:\?|$)/, async (route) => {
    const response = await route.fetch()
    const body = await response.json()
    const base = { thread_id: new URL(route.request().url()).pathname.split("/")[3], turn_id: "mixed-work", agent_id: "manager", created_at: new Date().toISOString() }
    body.events = [
      { ...base, seq: 1, kind: "reasoning", text: "Mixed thought content" },
      { ...base, seq: 2, kind: "tool_call", text: 'exec({"command":"printf mixed"})', tool_call_id: "mixed-call" },
      { ...base, seq: 3, kind: "tool_result", text: '{"stdout":"Mixed tool content","exit_code":0}', tool_call_id: "mixed-call" },
      { ...base, seq: 4, kind: "agent_message", text: "Mixed answer" },
      { ...base, seq: 5, kind: "done", text: "Mixed answer" },
    ]
    await route.fulfill({ response, json: body })
  })
  await freshConversation(page)
  const fold = page.getByTestId("work-fold")
  await expect(fold).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId("thought-scroll")).toBeHidden()
  await fold.click()
  await expect(page.getByTestId("thought-toggle")).toBeVisible()
  await page.getByTestId("thought-toggle").click()
  await expect(page.getByTestId("thought-scroll")).toBeVisible()
  await fold.click()
  await expect(page.getByTestId("thought-scroll")).toBeHidden()
  await expect(fold).toHaveAttribute("aria-expanded", "false")
})

for (const kind of ["reasoning", "tool"] as const) {
  test(`opens a single ${kind} without an outer group`, async ({ page }) => {
    // Replay fixtures exercise the same reducer and controls as stored history.
    await page.route(/\/api\/threads\/[^/]+\/log(?:\?|$)/, async (route) => {
      const response = await route.fetch()
      const body = await response.json()
      const thread_id = new URL(route.request().url()).pathname.split("/")[3]
      const base = { thread_id, turn_id: "single-work", agent_id: "manager", created_at: new Date().toISOString() }
      body.events = kind === "reasoning"
        ? [{ ...base, seq: 1, kind, text: "Replay thought content" }]
        : [
            { ...base, seq: 1, kind: "tool_call", text: 'exec({"command":"printf replay"})', tool_call_id: "single-call" },
            { ...base, seq: 2, kind: "tool_result", text: '{"stdout":"Replay tool content","exit_code":0}', tool_call_id: "single-call" },
          ]
      body.events.push(
        { ...base, seq: 3, kind: "agent_message", text: "Replay answer" },
        { ...base, seq: 4, kind: "done", text: "Replay answer" },
      )
      await route.fulfill({ response, json: body })
    })
    await freshConversation(page)
    const transcript = page.getByTestId("transcript")
    await expect(transcript.getByText("Replay answer")).toBeVisible()
    await expect(transcript.getByTestId("work-fold")).toHaveCount(0)
    const toggle = kind === "reasoning"
      ? transcript.getByTestId("thought-toggle")
      : transcript.getByRole("button", { name: /exec.*printf replay/ })
    const content = transcript.getByText(kind === "reasoning" ? "Replay thought content" : "Replay tool content", { exact: true })
    await expect(toggle).toHaveAttribute("aria-expanded", "false")
    await expect(content).toBeHidden()
    await toggle.click()
    await expect(content).toBeVisible()
    await page.screenshot({ path: `test-results/single-${kind}.png` })
    await toggle.click()
    await expect(content).toBeHidden()
  })
}
