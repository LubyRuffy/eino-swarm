import { expect, test, type Page } from "@playwright/test"
import { mkdtempSync } from "node:fs"
import { tmpdir } from "node:os"
import { basename, join } from "node:path"

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
  // brings it back. The hide/show control lives in the window title bar, so
  // hiding the list must not trap the user without a way to restore it.
  const hideList = page.getByRole("banner").getByRole("button", {
    name: "Hide conversations",
  })
  await expect(hideList).toBeVisible()
  const newConversation = page.getByRole("button", { name: "New conversation", exact: true })
  const headerBox = await page.getByRole("banner").boundingBox()
  const listBox = await newConversation.boundingBox()
  expect(headerBox).not.toBeNull()
  expect(listBox).not.toBeNull()
  expect(headerBox!.y + headerBox!.height).toBeLessThanOrEqual(listBox!.y + 1)

  await expect(newConversation).toBeVisible()
  await page.keyboard.press("ControlOrMeta+b")
  await expect(newConversation).toBeHidden()
  await expect(
    page.getByRole("banner").getByRole("button", { name: "Show conversations" }),
  ).toBeVisible()
  await page.keyboard.press("ControlOrMeta+b")
  await expect(page.getByRole("button", { name: "New conversation", exact: true })).toBeVisible()
})

test("semantic search is off until a model is pinned", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models" }).click()
  await expect(dialog.getByLabel("Semantic search")).not.toBeChecked()
  await expect(dialog.getByLabel("Embedding model")).toHaveCount(0)
})

test("the command palette finds a conversation by words in its body", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  const box = page.getByTestId("composer-input")
  await box.fill("unique-body needle for the palette")
  await box.press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Working")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", { timeout: 60_000 })
  await renameRecentsRow(page, 0, "gamma")

  await page.keyboard.press("ControlOrMeta+k")
  const palette = page.getByPlaceholder(
    "Search conversations, or type a command…",
  )
  await expect(palette).toBeVisible()
  const pending = page.waitForResponse((res) => res.url().includes("/api/search"))
  await palette.fill("unique-body")
  const res = await pending
  expect(res.ok()).toBeTruthy()
  const body = (await res.json()) as {
    hits?: Array<{ title?: string; snippet?: string }>
  }
  expect(body.hits?.some((h) => h.title === "gamma")).toBeTruthy()
  const dialog = page.getByRole("dialog")
  await expect(
    dialog.getByRole("group", { name: "Conversations" }).getByText("gamma", {
      exact: true,
    }),
  ).toBeVisible()
  await expect(dialog.getByText(/unique-body needle/)).toBeVisible()
})

test("the terminal button opens a shell in the conversation workspace", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await page.getByRole("banner").getByRole("button", { name: "Open terminal" }).click()
  const panel = page.getByTestId("terminal-panel")
  await expect(panel).toBeVisible()
  await page.getByRole("banner").getByRole("button", { name: "Open terminal" }).click()
  await expect(panel.getByRole("button", { name: "Close this terminal" })).toHaveCount(2)
  await page.keyboard.press("ControlOrMeta+j")
  await expect(panel).toBeHidden()
  await page.keyboard.press("ControlOrMeta+j")
  await expect(panel).toBeVisible()
})

test("a terminal in a project conversation starts in that project's directory", async ({
  page,
}) => {
  const dir = mkdtempSync(join(tmpdir(), "zwai-term-"))
  const label = basename(dir)
  await page.goto("/")
  await page.getByRole("button", { name: "New project" }).click()
  const name = `Term ${Date.now()}`
  await page.getByLabel("Name").fill(name)
  await page.getByLabel("Working directory").fill(dir)
  await page.getByRole("button", { name: "Create project" }).click()
  await page.getByRole("button", { name: `New conversation in ${name}` }).click()
  await page.getByRole("banner").getByRole("button", { name: "Open terminal" }).click()
  const panel = page.getByTestId("terminal-panel")
  await expect(panel).toBeVisible()
  await expect(panel.getByText(label, { exact: true })).toBeVisible({
    timeout: 10_000,
  })
})

