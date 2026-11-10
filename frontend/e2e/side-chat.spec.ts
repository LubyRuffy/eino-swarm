import { expect, test } from "@playwright/test"

import { composer, freshConversation, send, statusBadge, waitForIdle } from "./session"

test("asks follow-up questions about selected task text without feeding the task", async ({ page }) => {
  await freshConversation(page)
  const task = "Compare the two designs and summarize the tradeoffs."
  await send(page, task)
  const row = page.locator('[data-testid="thread-row"][aria-current="true"]')
  const id = await row.getAttribute("data-id")
  expect(id).toBeTruthy()
  const thread = `/api/threads/${id}`
  const initial = (await (await page.request.get(thread)).json()).status
  expect(initial).toMatchObject({ running: true, turn_id: expect.any(String) })

  const transcript = page.getByTestId("transcript")
  await transcript.getByText(task, { exact: true }).selectText()
  await page.getByRole("menuitem", { name: "Ask in side chat" }).click()
  const side = page.getByRole("complementary", { name: "Side chat" })
  await expect(side.getByTestId("side-chat-quote")).toContainText(task)
  await expect(transcript).toBeVisible()
  const input = side.getByRole("textbox", { name: "Side chat question" })
  const submit = side.getByRole("button", { name: "Send side question" })
  const sideRequest = () => page.waitForRequest((req) => req.method() === "POST" && new URL(req.url()).pathname === thread + "/side-chat")

  const first = "What should I inspect first?"
  const firstRequest = sideRequest()
  await input.fill(first)
  await submit.click()
  expect((await firstRequest).postDataJSON()).toMatchObject({ question: first, quote: task })
  const firstAnswer = `Side chat: ${first}`
  await expect(side.getByRole("log")).toContainText(firstAnswer)

  const second = "Which tradeoff does that address?"
  const secondRequest = sideRequest()
  await input.fill(second)
  await submit.click()
  expect((await secondRequest).postDataJSON()).toMatchObject({
    question: second, history: [{ question: first, answer: firstAnswer }],
  })
  await expect(side.getByRole("log")).toContainText(`Side chat: ${second}`)
  await waitForIdle(page)
  const { turns } = await (await page.request.get(thread + "/turns")).json()
  expect(turns).toHaveLength(1)
  expect(turns[0]).toMatchObject({ id: initial.turn_id, status: "done" })
  const { events } = await (await page.request.get(thread + "/log")).json()
  expect(JSON.stringify(events)).not.toContain(first)
  expect(JSON.stringify(events)).not.toContain(second)
  expect(JSON.stringify(await (await page.request.get(thread + "/followups")).json())).not.toContain(first)
  expect(JSON.stringify(await (await page.request.get(thread + "/followups")).json())).not.toContain(second)
  await expect(transcript.getByText(first, { exact: true })).toHaveCount(0)
})

test("opens, resizes and selects a model in a temporary side chat", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/side What is known so far?")
  await composer(page).press("Enter")
  const side = page.getByRole("complementary", { name: "Side chat" })
  await expect(side.getByRole("textbox", { name: "Side chat question" })).toHaveValue("What is known so far?")
  await expect(statusBadge(page)).toContainText("Idle")
  await expect(page.getByTestId("user-message")).toHaveCount(0)
  const start = (await side.boundingBox())!.width
  const separator = side.getByRole("separator", { name: "Resize side chat" })
  const edge = (await separator.boundingBox())!
  const x = edge.x + edge.width / 2
  const y = edge.y + edge.height / 2
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x - 96, y, { steps: 4 })
  await page.mouse.up()
  await expect.poll(async () => (await side.boundingBox())!.width).toBe(start + 96)
  await separator.focus()
  await separator.press("ArrowRight")
  await expect.poll(async () => (await side.boundingBox())!.width).toBe(start + 72)

  const { models } = await (await page.request.get("/api/models")).json()
  const chosen = models.find((row: { ready: boolean; model: string }) => row.ready && row.model)
  expect(chosen).toBeTruthy()
  const picker = side.getByRole("button", { name: "Side chat model" })
  await expect(picker).toHaveText("Follow task model")
  await picker.click()
  await page.getByRole("option", { name: chosen.label || chosen.model, exact: true }).click()
  const request = page.waitForRequest((req) => req.method() === "POST" && req.url().endsWith("/side-chat"))
  await side.getByRole("button", { name: "Send side question" }).click()
  expect((await request).postDataJSON()).toMatchObject({ provider_id: chosen.provider_id, model: chosen.model })
  await expect(side.getByRole("log")).toContainText("Side chat: What is known so far?")
  await expect(picker).toBeEnabled()
  await picker.click()
  await page.getByRole("option", { name: "Follow task model", exact: true }).click()
  await expect(picker).toHaveText("Follow task model")
  await expect(page.getByTestId("user-message")).toHaveCount(0)
  await side.getByRole("button", { name: "Close side chat" }).click()
  await expect(side).toHaveCount(0)
  await page.getByRole("button", { name: "Open side chat" }).click()
  await expect(side.getByRole("textbox", { name: "Side chat question" })).toHaveValue("")
})
