import { expect, test } from "@playwright/test"

test("phone settings shows a pairing QR control", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Settings" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByRole("tab", { name: "Phone" }).click()
  await expect(dialog.getByLabel("Hub URL")).toBeVisible()
  await expect(dialog.getByLabel("Host Token")).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Show pairing QR" })).toBeVisible()
  await dialog.getByRole("button", { name: "Show pairing QR" }).click()
  await expect(dialog.getByRole("alert")).toBeVisible()
  await expect(page.getByTestId("remote-qr")).toHaveCount(0)
})
