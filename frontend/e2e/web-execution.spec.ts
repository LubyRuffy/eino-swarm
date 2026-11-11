import { expect, test } from "@playwright/test"

test("Web tool execution settings persist local and cloud choices across reloads", async ({ page, request }, info) => {
  const { settings } = await (await request.get("/api/settings")).json()
  try {
    expect(settings.tools.web_execution).toBe("auto")
    await page.goto("/")
    await page.getByRole("button", { name: "Settings" }).click()
    let dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Tools" }).click()
    let execution = dialog.getByLabel("Web tools run on")
    await expect(execution).toHaveText("Automatic")
    await expect(dialog.getByText(/Cloud after server sign-in/)).toBeVisible()
    await expect(dialog.getByRole("heading", { name: "Proxy", exact: true })).toBeVisible()
    await execution.click()
    await page.getByRole("option", { name: "This device" }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.tools.web_execution).toBe("local")

    await page.reload()
    await page.getByRole("button", { name: "Settings" }).click()
    dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Tools" }).click()
    execution = dialog.getByLabel("Web tools run on")
    await expect(execution).toHaveText("This device")
    await execution.click()
    await page.getByRole("option", { name: "Server cloud" }).click()
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await expect(dialog).toBeHidden()
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.tools.web_execution).toBe("cloud")

    await page.getByRole("button", { name: "Settings" }).click()
    dialog = page.getByRole("dialog")
    await dialog.getByRole("tab", { name: "Tools" }).click()
    await expect(dialog.getByLabel("Web tools run on")).toHaveText("Server cloud")
    await expect(dialog.getByRole("heading", { name: "Proxy", exact: true })).toHaveCount(0)
    await expect(dialog.getByLabel("HTTP")).toHaveCount(0)
    await page.screenshot({ path: info.outputPath("web-tools-execution.png"), animations: "disabled" })
  } finally {
    await request.put("/api/settings", { data: { tools: settings.tools } })
  }
})