test("an external link opens a new window instead of replacing the app", async ({
  page,
}) => {
  await page.goto("/")
  await expect(
    page.getByRole("button", { name: "New conversation", exact: true }),
  ).toBeVisible()
  const appURL = page.url()
  await page.evaluate(() => {
    const a = document.createElement("a")
    a.href = "https://example.invalid/leave"
    a.textContent = "leave-app"
    document.body.appendChild(a)
  })
  const popupPromise = page.waitForEvent("popup")
  await page.getByRole("link", { name: "leave-app" }).click()
  const popup = await popupPromise
  await expect(page).toHaveURL(appURL)
  await expect(
    page.getByRole("button", { name: "New conversation", exact: true }),
  ).toBeVisible()
  expect(popup).not.toBe(page)
  await popup.close()
})

test("Projects and Recents share one left gutter", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  const projects = page.getByText("Projects", { exact: true })
  const recents = page.getByText("Recents", { exact: true })
  await expect(recents).toBeVisible()
  const projectBox = await projects.boundingBox()
  const recentsBox = await recents.boundingBox()
  expect(projectBox).not.toBeNull()
  expect(recentsBox).not.toBeNull()
  expect(Math.abs(projectBox!.x - recentsBox!.x)).toBeLessThan(2)

  const folderBox = await page.getByTestId("project-list").boundingBox()
  const threadBox = await page.getByTestId("recents-list").locator("div").first().boundingBox()
  expect(folderBox).not.toBeNull()
  expect(threadBox).not.toBeNull()
  expect(Math.abs(folderBox!.x - threadBox!.x)).toBeLessThan(2)

  await page.getByRole("button", { name: "New project" }).click()
  const name = `Align ${Date.now()}`
  await page.getByLabel("Name").fill(name)
  await page.getByRole("button", { name: "Create project" }).click()
  // Recents already has rows from earlier specs; the gutter is the same on
  // every row, so the first is enough. The new folder is the one we named.
  const recentName = await page
    .getByTestId("recents-list")
    .getByTestId("row-label")
    .first()
    .boundingBox()
  const projectName = await page
    .getByTestId("project-row")
    .filter({ hasText: name })
    .getByTestId("row-label")
    .boundingBox()
  expect(recentName).not.toBeNull()
  expect(projectName).not.toBeNull()
  expect(Math.abs(recentName!.x - projectName!.x)).toBeLessThan(2)
})

test("collapsing Recents hides its conversations across reload", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row").first(),
  ).toBeVisible()
  const recentsFold = page.getByRole("button", { name: "Recents" }).locator("[data-testid=section-fold]")
  await expect(recentsFold).toHaveCSS("opacity", "0")
  await page.getByRole("button", { name: "Recents" }).hover()
  await expect(recentsFold).toHaveCSS("opacity", "1")
  await page.getByRole("button", { name: "Recents" }).click()
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row"),
  ).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Recents" })).toHaveAttribute(
    "aria-expanded",
    "false",
  )
  await expect(recentsFold).toHaveCSS("opacity", "1")
  await page.reload()
  await expect(page.getByRole("button", { name: "Recents" })).toHaveAttribute(
    "aria-expanded",
    "false",
  )
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row"),
  ).toHaveCount(0)
})

test("the composer sits on the transcript without a dock hairline", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  const fade = page.getByTestId("composer-fade")
  const input = page.getByTestId("composer-input")
  const stage = page.getByTestId("composer-stage")
  await expect(fade).toBeVisible()
  await expect(input).toBeVisible()
  const stageBox = await stage.boundingBox()
  const inputBox = await input.boundingBox()
  const fadeBox = await fade.boundingBox()
  expect(stageBox).not.toBeNull()
  expect(inputBox).not.toBeNull()
  expect(fadeBox).not.toBeNull()
  // the box is painted on the conversation, not in a slab under a hairline
  expect(inputBox!.y).toBeGreaterThan(stageBox!.y)
  expect(inputBox!.y + inputBox!.height).toBeLessThanOrEqual(
    stageBox!.y + stageBox!.height + 1,
  )
  expect(fadeBox!.y).toBeLessThan(inputBox!.y)
  // Default Textarea fill overflowed the rounded card and ate the top
  // corners. Nested composer chrome has to stay transparent.
  const bg = await input.evaluate((el) => getComputedStyle(el).backgroundColor)
  expect(bg === "rgba(0, 0, 0, 0)" || bg === "transparent").toBeTruthy()
})

