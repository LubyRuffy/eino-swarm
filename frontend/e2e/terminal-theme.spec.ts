import { expect, test, type Locator, type Page, type WebSocket } from "@playwright/test"

import { freshConversation } from "./session"

type ThemeChoice = "Light" | "Dark" | "Match the system"

async function appearance(page: Page, theme: ThemeChoice, palette?: "ZWAI" | "FOFA") {
  await page.getByRole("button", { name: "Settings", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("radio", { name: theme, exact: true }).click()
  if (palette) {
    await dialog.getByRole("combobox", { name: "Color theme" }).click()
    await page.getByRole("option", { name: palette, exact: true }).click()
  }
  await dialog.getByRole("button", { name: "Back to app", exact: true }).click()
  await expect(dialog).toBeHidden()
}

async function openTerminal(page: Page) {
  const ready = page.waitForEvent("websocket", {
    predicate: (socket) => new URL(socket.url()).pathname.endsWith("/terminal"),
  }).then(async (socket) => {
    const frame = await socket.waitForEvent("framereceived", {
      predicate: ({ payload }) => typeof payload === "string" && payload.includes('"type":"ready"'),
    })
    return JSON.parse(frame.payload as string) as { cwd: string }
  })
  await page.getByRole("banner").getByRole("button", { name: "Open terminal" }).click()
  const metadata = await ready
  const active = page.locator('[data-testid^="terminal-session-"]:visible')
  await expect(active).toHaveCount(1)
  const id = (await active.getAttribute("data-testid"))!
  return { terminal: page.getByTestId(id), ...metadata }
}

async function markerOutput(page: Page, terminal: Locator, marker: string) {
  await terminal.locator(".xterm-helper-textarea").focus()
  await page.keyboard.type(`printf '\\033[2J\\033[H'; printf '%s\\n' '${marker}'`)
  await page.keyboard.press("Enter")
  // The echoed command also contains the marker; wait for the output row.
  const output = terminal.locator(".xterm-rows > div").filter({ hasText: new RegExp(`^${marker}$`) })
  await expect(output).toHaveCount(1)
  await expect(output).toHaveText(marker)
  return output
}

async function paint(terminal: Locator) {
  return terminal.evaluate((host) => {
    const root = getComputedStyle(document.documentElement)
    const probe = document.createElement("span")
    probe.style.backgroundColor = `hsl(${root.getPropertyValue("--card")})`
    probe.style.color = `hsl(${root.getPropertyValue("--foreground")})`
    document.body.append(probe)
    const expected = getComputedStyle(probe)
    const expectedBackground = expected.backgroundColor
    const expectedForeground = expected.color
    probe.remove()
    return {
      background: getComputedStyle(host.querySelector(".xterm-scrollable-element")!).backgroundColor,
      foreground: getComputedStyle(host.querySelector(".xterm-rows")!).color,
      expectedBackground,
      expectedForeground,
    }
  })
}

async function expectCurrentPaint(terminal: Locator) {
  await expect.poll(async () => {
    const colour = await paint(terminal)
    return colour.background === colour.expectedBackground && colour.foreground === colour.expectedForeground
  }).toBe(true)
  const { background, foreground } = await paint(terminal)
  expect(background).not.toBe("rgba(0, 0, 0, 0)")
  expect(foreground).not.toBe(background)
  return { background, foreground }
}

function terminalConnections(page: Page) {
  const sockets: WebSocket[] = []
  const closed: WebSocket[] = []
  page.on("websocket", (socket) => {
    if (!new URL(socket.url()).pathname.endsWith("/terminal")) return
    sockets.push(socket)
    socket.on("close", () => closed.push(socket))
  })
  return { sockets, closed }
}

test("open and hidden terminals follow theme and palette without replacing their shells", async ({ page, request }, info) => {
  const settings = await request.get("/api/settings")
  expect(settings.ok()).toBe(true)
  const originalUI = (await settings.json()).settings.ui
  const connections = terminalConnections(page)
  await freshConversation(page)
  try {
    await appearance(page, "Light", "ZWAI")
    const first = await openTerminal(page)
    const marker = "terminal-theme-preserved-output"
    const output = await markerOutput(page, first.terminal, marker)
    const light = await expectCurrentPaint(first.terminal)
    await appearance(page, "Dark")
    await expect(page.locator("html")).toHaveClass(/dark/)
    const dark = await expectCurrentPaint(first.terminal)
    expect(dark).not.toEqual(light)
    await expect(output).toHaveText(marker)
    await page.screenshot({ path: info.outputPath("terminal-dark.png"), animations: "disabled" })

    const second = await openTerminal(page)
    await expect(first.terminal).toBeHidden()
    await appearance(page, "Dark", "FOFA")
    await expect(page.locator("html")).toHaveAttribute("data-palette", "fofa")
    const fofa = await expectCurrentPaint(second.terminal)
    expect(fofa).not.toEqual(dark)
    // Hidden tabs keep the same shell and must already have the new colours
    // when selected; switching tabs cannot be the mechanism that repairs them.
    expect(await expectCurrentPaint(first.terminal)).toEqual(fofa)
    const tabLabel = first.cwd.replace(/[\\/]+$/, "").split(/[\\/]/).pop()!
    await page.getByTestId("terminal-panel").getByRole("button", { name: tabLabel, exact: true }).first().click()
    await expect(first.terminal).toBeVisible()
    await expect(output).toHaveText(marker)

    await appearance(page, "Light", "ZWAI")
    await expect(page.locator("html")).not.toHaveClass(/dark/)
    expect(await expectCurrentPaint(first.terminal)).toEqual(light)
    expect(await expectCurrentPaint(second.terminal)).toEqual(light)
    await expect(output).toHaveText(marker)
    expect(connections.sockets).toHaveLength(2)
    expect(connections.closed).toHaveLength(0)
    await page.screenshot({ path: info.outputPath("terminal-light.png"), animations: "disabled" })
  } finally {
    // Palette is a server preference shared with the following E2E cases.
    const restored = await request.put("/api/settings", { data: { ui: originalUI } })
    expect(restored.ok()).toBe(true)
  }
})

test("system appearance changes recolour a running terminal while explicit themes stay pinned", async ({ page }, info) => {
  const connections = terminalConnections(page)
  await page.emulateMedia({ colorScheme: "light" })
  await freshConversation(page)
  await appearance(page, "Match the system")
  const { terminal } = await openTerminal(page)
  const marker = "terminal-system-theme-output"
  const output = await markerOutput(page, terminal, marker)
  const light = await expectCurrentPaint(terminal)
  await page.emulateMedia({ colorScheme: "dark" })
  await expect(page.locator("html")).toHaveClass(/dark/)
  const dark = await expectCurrentPaint(terminal)
  expect(dark).not.toEqual(light)
  await expect(output).toHaveText(marker)
  await page.screenshot({ path: info.outputPath("terminal-system-dark.png"), animations: "disabled" })
  await page.emulateMedia({ colorScheme: "light" })
  await expect(page.locator("html")).not.toHaveClass(/dark/)
  expect(await expectCurrentPaint(terminal)).toEqual(light)

  await appearance(page, "Light")
  await page.emulateMedia({ colorScheme: "dark" })
  await expect(page.locator("html")).not.toHaveClass(/dark/)
  expect(await expectCurrentPaint(terminal)).toEqual(light)
  await appearance(page, "Dark")
  await page.emulateMedia({ colorScheme: "light" })
  await expect(page.locator("html")).toHaveClass(/dark/)
  expect(await expectCurrentPaint(terminal)).toEqual(dark)
  await expect(output).toHaveText(marker)
  expect(connections.sockets).toHaveLength(1)
  expect(connections.closed).toHaveLength(0)
})
