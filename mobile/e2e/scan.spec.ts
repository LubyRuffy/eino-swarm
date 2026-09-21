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

test("a saved ticket shows host tabs and connecting, not the scan form", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem(
      "zwai.remote.link",
      JSON.stringify({
        hubURL: "http://127.0.0.1:9",
        ticket: "tick",
        hostPub: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
        sessionID: "ab".repeat(16),
        fingerprint: "fp",
      }),
    )
  })
  await page.goto("/")
  await expect(page.getByRole("tablist", { name: "电脑" })).toBeVisible()
  await expect(page.getByRole("button", { name: "添加 PC" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "扫码绑定这台 PC" })).toHaveCount(0)
  await expect(page.getByLabel("配对 URI")).toHaveCount(0)
  await expect(page.getByRole("status").or(page.getByRole("alert"))).toBeVisible()
  await expect(page.getByRole("alert")).toBeVisible({ timeout: 20_000 })
  await expect(page.getByRole("heading", { name: "扫码绑定这台 PC" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "重试" })).toBeVisible()
  await page.getByRole("button", { name: "添加 PC" }).click()
  const add = page.getByRole("dialog", { name: "添加 PC" })
  await expect(add).toBeVisible()
  await expect(add.getByRole("heading", { name: "扫码绑定这台 PC" })).toHaveCount(0)
  await expect(add.getByRole("alert")).toHaveCount(0)
})