test("dragging the conversation list border resizes it without selecting text", async ({
  page,
}) => {
  await page.goto("/")
  const handle = page.getByRole("separator", { name: "Resize the conversation list" })
  const list = page.getByTestId("conversation-list")
  const leading = page.getByTestId("titlebar-leading")
  const start = await list.boundingBox()
  const box = await handle.boundingBox()
  expect(start).not.toBeNull()
  expect(box).not.toBeNull()
  await page.mouse.move(box!.x + 1, box!.y + 80)
  await page.mouse.down()
  await page.mouse.move(box!.x + 80, box!.y + 80, { steps: 8 })
  const selected = await page.evaluate(
    () => window.getSelection()?.toString().trim() ?? "",
  )
  await page.mouse.up()
  expect(selected).toBe("")
  const dragged = await list.boundingBox()
  expect(dragged).not.toBeNull()
  expect(dragged!.width).toBeGreaterThan(start!.width + 40)
  const lead = await leading.boundingBox()
  expect(lead).not.toBeNull()
  expect(Math.abs(lead!.width - dragged!.width)).toBeLessThan(2)

  await page.reload()
  const remembered = await page.getByTestId("conversation-list").boundingBox()
  expect(remembered).not.toBeNull()
  expect(Math.abs(remembered!.width - dragged!.width)).toBeLessThan(2)
})

test("dragging the panel border does not select text", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  const handle = page.getByRole("separator", { name: "Resize the side panel" })
  const box = await handle.boundingBox()
  expect(box).not.toBeNull()
  // A real mouse drag across the transcript used to paint a selection.
  await page.mouse.move(box!.x + 1, box!.y + 80)
  await page.mouse.down()
  await page.mouse.move(box!.x - 180, box!.y + 80, { steps: 10 })
  const selected = await page.evaluate(
    () => window.getSelection()?.toString().trim() ?? "",
  )
  await page.mouse.up()
  expect(selected).toBe("")
})

test("escape stops a running turn", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
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

test("⌘F searches the conversation and Escape closes find before stopping a turn", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  const input = page.getByTestId("composer-input")
  await input.fill("Unique phrase for find")
  await input.press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Working")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })

  await page.keyboard.press("ControlOrMeta+f")
  const find = page.getByLabel("Find in conversation")
  await expect(find).toBeVisible()
  await expect(find).toBeFocused()
  await find.fill("Unique phrase")
  await expect(page.getByTestId("conversation-find-count")).toContainText(
    /1 \/ \d+ results/,
  )

  await page.keyboard.press("Escape")
  await expect(page.getByTestId("conversation-find")).toBeHidden()

  await input.fill("Something that takes a while")
  await input.press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Working")
  await page.keyboard.press("ControlOrMeta+f")
  await expect(page.getByLabel("Find in conversation")).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(page.getByTestId("conversation-find")).toBeHidden()
  await expect(page.getByTestId("status-badge")).toContainText("Working")
  await page.keyboard.press("Escape")
  await expect(page.getByTestId("status-badge")).toContainText("Idle", {
    timeout: 60_000,
  })
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
  await dialog.getByRole("button", { name: "Back to app" }).click()

  expect(crashes).toEqual([])
})

test("settings round-trip through the config file", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()

  const dialog = page.getByRole("dialog")
  await expect(dialog.getByText("config.yaml")).toBeVisible()

  await dialog.getByRole("tab", { name: "Swarm" }).click()
  await expect(
    dialog.getByLabel("Name conversations automatically"),
  ).toBeChecked()
  const concurrency = dialog.getByLabel("Sub-agents at once")
  await concurrency.fill("3")
  await dialog.getByRole("button", { name: "Back to app" }).click()
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
  await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
})

test("per-note memory cap round-trips through settings", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Memory" }).click()
    const cap = dialog.getByLabel("Per-note cap (characters)")
    await expect(cap).toHaveValue(String(settings.memory.entry_max))
    await cap.fill("280")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.memory.entry_max).toBe(280)

    await page.getByRole("button", { name: "Settings" }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "Memory" }).click()
    await expect(
      page.getByRole("dialog").getByLabel("Per-note cap (characters)"),
    ).toHaveValue("280")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", { data: { memory: settings.memory } })
  }
})

