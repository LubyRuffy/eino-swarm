import { expect, test, type Page } from "@playwright/test"
import { mkdtempSync } from "node:fs"
import { tmpdir } from "node:os"
import { basename, join } from "node:path"

import { readComposerPlate } from "./composer-plate"

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

test("Projects and Conversations share one left gutter", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  const projects = page.getByText("Projects", { exact: true })
  const recents = page.getByText("Conversations", { exact: true })
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

test("collapsing Conversations hides its rows across reload", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row").first(),
  ).toBeVisible()
  const recentsHeader = page.getByRole("button", { name: "Conversations", exact: true })
  const recentsFold = recentsHeader.locator("[data-testid=section-fold]")
  await expect(recentsFold).toHaveCSS("opacity", "0")
  await recentsHeader.hover()
  await expect(recentsFold).toHaveCSS("opacity", "1")
  await recentsHeader.click()
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row"),
  ).toHaveCount(0)
  await expect(recentsHeader).toHaveAttribute("aria-expanded", "false")
  await expect(recentsFold).toHaveCSS("opacity", "1")
  await page.reload()
  await expect(page.getByRole("button", { name: "Conversations", exact: true })).toHaveAttribute(
    "aria-expanded",
    "false",
  )
  await expect(
    page.getByTestId("recents-list").getByTestId("thread-row"),
  ).toHaveCount(0)
})

test("the Conversations header icon starts a conversation outside a project", async ({
  page,
}) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  const rows = page.getByTestId("recents-list").getByTestId("thread-row")
  await expect(rows.first()).toBeVisible()
  const active = page.getByTestId("recents-list").locator('[data-testid="thread-row"][aria-current="true"]')
  const before = await active.getAttribute("data-id")
  await page.getByTestId("recents-new").click()
  await expect(active).not.toHaveAttribute("data-id", before!)
  await expect(page.getByRole("button", { name: "Conversations", exact: true })).toHaveAttribute(
    "aria-expanded",
    "true",
  )
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
  // Pins + box share a slab. Join sits on that plate — a % opaque hang
  // used to cover chip gaps and the last answer depending on dock height.
  const plate = await readComposerPlate(page)
  expect(plate).not.toBeNull()
  expect(plate!.pinsInDock && plate!.slabOpaque).toBe(true)
  expect(Math.abs(plate!.slabH - plate!.rootH)).toBeLessThan(2)
  expect(Math.abs(plate!.fadeBottom - plate!.dockTop)).toBeLessThan(2)
  expect(plate!.fadeH).toBeGreaterThan(80)
  expect(plate!.fadeH).toBeLessThanOrEqual(100)
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


test("switches conversation width from the app menu and fills the pane", async ({
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
    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch to wide layout" }).click()
    await expect(root).toHaveAttribute("data-content-width", "full")
    await page.getByRole("button", { name: "App menu" }).click()
    await expect(
      page.getByRole("menuitem", { name: "Switch to standard layout" }),
    ).toHaveAttribute("aria-pressed", "true")

    const after = await columnFill(page)
    expect(after).toBeGreaterThan(before)
    expect(after).toBeGreaterThan(0.9)

    await page.reload()
    await expect(root).toHaveAttribute("data-content-width", "full")

    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch to standard layout" }).click()
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

  await page.getByRole("button", { name: "App menu" }).click()
  await page.getByRole("menuitem", { name: "Switch theme" }).click()
  await expect(root).toHaveClass(wasDark ? /^(?!.*dark).*$/ : /dark/)

  await page.reload()
  await expect(root).toHaveClass(wasDark ? /^(?!.*dark).*$/ : /dark/)
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

test("desktop shell shows the build and asks before installing a release", async ({
  page,
}) => {
  await page.route("**/api/update**", async (route) => {
    if (route.request().method() === "POST") {
      await route.fulfill({ json: { status: "restarting" } })
      return
    }
    await route.fulfill({
      json: {
        status: "available",
        offer: { version: "9.9.9", asset_name: "zwai-9.9.9-darwin-arm64.zip" },
      },
    })
  })
  await page.goto("/?shell=desktop")
  await expect(page.getByTestId("sidebar-version")).toContainText(/Version /)
  const notice = page.getByTestId("desktop-update")
  await expect(notice.getByRole("status")).toHaveText(
    "Version 9.9.9 is available. Update?",
  )
  await notice.getByRole("button", { name: "Update" }).click()
  await expect(notice.getByRole("status")).toHaveText(
    "Installing the update. zwai will reopen.",
  )
})
