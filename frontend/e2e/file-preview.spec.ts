import { readFileSync, statSync, utimesSync, writeFileSync } from "node:fs"
import { join } from "node:path"
import { expect, test, type Page } from "@playwright/test"

const imageBytes = readFileSync(new URL("./fixtures/preview-landscape.png", import.meta.url))

async function upload(page: Page, threadId: string, name: string, bytes: Buffer, mimeType = "text/plain") {
  const response = await page.request.post(`/api/threads/${threadId}/files`, {
    multipart: { files: { name, mimeType, buffer: bytes } },
  })
  expect(response.ok()).toBe(true)
  const { files } = await response.json()
  return files[0].rel_path as string
}

async function workspace(page: Page, title: string, name: string, bytes: Buffer, mimeType?: string) {
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const path = await upload(page, thread.id, name, bytes, mimeType)
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  await page.getByRole("tab", { name: "Files", exact: true }).click()
  const tree = page.getByTestId("file-tree")
  await expect(tree.getByRole("treeitem", { name, exact: true })).toBeVisible()
  return { threadId: thread.id as string, path, tree }
}

test("opens Markdown from Files with rendered content, source and keyboard return", async ({ page }, info) => {
  const source = "# Preview fixture\n\nReadable **document**.\n\n| Item | State |\n| --- | --- |\n| Alpha | Ready |\n\n```go\nfunc main() {}\n```\n"
  const { tree } = await workspace(page, info.title, "guide.md", Buffer.from(source))
  const row = tree.getByRole("treeitem", { name: "guide.md", exact: true })
  await row.click()
  const preview = page.getByTestId("file-preview")
  await expect(preview).toBeVisible()
  await expect(preview.getByRole("heading", { name: "Preview fixture", exact: true })).toBeVisible()
  await expect(preview.getByRole("table")).toContainText("Alpha")
  await expect(preview.locator(".text-syntax-keyword", { hasText: "func" })).toBeVisible()
  await preview.getByRole("button", { name: "Source", exact: true }).click()
  await expect(preview.getByRole("heading", { name: "Preview fixture", exact: true })).toHaveCount(0)
  await expect(preview.locator("pre")).toHaveText(source)
  await preview.getByRole("button", { name: "Preview", exact: true }).click()
  await expect(preview.getByRole("heading", { name: "Preview fixture", exact: true })).toBeVisible()
  await expect(preview).toHaveCSS("opacity", "1")
  writeFileSync(info.outputPath("markdown-preview.png"), await page.screenshot(), { mode: 0o600 })
  await page.keyboard.press("Escape")
  await expect(preview).toBeHidden()
  await expect(tree).toBeFocused()
  await tree.press("Enter")
  await expect(preview.getByRole("heading", { name: "Preview fixture", exact: true })).toBeVisible()
  await page.keyboard.press("Escape")
  const root = page.locator("html")
  if (!await root.evaluate((element) => element.classList.contains("dark"))) {
    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch theme" }).click()
  }
  await expect(root).toHaveClass(/dark/)
  await row.click()
  await expect(preview.getByRole("heading", { name: "Preview fixture", exact: true })).toBeVisible()
  await expect(preview).toHaveCSS("opacity", "1")
  writeFileSync(info.outputPath("markdown-preview-dark.png"), await page.screenshot(), { mode: 0o600 })
})

for (const extension of ["html", "svg"]) {
  test(`shows ${extension.toUpperCase()} as source without executing workspace content`, async ({ page }, info) => {
    const source = extension === "html"
      ? '<div id="preview-script-target">document</div><script>window.__previewExecuted = true</script>'
      : '<svg xmlns="http://www.w3.org/2000/svg" onload="window.__previewExecuted = true"><text>document</text></svg>'
    const name = `document.${extension}`
    const { tree } = await workspace(page, info.title, name, Buffer.from(source), extension === "svg" ? "image/svg+xml" : "text/html")
    await tree.getByRole("treeitem", { name, exact: true }).click()
    const preview = page.getByTestId("file-preview")
    await expect(preview.locator("pre")).toHaveText(source)
    await expect(preview.locator("iframe, object, img, script, #preview-script-target")).toHaveCount(0)
    expect(await page.evaluate(() => (window as Window & { __previewExecuted?: boolean }).__previewExecuted)).toBeUndefined()
  })
}

