import { expect, test, type Page } from "@playwright/test"

async function openProvider(page: Page, heading: string) {
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models", exact: true }).click()
  await dialog.getByRole("button", { name: `${heading} details`, exact: true }).click()
  return dialog
}

test("provider protocol and cloud tool options save and reload through Settings", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  const heading = (provider.label || provider.id).trim()
  const tools = [{ type: "file_search", vector_store_ids: ["store-a"], max_num_results: 3 }]
  try {
    await page.goto("/")
    const dialog = await openProvider(page, heading)
    await expect(dialog.getByLabel("Cloud built-in tools (JSON)")).toHaveValue("[]")
    await dialog.getByLabel("API mode", { exact: true }).click()
    await page.getByRole("option", { name: "Responses", exact: true }).click()
    await dialog.getByLabel("Cloud built-in tools (JSON)").fill(JSON.stringify(tools))
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()

    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.models.providers[0]).toMatchObject({ api_mode: "responses", builtin_tools: tools })
    await page.reload()
    const reopened = await openProvider(page, heading)
    await expect(reopened.getByLabel("API mode", { exact: true })).toContainText("Responses")
    expect(JSON.parse(await reopened.getByLabel("Cloud built-in tools (JSON)").inputValue())).toEqual(tools)
    await reopened.getByRole("button", { name: "Back to app" }).click()
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})

test("invalid JSON and a legacy protocol never silently save or discard cloud tools", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  const heading = (provider.label || provider.id).trim()
  await request.put("/api/settings", {
    data: { models: { ...settings.models, providers: [{ ...provider, api_mode: "responses", builtin_tools: [] }] } },
  })
  try {
    await page.goto("/")
    const dialog = await openProvider(page, heading)
    const editor = dialog.getByLabel("Cloud built-in tools (JSON)")
    await editor.fill("[")
    await expect(editor).toHaveAttribute("aria-invalid", "true")
    await expect(dialog.getByRole("alert")).toContainText("Enter a JSON array")
    await dialog.getByRole("tab", { name: "Swarm", exact: true }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeVisible()
    await expect(editor).toHaveValue("[")
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.models.providers[0].builtin_tools ?? []).toEqual([])

    const draft = '[{"type":"code_interpreter","container":{"type":"auto"}}]'
    await editor.fill(draft)
    await dialog.getByLabel("API mode", { exact: true }).click()
    await page.getByRole("option", { name: "Chat Completions", exact: true }).click()
    await expect(dialog.getByRole("alert")).toContainText("Select Responses")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeVisible()
    await expect(editor).toHaveValue(draft)
    await editor.fill("[]")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.models.providers[0].api_mode).toBe("chat_completions")
    expect(saved.settings.models.providers[0].builtin_tools ?? []).toEqual([])
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})

test("deleting a provider also discards its JSON draft when a new provider reuses the ID", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  await request.put("/api/settings", {
    data: { models: { ...settings.models, providers: [...settings.models.providers, { ...settings.models.providers[0], id: "provider-2", label: "Second", api_mode: "responses", builtin_tools: [] }] } },
  })
  try {
    await page.goto("/")
    const dialog = await openProvider(page, "Second")
    await dialog.getByLabel("Cloud built-in tools (JSON)").fill("[")
    await dialog.getByRole("button", { name: "Remove Second", exact: true }).click()
    await page.getByRole("button", { name: "Remove provider", exact: true }).click()
    await dialog.getByRole("button", { name: "Add a provider", exact: true }).click()
    await expect(dialog.getByRole("button", { name: "provider-2 details", exact: true })).toBeVisible()
    await expect(dialog.getByLabel("Cloud built-in tools (JSON)")).toHaveValue("[]")
    await expect(dialog.getByLabel("Cloud built-in tools (JSON)")).toHaveAttribute("aria-invalid", "false")
    await expect(dialog.getByLabel("API mode", { exact: true })).toContainText("Chat Completions")
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const saved = await (await request.get("/api/settings")).json()
    expect(saved.settings.models.providers[1].id).toBe("provider-2")
    expect(saved.settings.models.providers[1].builtin_tools ?? []).toEqual([])
    expect(saved.settings.models.providers[1].api_mode).toBe("chat_completions")
  } finally {
    await request.put("/api/settings", { data: { models: settings.models } })
  }
})
