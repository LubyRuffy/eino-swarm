import { expect, test, type Request } from "@playwright/test"

test("a desktop font save preserves PC timeouts changed after the dialog loaded", async ({ page, request }, testInfo) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  const font = dialog.getByRole("combobox", { name: "UI font", exact: true })
  await expect(font).toBeVisible()
  const { settings } = await (await request.get("/api/settings")).json()
  const pendingWrites = new Set<Request>()
  try {
    const providers = settings.models.providers.map((p: Record<string, unknown>) => ({
      ...p, timeout_seconds: 99, first_byte_timeout_seconds: 43,
    }))
    // The narrow phone RPC is independently tested over encrypted Pairlink.
    // This concurrent HTTP write uses the same real settings/persistence path.
    expect((await request.put("/api/settings", { data: {
      swarm: { ...settings.swarm, agent_timeout_seconds: 73 },
      models: { ...settings.models, providers },
    } })).ok()).toBe(true)
    const desktopWrites: Record<string, unknown>[] = []
    page.on("request", (r) => {
      if (r.method() === "PUT" && new URL(r.url()).pathname === "/api/settings") {
        pendingWrites.add(r)
        desktopWrites.push(r.postDataJSON())
      }
    })
    page.on("requestfinished", (r) => pendingWrites.delete(r))
    page.on("requestfailed", (r) => pendingWrites.delete(r))
    await font.click()
    await page.getByRole("option", { name: "Serif", exact: true }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    const { settings: saved } = await (await request.get("/api/settings")).json()
    expect(saved.ui.font).toBe("serif")
    expect(saved.swarm.agent_timeout_seconds).toBe(73)
    expect(saved.models.providers.map((p: Record<string, unknown>) => [p.timeout_seconds, p.first_byte_timeout_seconds])).toEqual(providers.map(() => [99, 43]))
    expect(desktopWrites.length).toBeGreaterThan(0)
    for (const write of desktopWrites) expect(Object.keys(write)).toEqual(["ui"])
    await page.getByRole("button", { name: "Settings" }).click()
    await dialog.getByRole("tab", { name: "Swarm" }).click()
    await expect(dialog.getByLabel("Sub-agent timeout (seconds)")).toHaveValue("73")
    await page.screenshot({ path: testInfo.outputPath("desktop-font-preserved-PC-timeouts.png") })
    await testInfo.attach("persisted-timeouts-and-desktop-patches", { body: JSON.stringify({ agent_timeout_seconds: saved.swarm.agent_timeout_seconds, providers: saved.models.providers.map((p: Record<string, unknown>) => ({ id: p.id, timeout_seconds: p.timeout_seconds, first_byte_timeout_seconds: p.first_byte_timeout_seconds })), desktopWrites }), contentType: "application/json" })
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
  } finally {
    // This suite shares one server. Settle this sheet (including its direct
    // appearance write) before restoring only the sections this case changed.
    if (await dialog.isVisible()) {
      await dialog.getByRole("button", { name: "Back to app" }).click()
      await expect(dialog).toBeHidden()
    }
    await expect.poll(() => pendingWrites.size).toBe(0)
    expect((await request.put("/api/settings", { data: {
      ui: settings.ui, swarm: settings.swarm, models: settings.models,
    } })).ok()).toBe(true)
  }
})
