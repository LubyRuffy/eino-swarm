import { expect, test } from "@playwright/test"

import { composer, freshConversation, reloadOpenConversation, statusBadge, waitForIdle } from "./session"

test("plan command drafts then implements", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/plan inspect then change")
  await composer(page).press("Enter")
  await expect(page.getByTestId("plan-banner")).toContainText("Planning")
  await expect(statusBadge(page)).toContainText("Working")
  const ask = page.getByTestId("ask-card")
  await expect(ask).toBeVisible({ timeout: 30_000 })
  await expect(ask).toContainText("Your answer needed")
  await expect(ask.getByTestId("ask-mark")).toBeVisible()
  await expect(ask).not.toHaveClass(/animate-ask-ring/)
  await expect(ask.locator("[data-testid=ask-mark] .animate-ping")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Your turn")
  await expect(
    statusBadge(page).locator("[data-testid=ask-mark] .animate-ping"),
  ).toHaveCount(1)
  expect(await ask.evaluate((el) => {
    const col = el.closest(".content-column")
    if (!(col instanceof HTMLElement)) return false
    const cw = col.getBoundingClientRect().width
    return cw > 512 && Math.abs(el.getBoundingClientRect().width - cw) < 2
  })).toBe(true)
  await page.getByTestId("ask-option-safer").click()
  await page.getByTestId("ask-submit").click()
  await waitForIdle(page)
  await expect(page.getByTestId("plan-text")).toContainText("# Plan")
  await expect(page.getByTestId("transcript")).toContainText("Plan updated.")
  await page.getByTestId("plan-implement").click()
  await expect(statusBadge(page)).toContainText("Working")
  await waitForIdle(page)
  await expect(page.getByTestId("plan-banner")).toHaveCount(0)
  await expect(page.getByTestId("transcript")).toContainText(
    "The human accepted the plan. Execute it.",
  )
})

test("a choice without an option id survives reload and resumes the same turn", async ({ page, request }, info) => {
  await freshConversation(page)
  const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  expect(threadID).toBeTruthy()
  await composer(page).fill("/plan inspect then change")
  await composer(page).press("Enter")
  await expect(statusBadge(page)).toContainText("Your turn", { timeout: 30_000 })
  const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
  expect(turns).toHaveLength(1)
  const turnID = turns[0].id as string
  type Event = { kind: string; text: string; tool_call_id?: string }
  const { events } = await (await request.get(`/api/trace/${turnID}`)).json() as { events: Event[] }
  const call = events.find((event) => event.kind === "tool_call" && event.text.startsWith("ask_user("))
  expect(call?.tool_call_id).toBeTruthy()
  const args = JSON.parse(call!.text.slice("ask_user(".length, -1)) as { questions: { options: { id?: string; label: string }[] }[] }
  expect(args.questions[0].options[1]).not.toHaveProperty("id")
  const chosenLabel = args.questions[0].options[1].label
  const ask = page.getByTestId("ask-card")
  await page.screenshot({ path: info.outputPath("missing-option-id-live.png"), animations: "disabled" })
  await expect(ask).toBeVisible()
  await expect(ask.getByRole("radio")).toHaveCount(3)
  await expect(ask.getByRole("radio", { name: chosenLabel, exact: true })).toBeVisible()

  await reloadOpenConversation(page)
  await expect(statusBadge(page)).toContainText("Your turn")
  await expect(ask.getByRole("radio")).toHaveCount(3)
  await page.screenshot({ path: info.outputPath("missing-option-id-replayed.png"), animations: "disabled" })
  await ask.getByRole("radio", { name: chosenLabel, exact: true }).click()
  await ask.getByTestId("ask-submit").click()
  await waitForIdle(page)

  const finished = await (await request.get(`/api/trace/${turnID}`)).json() as { turn: { id: string; status: string }; events: Event[] }
  expect(finished.turn).toMatchObject({ id: turnID, status: "done" })
  const results = finished.events.filter((event) => event.kind === "tool_result" && event.tool_call_id === call!.tool_call_id)
  expect(results).toHaveLength(1)
  expect(JSON.parse(results[0].text)).toMatchObject({ answers: { approach: { answers: [chosenLabel] } } })
  expect(finished.events.filter((event) => event.kind === "done")).toHaveLength(1)
  const after = await (await request.get(`/api/threads/${threadID}/turns`)).json()
  expect(after.turns).toHaveLength(1)
  expect(after.turns[0]).toMatchObject({ id: turnID, status: "done" })

  await reloadOpenConversation(page)
  await expect(ask).toContainText(chosenLabel)
  await expect(ask.getByTestId("ask-submit")).toHaveCount(0)
  await expect(ask.getByRole("radio")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Idle")
  await page.screenshot({ path: info.outputPath("missing-option-id-settled.png"), animations: "disabled" })
})


test("a plan objective keeps its literal goal marker without activating a goal", async ({ page, request }, info) => {
  await freshConversation(page)
  const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  expect(threadID).toBeTruthy()
  const objective = "Inspect the assigned operation /goal"
  const sent = page.waitForRequest((request) => request.method() === "POST"
    && new URL(request.url()).pathname === `/api/threads/${threadID}/turns`)
  await composer(page).fill(`/plan ${objective}`)
  await composer(page).press("Enter")
  expect((await sent).postDataJSON()).toEqual({ text: `/plan ${objective}` })
  await expect(page.getByTestId("plan-banner")).toContainText("Planning")
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Your turn", { timeout: 30_000 })
  await expect(page.getByTestId("user-message")).toHaveText(objective)
  const { thread } = await (await request.get(`/api/threads/${threadID}`)).json()
  expect(thread).toMatchObject({ goal: "", plan_mode: true })
  const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
  expect(turns).toHaveLength(1)
  expect(turns[0]).toMatchObject({ user_text: objective })

  await page.getByTestId("ask-option-safer").click()
  await page.getByTestId("ask-submit").click()
  await waitForIdle(page)
  await reloadOpenConversation(page)
  await expect(page.getByTestId("plan-banner")).toContainText("Planning")
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("user-message")).toHaveText(objective)
  const replayed = await (await request.get(`/api/threads/${threadID}`)).json()
  expect(replayed.thread).toMatchObject({ goal: "", plan_mode: true })
  const trace = await (await request.get(`/api/trace/${turns[0].id}`)).json()
  expect(trace.events.some((event: { kind: string }) => event.kind === "goal" || event.kind === "goal_complete")).toBe(false)
  await page.screenshot({ path: info.outputPath("plan-retains-literal-goal.png"), animations: "disabled" })
})