test("zooms a decoded workspace image, restores its fit and downloads unchanged bytes", async ({ page }, info) => {
  const { tree, threadId, path } = await workspace(page, info.title, "landscape.png", imageBytes, "image/png")
  const saved = await page.request.get(`/api/threads/${threadId}/download/${path.split("/").map(encodeURIComponent).join("/")}`)
  expect(saved.ok()).toBe(true)
  expect(saved.headers()["content-type"]).toMatch(/^image\/png(?:;|$)/)
  expect(await saved.body()).toEqual(imageBytes)
  await tree.getByRole("treeitem", { name: "landscape.png", exact: true }).click()
  const preview = page.getByTestId("file-preview")
  const image = preview.getByRole("img", { name: "landscape.png", exact: true })
  await expect(image).toBeVisible()
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => [element.naturalWidth, element.naturalHeight]))
    .toEqual([imageBytes.readUInt32BE(16), imageBytes.readUInt32BE(20)])
  await expect.poll(() => preview.evaluate((element) =>
    element.getAnimations().every((animation) => animation.playState !== "running"),
  )).toBe(true)
  const viewport = preview.getByTestId("image-preview-viewport")
  const level = preview.getByLabel("Zoom level", { exact: true })
  const zoomIn = preview.getByRole("button", { name: "Zoom in", exact: true })
  const zoomOut = preview.getByRole("button", { name: "Zoom out", exact: true })
  const actualSize = preview.getByRole("button", { name: "Actual size", exact: true })
  const fit = preview.getByRole("button", { name: "Fit to window", exact: true })
  await expect(zoomIn).toBeEnabled()
  const fittedSize = await image.boundingBox()
  const fittedLevel = await level.textContent()
  expect(fittedSize).toBeTruthy()
  expect(fittedSize!.width).toBeLessThanOrEqual(1200)
  expect(fittedSize!.height).toBeLessThanOrEqual(800)
  expect(fittedSize!.width / fittedSize!.height).toBeCloseTo(1.5, 2)
  const initialViewport = await viewport.boundingBox()
  expect(initialViewport).toBeTruthy()
  expect(fittedSize!.width).toBeLessThanOrEqual(initialViewport!.width + 1)
  expect(fittedSize!.height).toBeLessThanOrEqual(initialViewport!.height + 1)

  await zoomIn.click()
  await expect.poll(async () => (await image.boundingBox())!.width).toBeGreaterThan(fittedSize!.width)
  const enlargedSize = await image.boundingBox()
  expect(enlargedSize!.width / enlargedSize!.height).toBeCloseTo(1.5, 2)
  await zoomOut.click()
  await expect.poll(async () => (await image.boundingBox())!.width).toBeLessThan(enlargedSize!.width)
  await actualSize.click()
  await expect(level).toHaveText("100%")
  await expect.poll(() => image.evaluate((element) => [element.getBoundingClientRect().width, element.getBoundingClientRect().height]))
    .toEqual([1200, 800])

  for (let clicks = 0; clicks < 60 && await zoomIn.isEnabled(); clicks++) await zoomIn.click()
  await expect(zoomIn).toBeDisabled()
  await expect(level).toHaveText("400%")
  await expect.poll(() => image.evaluate((element) => [element.getBoundingClientRect().width, element.getBoundingClientRect().height]))
    .toEqual([4800, 3200])
  const overflow = await viewport.evaluate((element) => ({
    width: element.scrollWidth, height: element.scrollHeight, clientWidth: element.clientWidth, clientHeight: element.clientHeight,
  }))
  expect(overflow.width).toBeGreaterThan(overflow.clientWidth)
  expect(overflow.height).toBeGreaterThan(overflow.clientHeight)
  await viewport.evaluate((element) => { element.scrollLeft = element.scrollWidth; element.scrollTop = element.scrollHeight })
  const corner = await viewport.evaluate((element) => {
    const bounds = element.getBoundingClientRect()
    const image = element.querySelector("img")!.getBoundingClientRect()
    return { x: element.scrollLeft, y: element.scrollTop, right: image.right, bottom: image.bottom,
      left: bounds.left, top: bounds.top, viewportRight: bounds.left + element.clientWidth, viewportBottom: bounds.top + element.clientHeight }
  })
  expect(corner.x).toBeGreaterThan(0)
  expect(corner.y).toBeGreaterThan(0)
  expect(corner.right).toBeGreaterThan(corner.left)
  expect(corner.bottom).toBeGreaterThan(corner.top)
  expect(corner.right).toBeLessThanOrEqual(corner.viewportRight + 1)
  expect(corner.bottom).toBeLessThanOrEqual(corner.viewportBottom + 1)
  writeFileSync(info.outputPath("image-preview-zoomed.png"), await page.screenshot(), { mode: 0o600 })

  for (let clicks = 0; clicks < 60 && await zoomOut.isEnabled(); clicks++) await zoomOut.click()
  await expect(zoomOut).toBeDisabled()
  await expect(level).toHaveText("10%")
  await expect.poll(() => image.evaluate((element) => [element.getBoundingClientRect().width, element.getBoundingClientRect().height]))
    .toEqual([120, 80])
  await fit.click()
  await expect(level).toHaveText(fittedLevel!)
  await expect.poll(async () => (await image.boundingBox())!.width).toBeCloseTo(fittedSize!.width, 0)
  await expect.poll(() => viewport.evaluate((element) => [element.scrollLeft, element.scrollTop])).toEqual([0, 0])

  await actualSize.click()
  const oldImageURL = await image.getAttribute("src")
  await preview.getByRole("button", { name: "Refresh", exact: true }).click()
  await expect(image).toBeVisible()
  await expect(image).not.toHaveAttribute("src", oldImageURL!)
  await expect(level).toHaveText(fittedLevel!)
  await expect.poll(async () => (await image.boundingBox())!.width).toBeCloseTo(fittedSize!.width, 0)
  await actualSize.click()
  await page.keyboard.press("Escape")
  await expect(preview).toBeHidden()
  await expect(tree).toBeFocused()
  await tree.press("Enter")
  await expect(level).toHaveText(fittedLevel!)

  const downloaded = page.waitForEvent("download")
  await preview.locator("a[download]").click()
  const download = await downloaded
  expect(download.suggestedFilename()).toBe("landscape.png")
  const downloadedPath = await download.path()
  expect(downloadedPath).toBeTruthy()
  expect(readFileSync(downloadedPath!)).toEqual(imageBytes)
  await page.keyboard.press("Escape")
  await page.reload()
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: info.title, exact: true }).click()
  await page.getByRole("tab", { name: "Files", exact: true }).click()
  await tree.getByRole("treeitem", { name: "landscape.png", exact: true }).click()
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => [element.naturalWidth, element.naturalHeight]))
    .toEqual([1200, 800])
  const reloaded = await page.request.get(`/api/threads/${threadId}/download/${path.split("/").map(encodeURIComponent).join("/")}`)
  expect(reloaded.headers()["content-type"]).toMatch(/^image\/png(?:;|$)/)
  expect(await reloaded.body()).toEqual(imageBytes)

  await page.setViewportSize({ width: 480, height: 720 })
  await fit.click()
  const narrowPreview = await preview.boundingBox()
  expect(narrowPreview).toBeTruthy()
  expect(narrowPreview!.x).toBeGreaterThanOrEqual(0)
  expect(narrowPreview!.x + narrowPreview!.width).toBeLessThanOrEqual(481)
  for (const control of [zoomIn, zoomOut, actualSize, fit, level, preview.locator("a[download]")]) {
    await expect(control).toBeVisible()
    const bounds = await control.boundingBox()
    expect(bounds!.x).toBeGreaterThanOrEqual(narrowPreview!.x)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(narrowPreview!.x + narrowPreview!.width + 1)
  }
  const narrowImage = await image.boundingBox()
  const narrowViewport = await viewport.boundingBox()
  expect(narrowImage!.width).toBeLessThanOrEqual(narrowViewport!.width + 1)
  expect(narrowImage!.height).toBeLessThanOrEqual(narrowViewport!.height + 1)
  writeFileSync(info.outputPath("image-preview-narrow.png"), await page.screenshot(), { mode: 0o600 })
})

