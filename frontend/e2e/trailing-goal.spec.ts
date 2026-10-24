import { expect, test, type APIRequestContext, type Page, type TestInfo } from "@playwright/test"
import { writeFile } from "node:fs/promises"

import { composer, freshConversation, reloadOpenConversation, waitForIdle } from "./session"

const sendButton = (page: Page) => page.getByRole("button", { name: "Send", exact: true })
const selectedThread = (page: Page) => page.locator('[data-testid="thread-row"][aria-current="true"]')
const draftPath = (id: string) => `/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`

function observeSubmissions(page: Page, id: string) {
  const goals: unknown[] = []
  const messages: unknown[] = []
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === "PATCH" && path === `/api/threads/${id}`) {
      const body = request.postDataJSON()
      if (Object.hasOwn(body, "goal")) goals.push(body)
    }
    if (request.method() === "POST" && [`/api/threads/${id}/steer`, `/api/threads/${id}/turns`, `/api/threads/${id}/followups`].includes(path)) {
      messages.push(request.postDataJSON())
    }
  })
  return { goals, messages }
}

async function submitStandingObjective(
  page: Page,
  request: APIRequestContext,
  info: TestInfo,
  objective: string,
  submit: "click" | "enter",
) {
  const id = (await selectedThread(page).getAttribute("data-id"))!
  const text = await composer(page).inputValue()
  await expect(page.getByTestId("composer-draft-status")).toContainText("Draft saved on this computer")
  const before = (await (await request.get(draftPath(id))).json()).draft
  expect(before).toMatchObject({ text, state: "editing" })
  const observed = observeSubmissions(page, id)

  // The screenshot's failure was a populated objective with a disabled arrow.
  await expect(sendButton(page)).toBeEnabled()
  await expect(page.getByTestId("composer-command-hint")).toHaveCount(0)
  await page.screenshot({ path: info.outputPath("trailing-goal-ready.png"), animations: "disabled" })
  if (submit === "click") await sendButton(page).click()
  else await composer(page).press("Enter")

  await expect(page.getByTestId("goal-banner")).toContainText(objective)
  await expect(composer(page)).toHaveValue("")
  await waitForIdle(page)
  // Setting a standing objective starts one human turn; the command token
  // must neither become that turn's text nor cause an extra ordinary send.
  expect(observed.goals).toEqual([])
  expect(observed.messages).toEqual([{ text: `/goal ${objective}` }])
  const { thread } = await (await request.get(`/api/threads/${id}`)).json()
  expect(thread).toMatchObject({ goal: objective, goal_complete: true, plan_mode: false })
  await expect(page.getByTestId("plan-banner")).toHaveCount(0)
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("transcript")).toContainText("Standing objective completed.")
  const { turns } = await (await request.get(`/api/threads/${id}/turns`)).json()
  expect(turns).toHaveLength(1)
  await expect(page.getByTestId("user-message")).toHaveCount(1)
  await expect(page.getByTestId("user-message")).toHaveText(objective)
  const cleared = (await (await request.get(draftPath(id))).json()).draft
  expect(cleared).toMatchObject({ text: "", state: "editing", files: [], quotes: [] })
  expect(cleared.revision).toBeGreaterThan(before.revision)

  await reloadOpenConversation(page)
  await expect(page.getByTestId("plan-banner")).toHaveCount(0)
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("transcript")).toContainText("Standing objective completed.")
  await expect(composer(page)).toHaveValue("")
  await expect(page.getByTestId("user-message")).toHaveCount(1)
  expect(observed.goals).toHaveLength(0)
  expect(observed.messages).toHaveLength(1)
  await page.screenshot({ path: info.outputPath("trailing-goal-retained.png"), animations: "disabled" })
}

for (const scenario of [
  { name: "a space", objective: "Inspect the assigned operation", separator: " ", submit: "click" },
  { name: "a newline", objective: "Inspect the first phase.\nContinue with verification.", separator: "\n", submit: "enter" },
  { name: "a Chinese objective without a space", objective: "检查当前状态并完成验证", separator: "", submit: "enter" },
] as const) {
  test(`a trailing /goal after ${scenario.name} sends the complete preceding objective`, async ({ page, request }, info) => {
    await freshConversation(page)
    await composer(page).fill(scenario.objective)
    if (scenario.separator === "\n") await composer(page).press("Shift+Enter")
    else if (scenario.separator) await composer(page).pressSequentially(scenario.separator)
    await composer(page).pressSequentially("/goal")
    await expect(composer(page)).toHaveValue(`${scenario.objective}${scenario.separator}/goal`)
    await submitStandingObjective(page, request, info, scenario.objective, scenario.submit)
  })
}