test("personality round-trips through settings", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Personality" }).click()
    const box = dialog.getByLabel("Personal preferences")
    await expect(box).toHaveValue(settings.personality?.instructions ?? "")
    await box.fill("prefer compact replies")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.personality.instructions).toBe(
      "prefer compact replies",
    )

    await page.getByRole("button", { name: "Settings" }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "Personality" }).click()
    await expect(
      page.getByRole("dialog").getByLabel("Personal preferences"),
    ).toHaveValue("prefer compact replies")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", {
      data: { personality: settings.personality ?? { instructions: "" } },
    })
  }
})

test("pins a title-generation model when more than one name is listed", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [
          {
            ...provider,
            model: "alpha",
            catalog: ["alpha", "beta"],
          },
        ],
      },
      swarm: { ...settings.swarm, title_provider: "", title_model: "" },
    },
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByText("Auxiliary models").scrollIntoViewIfNeeded()
    await expect(dialog.getByText("Auxiliary models")).toBeVisible()
    const picker = dialog.getByLabel("Title generation model")
    await expect(picker).toContainText("Automatic")
    await picker.click()
    await page.getByRole("option", { name: "beta" }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.swarm.title_model).toBe("beta")
    expect(saved.settings.swarm.title_provider).toBe(provider.id)

    await page.getByRole("button", { name: "Settings" }).click()
    await expect(
      page.getByRole("dialog").getByLabel("Title generation model"),
    ).toContainText("beta")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", {
      data: { models: settings.models, swarm: settings.swarm },
    })
  }
})

test("discovers models into the default dropdown", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [{ ...provider, base_url: "http://endpoint.invalid/v1" }],
      },
    },
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    const heading = (provider.label || provider.id).trim()
    await expect(
      dialog.getByRole("button", { name: `${heading} details` }),
    ).toBeVisible()
    await expect(dialog.getByRole("textbox", { name: "Base URL" })).toHaveCount(0)
    await dialog.getByRole("button", { name: `${heading} details` }).click()
    await expect(dialog.getByLabel("Provider")).toBeVisible()
    await expect(dialog.getByLabel("Base URL")).toHaveValue(
      "http://endpoint.invalid/v1",
    )
    await dialog.getByRole("button", { name: "Discover models" }).click()
    // Opening the Radix listbox inside a Dialog marks the dialog aria-hidden,
    // so Back to app would hang. The combobox showing the name is enough: that is
    // the control the composer also reads after the sheet writes.
    await expect(
      dialog.getByRole("combobox", { name: "Default model" }),
    ).toContainText("mock")
    await dialog.getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})

test("a failed model listing toasts over the open provider", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1100, height: 700 })
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [{ ...provider, base_url: "http://endpoint.invalid/v1" }],
      },
    },
  })
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    const heading = (provider.label || provider.id).trim()
    await dialog.getByRole("button", { name: `${heading} details` }).click()
    const discover = dialog.getByRole("button", { name: "Discover models" })
    await expect(discover).toBeEnabled()
    await page.route("**/api/models/discover", async (route) => {
      await route.fulfill({
        status: 502,
        contentType: "application/json",
        body: JSON.stringify({
          error: "provider: list models: connect: no route to host",
        }),
      })
    })
    await discover.click()
    const toast = page.getByRole("alert")
    await expect(toast).toContainText("Couldn't list models")
    await expect(toast).toContainText("connect: no route to host")
    await expect(toast).toBeInViewport()
    await toast.getByRole("button", { name: "Dismiss" }).click()
    await expect(toast).toHaveCount(0)
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})

test("Back to app stays on screen when Swarm is taller than the window", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1100, height: 700 })
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Swarm" }).click()

  const back = dialog.getByRole("button", { name: "Back to app" })
  await expect(back).toBeVisible()
  const box = await back.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.y + box!.height).toBeLessThanOrEqual(700)

  const last = dialog.getByLabel("Goal auto-compact at (%)")
  await last.scrollIntoViewIfNeeded()
  const lastBox = await last.boundingBox()
  const backBox = await back.boundingBox()
  expect(lastBox).not.toBeNull()
  expect(backBox).not.toBeNull()
  expect(backBox!.y).toBeGreaterThanOrEqual(0)
  expect(backBox!.y + backBox!.height).toBeLessThanOrEqual(700)
  // Back to app lives in the left rail, not in the scrolling page.
  expect(backBox!.y + backBox!.height).toBeLessThanOrEqual(lastBox!.y + 1)
})