test("does not offer working zoom controls for an image that cannot decode", async ({ page }, info) => {
  const { tree } = await workspace(page, info.title, "broken.png", imageBytes, "image/png")
  await page.route("**/download/**", (route) => route.fulfill({
    status: 200, contentType: "image/png", body: Buffer.from("invalid image bytes"),
  }))
  await tree.getByRole("treeitem", { name: "broken.png", exact: true }).click()
  const preview = page.getByTestId("file-preview")
  await expect(preview.getByRole("alert")).toBeVisible()
  for (const name of ["Zoom in", "Zoom out", "Actual size", "Fit to window"]) {
    const button = preview.getByRole("button", { name, exact: true })
    if (await button.count()) await expect(button).toBeDisabled()
  }
  await expect(preview.locator("a[download]")).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(preview).toBeHidden()
  await expect(tree).toBeFocused()
})

test("refreshing a removed file shows its failure and can read its replacement", async ({ page }, info) => {
  const { threadId, path, tree } = await workspace(page, info.title, "status.txt", Buffer.from("Original bytes"))
  await tree.getByRole("treeitem", { name: "status.txt", exact: true }).click()
  const preview = page.getByTestId("file-preview")
  await expect(preview.locator("pre")).toHaveText("Original bytes")
  const url = `/api/threads/${threadId}/download/${path.split("/").map(encodeURIComponent).join("/")}`
  expect((await page.request.delete(url)).ok()).toBe(true)
  await preview.getByRole("button", { name: "Refresh", exact: true }).click()
  await expect(preview.getByRole("alert")).toBeVisible()
  await expect(preview.locator("pre")).toHaveCount(0)
  expect(await upload(page, threadId, "status.txt", Buffer.from("Replacement bytes"))).toBe(path)
  await preview.getByRole("button", { name: "Refresh", exact: true }).click()
  await expect(preview.locator("pre")).toHaveText("Replacement bytes")
  await expect(preview.getByRole("alert")).toHaveCount(0)
})