test("picking goal after an existing objective makes the arrow send that objective", async ({ page, request }, info) => {
  await freshConversation(page)
  const objective = "Verify the complete assigned operation"
  await composer(page).fill(objective)
  await composer(page).pressSequentially(" /")
  await expect(page.getByTestId("slash-menu")).toBeVisible()
  await page.getByTestId("slash-command-goal").click()
  await expect(page.getByTestId("slash-menu")).toHaveCount(0)
  await expect(composer(page)).toHaveValue(`${objective} /goal `)
  await submitStandingObjective(page, request, info, objective, "click")
})

for (const scenario of [
  { name: "a trailing goal token", objective: "Inspect the assigned operation /goal" },
  { name: "a leading plan token", objective: "/plan Inspect the assigned operation" },
]) {
  test(`a leading goal owns its entire argument containing ${scenario.name}`, async ({ page, request }, info) => {
    await freshConversation(page)
    await composer(page).fill(`/goal ${scenario.objective}`)
    await submitStandingObjective(page, request, info, scenario.objective, "click")
  })
}

test("bare goals still need an objective while unknown commands, quotations and paths remain messages", async ({ page, request }, info) => {
  await freshConversation(page)
  const id = (await selectedThread(page).getAttribute("data-id"))!
  const observed = observeSubmissions(page, id)
  for (const text of ["/goal", "/goal "]) {
    await composer(page).fill(text)
    await expect(sendButton(page)).toBeDisabled()
    await expect(page.getByTestId("composer-command-hint")).toBeVisible()
    await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  }

  const ordinary = ["Inspect /goals", "Inspect \"/goal\"", "Inspect /tmp/goal", "Inspect /tmp/中文/goal", "Inspect ./中文/goal"]
  for (const text of ordinary) {
    await test.step(text, async () => {
      await composer(page).fill(text)
      await expect(sendButton(page)).toBeEnabled()
      await expect(page.getByTestId("composer-command-hint")).toHaveCount(0)
      await expect(page.getByTestId("slash-menu")).toHaveCount(0)
      const sent = page.waitForResponse((response) => response.request().method() === "POST"
        && new URL(response.url()).pathname === `/api/threads/${id}/steer`)
      await composer(page).press("Enter")
      expect((await sent).status()).toBe(202)
      await waitForIdle(page)
      await expect(page.getByTestId("user-message").last()).toHaveText(text)
      await expect(page.getByTestId("goal-banner")).toHaveCount(0)
    })
  }
  expect(observed.goals).toEqual([])
  expect(observed.messages).toEqual(ordinary.map((text) => ({ text })))
  const { thread } = await (await request.get(`/api/threads/${id}`)).json()
  expect(thread.goal).toBeFalsy()
  await page.screenshot({ path: info.outputPath("literal-goal-messages.png"), animations: "disabled" })
})

test("a trailing goal marker starts a new objective after the old one completed", async ({ page, request }, info) => {
  await freshConversation(page)
  await composer(page).fill("/goal Inspect the previous operation.")
  await composer(page).press("Enter")
  await expect(page.getByTestId("transcript").getByText("Standing objective completed.", { exact: true })).toHaveCount(1)
  await waitForIdle(page)
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("status-badge")).toContainText("Idle", { timeout: 60_000 })
  const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  expect(threadID).toBeTruthy()
  const objective = "检查新的 assigned operation"
  const activated = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
  await composer(page).fill(`${objective} /goal  \n`)
  await expect(page.getByRole("button", { name: "Send", exact: true })).toBeEnabled()
  await composer(page).press("Enter")
  const sent = await activated
  expect(sent.status()).toBe(202)
  expect(sent.request().postDataJSON()).toEqual({ text: `/goal ${objective}` })
  const activation = await (await request.get(`/api/threads/${threadID}`)).json()
  expect(activation.thread).toMatchObject({ goal: objective, goal_complete: false, goal_blocked: false, goal_capped: false, goal_idle: false })
  await expect(page.getByTestId("goal-banner")).toContainText(objective)
  await waitForIdle(page)
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
  await expect(page.getByTestId("status-badge")).toContainText("Idle", { timeout: 60_000 })
  await expect(page.getByTestId("transcript").getByText("Standing objective completed.", { exact: true })).toHaveCount(2)
  const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
  const turn = turns.at(-1)
  expect(turn).toMatchObject({ user_text: objective, status: "done" })
  const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
  expect(trace.events.some((event: { kind: string }) => event.kind === "goal_complete")).toBe(true)
  await page.screenshot({ path: info.outputPath("trailing-goal-completed-objective.png"), animations: "disabled" })
  await writeFile(info.outputPath("trailing-goal-evidence.json"), JSON.stringify({ activation, turn, trace }, null, 2), { mode: 0o600 })
})
