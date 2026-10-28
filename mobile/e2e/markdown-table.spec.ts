import { expect, test, type Locator, type Page } from "@playwright/test"

const DETAIL = "服务会在后台完成连接检查，结果会保留在当前会话中，方便稍后查看运行情况。".repeat(8)
const TOKEN = "https://example.invalid/" + "segment".repeat(45)
const MARKDOWN = [
  "| Code | Description |",
  "| --- | --- |",
  `| HTTP | ${DETAIL} |`,
  `| 部署状态 | ${DETAIL} |`,
  "",
  "| Name | Resource |",
  "| --- | --- |",
  `| Endpoint | \`${TOKEN}\` |`,
  "",
  "| Left | Center | Right | Fourth | Fifth | Last |",
  "| :--- | :---: | ---: | --- | --- | --- |",
  "| alpha | ready | 12 | fourth | fifth | final value |",
].join("\n")

async function sendTables(page: Page) {
  await page.goto("/?mock=1&tick=0")
  await page.getByRole("button", { name: "返回", exact: true }).click()
  await page.getByRole("button", { name: "打开 Sweep the unused exports", exact: true }).click()
  await page.getByLabel("消息", { exact: true }).fill(MARKDOWN)
  await page.locator("form").getByRole("button", { name: "发送", exact: true }).click()
  await expect(page.getByTestId("markdown-table")).toHaveCount(3)
}

async function expectSingleLine(cell: Locator) {
  // Measure rendered text, not the row: the adjacent description is allowed
  // to wrap while a short label remains readable on one line.
  const text = await cell.evaluate((element) => {
    const range = document.createRange()
    range.selectNodeContents(element)
    return {
      height: range.getBoundingClientRect().height,
      lineHeight: Number.parseFloat(getComputedStyle(element).lineHeight),
    }
  })
  expect(text.height).toBeGreaterThan(0)
  expect(text.height).toBeLessThanOrEqual(text.lineHeight + 1)
}

for (const width of [320, 430]) {
  test(`tables keep short labels readable and wide columns scroll inside ${width}px`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 812 })
    await sendTables(page)
    const tables = page.getByTestId("markdown-table")
    const summary = tables.nth(0)
    await summary.scrollIntoViewIfNeeded()
    await expectSingleLine(summary.getByRole("cell", { name: "HTTP", exact: true }))
    await expectSingleLine(summary.getByRole("cell", { name: "部署状态", exact: true }))
    const cells = summary.locator("tbody tr").first().locator("td")
    const labelBox = await cells.nth(0).boundingBox()
    const detailBox = await cells.nth(1).boundingBox()
    expect(labelBox).not.toBeNull()
    expect(detailBox).not.toBeNull()
    expect(detailBox!.width).toBeGreaterThan(labelBox!.width * 1.25)
    await page.screenshot({ path: info.outputPath("readable-labels.png") })

    const resource = tables.nth(1)
    await resource.scrollIntoViewIfNeeded()
    await expect(resource.locator("code")).toHaveText(TOKEN)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)

    const wide = tables.nth(2)
    await wide.scrollIntoViewIfNeeded()
    const overflow = await wide.evaluate((element) => ({
      client: element.clientWidth,
      scroll: element.scrollWidth,
      right: element.getBoundingClientRect().right,
    }))
    expect(overflow.right).toBeLessThanOrEqual(width)
    expect(overflow.scroll).toBeGreaterThan(overflow.client)
    const aligned = wide.locator("tbody td")
    for (const [column, alignment] of ["left", "center", "right"].entries()) {
      await expect(aligned.nth(column)).toHaveCSS("text-align", alignment)
    }
    await wide.evaluate((element) => { element.scrollLeft = element.scrollWidth })
    expect(await wide.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
    const lastBox = await wide.getByRole("cell", { name: "final value", exact: true }).boundingBox()
    expect(lastBox).not.toBeNull()
    expect(lastBox!.x + lastBox!.width).toBeLessThanOrEqual(overflow.right + 1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
    await page.screenshot({ path: info.outputPath("wide-table-scrolled.png") })
  })
}
