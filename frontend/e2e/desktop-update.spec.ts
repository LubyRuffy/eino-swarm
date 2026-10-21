import { expect, test, type Locator, type Page, type Route } from "@playwright/test"

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
  await expect(notice.getByRole("alert")).toHaveText("GitHub request timed out")
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
    if (new URL(route.request().url()).pathname === "/api/update/progress") {
      await route.fulfill({ json: { stage: "idle", downloaded_bytes: 0, total_bytes: 0 } })
    } else if (route.request().method() === "POST") {
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

// Keep the install response pending while the real controller polls changing
// backend snapshots. GET check counts exclude those independent observations.
async function installationFeed(page: Page) {
  const state = {
    checks: 0,
    versions: [] as string[],
    polls: 0,
    failPoll: false,
    progress: { stage: "downloading", version: "9.9.9", downloaded_bytes: 1024, total_bytes: 4096 },
  }
  const pending: Route[] = []
  await page.route("**/api/update**", async (route) => {
    const request = route.request()
    if (new URL(request.url()).pathname === "/api/update/progress") {
      state.polls++
      if (state.failPoll) await route.abort("failed")
      else await route.fulfill({ json: state.progress })
    } else if (request.method() === "POST") {
      state.versions.push(request.postDataJSON().version)
      pending.push(route)
    } else {
      state.checks++
      await route.fulfill({ json: offer })
    }
  })
  return {
    state,
    async finish(status: number, json: Record<string, string>) {
      const route = pending.shift()
      expect(route, "an explicit install request must be pending").toBeDefined()
      await route!.fulfill({ status, json })
    },
  }
}

async function expectRed(locator: Locator, property: "color" | "backgroundColor") {
  const color = await locator.evaluate((element, name) => getComputedStyle(element)[name], property)
  const channels = color.match(/^rgba?\((\d+), (\d+), (\d+)/)?.slice(1).map(Number)
  expect(channels, `computed ${property} must be an RGB red: ${color}`).toBeDefined()
  const [red, green, blue] = channels!
  expect(red).toBeGreaterThan(150)
  expect(red).toBeGreaterThan(green * 2)
  expect(red).toBeGreaterThan(blue * 2)
}

test("an install failure is red in both themes and retries the confirmed version without another check", async ({ page }) => {
  await clock(page)
  const feed = await installationFeed(page)
  await page.goto("/?shell=desktop")
  const notice = page.getByTestId("desktop-update")
  await notice.getByRole("button", { name: "Update", exact: true }).click()
  await expect(notice.getByRole("status")).toHaveText("Downloading the update · 25% (1.0 KB / 4.0 KB)")
  await expect(notice.getByRole("progressbar", { name: "Update download progress" })).toHaveAttribute("aria-valuenow", "25")
  await feed.finish(409, { error: "update download timed out" })
  await expect(notice.getByRole("alert")).toHaveText("update download timed out")
  const stoppedPolls = feed.state.polls
  await page.clock.fastForward(2000)
  expect(feed.state.polls).toBe(stoppedPolls)

  for (const theme of ["light", "dark"]) {
    await page.evaluate((dark) => document.documentElement.classList.toggle("dark", dark), theme === "dark")
    await expectRed(notice.getByRole("alert"), "color")
    await expectRed(notice.getByRole("button", { name: "Retry update installation" }), "backgroundColor")
  }
  await foreground(page)
  await page.evaluate(() => window.dispatchEvent(new Event("online")))
  await expect(notice.getByRole("alert")).toHaveText("update download timed out")
  expect(feed.state.checks).toBe(1)

  feed.state.progress = { ...feed.state.progress, downloaded_bytes: 2048 }
  await notice.getByRole("button", { name: "Retry update installation" }).click()
  await expect(notice.getByRole("status")).toHaveText("Downloading the update · 50% (2.0 KB / 4.0 KB)")
  await expect(notice.getByRole("button", { name: "Retry update installation" })).toHaveCount(0)
  await expect(notice.getByRole("button", { name: "Update", exact: true })).toHaveCount(0)
  expect(feed.state.versions).toEqual(["9.9.9", "9.9.9"])
  expect(feed.state.checks).toBe(1)
  await feed.finish(200, { status: "restarting" })
  await expect(notice.getByRole("status")).toHaveText("Installing the update. zwai will reopen.")
  const finishedPolls = feed.state.polls
  await page.clock.fastForward(2000)
  expect(feed.state.polls).toBe(finishedPolls)
})

test("download observations show real bytes, tolerate polling failure and distinguish unknown size from installation", async ({ page }) => {
  await clock(page)
  const feed = await installationFeed(page)
  await page.goto("/?shell=desktop")
  const notice = page.getByTestId("desktop-update")
  await notice.getByRole("button", { name: "Update", exact: true }).click()
  await expect(notice.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "25")
  feed.state.progress = { ...feed.state.progress, downloaded_bytes: 3072 }
  await page.clock.fastForward(500)
  await expect(notice.getByRole("status")).toHaveText("Downloading the update · 75% (3.0 KB / 4.0 KB)")
  await expect(notice.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "75")

  feed.state.failPoll = true
  const beforeFailure = feed.state.polls
  await page.clock.fastForward(500)
  await expect.poll(() => feed.state.polls).toBe(beforeFailure + 1)
  await expect(notice.getByRole("alert")).toHaveCount(0)
  await expect(notice.getByRole("status")).toHaveText("Downloading the update · 75% (3.0 KB / 4.0 KB)")
  await expect(notice.getByRole("button", { name: /Retry/ })).toHaveCount(0)
  await manualCheck(page)
  expect(feed.state.checks).toBe(1)
  expect(feed.state.versions).toEqual(["9.9.9"])

  feed.state.failPoll = false
  feed.state.progress = { ...feed.state.progress, downloaded_bytes: 4096, total_bytes: 0 }
  await page.clock.fastForward(500)
  await expect(notice.getByRole("status")).toHaveText("Downloading the update · 4.0 KB downloaded")
  await expect(notice.getByRole("status")).not.toContainText("%")
  await expect(notice.getByRole("progressbar")).not.toHaveAttribute("aria-valuenow")

  feed.state.progress = { ...feed.state.progress, stage: "installing" }
  await page.clock.fastForward(500)
  await expect(notice.getByRole("status")).toHaveText("Installing the update")
  await expect(notice.getByRole("progressbar")).toHaveCount(0)
  await expect(notice.getByRole("button", { name: /Retry/ })).toHaveCount(0)
  await feed.finish(200, { status: "restarting" })
  await expect(notice.getByRole("status")).toHaveText("Installing the update. zwai will reopen.")
  const stoppedPolls = feed.state.polls
  await page.clock.fastForward(2000)
  expect(feed.state.polls).toBe(stoppedPolls)
})
