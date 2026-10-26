import { expect, test, type Page } from "@playwright/test"
import { readFileSync } from "node:fs"

const source = readFileSync(new URL("../../internal/desktop/recovery_cover.go", import.meta.url), "utf8")
const generated = source.match(/const recoveryCoverScript = `([\s\S]*?)`/)
if (!generated) throw new Error("the native corner background script is missing")
// The same browser assertions can be run against a frozen historical script to
// preserve the original failure independently of concurrent implementation.
const script = process.env.ZWAI_TEST_RECOVERY_COVER_SCRIPT
  ? readFileSync(process.env.ZWAI_TEST_RECOVERY_COVER_SCRIPT, "utf8")
  : generated[1]

const colour = (css: string) => css.match(/[\d.]+/g)!.slice(0, 3).map(Number)

async function pointerState(page: Page) {
  return page.evaluate(() => ({
    value: document.body.style.getPropertyValue("pointer-events"),
    priority: document.body.style.getPropertyPriority("pointer-events"),
    computed: getComputedStyle(document.body).pointerEvents,
  }))
}

test("a real modal keeps its input guard while the native cover reads the dimmed background", async ({ page }, info) => {
  await page.goto("/?shell=desktop")
  const header = page.locator("header").first()
  await expect(header).toBeVisible()
  const background = colour(await header.evaluate((element) => getComputedStyle(element).backgroundColor))
  await page.getByRole("button", { name: "New project", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toBeVisible()
  const overlay = page.locator('body > div[data-state="open"]:not([role="dialog"])').first()
  await expect(overlay).toHaveCSS("background-color", "rgba(0, 0, 0, 0.6)")
  await expect(overlay).toHaveCSS("opacity", "1")
  await expect(page.locator("body")).toHaveCSS("pointer-events", "none")

  // The production nonce has pointer-events:none. Temporarily restoring the
  // body's hit testing must still omit that actual visible probe from sampling.
  await page.evaluate(() => {
    const probe = document.createElement("div")
    probe.id = "zwai-native-render-probe"
    probe.style.cssText = "position:fixed;left:0;top:0;width:16px;height:16px;pointer-events:none;z-index:2147483647;background:rgb(255,0,255)"
    document.documentElement.appendChild(probe)
  })
  const expected = background.map((channel) => Math.round(channel * 0.4))
  const evidence: unknown[] = []
  try {
    for (const important of [false, true]) {
      if (important) await page.evaluate(() => document.body.style.setProperty("pointer-events", "none", "important"))
      const before = await pointerState(page)
      expect(before.computed).toBe("none")
      const sampled = await page.evaluate(script)
      const after = await pointerState(page)
      evidence.push({ before, sampled, after, expected })
      expect(after).toEqual(before)
      expect(sampled).toEqual(expected)
      await expect(dialog).toBeVisible()
    }
    await page.evaluate(() => document.getElementById("zwai-native-render-probe")?.remove())
    await page.screenshot({ path: info.outputPath("modal-background-sampling.png"), animations: "disabled" })
    await info.attach("native-modal-colours", { body: JSON.stringify(evidence, null, 2), contentType: "application/json" })
  } finally {
    await page.evaluate(() => {
      document.getElementById("zwai-native-render-probe")?.remove()
      if (document.body.style.getPropertyPriority("pointer-events") === "important") {
        document.body.style.setProperty("pointer-events", "none")
      }
    })
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click()
    await expect(dialog).toBeHidden()
  }
})

test("native corner notifications follow real settings and theme changes", async ({ page, request }, info) => {
  const settings = await request.get("/api/settings")
  expect(settings.ok()).toBe(true)
  const originalUI = (await settings.json()).settings.ui
  await page.goto("/?shell=desktop")
  await expect(page.locator("header").first()).toBeVisible()
  const originalTheme = await page.evaluate(() => localStorage.getItem("zwai.theme"))
  try {
    await page.evaluate(() => {
      const host = window as Window & { _wails?: { invoke(message: string): void }; __coverNotices?: Array<{ type: string; colour: number[] }> }
      host.__coverNotices = []
      host._wails = { invoke(message) { if (message.startsWith("{")) host.__coverNotices!.push(JSON.parse(message)) } }
    })
    const notices = () => page.evaluate(() => (window as Window & { __coverNotices?: Array<{ type: string; colour: number[] }> }).__coverNotices ?? [])
    const headerColour = async () => colour(await page.locator("header").first().evaluate((element) => getComputedStyle(element).backgroundColor))
    expect(await page.evaluate(script)).toEqual(await headerColour())
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const dialog = page.getByRole("dialog")
    await expect(dialog).toBeVisible()
    await expect(dialog).toHaveCSS("opacity", "1")
    const corner = page.getByTestId("settings-titlebar").locator(":scope > div").first()
    await expect(corner).toBeVisible()
    const settingsColour = async () => colour(await corner.evaluate((element) => getComputedStyle(element).backgroundColor))
    await expect.poll(async () => (await notices()).at(-1)?.colour).toEqual(await settingsColour())
    await dialog.getByRole("radio", { name: "Dark", exact: true }).click()
    await expect(page.locator("html")).toHaveClass(/dark/)
    await expect.poll(async () => (await notices()).at(-1)?.colour).toEqual(await settingsColour())
    await dialog.getByRole("button", { name: "Back to app", exact: true }).click()
    await expect(dialog).toBeHidden()
    await expect.poll(async () => (await notices()).at(-1)?.colour).toEqual(await headerColour())
    expect((await notices()).every((notice) => notice.type === "zwai:render-cover" && notice.colour.length === 3)).toBe(true)
    await page.screenshot({ path: info.outputPath("native-cover-dark-theme.png"), animations: "disabled" })
    await info.attach("native-corner-notifications", { body: JSON.stringify(await notices(), null, 2), contentType: "application/json" })
  } finally {
    // Settings are shared by the suite; restore the exact server palette and
    // the browser's local theme even when a notification assertion fails.
    const restored = await request.put("/api/settings", { data: { ui: originalUI } })
    expect(restored.ok()).toBe(true)
    await page.evaluate((theme) => {
      if (theme === null) localStorage.removeItem("zwai.theme")
      else localStorage.setItem("zwai.theme", theme)
    }, originalTheme)
    await page.reload()
    expect((await (await request.get("/api/settings")).json()).settings.ui).toEqual(originalUI)
  }
})
