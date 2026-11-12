import { resolve } from "node:path"
import { expect, test } from "@playwright/test"

test("MCP connection configuration persists and discovers the real server's tool catalog", async ({ page, request }, info) => {
  const name = `remote.example ${Date.now()}`
  let id = ""
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    let dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "MCP", exact: true }).click()
    await dialog.getByRole("button", { name: "Add server" }).click()
    await expect(dialog.getByLabel("Server ID")).toHaveCount(0)
    const displayName = dialog.getByLabel("Display name", { exact: true })
    await expect(displayName).toHaveAttribute("autocorrect", "off")
    await expect(displayName).toHaveAttribute("spellcheck", "false")
    await displayName.fill(name)
    await dialog.getByLabel("Command", { exact: true }).fill("python3")
    await dialog.getByLabel("Arguments (JSON array)").fill(JSON.stringify([resolve("../tools/mcp_fixture.py")]))
    await dialog.getByLabel("Environment variables (JSON object)").fill(JSON.stringify({ ZWAI_MCP_FIXTURE_SECRET: "write-only-fixture" }))
    await dialog.getByRole("button", { name: "Save connection", exact: true }).click()
    await expect(dialog.getByText(name, { exact: true })).toBeVisible()
    await expect(dialog.getByRole("heading", { name: "Tool catalog", exact: true })).toHaveCount(0)
    await dialog.getByRole("button", { name: `Test ${name} and list tools`, exact: true }).click()
    await expect(dialog.getByText("Connected · 1 tools")).toBeVisible()
    const row = dialog.locator("[data-settings-row]", { hasText: name })
    await expect(row.locator("[data-mcp-status='on']")).toHaveClass(/text-done/)
    await expect(row.locator("[data-mcp-status='connected']")).toHaveClass(/text-done/)
    const catalog = dialog.getByRole("region", { name: `${name} Tool catalog` })
    await catalog.getByRole("button", { name: "echo", exact: true }).click()
    await expect(catalog.getByText("Return the supplied acceptance text.")).toBeVisible()
    await expect(catalog.locator("pre")).toContainText('"required"')
    await expect(catalog.locator("pre")).toContainText('"text"')
    await expect(catalog.locator("pre [style*='--shiki-light']").first()).toBeVisible()
    await page.screenshot({ path: info.outputPath("mcp-configuration-and-catalog.png"), animations: "disabled" })

    const list = await (await request.get("/api/mcp")).json()
    const saved = list.servers.find((server: { name: string }) => server.name === name)
    expect(saved.id).toMatch(/^[a-zA-Z0-9_-]{1,32}$/)
    expect(saved.id).not.toBe(name)
    expect(saved.name).toBe(name)
    id = saved.id
    await expect(dialog.getByText(id)).toHaveCount(0)
    expect(saved.env_keys).toEqual(["ZWAI_MCP_FIXTURE_SECRET"])
    expect(JSON.stringify(saved)).not.toContain("write-only-fixture")
    await page.reload()
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "MCP", exact: true }).click()
    await dialog.getByRole("button", { name: `Edit ${name}`, exact: true }).click()
    await expect(dialog.getByLabel("Server ID")).toHaveCount(0)
    await expect(dialog.getByLabel("Command", { exact: true })).toHaveValue("python3")
    await expect(dialog.getByLabel("Environment variables (JSON object)")).toHaveValue("")
    await expect(dialog.getByText(/Saved keys: ZWAI_MCP_FIXTURE_SECRET/)).toBeVisible()
    await page.setViewportSize({ width: 820, height: 900 })
    await expect(dialog.getByLabel("Arguments (JSON array)")).toBeVisible()
    expect(await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath("mcp-editor-narrow.png"), animations: "disabled" })
    await dialog.getByLabel("Display name", { exact: true }).fill(`${name} saved`)
    await dialog.getByRole("button", { name: "Save connection", exact: true }).click()
    await expect(dialog.getByText(`${name} saved`, { exact: true })).toBeVisible()
    const preserved = await (await request.get("/api/mcp")).json()
    expect(preserved.servers.find((server: { id: string }) => server.id === id).env_keys).toEqual(["ZWAI_MCP_FIXTURE_SECRET"])
    await dialog.getByRole("button", { name: `Remove ${name} saved`, exact: true }).click()
    await page.getByRole("button", { name: "Remove server", exact: true }).click()
    await expect(dialog.getByText(`${name} saved`, { exact: true })).toBeHidden()
  } finally {
    if (!id) {
      const list = await (await request.get("/api/mcp")).json()
      id = list.servers.find((server: { id: string; name: string }) => server.name === name || server.name === `${name} saved`)?.id ?? ""
    }
    if (id) await request.delete(`/api/mcp/${encodeURIComponent(id)}`)
  }
})

test("MCP JSON text adds a one-line command and keeps its credential hidden", async ({ page, request }) => {
  const name = `json-${Date.now()}`
  const fixture = resolve("../tools/mcp_fixture.py")
  const content = JSON.stringify({
    mcpServers: {
      [name]: { command: `python3 ${fixture}`, env: { ZWAI_MCP_FIXTURE_SECRET: "write-only-json" } },
    },
  })
  let id = ""
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "MCP", exact: true }).click()
    await dialog.getByRole("button", { name: "Edit JSON", exact: true }).click()
    const editor = dialog.getByLabel("MCP JSON")
    await expect(editor).toHaveAttribute("spellcheck", "false")
    await editor.fill(content)
    await page.setViewportSize({ width: 820, height: 900 })
    expect(await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    await dialog.getByRole("button", { name: "Apply JSON", exact: true }).click()
    await expect(dialog.getByText(name, { exact: true })).toBeVisible()
    await expect(editor).toBeHidden()
    const list = await (await request.get("/api/mcp")).json()
    const saved = list.servers.find((server: { name: string }) => server.name === name)
    expect(saved.command).toBe("python3")
    expect(saved.args).toEqual([fixture])
    expect(saved.env_keys).toEqual(["ZWAI_MCP_FIXTURE_SECRET"])
    expect(JSON.stringify(saved)).not.toContain("write-only-json")
    id = saved.id
    await dialog.getByRole("button", { name: `Test ${name} and list tools`, exact: true }).click()
    await expect(dialog.getByText("Connected · 1 tools")).toBeVisible()
    await dialog.getByRole("button", { name: "Edit JSON", exact: true }).click()
    const exported = dialog.getByLabel("MCP JSON")
    await expect(exported).not.toHaveValue(/write-only-json/)
    const document = JSON.parse(await exported.inputValue())
    document.mcpServers[id].name = `${name} saved`
    await exported.fill(JSON.stringify(document))
    await dialog.getByRole("button", { name: "Apply JSON", exact: true }).click()
    await expect(dialog.getByText(`${name} saved`, { exact: true })).toBeVisible()
    const preserved = await (await request.get("/api/mcp")).json()
    expect(preserved.servers.find((server: { id: string }) => server.id === id).env_keys).toEqual(["ZWAI_MCP_FIXTURE_SECRET"])
    await dialog.getByRole("button", { name: `Remove ${name} saved`, exact: true }).click()
    await page.getByRole("button", { name: "Remove server", exact: true }).click()
    await expect(dialog.getByText(`${name} saved`, { exact: true })).toBeHidden()
  } finally {
    if (!id) {
      const list = await (await request.get("/api/mcp")).json()
      id = list.servers.find((server: { name: string }) => server.name === name || server.name === `${name} saved`)?.id ?? ""
    }
    if (id) await request.delete(`/api/mcp/${encodeURIComponent(id)}`)
  }
})
