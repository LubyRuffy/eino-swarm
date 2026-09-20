import { expect, test } from "@playwright/test"

test("phone settings shows a pairing QR control", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByLabel("Hub URL")).toBeVisible()
  await expect(dialog.getByLabel("Host Token")).toHaveCount(0)
  await expect(dialog.getByLabel("Event text on the phone")).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Show pairing QR" })).toBeVisible()
  await dialog.getByRole("button", { name: "Show pairing QR" }).click()
  const toast = page.getByRole("alert")
  await expect(toast).toContainText("Couldn't set up the phone")
  await expect(toast).toBeInViewport()
  await expect(page.getByTestId("remote-qr")).toHaveCount(0)
  await toast.getByRole("button", { name: "Dismiss" }).click()
  await expect(toast).toHaveCount(0)
})
