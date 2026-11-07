import { readFile, writeFile } from "node:fs/promises"
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

async function pinchDriver(page: Page, browserName: string, viewport: Locator, localPoint?: { x: number; y: number }) {
  const bounds = await viewport.boundingBox()
  if (!bounds) throw new Error("Reading surface has no visible bounds")
  const center = { x: bounds.x + (localPoint?.x ?? bounds.width / 2), y: bounds.y + (localPoint?.y ?? bounds.height / 2) }
  const cdp = browserName === "chromium" ? await page.context().newCDPSession(page) : undefined
  const pinch = async (from: number, to: number) => {
    const frames = [from, (from + to) / 2, to].map((halfWidth) => [
      { id: 1, x: center.x - halfWidth, y: center.y },
      { id: 2, x: center.x + halfWidth, y: center.y },
    ])
    if (cdp) {
      await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: frames[0] })
      for (const touchPoints of frames.slice(1)) {
        await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints })
      }
      await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] })
      return
    }
    // Playwright WebKit exposes a single touchscreen tap. Dispatch two-contact
    // DOM events here to check its handler and layout. XCTest drives real pinches.
    await viewport.evaluate((element, frames) => {
      const webkit = document as Document & {
        createTouch(view: Window, target: Element, id: number, pageX: number, pageY: number, screenX: number, screenY: number): Touch
        createTouchList(...touches: Touch[]): TouchList
      }
      const points = frames.map((frame) => frame.map(({ id, x, y }) => webkit.createTouch(window, element, id, x, y, x, y)))
      const send = (type: string, touches: Touch[], changedTouches: Touch[]) => {
        // This WebKit requires TouchList objects in the constructor.
        const list = (items: Touch[]) => webkit.createTouchList(...items) as unknown as Touch[]
        element.dispatchEvent(new TouchEvent(type, {
          bubbles: true, cancelable: true,
          touches: list(touches), changedTouches: list(changedTouches), targetTouches: list(touches),
        }))
      }
      send("touchstart", points[0].slice(0, 1), points[0].slice(0, 1))
      send("touchstart", points[0], points[0].slice(1))
      for (const frame of points.slice(1)) send("touchmove", frame, frame)
      send("touchend", points[2].slice(0, 1), points[2].slice(1))
      send("touchend", [], points[2].slice(0, 1))
    }, frames)
  }
  return { cdp, center, pinch }
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

test("EPUB pinching reflows authored text around the reading position", async ({ page, browserName }, info) => {
  await page.setViewportSize({ width: 320, height: 700 })
  const preview = await openOutput(page, "reading-test.epub", "/?mock=1&tick=0&file-preview=epub")
  const frame = preview.locator("iframe")
  const chapter = frame.contentFrame()
  const navigation = preview.getByRole("group", { name: "电子书导航" })
  const controls = preview.getByRole("group", { name: "文字操作" })
  const centerButton = navigation.getByRole("button", { name: "阅读工具" })
  const text = chapter.getByText(/^Passage 7\./)
  await expect(chapter.getByRole("heading", { name: "The arrival" })).toBeInViewport()
  await navigation.getByRole("button", { name: "下一页" }).tap()
  await expect(text).toBeInViewport()
  const frameBounds = await frame.boundingBox()
  const initialScale = await page.evaluate(() => visualViewport!.scale)
  const anchor = await text.evaluateHandle((paragraph) => {
    const doc = paragraph.ownerDocument
    const root = doc.documentElement
    const node = paragraph.firstChild as Text
    const range = doc.createRange()
    for (let index = 0; index < node.length; index++) {
      range.setStart(node, index)
      range.setEnd(node, index + 1)
      const rect = range.getBoundingClientRect()
      if (rect.width && Math.abs(rect.left - root.clientWidth / 2) < 12 && rect.top >= 0 && rect.bottom < root.clientHeight) return range
    }
    throw new Error("No visible line in the middle of the EPUB page")
  })
  const localPoint = await anchor.evaluate((range) => {
    const rect = range.getBoundingClientRect()
    return { x: (rect.left + rect.right) / 2, y: (rect.top + rect.bottom) / 2 }
  })
  const isAnchorVisible = () => anchor.evaluate((range) => {
    const { left, top, bottom } = range.getBoundingClientRect()
    const root = range.startContainer.ownerDocument!.documentElement
    return left >= 0 && left <= root.clientWidth && bottom > 0 && top < root.clientHeight
  })
  const { cdp, pinch } = await pinchDriver(page, browserName, navigation, localPoint)
  try {
    expect(await isAnchorVisible()).toBe(true)
    await expect(controls).toBeHidden()
    await pinch(40, 60)
    await expect.poll(() => fontSize(text)).toBe(27)
    expect(await isAnchorVisible()).toBe(true)
    expect(await frame.boundingBox()).toEqual(frameBounds)
    expect(await page.evaluate(() => visualViewport!.scale)).toBe(initialScale)
    await expect(controls).toBeHidden()
    await writeFile(info.outputPath("epub-font-pinched.png"), await page.screenshot(), { mode: 0o600 })

    await pinch(60, 40)
    await expect.poll(() => fontSize(text)).toBe(18)
    await centerButton.tap()
    await expect(controls).toBeVisible()
    await controls.getByRole("button", { name: "放大文字" }).tap()
    await expect.poll(() => fontSize(text)).toBe(20)
    await preview.getByRole("combobox", { name: "目录" }).selectOption({ label: "The garden" })
    const garden = chapter.getByText("From the window, they could see the old road.")
    await expect(garden).toBeInViewport()
    await expect.poll(() => fontSize(garden)).toBe(20)
    await expect(controls).toBeHidden()
    await centerButton.tap()
    await controls.getByRole("button", { name: "重置字号" }).tap()
    await expect.poll(() => fontSize(garden)).toBe(18)
    expect(await frame.boundingBox()).toEqual(frameBounds)
  } finally {
    await anchor.dispose()
    await cdp?.detach()
  }
})

