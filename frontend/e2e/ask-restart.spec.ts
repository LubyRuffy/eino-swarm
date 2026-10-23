import { expect, test } from "@playwright/test"

test("the named recovery event restores the original ask before any history refresh, then one answer survives reload", async ({ page }, info) => {
  const title = `Ask restart replay ${info.workerIndex}`
  const response = await page.request.post("/api/threads", { data: { title } })
  expect(response.ok()).toBe(true)
  const created = await response.json()
  const id = created.thread?.id ?? created.id
  expect(id).toBeTruthy()
  const args = JSON.stringify({ questions: [{ id: "path", prompt: "Choose a path", options: [{ id: "one", label: "First" }, { id: "two", label: "Second" }] }] })
  const base = { thread_id: id, turn_id: "restart-ask-turn", agent_id: "manager", tool_call_id: "ask-original", created_at: "2026-10-23T12:00:00Z" }
  const recovered = { ...base, seq: 5, kind: "ask_recovered", text: args }
  const events = [
    { ...base, seq: 1, kind: "user_message", text: "Inspect the workspace" },
    { ...base, seq: 2, kind: "tool_call", text: `ask_user(${args})` },
    { ...base, seq: 3, kind: "tool_result", text: '{"outcome":"unknown","error":"outcome_unknown"}' },
    { ...base, seq: 4, kind: "engine_restart_checkpoint", text: '{"pause":"ask","manager_history":["private seed"]}' },
  ]
  let answered = false, logReads = 0, answers = 0, streams = 0
  let release!: () => void
  const barrier = new Promise<void>((resolve) => { release = resolve })
  await page.route(`**/api/threads/${id}/log*`, async (route) => {
    logReads++
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, events, has_more: false } })
  })
  await page.route(`**/api/threads/${id}`, async (route) => {
    if (route.request().method() !== "GET") return route.continue()
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, status: { running: !answered, turn_id: base.turn_id, awaiting_answer: !answered && events.some((e) => e.kind === "ask_recovered") } } })
  })
  await page.route(`**/api/threads/${id}/events*`, async (route) => {
    if (++streams === 1) await barrier
    const status = { running: !answered, turn_id: base.turn_id, awaiting_answer: !answered }
    const body = `event: ready\ndata: ${JSON.stringify({ seq: answered ? 7 : 4, status })}\n\n` +
      (!answered ? `event: ask_recovered\nid: 5\ndata: ${JSON.stringify(recovered)}\n\n` : "")
    await route.fulfill({ contentType: "text/event-stream", body })
  })
  await page.route(`**/api/threads/${id}/answers`, async (route) => {
    answers++
    expect(route.request().postDataJSON()).toMatchObject({ call_id: "ask-original", answers: { path: { answers: ["First"] } } })
    answered = true
    events.push({ ...base, seq: 6, kind: "tool_result", text: '{"answers":{"path":{"answers":["First"]}}}' }, { ...base, seq: 7, kind: "done", text: "" })
    await route.fulfill({ json: { answered: true } })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  const card = page.getByTestId("ask-card")
  await expect(card).toBeVisible()
  await expect(card.getByTestId("ask-submit")).toHaveCount(0)
  const readsBeforeLive = logReads
  events.push(recovered)
  release()
  await expect(card.getByTestId("ask-submit")).toBeVisible()
  await expect(card.getByRole("radio")).toHaveCount(3)
  expect(logReads).toBe(readsBeforeLive)
  await expect(page.getByTestId("status-badge")).toContainText("Your turn")
  await page.screenshot({ path: info.outputPath("ask-original-recovered-live.png") })
  await page.reload()
  await page.getByRole("button", { name: title, exact: true }).click()
  await expect(card.getByTestId("ask-submit")).toBeVisible()
  await card.getByRole("radio", { name: "First", exact: true }).click()
  await card.getByTestId("ask-submit").click()
  await expect.poll(() => answers).toBe(1)
  await page.reload()
  await page.getByRole("button", { name: title, exact: true }).click()
  await expect(card).toContainText("First")
  await expect(card.getByTestId("ask-submit")).toHaveCount(0)
  await expect(card.getByRole("radio")).toHaveCount(0)
  await expect(page.getByTestId("status-badge")).toContainText("Idle")
  expect(answers).toBe(1)
  await page.getByRole("tab", { name: "Trace", exact: true }).click()
  await page.getByRole("button", { name: /Full log/ }).click()
  await expect(page.getByTestId("trace-log")).toContainText("ask_recovered")
  await expect(page.getByTestId("trace-log")).toContainText("ask-original")
  await expect(page.getByTestId("trace-log")).not.toContainText("private seed")
  await page.screenshot({ path: info.outputPath("ask-original-settled-trace.png") })
})