test("the Models tab does not clip the add-endpoint button's border", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  const button = dialog.getByRole("button", { name: "Add a provider" })
  await button.scrollIntoViewIfNeeded()
  const room = await button.evaluate((el) => {
    const panel = el.closest('[role="tabpanel"]')
    if (!(panel instanceof HTMLElement)) return Number.NEGATIVE_INFINITY
    return panel.getBoundingClientRect().bottom - el.getBoundingClientRect().bottom
  })
  // A 1px outline flush with the overflow clip looks like the button has
  // no bottom border. The scrollport keeps padding under the last control.
  expect(room).toBeGreaterThanOrEqual(1)
})

test("switches conversation width from the title bar and fills the pane", async ({
  page,
  request,
}) => {
  // comfortable is a 48rem column; without a wide pane the two modes look
  // the same. Collapse the right panel so the fill is visible.
  await page.goto("/")
  try {
    await page.keyboard.press("ControlOrMeta+\\")
    await expect(page.getByRole("tab", { name: "Agents" })).toBeHidden()

    const root = page.locator("html")
    await expect(root).toHaveAttribute("data-content-width", "comfortable")

    const before = await columnFill(page)
    await page.getByRole("button", { name: "Switch to wide layout" }).click()
    await expect(root).toHaveAttribute("data-content-width", "full")
    await expect(
      page.getByRole("button", { name: "Switch to standard layout" }),
    ).toHaveAttribute("aria-pressed", "true")

    const after = await columnFill(page)
    expect(after).toBeGreaterThan(before)
    expect(after).toBeGreaterThan(0.9)

    await page.reload()
    await expect(root).toHaveAttribute("data-content-width", "full")

    await page.getByRole("button", { name: "Switch to standard layout" }).click()
    await expect(root).toHaveAttribute("data-content-width", "comfortable")
  } finally {
    await request.put("/api/settings", {
      data: { ui: { content_width: "comfortable" } },
    })
  }
})

async function columnFill(page: Page): Promise<number> {
  return page.evaluate(() => {
    const col = document.querySelector(".content-column")
    const stage = document.querySelector('[data-testid="composer-stage"]')
    if (!(col instanceof HTMLElement) || !(stage instanceof HTMLElement)) {
      return 0
    }
    const cw = col.getBoundingClientRect().width
    const sw = stage.getBoundingClientRect().width
    return sw > 0 ? cw / sw : 0
  })
}

test("switches theme and remembers it", async ({ page }) => {
  await page.goto("/")
  const root = page.locator("html")
  const wasDark = await root.evaluate((el) => el.classList.contains("dark"))

  await page.getByRole("button", { name: "Switch theme" }).click()
  await expect(root).toHaveClass(wasDark ? /^(?!.*dark).*$/ : /dark/)

  await page.reload()
  await expect(root).toHaveClass(wasDark ? /^(?!.*dark).*$/ : /dark/)
})

test("switches the chrome language and restores English", async ({
  page,
  request,
}) => {
  // Locale is stored in the shared e2e config.yaml. Leave it English or
  // every later spec that looks for "Idle" / "New conversation" dies.
  await page.goto("/")
  try {
    await page.getByRole("button", { name: "Switch language" }).click()
    await expect(page.getByRole("button", { name: "切换语言" })).toBeVisible()
    await expect(
      page.getByRole("button", { name: "新对话", exact: true }),
    ).toBeVisible()
    await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN")
  } finally {
    await request.put("/api/settings", { data: { ui: { locale: "system" } } })
    const zh = page.getByRole("button", { name: "切换语言" })
    if (await zh.isVisible()) await zh.click()
    await expect(page.getByRole("button", { name: "Switch language" })).toBeVisible()
    await expect(page.locator("html")).toHaveAttribute("lang", "en")
  }
})