test("refresh reads changed bytes even when file size and modification time stay the same", async ({ page }, info) => {
  const original = Buffer.from("Original bytes")
  const updated = Buffer.from("Revision bytes")
  expect(updated.length).toBe(original.length)
  const { threadId, path, tree } = await workspace(page, info.title, "current.txt", original)
  await tree.getByRole("treeitem", { name: "current.txt", exact: true }).click()
  const preview = page.getByTestId("file-preview")
  await expect(preview.getByTestId("file-source")).toHaveText(original.toString())
  // This is the suite's private workspace. Preserve Last-Modified so a
  // conditional response could incorrectly serve the browser's earlier bytes.
  const listed = await page.request.get(`/api/threads/${threadId}/files`)
  expect(listed.ok()).toBe(true)
  const listing = await listed.json()
  const filename = join(listing.workspace as string, path)
  const before = statSync(filename)
  writeFileSync(filename, updated)
  utimesSync(filename, before.atime, before.mtime)
  const refreshed = page.waitForResponse((response) => response.url().includes(`/download/${path}`))
  await preview.getByRole("button", { name: "Refresh", exact: true }).click()
  expect((await refreshed).status()).toBe(200)
  await expect(preview.getByTestId("file-source")).toHaveText(updated.toString())
})

test("contains a wide Markdown table in a short preview window", async ({ page }, info) => {
  await page.setViewportSize({ width: 1000, height: 640 })
  const source = `# Compact preview\n\n| Token | Description |\n| --- | --- |\n| ${"unbroken".repeat(100)} | ${"Long content ".repeat(50)} |\n`
  const { tree } = await workspace(page, info.title, "wide.md", Buffer.from(source))
  await tree.getByRole("treeitem", { name: "wide.md", exact: true }).click()
  const preview = page.getByTestId("file-preview")
  await expect(preview.getByRole("table")).toBeVisible()
  // Visibility can precede completion of the dialog's entrance animation.
  // Measure its settled layout rather than the intermediate scale/translation.
  await expect.poll(() => preview.evaluate((element) =>
    element.getAnimations().every((animation) => animation.playState !== "running"),
  )).toBe(true)
  const box = await preview.boundingBox()
  expect(box).toBeTruthy()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(1001)
  expect(box!.y + box!.height).toBeLessThanOrEqual(641)
  const table = await preview.getByTestId("markdown-table").boundingBox()
  expect(table).toBeTruthy()
  expect(table!.x + table!.width).toBeLessThanOrEqual(box!.x + box!.width + 1)
  writeFileSync(info.outputPath("markdown-preview-short.png"), await page.screenshot(), { mode: 0o600 })
})

