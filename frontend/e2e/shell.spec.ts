import { expect, test } from "@playwright/test"

test("keyboard shortcuts open the palette, a conversation and the panel", async ({
  page,
}) => {
  await page.goto("/")

  await page.keyboard.press("ControlOrMeta+k")
  const palette = page.getByPlaceholder(
    "Search conversations, or type a command…",
  )
  await expect(palette).toBeVisible()
  await palette.press("Escape")

  await page.keyboard.press("ControlOrMeta+n")
  await expect(page.getByTestId("thread-title")).toHaveText("New conversation")

  // ⌘\ collapses the right-hand panel and brings it back
  await expect(page.getByRole("tab", { name: "Agents" })).toBeVisible()
  await page.keyboard.press("ControlOrMeta+\\")
  await expect(page.getByRole("tab", { name: "Agents" })).toBeHidden()
  await page.keyboard.press("ControlOrMeta+\\")
  await expect(page.getByRole("tab", { name: "Agents" })).toBeVisible()

  // ⌘B hides the conversation list the way Codex/Cursor hide theirs, and
  // brings it back. New conversation lives in that list, so hiding it must
  // not trap the user without a way to restore it.
  await expect(page.getByRole("button", { name: "New conversation" })).toBeVisible()
  await page.keyboard.press("ControlOrMeta+b")
  await expect(page.getByRole("button", { name: "New conversation" })).toBeHidden()
  await expect(page.getByRole("button", { name: "Show conversations" })).toBeVisible()
  await page.keyboard.press("ControlOrMeta+b")
  await expect(page.getByRole("button", { name: "New conversation" })).toBeVisible()
})

test("escape stops a running turn", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation" }).click()
  await page.getByTestId("composer-input").fill("Something that takes a while")
  await page.getByTestId("composer-input").press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Working")

  await page.keyboard.press("Escape")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })
  // stopping is not failing: the partial answer stays on screen
  await expect(page.getByTestId("transcript")).toContainText(
    "Something that takes a while",
  )
})

// A config that has never been saved from the UI is the state every install
// starts in, and it used to crash the Tools tab: Go marshals an empty exception
// list as null, and the tab read it as an array. Saving anything hid the bug,
// so this checks the wire shape as well as the render.
test("the tool catalogue renders on a config that was never saved", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  expect(Array.isArray(settings.tools.disabled)).toBe(true)
  expect(Array.isArray(settings.tools.enabled)).toBe(true)

  const crashes: string[] = []
  page.on("pageerror", (e) => crashes.push(e.message))

  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Tools" }).click()

  const tools = dialog.getByRole("tabpanel")
  await expect(tools.getByText("read", { exact: true })).toBeVisible()
  // every group is reachable, including the one furthest down
  await expect(tools.getByText("web_search", { exact: true })).toBeVisible()
  await expect(dialog.getByLabel("Skip the proxy for")).toBeVisible()
  await dialog.getByRole("button", { name: "Cancel" }).click()

  expect(crashes).toEqual([])
})

test("settings round-trip through the config file", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()

  const dialog = page.getByRole("dialog")
  await expect(dialog.getByText("config.yaml")).toBeVisible()

  await dialog.getByRole("tab", { name: "Swarm" }).click()
  const concurrency = dialog.getByLabel("Sub-agents at once")
  await concurrency.fill("3")
  await dialog.getByRole("button", { name: "Save" }).click()
  await expect(dialog).toBeHidden()

  // reopening reads it back from the server, not from React state
  await page.getByRole("button", { name: "Settings" }).click()
  await page.getByRole("dialog").getByRole("tab", { name: "Swarm" }).click()
  await expect(
    page.getByRole("dialog").getByLabel("Sub-agents at once"),
  ).toHaveValue("3")

  // and the tool catalogue is listed with a switch per tool
  await page.getByRole("dialog").getByRole("tab", { name: "Tools" }).click()
  const tools = page.getByRole("dialog").getByRole("tabpanel")
  await expect(tools.getByText("read", { exact: true })).toBeVisible()
  expect(await tools.getByRole("switch").count()).toBeGreaterThan(5)
  await page.getByRole("dialog").getByRole("button", { name: "Cancel" }).click()
})

test("switches theme and remembers it", async ({ page }) => {
  await page.goto("/")
  const root = page.locator("html")
  const wasDark = await root.evaluate((el) => el.classList.contains("dark"))

  await page.getByRole("button", { name: "Switch theme" }).click()
  await expect(root).toHaveClass(wasDark ? /^(?!.*dark).*$/ : /dark/)

  await page.reload()
  await expect(root).toHaveClass(wasDark ? /^(?!.*dark).*$/ : /dark/)
})

test("renames and deletes a conversation from the sidebar", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation" }).click()
  await page.getByTestId("composer-input").fill("Name this one")
  await page.getByTestId("composer-input").press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })

  const row = page.locator("aside").first().getByText("Name this one").first()
  await row.hover()
  await page
    .locator("aside")
    .first()
    .getByRole("button", { name: "More" })
    .first()
    .click()
  await page.getByRole("menuitem", { name: "Rename" }).click()
  const input = page.locator("aside").first().locator("input")
  await input.fill("Renamed by the test")
  await input.press("Enter")
  await expect(
    page.locator("aside").first().getByText("Renamed by the test"),
  ).toBeVisible()

  await page
    .locator("aside")
    .first()
    .getByRole("button", { name: "More" })
    .first()
    .click()
  await page.getByRole("menuitem", { name: "Delete" }).click()
  await expect(
    page.locator("aside").first().getByText("Renamed by the test"),
  ).toBeHidden()
})
