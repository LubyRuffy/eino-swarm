import { expect, test, type APIRequestContext, type Page } from "@playwright/test"

type PresenceClient = { id: string; surface: string }
type LauncherHold = Window & { launcherHoldEnded?: boolean; launcherHoldError?: boolean }

// Keep an acknowledged launcher GET in another page so reloading or closing
// the app cannot itself end the native process's presence lifetime.
async function holdLauncherPresence(page: Page, id: string) {
  await page.goto("/api/meta")
  await page.evaluate(async (id) => {
    const response = await fetch(`/api/presence/${encodeURIComponent(id)}`)
    if (!response.ok || !response.body) throw new Error("launcher presence was not held")
    const reader = response.body.getReader()
    if ((await reader.read()).done) throw new Error("launcher presence ended before acknowledgement")
    const state = window as LauncherHold
    state.launcherHoldEnded = false
    void (async () => {
      while (!(await reader.read()).done) { /* wait for native window exit */ }
      state.launcherHoldEnded = true
    })().catch(() => { state.launcherHoldError = true })
  }, id)
}

async function clients(request: APIRequestContext): Promise<PresenceClient[]> {
  const response = await request.get("/api/meta")
  expect(response.ok()).toBe(true)
  const meta = await response.json() as { clients: PresenceClient[] }
  return meta.clients
}

test("desktop reload keeps the launcher's presence slot alive", async ({ page, request }) => {
  const response = await request.post("/api/presence", { data: { surface: "desktop" } })
  expect(response.ok()).toBe(true)
  const { id } = await response.json() as { id: string }
  const launcher = await page.context().newPage()
  let frontendReservations = 0
  let frontendHolds = 0
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname
    if (path === "/api/presence" && request.method() === "POST") {
      frontendReservations++
    }
    if (path.startsWith("/api/presence/") && request.method() === "GET") frontendHolds++
  })
  try {
    await holdLauncherPresence(launcher, id)
    await expect.poll(async () => (await clients(request)).filter((c) => c.surface === "desktop").map((c) => c.id)).toEqual([id])
    await page.goto(`/?shell=desktop&presence=${encodeURIComponent(id)}`)
    await expect(page.getByRole("heading", { name: "What should we work on?" })).toBeVisible()
    expect(await launcher.evaluate(() => (window as LauncherHold).launcherHoldEnded)).toBe(false)
    await expect.poll(async () => (await clients(request)).filter((c) => c.surface === "desktop").map((c) => c.id)).toEqual([id])
    expect(frontendReservations).toBe(0)
    expect(frontendHolds).toBe(0)
    expect(await launcher.evaluate(() => (window as LauncherHold).launcherHoldError)).toBeUndefined()
    await page.reload()
    await expect(page.getByRole("heading", { name: "What should we work on?" })).toBeVisible()
    await expect.poll(async () => (await clients(request)).filter((c) => c.surface === "desktop").map((c) => c.id)).toEqual([id])
    expect(await launcher.evaluate(() => (window as LauncherHold).launcherHoldEnded)).toBe(false)
    expect(frontendReservations).toBe(0)
    expect(frontendHolds).toBe(0)
    await page.close()
    expect(await launcher.evaluate(() => (window as LauncherHold).launcherHoldEnded)).toBe(false)
    expect(await launcher.evaluate(() => (window as LauncherHold).launcherHoldError)).toBeUndefined()
    expect((await clients(request)).filter((c) => c.surface === "desktop").map((c) => c.id)).toEqual([id])
    await launcher.close()
    await expect.poll(async () => (await clients(request)).filter((c) => c.surface === "desktop")).toEqual([])
  } finally {
    if (!launcher.isClosed()) await launcher.close()
  }
})

test("a web tab leaves a native-owned hold alone and reserves its own presence", async ({ page, request }) => {
  const response = await request.post("/api/presence", { data: { surface: "desktop" } })
  expect(response.ok()).toBe(true)
  const { id } = await response.json() as { id: string }
  const launcher = await page.context().newPage()
  let frontendReservations = 0
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/presence" && request.method() === "POST") {
      frontendReservations++
    }
  })
  try {
    await holdLauncherPresence(launcher, id)
    await page.goto(`/?presence=${encodeURIComponent(id)}`)
    await expect(page.getByRole("heading", { name: "What should we work on?" })).toBeVisible()
    await expect.poll(async () => (await clients(request)).filter((c) => c.surface === "web").length).toBe(1)
    const current = await clients(request)
    expect(current.filter((c) => c.surface === "desktop").map((c) => c.id)).toEqual([id])
    expect(current.find((c) => c.surface === "web")?.id).not.toBe(id)
    expect(frontendReservations).toBe(1)
    expect(await launcher.evaluate(() => (window as LauncherHold).launcherHoldEnded)).toBe(false)
    expect(await launcher.evaluate(() => (window as LauncherHold).launcherHoldError)).toBeUndefined()
  } finally {
    await launcher.close()
  }
})
