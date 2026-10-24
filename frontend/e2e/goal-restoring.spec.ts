import { expect, test, type Page, type APIRequestContext } from "@playwright/test"

async function openGoal(page: Page, request: APIRequestContext) {
  const created = await request.post("/api/threads", { data: { title: "Goal restoration ownership" } })
  const { thread } = await created.json()
  expect((await request.patch(`/api/threads/${thread.id}`, { data: { goal: "Existing objective" } })).ok()).toBe(true)
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.locator(`[data-testid="thread-row"][data-id="${thread.id}"]`).getByTestId("row-label").click()
  await page.getByTestId("goal-text").click()
  await page.getByTestId("goal-edit").fill("Retained objective edit")
  return thread.id as string
}

test("a focused goal edit survives Enter and blur while restoration blocks goal changes", async ({ page, request }, info) => {
  let startup = "ready", patches = 0
  await page.route("**/api/meta", async (route) => {
    const response = await route.fetch()
    await route.fulfill({ response, json: { ...await response.json(), startup, restore: { completed: 0, total: 1 } } })
  })
  const id = await openGoal(page, request)
  await page.route(`**/api/threads/${id}`, async (route) => {
    if (route.request().method() !== "PATCH") { await route.continue(); return }
    patches++
    await route.fulfill({ status: 409, json: { code: "restoring", error: "Engine is restoring unfinished tasks." } })
  })
  await expect(page.getByTestId("goal-edit")).toBeFocused()
  startup = "restoring"
  await expect(page.getByTestId("engine-startup")).toBeVisible()
  await page.keyboard.press("Enter")
  await page.getByTestId("thread-title").click()
  expect(patches).toBe(0)
  await expect(page.getByTestId("goal-edit")).toBeDisabled()
  await expect(page.getByTestId("goal-edit")).toHaveValue("Retained objective edit")
  await expect(page.getByRole("button", { name: "Clear goal" })).toBeDisabled()
  startup = "ready"
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  await expect(page.getByTestId("goal-edit")).toBeEnabled()
  expect(patches).toBe(0)
  await page.screenshot({ path: info.outputPath("focused-goal-edit-after-restoration.png") })
})

test("a restoring goal PATCH refusal keeps the editor and original conversation until an acknowledged retry", async ({ page, request }, info) => {
  const id = await openGoal(page, request)
  let patches = 0, accept = false
  await page.route(`**/api/threads/${id}`, async (route) => {
    if (route.request().method() !== "PATCH") { await route.continue(); return }
    patches++
    if (accept) { await route.continue(); return }
    await route.fulfill({ status: 409, json: { code: "restoring", error: "Engine is restoring unfinished tasks." } })
  })
  await page.getByTestId("goal-edit").press("Enter")
  await expect(page.getByText("Engine is restoring unfinished tasks.", { exact: true }).first()).toBeVisible()
  await expect(page.getByTestId("goal-edit")).toHaveValue("Retained objective edit")
  await expect(page.locator('[data-testid="thread-row"][aria-current="true"]')).toHaveAttribute("data-id", id)
  expect((await (await request.get(`/api/threads/${id}`)).json()).thread.goal).toBe("Existing objective")
  await expect(page.getByTestId("engine-startup")).toHaveCount(0)
  expect(patches).toBe(1)
  await page.screenshot({ path: info.outputPath("refused-goal-edit-retains-text.png") })
  accept = true
  await page.getByTestId("goal-edit").press("Enter")
  await expect(page.getByTestId("goal-edit")).toHaveCount(0)
  await expect(page.getByTestId("goal-text")).toHaveText("Retained objective edit")
  expect(patches).toBe(2)
})
