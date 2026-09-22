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

test("scan opens a viewfinder whose beam sweeps the frame", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "扫描二维码" }).click()
  const finder = page.getByRole("dialog", { name: "扫描二维码" })
  await expect(finder).toBeVisible()
  await expect(finder.getByText("把配对二维码放进取景框")).toBeVisible()
  await expect(finder.locator(".scan-corner")).toHaveCount(4)
  const beam = finder.locator(".scan-beam")
  await expect(beam).toBeVisible()
  const motion = await beam.evaluate((el) => ({
    name: getComputedStyle(el).animationName,
    running: el.getAnimations().some((anim) => anim.playState === "running"),
  }))
  expect(motion.name).toBe("scan-beam")
  expect(motion.running).toBe(true)
  await expect
    .poll(() => finder.locator("video").evaluate((el) => (el as HTMLVideoElement).readyState))
    .toBeGreaterThan(0)
  await finder.getByRole("button", { name: "关闭扫码" }).click()
  await expect(finder).toHaveCount(0)
  await expect(page.getByLabel("配对 URI")).toBeVisible()
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
