import { expect, test, type Page } from "@playwright/test"

const sixHours = 6 * 60 * 60 * 1000
const offer = { status: "available", offer: { version: "9.9.9" } }

async function clock(page: Page) {
  await page.clock.install({ time: new Date("2026-10-12T00:00:00Z") })
  await page.clock.pauseAt(new Date("2026-10-12T00:00:01Z"))
}

async function foreground(page: Page) {
  await page.evaluate(() => {
    window.dispatchEvent(new Event("focus"))
    document.dispatchEvent(new Event("visibilitychange"))
  })
}

async function manualCheck(page: Page) {
  await page.getByRole("button", { name: "App menu", exact: true }).click()
  await page.getByRole("menuitem", { name: "Check for updates", exact: true }).click()
}

test("a long-lived desktop checks again after six hours and foreground events do not flood successful checks", async ({ page }) => {
  await clock(page)
  const urls: string[] = []
  let installs = 0
  await page.route("**/api/update**", async (route) => {
    if (route.request().method() === "POST") {
      installs++
      await route.fulfill({ json: { status: "restarting" } })
    } else {
      urls.push(route.request().url())
      await route.fulfill({ json: urls.length === 1 ? { status: "current" } : offer })
    }
  })
  await page.goto("/?shell=desktop")
  await expect.poll(() => urls.length).toBe(1)
  await expect(page.getByTestId("desktop-update")).toHaveCount(0)
  for (let i = 0; i < 5; i++) await foreground(page)
  await page.clock.fastForward(sixHours - 1)
  expect(urls).toHaveLength(1)
  await page.clock.fastForward(1)
  await expect(page.getByTestId("desktop-update").getByRole("status")).toHaveText("Version 9.9.9 is available. Update?")
  expect(urls).toHaveLength(2)
  expect(urls.every((url) => !url.includes("fresh=1"))).toBe(true)
  expect(installs).toBe(0)
})

test("online retries an initial failure and later background errors preserve the available update", async ({ page }) => {
  await clock(page)
  let checks = 0
  await page.route("**/api/update**", async (route) => {
    checks++
    if (checks === 1) await route.abort("internetdisconnected")
    else await route.fulfill({ json: checks === 3 ? { status: "error", message: "release request timed out" } : offer })
  })
  const offline = page.waitForEvent("requestfailed", (request) => request.url().includes("/api/update"))
  await page.goto("/?shell=desktop")
  await offline
  await expect(page.getByTestId("desktop-update")).toHaveCount(0)
  await page.evaluate(() => window.dispatchEvent(new Event("online")))
  const notice = page.getByTestId("desktop-update")
  await expect(notice.getByRole("status")).toHaveText("Version 9.9.9 is available. Update?")
  expect(checks).toBe(2)
  await page.clock.fastForward(sixHours)
  await expect.poll(() => checks).toBe(3)
  await expect(notice.getByRole("status")).toHaveText("Version 9.9.9 is available. Update?")
  await foreground(page)
  await expect.poll(() => checks).toBe(4)
  await expect(notice.getByRole("status")).toHaveText("Version 9.9.9 is available. Update?")
  await foreground(page)
  expect(checks).toBe(4)
})

test("manual checks explain unsupported builds and let the user retry and close errors", async ({ page }) => {
  const fresh: boolean[] = []
  await page.route("**/api/update**", async (route) => {
    fresh.push(new URL(route.request().url()).searchParams.get("fresh") === "1")
    const response = fresh.length <= 2
      ? { status: "unsupported", message: "Use the packaged Mac app" }
      : fresh.length === 3 ? { status: "error", message: "GitHub request timed out" } : { status: "current" }
    await route.fulfill({ json: response })
  })
  await page.goto("/?shell=desktop")
  await expect.poll(() => fresh.length).toBe(1)
  await manualCheck(page)
  const notice = page.getByTestId("desktop-update")
  await expect(notice.getByRole("status")).toHaveText("Use the packaged Mac app")
  await notice.getByRole("button", { name: "Close update notice" }).click()
  await expect(notice).toHaveCount(0)
  await manualCheck(page)
  await expect(notice.getByRole("status")).toHaveText("GitHub request timed out")
  await notice.getByRole("button", { name: "Retry update check" }).click()
  await expect(notice.getByRole("status")).toHaveText("This is the latest version")
  await notice.getByRole("button", { name: "Close update notice" }).click()
  await expect(notice).toHaveCount(0)
  expect(fresh).toEqual([false, true, true, true])
})

test("a pending install blocks checks and keeps only one explicitly confirmed install", async ({ page }) => {
  await clock(page)
  let checks = 0
  let installs = 0
  let finish!: () => Promise<void>
  await page.route("**/api/update**", async (route) => {
    if (route.request().method() === "POST") {
      installs++
      finish = () => route.fulfill({ json: { status: "restarting" } })
    } else {
      checks++
      await route.fulfill({ json: offer })
    }
  })
  await page.goto("/?shell=desktop")
  const notice = page.getByTestId("desktop-update")
  await notice.getByRole("button", { name: "Update", exact: true }).click()
  await expect(notice.getByRole("status")).toHaveText("Downloading the update")
  await expect.poll(() => installs).toBe(1)
  await foreground(page)
  await page.evaluate(() => window.dispatchEvent(new Event("online")))
  await page.clock.fastForward(sixHours)
  await manualCheck(page)
  expect(checks).toBe(1)
  expect(installs).toBe(1)
  await expect(notice.getByRole("status")).toHaveText("Downloading the update")
  await expect(notice.getByRole("button", { name: "Update", exact: true })).toHaveCount(0)
  await finish()
  await expect(notice.getByRole("status")).toHaveText("Installing the update. zwai will reopen.")
})

test("a delayed startup response cannot hide the result of a newer manual check", async ({ page }) => {
  let finishStartup!: () => Promise<void>
  await page.route("**/api/update**", async (route) => {
    if (new URL(route.request().url()).searchParams.get("fresh") === "1") {
      await route.fulfill({ json: offer })
    } else {
      finishStartup = () => route.fulfill({ json: { status: "current" } })
    }
  })
  await page.goto("/?shell=desktop")
  await manualCheck(page)
  const notice = page.getByTestId("desktop-update")
  await expect(notice.getByRole("status")).toHaveText("Version 9.9.9 is available. Update?")
  await finishStartup()
  await expect(notice.getByRole("status")).toHaveText("Version 9.9.9 is available. Update?")
})

test("a browser tab does not check or offer desktop updates", async ({ page }) => {
  let checks = 0
  await page.route("**/api/update**", async (route) => {
    checks++
    await route.fulfill({ json: offer })
  })
  await page.goto("/")
  await foreground(page)
  await page.evaluate(() => window.dispatchEvent(new Event("online")))
  await page.getByRole("button", { name: "App menu", exact: true }).click()
  await expect(page.getByRole("menuitem", { name: "Check for updates", exact: true })).toHaveCount(0)
  await expect(page.getByTestId("desktop-update")).toHaveCount(0)
  expect(checks).toBe(0)
})
