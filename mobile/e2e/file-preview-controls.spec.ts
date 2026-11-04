import { readFile } from "node:fs/promises"
import { expect, test, type Download, type Locator, type Page } from "@playwright/test"
import { epubPath, largeLandscapePath, mockOutputFiles } from "../src/lib/mock-files"

async function openOutput(page: Page, name: string, url = "/?mock=1&tick=0") {
  await page.goto(url)
  await page.getByRole("button", { name: "产出物", exact: true }).click()
  await page.getByRole("button", { name: "outputs", exact: true }).click()
  await page.getByRole("button", { name, exact: true }).click()
  return page.getByTestId("phone-file-preview")
}

async function downloadBytes(page: Page) {
  const downloading = page.waitForEvent("download")
  await page.getByRole("link", { name: "下载", exact: true }).click()
  const download = await downloading
  const path = await download.path()
  expect(path).not.toBeNull()
  return { name: download.suggestedFilename(), bytes: await readFile(path!) }
}

async function fontSize(element: Locator) {
  return element.evaluate((node) => Number.parseFloat(getComputedStyle(node).fontSize))
}

test("EPUB gives the screen to reading and opens navigation only on a center tap", async ({ page }, info) => {
  await page.setViewportSize({ width: 320, height: 700 })
  const scenario = "?mock=1&tick=0&file-preview=epub"
  const name = "reading-test.epub"
  const preview = await openOutput(page, name, "/" + scenario)
  const frame = preview.locator("iframe")
  const chapter = frame.contentFrame()
  const arrival = chapter.getByRole("heading", { name: "The arrival" })
  const garden = chapter.getByRole("heading", { name: "The garden" })
  const firstPassage = chapter.getByText(/^Passage 1\./)
  const finalPassage = chapter.getByText(/^Passage 35\./)
  const contents = preview.getByRole("combobox", { name: "目录" })
  const download = preview.getByRole("link", { name: "下载", exact: true })
  const tap = async (fraction: number) => {
    await expect(frame).toBeVisible()
    const box = await frame.boundingBox()
    if (!box) throw new Error("EPUB reading surface has no visible bounds")
    await page.touchscreen.tap(box.x + box.width * fraction, box.y + box.height / 2)
  }

  await expect(arrival).toBeInViewport()
  await expect(firstPassage).toBeInViewport()
  await expect(finalPassage).not.toBeInViewport()
  await expect(page.getByRole("heading", { name, exact: true })).toHaveCount(1)
  await expect(preview.getByText(epubPath, { exact: true })).toHaveCount(0)
  await expect(preview.getByText(name, { exact: true })).toHaveCount(0)
  await expect(contents).toBeHidden()
  await expect(download).toBeHidden()
  await expect(preview.getByRole("button", { name: /上一个|下一个/ })).toHaveCount(0)
  const readingBounds = await frame.boundingBox()
  const previewBounds = await preview.boundingBox()
  expect(readingBounds!.height).toBeGreaterThan(previewBounds!.height * 0.9)
  await page.screenshot({ path: info.outputPath("epub-reading.png") })

  // The first turn stays within the long first chapter. It must move the
  // visible text, and a left tap must return to the same first page.
  await tap(0.12)
  await expect(arrival).toBeInViewport()
  await tap(0.88)
  await expect(arrival).not.toBeInViewport()
  await expect(frame).toHaveAttribute("title", "The arrival")
  await expect(chapter.getByText(/^Passage 7\./)).toBeInViewport()
  await expect(finalPassage).not.toBeInViewport()
  await expect(download).toBeHidden()
  await page.screenshot({ path: info.outputPath("epub-page-turned.png") })
  await tap(0.12)
  await expect(arrival).toBeInViewport()

  await tap(0.5)
  await expect(contents).toBeVisible()
  await expect(download).toBeVisible()
  expect(await frame.boundingBox()).toEqual(readingBounds)
  await expect(arrival).toBeInViewport()
  await page.screenshot({ path: info.outputPath("epub-controls.png") })
  const downloaded = await downloadBytes(page)
  expect(downloaded.name).toBe(name)
  expect(downloaded.bytes).toEqual(Buffer.from(mockOutputFiles(scenario)[epubPath].bytes))
  await tap(0.5)
  await expect(contents).toBeHidden()
  await expect(download).toBeHidden()
  await expect(arrival).toBeInViewport()

  await tap(0.5)
  await contents.selectOption({ label: "The garden" })
  await expect(garden).toBeInViewport()
  // A left tap from the first page of the next chapter returns to the
  // previous chapter's last page. A right tap crosses that boundary again.
  await tap(0.12)
  await expect(finalPassage).toBeInViewport()
  await tap(0.88)
  await expect(garden).toBeInViewport()

  await page.getByRole("button", { name: "返回列表", exact: true }).click()
  await page.getByRole("button", { name, exact: true }).click()
  await expect(arrival).toBeInViewport()
  await expect(download).toBeHidden()
})

