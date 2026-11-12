import { expect, test } from "@playwright/test"

test("phone settings hides the server address and self-hosted connection editor", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByRole("button", { name: "Sign in", exact: true })).toBeVisible()
  await expect(dialog.getByRole("switch", { name: "This PC's connection", exact: true })).not.toBeChecked()
  await expect(dialog.getByText("Configured server", { exact: true })).toHaveCount(0)
  await expect(dialog.getByRole("button", { name: "Advanced / self-hosted server", exact: true })).toHaveCount(0)
  await expect(dialog.getByLabel("Hub URL")).toHaveCount(0)
  await expect(dialog.getByLabel("Host Token")).toHaveCount(0)
  await expect(dialog.getByLabel("Connection sign-in")).toHaveCount(0)
  await expect(dialog.getByLabel("Event text on the phone")).toHaveCount(0)
  await expect(dialog.getByLabel("This computer's name")).toHaveCount(0)
  await expect(dialog.getByText(/Stored in/)).toHaveCount(0)
  await expect(dialog.getByRole("heading", { name: "Other settings" })).toHaveCount(0)
  await expect(dialog.getByLabel("Keep this computer awake")).toHaveCount(0)
  await expect(dialog.getByText("Bound phones")).toHaveCount(0)
  await expect(dialog.getByText("No phones bound yet.")).toHaveCount(0)
  await expect(dialog.getByRole("heading", { name: "Pairing QR" })).toHaveCount(0)
  await expect(dialog.getByRole("button", { name: "Show pairing QR" })).toHaveCount(0)
  await expect(dialog.getByText("This PC is not connected to the hub.")).toHaveCount(0)
  await expect(page.getByTestId("remote-qr")).toHaveCount(0)
})

test("bound phones paints the reported model instead of a bare fingerprint", async ({
  page,
}) => {
  await page.route("**/api/remote/bindings", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        bindings: [
          {
            id: "b1",
            device_fp: "aa11bb22cc33dd44",
            device: "Phone 1.0 Device",
            created_at: "2026-09-20T16:00:00Z",
            last_seen: "2026-09-20T16:03:32Z",
            session_id: "s1",
          },
        ],
      }),
    })
  })
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByText("Phone 1.0 Device")).toBeVisible()
  await expect(dialog.getByText(/aa11bb22cc33dd44/)).toBeVisible()
  await expect(dialog.getByText(/Last connected/)).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Revoke" })).toBeVisible()
})
