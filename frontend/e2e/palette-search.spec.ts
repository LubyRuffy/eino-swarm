import { expect, test, type APIRequestContext, type Page, type TestInfo } from "@playwright/test"
import { writeFile } from "node:fs/promises"

import { send, waitForIdle } from "./session"

const placeholder = "Search conversations, or type a command…"
const loading = "Searching conversations…"
const failure = "Conversation search failed. Showing matching local titles only."

async function namedThread(request: APIRequestContext, title: string) {
  const created = await request.post("/api/threads", { data: { title } })
  expect(created.status()).toBe(201)
  const { thread } = await created.json() as { thread: { id: string; title_auto: boolean } }
  expect(thread.title_auto).toBe(false)
  return thread.id
}

async function openPalette(page: Page) {
  await page.keyboard.press("ControlOrMeta+k")
  const input = page.getByPlaceholder(placeholder)
  await expect(input).toBeVisible()
  return input
}

async function screenshot(page: Page, info: TestInfo, name: string) {
  await writeFile(info.outputPath(name), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
}

test("a real body search hides unrelated commands and Enter opens its conversation", async ({ page, request }, info) => {
  const id = await namedThread(request, "Field journal")
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.locator(`[data-testid="thread-row"][data-id="${id}"]`).getByTestId("row-label").click()
  await send(page, "Record the orchard-quartz-observation in this conversation.")
  await waitForIdle(page)
  await page.reload()

  const input = await openPalette(page)
  const searched = page.waitForResponse((response) => new URL(response.url()).pathname === "/api/search")
  await input.fill("orchard-quartz-observation")
  const response = await searched
  expect(response.ok()).toBe(true)
  const body = await response.json() as { hits: Array<{ thread_id: string }> }
  expect(body.hits.map((hit) => hit.thread_id)).toContain(id)
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByRole("option")).toHaveCount(1)
  await expect(dialog.getByRole("option")).toContainText("Field journal")
  await expect(dialog.getByRole("option", { name: /New conversation/ })).toHaveCount(0)
  await expect(dialog.getByRole("status")).toHaveCount(0)
  await screenshot(page, info, "body-search-result.png")
  await input.press("Enter")
  await expect(page.getByPlaceholder(placeholder)).toBeHidden()
  await expect(page.locator('[data-testid="thread-row"][aria-current="true"]')).toHaveAttribute("data-id", id)
  await expect(page.getByTestId("transcript")).toContainText("orchard-quartz-observation")
})

test("loading, empty results and request failure stay distinct while matching commands still work", async ({ page, request }, info) => {
  const matching = await namedThread(request, "Orchard planning")
  await namedThread(request, "Harbor log")
  let release!: () => void
  const held = new Promise<void>((resolve) => { release = resolve })
  let requested!: () => void
  const started = new Promise<void>((resolve) => { requested = resolve })
  await page.route("**/api/search?**", async (route) => {
    const query = new URL(route.request().url()).searchParams.get("q")
    if (query === "quartz-pending") {
      requested()
      await held
      await route.fulfill({ json: { query, embedding: false, hits: [] } })
    } else if (query === "orchard") {
      await route.fulfill({ status: 503, json: { error: "search temporarily unavailable" } })
    } else if (query === "settings") {
      await route.fulfill({ json: { query, embedding: false, hits: [] } })
    } else await route.continue()
  })
  try {
    await page.goto("/")
    await page.getByTestId("dest-chats").click()
    const input = await openPalette(page)
    const dialog = page.getByRole("dialog")
    await input.fill("quartz-pending")
    await started
    await expect(dialog.getByRole("status")).toHaveText(loading)
    await expect(dialog.getByRole("listbox")).toHaveAttribute("aria-busy", "true")
    await expect(dialog.getByText("Nothing matches that.")).toHaveCount(0)
    await expect(dialog.getByRole("option")).toHaveCount(0)
    await screenshot(page, info, "search-pending.png")
    release()
    await expect(dialog.getByText("Nothing matches that.")).toBeVisible()
    await expect(dialog.getByRole("listbox")).toHaveAttribute("aria-busy", "false")
    await expect(dialog.getByRole("status")).toHaveCount(0)
    await screenshot(page, info, "search-empty.png")

    await input.fill("orchard")
    await expect(dialog.getByRole("alert")).toHaveText(failure)
    await expect(dialog.getByText("Nothing matches that.")).toHaveCount(0)
    await expect(dialog.getByRole("option")).toHaveCount(1)
    await expect(dialog.getByRole("option")).toContainText("Orchard planning")
    await expect(dialog.getByText("Harbor log")).toHaveCount(0)
    await screenshot(page, info, "search-failed-fallback.png")
    await input.press("Enter")
    await expect(page.locator('[data-testid="thread-row"][aria-current="true"]')).toHaveAttribute("data-id", matching)

    const command = await openPalette(page)
    await command.fill("settings")
    await expect(dialog.getByRole("status")).toHaveCount(0)
    await expect(dialog.getByRole("option")).toHaveCount(1)
    await expect(dialog.getByRole("option", { name: "Settings", exact: true })).toBeVisible()
    await command.press("Enter")
    await expect(page.getByPlaceholder(placeholder)).toBeHidden()
    await expect(page.getByRole("dialog").getByRole("tab", { name: "Models", exact: true })).toBeVisible()
  } finally {
    release()
    await page.unrouteAll({ behavior: "wait" })
  }
})