test("document text, headings, tables and code enlarge together while controls remain reachable", async ({ page }, info) => {
  await page.setViewportSize({ width: 320, height: 700 })
  await openOutput(page, "inspection.md")
  const content = page.getByTestId("phone-text-content")
  await expect(content.getByRole("heading", { name: "Inspection document" })).toBeVisible()
  const samples = [content.locator("p").first(), content.locator("h1"), content.locator("td").first(), content.locator("pre code")]
  const initialSizes = await Promise.all(samples.map(fontSize))
  const larger = page.getByRole("button", { name: "放大文字", exact: true })
  const smaller = page.getByRole("button", { name: "缩小文字", exact: true })
  const reset = page.getByRole("button", { name: "重置字号", exact: true })
  await larger.click()
  await expect(content).toHaveCSS("font-size", "16px")
  for (const [index, sample] of samples.entries()) {
    expect((await fontSize(sample)) / initialSizes[index]).toBeCloseTo(16 / 14, 2)
  }

  for (let n = 0; n < 6; n++) await larger.click()
  await expect(content).toHaveCSS("font-size", "28px")
  await expect(larger).toBeDisabled()
  // Enlarged glyphs need their own line space; fixed small-text leading clips
  // table rows and makes multiline code overlap on the phone.
  for (const sample of samples) {
    const metrics = await sample.evaluate((element) => {
      const style = getComputedStyle(element)
      return { font: Number.parseFloat(style.fontSize), line: Number.parseFloat(style.lineHeight) }
    })
    expect(metrics.line).toBeGreaterThanOrEqual(metrics.font)
  }
  // Scroll the actual text scrollport, which may wrap the rendered document.
  await content.evaluate((element) => {
    let scrollport: Element | null = element
    while (scrollport && !/(auto|scroll)/.test(getComputedStyle(scrollport).overflowY)) scrollport = scrollport.parentElement
    if (!scrollport) throw new Error("text preview has no scrollport")
    scrollport.scrollTop = scrollport.scrollHeight
  })
  // Consecutive fixture lines form one Markdown paragraph. Check its final
  // text and the actual scroll end rather than assuming a separate DOM node.
  await expect(content.locator("p").last()).toContainText("Paragraph 40:")
  await expect(content.locator("p").last()).toBeInViewport()
  expect(await content.evaluate((element) => element.scrollTop)).toBeGreaterThan(0)
  expect(await content.evaluate((element) => Math.abs(element.scrollHeight - element.clientHeight - element.scrollTop))).toBeLessThanOrEqual(1)
  await expect(smaller).toBeInViewport()
  await expect(reset).toBeInViewport()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320)
  await page.screenshot({ path: info.outputPath("document-enlarged-scrolled.png") })

  await reset.click()
  await expect(content).toHaveCSS("font-size", "14px")
  await smaller.click()
  await expect(content).toHaveCSS("font-size", "12px")
  await expect(smaller).toBeDisabled()
  await page.getByRole("button", { name: "返回列表", exact: true }).click()
  await page.getByRole("button", { name: "inspection.md", exact: true }).click()
  await expect(page.getByTestId("phone-text-content")).toHaveCSS("font-size", "14px")
})

test("source text zoom preserves source and the original download", async ({ page }) => {
  await openOutput(page, "sample.go")
  const content = page.getByTestId("phone-text-content")
  const source = 'package main\n\nconst value = "hello"\n'
  await expect(content.locator("pre")).toHaveText(source)
  const initialSize = await fontSize(content.locator("pre"))
  await page.getByRole("button", { name: "放大文字", exact: true }).click()
  expect((await fontSize(content.locator("pre"))) / initialSize).toBeCloseTo(16 / 14, 2)
  await expect(content.locator("pre")).toHaveText(source)
  const downloaded = await downloadBytes(page)
  expect(downloaded.name).toBe("sample.go")
  expect(downloaded.bytes).toEqual(Buffer.from(source))
})