test("font and conversation width round-trip through settings", async ({
  page,
  request,
}) => {
  await page.goto("/")
  try {
    await page.getByRole("button", { name: "Settings" }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "General" }).click()
    await dialog.getByRole("combobox", { name: "Font", exact: true }).click()
    await page.getByRole("option", { name: "Serif" }).click()
    await dialog.getByRole("combobox", { name: "Font size" }).click()
    await page.getByRole("option", { name: "Large" }).click()
    await dialog.getByRole("combobox", { name: "Conversation width" }).click()
    await page.getByRole("option", { name: "Wide" }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const root = page.locator("html")
    await expect(root).toHaveAttribute("data-font", "serif")
    await expect(root).toHaveAttribute("data-font-size", "large")
    await expect(root).toHaveAttribute("data-content-width", "full")
    await expect(root).toHaveCSS("font-size", "16px")

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.ui.font).toBe("serif")
    expect(saved.settings.ui.font_size).toBe("large")
    expect(saved.settings.ui.content_width).toBe("full")

    await page.getByRole("button", { name: "Settings" }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "General" }).click()
    await expect(
      page.getByRole("dialog").getByRole("combobox", { name: "Font", exact: true }),
    ).toContainText("Serif")
    await expect(
      page.getByRole("dialog").getByRole("combobox", { name: "Conversation width" }),
    ).toContainText("Wide")
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", {
      data: {
        ui: {
          locale: "system",
          font: "system",
          font_size: "medium",
          content_width: "comfortable",
        },
      },
    })
  }
})

test("renames and deletes a conversation from the sidebar", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
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

  await page.locator("aside").first().getByText("Renamed by the test").first().hover()
  await page
    .locator("aside")
    .first()
    .getByRole("button", { name: "More" })
    .first()
    .click()
  await page.getByRole("menuitem", { name: "Delete" }).click()
  await expect(page.getByRole("button", { name: "Delete conversation" })).toBeVisible()
  await page.getByRole("button", { name: "Delete conversation" }).click()
  await expect(
    page.locator("aside").first().getByText("Renamed by the test"),
  ).toBeHidden()
})

test("dragging a conversation pins that order across reload", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await renameRecentsRow(page, 0, "Alpha")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await renameRecentsRow(page, 0, "Beta")
  const recents = page.getByTestId("recents-list").getByTestId("thread-row")
  await expect(recents.nth(0)).toContainText("Beta")
  await html5Reorder(page, "thread-row", 1, 0, "recents-list")
  await expect(recents.nth(0)).toContainText("Alpha")
  await page.reload()
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row").nth(0),
  ).toContainText("Alpha")
})

async function renameRecentsRow(page: Page, index: number, name: string) {
  const row = page.getByTestId("recents-list").getByTestId("thread-row").nth(index)
  await row.hover()
  await row.getByRole("button", { name: "More" }).click()
  await page.getByRole("menuitem", { name: "Rename" }).click()
  const input = page.locator("aside").first().locator("input")
  await input.fill(name)
  await input.press("Enter")
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row").getByText(name),
  ).toBeVisible()
}

async function html5Reorder(
  page: Page,
  testId: string,
  fromIndex: number,
  toIndex: number,
  rootTestId?: string,
) {
  await page.evaluate(
    ({
      testId,
      fromIndex,
      toIndex,
      rootTestId,
    }: {
      testId: string
      fromIndex: number
      toIndex: number
      rootTestId?: string
    }) => {
      const scope = rootTestId
        ? document.querySelector(`[data-testid="${rootTestId}"]`)
        : document
      if (!scope) throw new Error("missing list")
      const rows = Array.from(
        scope.querySelectorAll(`[data-testid="${testId}"]`),
      )
      const source = rows[fromIndex]
      const target = rows[toIndex]
      if (!(source instanceof HTMLElement) || !(target instanceof HTMLElement)) {
        throw new Error("missing row")
      }
      source.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }))
      const dt = new DataTransfer()
      source.dispatchEvent(
        new DragEvent("dragstart", { bubbles: true, cancelable: true, dataTransfer: dt }),
      )
      target.dispatchEvent(
        new DragEvent("dragover", { bubbles: true, cancelable: true, dataTransfer: dt }),
      )
      target.dispatchEvent(
        new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: dt }),
      )
      source.dispatchEvent(new DragEvent("dragend", { bubbles: true, dataTransfer: dt }))
    },
    { testId, fromIndex, toIndex, rootTestId },
  )
}
