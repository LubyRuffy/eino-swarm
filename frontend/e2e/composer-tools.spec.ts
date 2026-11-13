import { expect, test, type APIRequestContext } from "@playwright/test"

import { freshConversation, reloadOpenConversation } from "./session"

test("the composer tool menu clears the input and lists MCP tools rather than servers", async ({ page, request }, info) => {
  const id = `e2e-tools-${Date.now()}`
  const hidden = `e2e-off-${Date.now()}`
  const original = await (await request.get("/api/settings")).json()
  try {
    const created = await request.put(`/api/mcp/${id}`, {
      data: {
        name: "Composer desk", transport: "stdio", command: "true", args: [], url: "",
        enabled: true, timeout_seconds: 1,
      },
    })
    expect(created.ok()).toBe(true)
    const parked = await request.put(`/api/mcp/${hidden}`, {
      data: {
        name: "Hidden desk", transport: "stdio", command: "true", args: [], url: "",
        enabled: false, timeout_seconds: 1,
      },
    })
    expect(parked.ok()).toBe(true)

    await freshConversation(page)
    await page.getByTestId("composer-input").fill("keep this request visible")
    await page.getByRole("button", { name: "Tools", exact: true }).click()
    const menu = page.getByTestId("composer-tools-menu")
    const read = page.getByRole("menuitemcheckbox", { name: "Read file (read)" })
    await expect(read).toHaveAttribute("aria-checked", "true")
    await expect(read.locator("[role=switch]")).toHaveAttribute("data-state", "checked")
    await expect(menu.getByRole("button", { name: "Built-in" })).toHaveAttribute("aria-expanded", "true")
    await expect(menu.getByRole("button", { name: "Composer desk" })).toHaveAttribute("aria-expanded", "false")
    await expect(menu.getByRole("group", { name: "Built-in" })).toBeVisible()
    await expect(menu.getByRole("group", { name: "Composer desk" })).toBeVisible()
    await expect(menu.getByText("read", { exact: true })).toHaveCount(0)
    await menu.getByRole("button", { name: "Details for Read file" }).hover()
    await expect(page.getByRole("tooltip")).toContainText("Read a text file")
    await expect(menu.getByText("Files", { exact: true })).toHaveCount(0)
    await expect(menu.getByRole("menuitemcheckbox", { name: "Composer desk" })).toHaveCount(0)
    await expect(menu.getByText("Hidden desk")).toHaveCount(0)
    await expect(menu.getByRole("alert")).toHaveText("Could not list tools")
    const menuBox = await menu.boundingBox()
    const composerBox = await page.locator("[data-composer-box]").boundingBox()
    expect(menuBox).toBeTruthy()
    expect(composerBox).toBeTruthy()
    expect(menuBox!.y + menuBox!.height).toBeLessThanOrEqual(composerBox!.y + 1)
    const fit = await read.locator("[role=switch]").evaluate((node) => {
      const sw = node.getBoundingClientRect()
      const list = node.closest("[data-testid=composer-tools-list]") as HTMLElement
      const listBox = list.getBoundingClientRect()
      const menu = node.closest("[data-testid=composer-tools-menu]")!.getBoundingClientRect()
      return {
        gutter: getComputedStyle(list).scrollbarGutter,
        clearOfBar: listBox.left + list.clientWidth - sw.right,
        clearOfMenu: menu.right - sw.right,
        height: sw.height,
      }
    })
    expect(fit.gutter).toBe("stable")
    expect(fit.clearOfBar).toBeGreaterThanOrEqual(4)
    expect(fit.clearOfMenu).toBeGreaterThanOrEqual(8)
    expect(fit.height).toBeLessThan(15)
    const filter = menu.getByRole("textbox", { name: "Filter tools" })
    await filter.fill("python")
    await expect(menu.getByRole("menuitemcheckbox", { name: "Read file (read)" })).toHaveCount(0)
    await expect(menu.getByRole("menuitemcheckbox", { name: "Run Python (python_runner)" })).toBeVisible()
    await filter.fill("")
    await expect(read).toBeVisible()
    await page.screenshot({ path: info.outputPath("composer-tools-switches.png"), animations: "disabled" })
    await read.click()
    await expect.poll(async () => {
      const settings = await (await request.get("/api/settings")).json()
      return settings.settings.tools.disabled.includes("read")
    }).toBe(true)
    const list = await (await request.get("/api/mcp")).json()
    expect(list.servers.find((server: { id: string }) => server.id === id)?.enabled).toBe(true)

    await reloadOpenConversation(page)
    await page.getByRole("button", { name: "Tools", exact: true }).click()
    await expect(page.getByRole("menuitemcheckbox", { name: "Read file (read)" })).toHaveAttribute("aria-checked", "false")
    await expect(page.getByRole("menuitemcheckbox", { name: "Read file (read)" }).locator("[role=switch]")).toHaveAttribute("data-state", "unchecked")
    await expect(page.getByTestId("composer-tools-menu").getByRole("menuitemcheckbox", { name: "Composer desk" })).toHaveCount(0)
  } finally {
    await restoreTools(request, original.settings?.tools)
    await request.delete(`/api/mcp/${encodeURIComponent(id)}`)
    await request.delete(`/api/mcp/${encodeURIComponent(hidden)}`)
  }
})

async function restoreTools(request: APIRequestContext, tools: unknown) {
  if (!tools) return
  await request.put("/api/settings", { data: { tools } })
}