test("decoded image zoom keeps every edge scrollable and downloading preserves image bytes", async ({ page }, info) => {
  await openOutput(page, "landscape.png")
  const viewport = page.getByTestId("phone-image-viewport")
  const image = viewport.getByRole("img", { name: "landscape.png" })
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => [element.naturalWidth, element.naturalHeight])).toEqual([1200, 800])
  expect(await image.evaluate(async (element: HTMLImageElement) => (await fetch(element.src)).headers.get("content-type"))).toBe("image/png")
  const fitted = await image.boundingBox()
  const bounds = await viewport.boundingBox()
  expect(fitted).not.toBeNull()
  expect(bounds).not.toBeNull()
  expect(fitted!.width).toBeLessThanOrEqual(bounds!.width)
  expect(fitted!.height).toBeLessThanOrEqual(bounds!.height)
  expect(fitted!.width / fitted!.height).toBeCloseTo(1.5, 2)

  await page.getByRole("button", { name: "原始大小（100%）", exact: true }).click()
  await expect(image).toHaveCSS("width", "1200px")
  await expect(page.getByLabel("图片缩放比例", { exact: true })).toHaveText("100%")
  const larger = page.getByRole("button", { name: "放大图片", exact: true })
  await larger.click()
  await expect(image).toHaveCSS("width", "1500px")
  const overflow = await viewport.evaluate((element) => {
    element.scrollLeft = element.scrollWidth
    element.scrollTop = element.scrollHeight
    return { width: element.clientWidth, height: element.clientHeight, scrollWidth: element.scrollWidth, scrollHeight: element.scrollHeight, left: element.scrollLeft, top: element.scrollTop }
  })
  expect(overflow.scrollWidth).toBeGreaterThan(overflow.width)
  expect(overflow.scrollHeight).toBeGreaterThan(overflow.height)
  expect(overflow.left).toBeGreaterThan(0)
  expect(overflow.top).toBeGreaterThan(0)
  await expect(larger).toBeInViewport()
  await page.screenshot({ path: info.outputPath("image-enlarged-scrolled.png") })

  const fit = page.getByRole("button", { name: "适应窗口", exact: true })
  await fit.click()
  await expect(fit).toHaveAttribute("aria-pressed", "true")
  await page.setViewportSize({ width: 320, height: 700 })
  await expect.poll(async () => {
    const currentImage = await image.boundingBox()
    const currentViewport = await viewport.boundingBox()
    return !!currentImage && !!currentViewport && currentImage.width <= currentViewport.width && currentImage.height <= currentViewport.height
  }).toBe(true)
  expect(await viewport.evaluate((element) => element.scrollLeft)).toBe(0)
  const downloaded = await downloadBytes(page)
  expect(downloaded.name).toBe("landscape.png")
  expect(downloaded.bytes).toEqual(await readFile(new URL("../../frontend/e2e/fixtures/preview-landscape.png", import.meta.url)))
  await page.getByRole("button", { name: "返回列表", exact: true }).click()
  await page.getByRole("button", { name: "landscape.png", exact: true }).click()
  await expect(fit).toBeEnabled()
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => [element.naturalWidth, element.naturalHeight])).toEqual([1200, 800])
  await expect(page.getByRole("button", { name: "适应窗口", exact: true })).toHaveAttribute("aria-pressed", "true")
})

test("large images show byte progress and let users request the original download early", async ({ page }, info) => {
  const scenario = "?mock=1&tick=0&file-preview=large-image&file-preview-delay=2500"
  const name = "large-landscape.png"
  const fixture = mockOutputFiles(scenario)[largeLandscapePath].bytes
  expect(fixture.length).toBeGreaterThan(8 * 1024 * 1024)
  const preview = await openOutput(page, name, "/" + scenario)
  const progress = preview.getByRole("progressbar", { name: "文件加载进度" })
  await expect(progress).toBeVisible()
  await expect(progress).toHaveAttribute("max", String(fixture.length))
  await expect(progress).toHaveAttribute("value", String(16 * 36 * 1024))
  const image = preview.getByRole("img", { name })
  await expect(image).toHaveCount(0)
  const download = preview.getByRole("button", { name: "下载", exact: true })
  await expect(download).toBeVisible()
  const downloads: Download[] = []
  page.on("download", (event) => downloads.push(event))
  await download.click()
  await expect(preview.getByText("接收完成后将自动下载", { exact: true })).toBeVisible()
  await expect(progress).toHaveAttribute("value", String(128 * 36 * 1024))
  expect(downloads).toHaveLength(0)
  await expect(preview.getByText("49% · 4.5 MB / 9.0 MB", { exact: true })).toBeVisible()
  await page.screenshot({ path: info.outputPath("large-image-loading.png") })
  await expect(progress).toHaveCount(0)
  await expect(image).toBeVisible()
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => [element.naturalWidth, element.naturalHeight])).toEqual([1200, 800])
  expect(await image.evaluate(async (element: HTMLImageElement) => (await fetch(element.src)).headers.get("content-type"))).toBe("image/png")
  await expect(preview.getByRole("alert")).toHaveCount(0)
  await expect(preview.getByRole("link", { name: "下载", exact: true })).toBeVisible()
  await expect.poll(() => downloads.length).toBe(1)
  const received = downloads[0]
  expect(received.suggestedFilename()).toBe(name)
  const path = await received.path()
  expect(path).not.toBeNull()
  expect((await readFile(path!)).equals(fixture)).toBe(true)
  expect(downloads).toHaveLength(1)
})