test("keeps short table labels readable beside long content and scrolls excess columns", async ({ page }, info) => {
  await page.setViewportSize({ width: 1000, height: 760 })
  const token = "identifier_".repeat(60)
  const source = [
    "# Table layout", "",
    "| Area | Details |", "| --- | --- |",
    `| GPU/API | ${"说明内容应有足够空间自然换行，同时保留短标签的可读宽度。".repeat(12)} |`,
    `| 部署范围 | \`${token}\` |`, "",
    "| Left | Center | Right | Fourth | Fifth | Sixth | Seventh | Eighth | Ninth | Last |",
    "| :--- | :---: | ---: | --- | --- | --- | --- | --- | --- | --- |",
    "| One | Two | Three | Four | Five | Six | Seven | Eight | Nine | End |",
  ].join("\n")
  const { tree } = await workspace(page, info.title, "table-layout.md", Buffer.from(source))
  await tree.getByRole("treeitem", { name: "table-layout.md", exact: true }).click()
  const preview = page.getByTestId("file-preview")
  await expect(preview.getByRole("heading", { name: "Table layout", exact: true })).toBeVisible()
  await expect.poll(() => preview.evaluate((element) =>
    element.getAnimations().every((animation) => animation.playState !== "running"),
  )).toBe(true)
  const tables = preview.getByTestId("markdown-table")
  for (const width of [1000, 640]) {
    await page.setViewportSize({ width, height: 760 })
    for (const label of ["GPU/API", "部署范围"]) {
      const cell = tables.first().getByRole("cell", { name: label, exact: true })
      // DOM containment alone misses a label squeezed into one-character lines.
      const dimensions = await cell.evaluate((element) => {
        const range = document.createRange()
        range.selectNodeContents(element)
        return { textHeight: range.getBoundingClientRect().height, lineHeight: parseFloat(getComputedStyle(element).lineHeight) }
      })
      expect(dimensions.textHeight).toBeGreaterThan(0)
      expect(dimensions.textHeight).toBeLessThanOrEqual(dimensions.lineHeight + 1)
    }
    const columns = await tables.first().locator("th").evaluateAll((cells) => cells.map((cell) => cell.getBoundingClientRect().width))
    expect(columns[1]).toBeGreaterThan(columns[0] * 2)
    await expect(tables.first().locator("code")).toHaveText(token)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
    const bounds = await tables.first().evaluate((element) => ({ client: element.clientWidth, scroll: element.scrollWidth }))
    expect(bounds.scroll).toBeLessThanOrEqual(bounds.client + 1)
    writeFileSync(info.outputPath(`table-layout-${width}.png`), await page.screenshot(), { mode: 0o600 })
  }
  const wide = tables.nth(1)
  await expect(wide.locator("tbody td").nth(0)).toHaveCSS("text-align", "left")
  await expect(wide.locator("tbody td").nth(1)).toHaveCSS("text-align", "center")
  await expect(wide.locator("tbody td").nth(2)).toHaveCSS("text-align", "right")
  expect(await wide.evaluate((element) => element.scrollWidth)).toBeGreaterThan(await wide.evaluate((element) => element.clientWidth))
  await wide.evaluate((element) => { element.scrollLeft = element.scrollWidth })
  expect(await wide.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
  const lastCell = await wide.getByRole("cell", { name: "End", exact: true }).boundingBox()
  const scrollport = await wide.boundingBox()
  expect(lastCell!.x + lastCell!.width).toBeLessThanOrEqual(scrollport!.x + scrollport!.width + 1)
})
