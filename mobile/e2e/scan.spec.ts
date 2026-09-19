import { expect, test } from "@playwright/test"

test("scan screen is the product path and paste uses the same URI", async ({
  page,
}) => {
  await page.goto("/")
  await expect(page.getByRole("button", { name: "扫描二维码" })).toBeVisible()
  await expect(page.getByLabel("配对 URI")).toBeVisible()
  await page.getByLabel("配对 URI").fill("https://example.test/not-this")
  await page.getByRole("button", { name: "粘贴并绑定" }).click()
  await expect(page.getByRole("alert")).toBeVisible()
  const pub = "A".repeat(43)
  await page
    .getByLabel("配对 URI")
    .fill("pairlink:v1:http://127.0.0.1:9:ScanCode01:" + pub)
  await page.getByRole("button", { name: "粘贴并绑定" }).click()
  await expect(page.getByRole("alert")).toBeVisible()
})
