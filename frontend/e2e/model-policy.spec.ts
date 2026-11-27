import { expect, test } from "@playwright/test"

test("functional model overrides save and restore without switching product mode", async ({ page, request }) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  const { models } = await (await request.get("/api/models")).json()
  const initial = models.find((row: { provider_id: string }) => row.provider_id === provider.id).model
  expect((await request.put("/api/settings", { data: { swarm: { ...settings.swarm, selection_translation: true }, models: { bindings: { translation: { provider_id: provider.id, model: initial } } } } })).ok()).toBeTruthy()
  try {
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Models", exact: true }).click()
    await dialog.getByRole("button", { name: "Configure Translation", exact: true }).click()
    const drawer = page.getByRole("dialog", { name: "Translation", exact: true })
    await expect(drawer.getByLabel("Translation Model", { exact: true })).toHaveValue(initial)
    await drawer.getByLabel("Translation Model", { exact: true }).fill("role-choice")
    await drawer.getByLabel("Translation Model", { exact: true }).press("Tab")
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.models.bindings.translation.model).toBe("role-choice")
    await drawer.getByRole("button", { name: "Use default", exact: true }).click()
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.models.bindings ?? {}).toEqual({})
    await expect(drawer.getByLabel("Translation Model", { exact: true })).toHaveCount(0)
    await page.keyboard.press("Escape")
    await expect(dialog.getByRole("combobox", { name: /^(Product|Execution) mode$/i })).toHaveCount(0)
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm, models: { bindings: settings.models.bindings ?? {} } } })
  }
})

test("managed policy keeps feature controls and hides editable model selection", async ({ page }) => {
  // This fixture exercises the UI projection. Direct mutation rejection is
  // covered separately through the real Go HTTP and phone RPC handlers.
  const policy = { mode: "gateway", configurable: false, ready: true, scope: "gateway:managed", defaults: { conversation: { provider_id: "gateway:managed", model: "account" } } }
  await page.route("**/api/meta", async route => {
    const data = await (await route.fetch()).json()
    await route.fulfill({ json: { ...data, model_policy: policy } })
  })
  await page.route("**/api/settings", async route => {
    const data = await (await route.fetch()).json()
    await route.fulfill({ json: { ...data, settings: { ...data.settings, model_policy: policy } } })
  })
  await page.goto("/")
  await expect(page.getByLabel("Model", { exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Settings", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Models", exact: true }).click()
  await expect(dialog.getByRole("button", { name: "Configure Translation", exact: true })).toBeVisible()
  await expect(dialog.getByText("Functional models", { exact: true })).toHaveCount(0)
})

test("embedding configuration alone enables search and clearing it survives reload", async ({ page, request }, info) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  try {
    await request.put("/api/settings", { data: { models: { bindings: {}, providers: settings.models.providers.map((p: { id: string }) => p.id === provider.id ? { ...p, model: "configured-embedding", catalog: ["configured-embedding"] } : p) }, search: { embedding: false, embedding_provider: "", embedding_model: "" } } })
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Models", exact: true }).click()
    await expect(dialog.getByText("Image generation", { exact: true })).toBeVisible()
    await expect(dialog.getByRole("combobox", { name: "Image generation", exact: true })).toContainText("Use default")
    await expect(dialog.getByRole("button", { name: "Configure Translation", exact: true })).toBeVisible()
    await expect(dialog.getByRole("switch", { name: "Semantic search", exact: true })).toHaveCount(0)
    await expect(dialog.getByRole("heading", { name: "Conversation search", exact: true })).toHaveCount(0)
    const picker = dialog.getByLabel("Embedding model Provider", { exact: true })
    await expect(picker).toContainText("off")
    await picker.click()
    await page.getByRole("option", { name: "configured-embedding", exact: true }).click()
    const model = dialog.getByLabel("Embedding model Model", { exact: true })
    await model.fill("configured-embedding")
    await model.press("Tab")
    await expect.poll(async () => (await (await request.get("/api/search?q=model-config-probe")).json()).embedding).toBe(true)
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.models.bindings.embedding.model).toBe("configured-embedding")
    await model.fill("")
    await model.press("Tab")
    await expect.poll(async () => (await (await request.get("/api/search?q=model-config-probe")).json()).embedding).toBe(false)
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await page.reload()
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    await page.getByRole("dialog").getByRole("tab", { name: "Models", exact: true }).click()
    await expect(page.getByLabel("Embedding model Provider", { exact: true })).toContainText("Not configured (off)")
    await expect(page.getByLabel("Embedding model Model", { exact: true })).toHaveCount(0)
    const { writeFile } = await import("node:fs/promises")
    await writeFile(info.outputPath("functional-model-settings.png"), await page.screenshot({ animations: "disabled" }), { mode: 0o600 })
  } finally {
    await request.put("/api/settings", { data: { models: { ...settings.models, bindings: settings.models.bindings ?? {} }, search: settings.search } })
  }
})
