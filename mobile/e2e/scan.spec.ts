import { expect, test } from "@playwright/test"

test("scan screen is the product path and paste uses the same URI", async ({
  page,
}) => {
  await page.goto("/")
  await expect(page.getByRole("button", { name: "Scan QR" })).toBeVisible()
  await expect(page.getByLabel("Pairing URI")).toBeVisible()
  await page.getByLabel("Pairing URI").fill("https://example.test/not-this")
  await page.getByRole("button", { name: "Paste and bind" }).click()
  await expect(page.getByRole("alert")).toBeVisible()
  const pub = "A".repeat(43)
  await page
    .getByLabel("Pairing URI")
    .fill("pairlink:v1:http://127.0.0.1:9:ScanCode01:" + pub)
  await page.getByRole("button", { name: "Paste and bind" }).click()
  await expect(page.getByRole("alert")).toBeVisible()
})