test("Markdown pinch changes the reading font while the page stays fixed", async ({ page, browserName }, info) => {
  await page.setViewportSize({ width: 320, height: 700 })
  const preview = await openOutput(page, "inspection.md")
  const content = preview.getByTestId("phone-text-content")
  const paragraph = content.locator("p").first()
  await expect(paragraph).toBeVisible()
  const initialScale = await page.evaluate(() => visualViewport!.scale)
  const controls = preview.getByRole("group", { name: "文字操作" })
  const controlsBounds = await controls.boundingBox()
  const { cdp, pinch } = await pinchDriver(page, browserName, content)
  try {
    await pinch(40, 60)
    await expect.poll(() => fontSize(paragraph)).toBe(21)
    await expect(content).toHaveCSS("font-size", "21px")
    expect(await controls.boundingBox()).toEqual(controlsBounds)
    expect(await page.evaluate(() => visualViewport!.scale)).toBe(initialScale)
    await writeFile(info.outputPath("markdown-font-pinched.png"), await page.screenshot(), { mode: 0o600 })
    await pinch(60, 40)
    await expect.poll(() => fontSize(paragraph)).toBe(14)
    await expect(controls.getByRole("button", { name: "重置字号" })).toBeDisabled()
  } finally {
    await cdp?.detach()
  }
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
  await expect(page.getByRole("button", { name: "适应宽度", exact: true })).toHaveAttribute("aria-pressed", "true")
  await expect(fit).toHaveAttribute("aria-pressed", "false")
})

test("a long image opens at the top at readable width and scrolls to the bottom", async ({ page }, info) => {
  await page.setViewportSize({ width: 320, height: 700 })
  const name = "tall.png"
  const preview = await openOutput(page, name, "/?mock=1&tick=0&file-preview=tall-image")
  const viewport = preview.getByTestId("phone-image-viewport")
  const image = viewport.getByRole("img", { name })
  const fitWidth = preview.getByRole("button", { name: "适应宽度", exact: true })
  const fitWindow = preview.getByRole("button", { name: "适应窗口", exact: true })
  const measure = () => image.evaluate((element: HTMLImageElement) => {
    const viewport = element.closest<HTMLElement>('[data-testid="phone-image-viewport"]')!
    const bounds = element.getBoundingClientRect()
    return {
      width: bounds.width,
      height: bounds.height,
      imageTop: bounds.top - viewport.getBoundingClientRect().top,
      viewportWidth: viewport.clientWidth,
      viewportHeight: viewport.clientHeight,
      scrollWidth: viewport.scrollWidth,
      scrollHeight: viewport.scrollHeight,
      scrollTop: viewport.scrollTop,
      pageOverflow: document.documentElement.scrollWidth - window.innerWidth,
    }
  })
  await expect.poll(() => image.evaluate((element: HTMLImageElement) => [element.naturalWidth, element.naturalHeight])).toEqual([512, 4096])
  await expect(fitWidth).toBeEnabled()
  await expect(fitWidth).toHaveAttribute("aria-pressed", "true")
  await expect(fitWindow).toHaveAttribute("aria-pressed", "false")
  const initial = await measure()
  expect(Math.abs(initial.width - initial.viewportWidth)).toBeLessThanOrEqual(1)
  expect(initial.height / initial.width).toBeCloseTo(8, 2)
  expect(initial.imageTop).toBeCloseTo(0)
  expect(initial.scrollTop).toBe(0)
  expect(initial.scrollHeight).toBeGreaterThan(initial.viewportHeight * 2)
  expect(initial.scrollWidth).toBe(initial.viewportWidth)
  expect(initial.pageOverflow).toBeLessThanOrEqual(0)
  await writeFile(info.outputPath("long-image-fit-width.png"), await page.screenshot(), { mode: 0o600 })

  await viewport.press("End")
  await expect.poll(async () => {
    const current = await measure()
    return Math.abs(current.scrollHeight - current.viewportHeight - current.scrollTop)
  }).toBeLessThanOrEqual(1)
  await expect(fitWidth).toBeInViewport()
  await expect(fitWindow).toBeInViewport()

  await fitWindow.click()
  await expect(fitWindow).toHaveAttribute("aria-pressed", "true")
  await expect(fitWidth).toHaveAttribute("aria-pressed", "false")
  const fitted = await measure()
  expect(fitted.height).toBeLessThanOrEqual(fitted.viewportHeight)
  expect(fitted.width).toBeLessThan(fitted.viewportWidth / 2)
  expect(fitted.scrollHeight).toBe(fitted.viewportHeight)
  await writeFile(info.outputPath("long-image-fit-window.png"), await page.screenshot(), { mode: 0o600 })

  await fitWidth.click()
  await expect(fitWidth).toHaveAttribute("aria-pressed", "true")
  await expect(fitWindow).toHaveAttribute("aria-pressed", "false")
  const restored = await measure()
  expect(Math.abs(restored.width - restored.viewportWidth)).toBeLessThanOrEqual(1)
  expect(restored.scrollTop).toBe(0)
  expect(restored.imageTop).toBeCloseTo(0)
})

