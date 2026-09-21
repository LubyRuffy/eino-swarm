import { expect, test } from "@playwright/test"

test("phone settings shows a pairing QR control", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByLabel("Hub URL")).toBeVisible()
  await expect(dialog.getByLabel("Host Token")).toHaveCount(0)
  await expect(dialog.getByLabel("Event text on the phone")).toBeVisible()
  await expect(dialog.getByLabel("Events on the phone")).toBeVisible()
  await expect(dialog.getByLabel("Keep this computer awake")).toBeVisible()
  await expect(dialog.getByText("Bound phones")).toBeVisible()
  await expect(dialog.getByText("No phones bound yet.")).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Show pairing QR" })).toBeVisible()
  await dialog.getByRole("button", { name: "Show pairing QR" }).click()
  const toast = page.getByRole("alert")
  await expect(toast).toContainText("Couldn't set up the phone")
  await expect(toast).toBeInViewport()
  await expect(page.getByTestId("remote-qr")).toHaveCount(0)
  await toast.getByRole("button", { name: "Dismiss" }).click()
  await expect(toast).toHaveCount(0)
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