test("server semantic ranking survives cmdk filtering and selects its first result", async ({ page, request }, info) => {
  const first = await namedThread(request, "Ranked semantic destination")
  const second = await namedThread(request, "Travel planning notes")
  await page.route("**/api/search?**", async (route) => {
    const query = new URL(route.request().url()).searchParams.get("q")
    await route.fulfill({ json: { query, embedding: true, hits: [
      { thread_id: first, title: "", snippet: "vessel departure", score: 2, source: "semantic" },
      { thread_id: second, title: "Travel planning notes", snippet: "regional connections", score: 1, source: "semantic" },
    ] } })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  const input = await openPalette(page)
  await input.fill("travel planning ")
  const options = page.getByRole("dialog").getByRole("option")
  await expect(options).toHaveCount(2)
  await expect(options.nth(0)).toContainText("Untitled")
  await expect(options.nth(0)).toContainText("vessel departure")
  await expect(options.nth(1)).toContainText("Travel planning notes")
  await screenshot(page, info, "semantic-server-ranking.png")
  await input.press("Enter")
  await expect(page.locator('[data-testid="thread-row"][aria-current="true"]')).toHaveAttribute("data-id", first)
})

test("visible Chinese command labels filter and Enter opens conversation find", async ({ page, request }, info) => {
  const id = await namedThread(request, "Localized command destination")
  const settingsResponse = await request.get("/api/settings")
  expect(settingsResponse.ok()).toBe(true)
  const { settings } = await settingsResponse.json() as { settings: { ui: { locale: string } } }
  await page.goto("/")
  await expect(page.locator("html")).toHaveAttribute("lang", "en")
  const cachedLocale = await page.evaluate(() => localStorage.getItem("zwai.locale"))
  try {
    expect((await request.put("/api/settings", { data: { ui: { locale: "zh" } } })).ok()).toBe(true)
    await page.reload()
    await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN")
    await page.getByTestId("dest-chats").click()
    await page.locator(`[data-testid="thread-row"][data-id="${id}"]`).getByTestId("row-label").click()
    await page.keyboard.press("ControlOrMeta+k")
    const input = page.getByPlaceholder("搜索会话，或输入命令…")
    const dialog = page.getByRole("dialog")
    await input.fill("浅色")
    await expect(dialog.getByRole("option")).toHaveCount(1)
    await expect(dialog.getByRole("option")).toContainText("切换浅色 / 深色")
    await input.fill("在会话中查找")
    await expect(dialog.getByRole("option")).toHaveCount(1)
    await expect(dialog.getByRole("option")).toContainText("在会话中查找")
    await screenshot(page, info, "localized-command.png")
    await input.press("Enter")
    await expect(input).toBeHidden()
    await expect(page.getByTestId("conversation-find").getByRole("textbox", { name: "在会话中查找" })).toBeVisible()
  } finally {
    const restored = await request.put("/api/settings", { data: { ui: { locale: settings.ui.locale } } })
    await page.reload()
    await page.evaluate((previous) => {
      if (previous === null) localStorage.removeItem("zwai.locale")
      else localStorage.setItem("zwai.locale", previous)
    }, cachedLocale)
    expect(restored.ok()).toBe(true)
    await expect(page.locator("html")).toHaveAttribute("lang", "en")
  }
})