test("two fingers resize the image without zooming the phone page", async ({ page, browserName }, info) => {
  await page.setViewportSize({ width: 320, height: 700 })
  const preview = await openOutput(page, "tall.png", "/?mock=1&tick=0&file-preview=tall-image")
  const viewport = preview.getByTestId("phone-image-viewport")
  const image = viewport.getByRole("img", { name: "tall.png" })
  const fitWidth = preview.getByRole("button", { name: "适应宽度", exact: true })
  const fitWindow = preview.getByRole("button", { name: "适应窗口", exact: true })
  const zoom = preview.getByLabel("图片缩放比例", { exact: true })
  const imageWidth = () => image.evaluate((element) => element.getBoundingClientRect().width)
  const pageScale = () => page.evaluate(() => window.visualViewport!.scale)
  await expect(fitWidth).toBeEnabled()
  await expect(fitWidth).toHaveAttribute("aria-pressed", "true")
  await expect(viewport).toHaveCSS("touch-action", "pan-x pan-y")
  const initialWidth = await imageWidth()
  const initialScale = await pageScale()
  const { cdp, center, pinch } = await pinchDriver(page, browserName, viewport)

  try {
    const initialZoom = await zoom.textContent()
    await pinch(36, 72)
    await expect.poll(imageWidth).toBeCloseTo(initialWidth * 2, 0)
    await expect(zoom).not.toHaveText(initialZoom!)
    await expect(fitWidth).toHaveAttribute("aria-pressed", "false")
    await expect(fitWindow).toHaveAttribute("aria-pressed", "false")
    expect(await pageScale()).toBeCloseTo(initialScale, 5)
    await expect(fitWidth).toBeInViewport()
    await expect(fitWindow).toBeInViewport()
    await writeFile(info.outputPath("long-image-pinched.png"), await page.screenshot(), { mode: 0o600 })

    await pinch(72, 54)
    await expect.poll(imageWidth).toBeCloseTo(initialWidth * 1.5, 0)
    expect(await pageScale()).toBeCloseTo(initialScale, 5)

    if (cdp) {
      const top = await viewport.evaluate((element) => element.scrollTop)
      await viewport.evaluate((element, previousTop) => {
        const settled = () => {
          if (element.scrollTop <= previousTop + 10) return
          element.setAttribute("data-test-swipe-ended", "true")
          element.removeEventListener("scrollend", settled)
        }
        element.addEventListener("scrollend", settled)
      }, top)
      const finger = (y: number) => [{ id: 1, x: center.x, y }]
      await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: finger(center.y + 60) })
      for (const offset of [30, 0, -30, -60]) {
        await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: finger(center.y + offset) })
      }
      await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] })
      await expect.poll(() => viewport.evaluate((element) => element.scrollTop)).toBeGreaterThan(top + 10)
      await expect(viewport).toHaveAttribute("data-test-swipe-ended", "true")
      await expect.poll(imageWidth).toBeCloseTo(initialWidth * 1.5, 0)
    }

    await fitWidth.click()
    await expect(fitWidth).toHaveAttribute("aria-pressed", "true")
    await expect(fitWindow).toHaveAttribute("aria-pressed", "false")
    await expect.poll(imageWidth).toBeCloseTo(initialWidth, 0)
    expect(await viewport.evaluate((element) => element.scrollTop)).toBe(0)
  } finally {
    await cdp?.detach()
  }
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
