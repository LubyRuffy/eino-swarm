import { expect, test } from "@playwright/test"

test("three clicks on the account portrait reveal model controls", async ({ page, request }) => {
  test.skip(process.env.ZWAI_E2E_FOFA !== "1", "runs against an unseeded FOFA install")
  // The web harness has no system browser, so the server reports sign-in as
  // unavailable. Desktop does not. The portrait lives behind that menu entry.
  await page.route("**/api/remote/status", async (route) => {
    const status = await (await route.fetch()).json()
    await route.fulfill({ json: { ...status, login_available: true } })
  })
  await page.route("**/api/remote/login", async (route) => {
    if (route.request().method() !== "POST") return route.continue()
    await route.fulfill({
      status: 202,
      json: {
        enabled: true, auth_mode: "account", online: false, has_token: false, login_available: true,
        login: { state: "authorization_pending", user_code: "ABCD-1234-EF56-7890" },
      },
    })
  })
  await page.goto("/")
  await expect(page.getByTestId("login-required")).toBeVisible()
  await expect(page.getByRole("button", { name: "Not now" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Use my own model service" })).toHaveCount(0)
  await expect(page.getByLabel("Model")).toHaveCount(0)

  await page.getByRole("button", { name: "Settings" }).click()
  const settings = page.getByRole("dialog")
  await expect(settings.getByRole("tab", { name: "Models" })).toHaveCount(0)
  await settings.getByRole("button", { name: "Back to app" }).click()

  await page.getByRole("button", { name: "Account" }).click()
  await page.getByRole("menuitem", { name: "Sign in", exact: true }).click()
  const account = page.getByTestId("desktop-account-dialog")
  const portrait = account.getByTestId("account-portrait")
  await portrait.click()
  await portrait.click()
  await portrait.click()
  await expect.poll(async () => {
    const meta = await (await request.get("/api/meta")).json()
    return meta.product_mode
  }).toBe("local")
  await account.getByRole("button", { name: "Close" }).click()
  await expect(page.getByTestId("login-required")).toHaveCount(0)
  await expect(page.getByLabel("Model")).toBeVisible()
  await page.getByRole("button", { name: "Settings" }).click()
  await expect(page.getByRole("dialog").getByRole("tab", { name: "Models" })).toBeVisible()
  await page.getByRole("button", { name: "Back to app" }).click()

  await page.getByRole("button", { name: "Account" }).click()
  await page.getByRole("menuitem", { name: /Sign in|Account details/ }).click()
  const restored = page.getByTestId("desktop-account-dialog").getByTestId("account-portrait")
  await restored.click()
  await restored.click()
  await restored.click()
  await expect.poll(async () => {
    const meta = await (await request.get("/api/meta")).json()
    return meta.product_mode
  }).toBe("fofa")
  await page.getByTestId("desktop-account-dialog").getByRole("button", { name: "Close" }).click()
  await expect(page.getByLabel("Model")).toHaveCount(0)
  await page.getByRole("button", { name: "Settings" }).click()
  await expect(page.getByRole("dialog").getByRole("tab", { name: "Models" })).toHaveCount(0)
})
