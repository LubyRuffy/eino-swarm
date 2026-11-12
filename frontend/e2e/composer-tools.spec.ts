import { expect, test, type APIRequestContext } from "@playwright/test"

import { freshConversation, reloadOpenConversation } from "./session"

test("the composer can remove a built-in tool and an MCP server before the next request", async ({ page, request }) => {
  const id = `e2e-tools-${Date.now()}`
  const original = await (await request.get("/api/settings")).json()
  try {
    const created = await request.put(`/api/mcp/${id}`, {
      data: {
        name: "Composer desk", transport: "stdio", command: "true", args: [], url: "",
        enabled: true, timeout_seconds: 30,
      },
    })
    expect(created.ok()).toBe(true)

    await freshConversation(page)
    await page.getByRole("button", { name: "Tools", exact: true }).click()
    const read = page.getByRole("menuitemcheckbox", { name: "Read file (read)" })
    const desk = page.getByRole("menuitemcheckbox", { name: "Composer desk" })
    await expect(read).toHaveAttribute("aria-checked", "true")
    await expect(desk).toHaveAttribute("aria-checked", "true")
    await read.click()
    await expect.poll(async () => {
      const settings = await (await request.get("/api/settings")).json()
      return settings.settings.tools.disabled.includes("read")
    }).toBe(true)
    await desk.click()
    await expect.poll(async () => {
      const list = await (await request.get("/api/mcp")).json()
      return list.servers.find((server: { id: string }) => server.id === id)?.enabled
    }).toBe(false)

    await reloadOpenConversation(page)
    await page.getByRole("button", { name: "Tools", exact: true }).click()
    await expect(page.getByRole("menuitemcheckbox", { name: "Read file (read)" })).toHaveAttribute("aria-checked", "false")
    await expect(page.getByRole("menuitemcheckbox", { name: "Composer desk" })).toHaveAttribute("aria-checked", "false")
  } finally {
    await restoreTools(request, original.settings?.tools)
    await request.delete(`/api/mcp/${encodeURIComponent(id)}`)
  }
})

async function restoreTools(request: APIRequestContext, tools: unknown) {
  if (!tools) return
  await request.put("/api/settings", { data: { tools } })
}
