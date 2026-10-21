import { expect, test, type Page } from "@playwright/test"
import { readFileSync } from "node:fs"

import { composer, filterFiles, freshConversation, reloadOpenConversation, send, waitForIdle } from "./session"

const tinyPngB64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
const jpegBytes = readFileSync(new URL("./fixtures/vision-photo.jpg", import.meta.url))
const jpegB64 = jpegBytes.toString("base64")
const previewPngBytes = readFileSync(new URL("./fixtures/preview-landscape.png", import.meta.url))

async function dropFilesOnComposer(
  page: Page,
  files: { name: string; mime: string; b64: string }[],
  phase: "dragenter" | "drop",
) {
  const dataTransfer = await page.evaluateHandle(async (payload) => {
    const dt = new DataTransfer()
    for (const f of payload) {
      const binary = atob(f.b64)
      const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0))
      dt.items.add(new File([bytes], f.name, { type: f.mime }))
    }
    return dt
  }, files)
  const zone = page.getByTestId("composer-drop")
  await zone.dispatchEvent("dragenter", { dataTransfer })
  await zone.dispatchEvent("dragover", { dataTransfer })
  if (phase === "drop") await zone.dispatchEvent("drop", { dataTransfer })
  await dataTransfer.dispose()
}

test("pastes an image as vision input, not a workspace file", async ({ page }) => {
  await freshConversation(page)
  await composer(page).evaluate((el, b64) => {
    const binary = atob(b64)
    const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0))
    const file = new File([bytes], "clip.png", { type: "image/png" })
    const data = new DataTransfer()
    data.items.add(file)
    const event = new Event("paste", { bubbles: true, cancelable: true })
    Object.defineProperty(event, "clipboardData", { value: data })
    el.dispatchEvent(event)
  }, tinyPngB64)
  await expect(page.getByTestId("composer-images")).toBeVisible()
  await expect(page.getByAltText("clip.png")).toBeVisible()
  await send(page, "what is on this")
  await waitForIdle(page)

  const thumb = page.getByTestId("user-message").getByTestId("input-images").locator("img")
  await expect(thumb).toBeVisible()
  await expect(thumb).toHaveAttribute("src", /\/input-images\/img_/)

  const files = await filterFiles(page, "clip.png")
  await expect(files.getByText("clip.png")).toBeHidden()

  await reloadOpenConversation(page)
  await expect(
    page.getByTestId("user-message").getByTestId("input-images").locator("img"),
  ).toBeVisible()
})

test("drops files and generic binary photos without changing their vision bytes", async ({ page, request }, info) => {
  await freshConversation(page)
  const files = [
    {
      name: "notes.txt",
      mime: "text/plain",
      b64: Buffer.from("workspace material").toString("base64"),
    },
    { name: "shot.png", mime: "image/png", b64: tinyPngB64 },
    // Finder/WKWebView labels a local photo as a generic binary. It must
    // still be vision, not an uploads/ chip.
    {
      name: "photo.jpg",
      mime: "application/octet-stream",
      b64: jpegB64,
    },
  ]
  await dropFilesOnComposer(page, files, "dragenter")
  await expect(page.getByTestId("composer-drop-overlay")).toBeVisible()
  await expect(page.getByTestId("composer-drop-overlay")).toContainText(
    "Drop files to attach",
  )
  await dropFilesOnComposer(page, files, "drop")
  await expect(page.getByTestId("composer-drop-overlay")).toBeHidden()
  await expect(page.getByTestId("composer-attachments")).toContainText("notes.txt")
  await expect(page.getByAltText("shot.png")).toBeVisible()
  await expect(page.getByAltText("photo.jpg")).toBeVisible()
  await expect.poll(() => page.getByAltText("photo.jpg").evaluate((image: HTMLImageElement) =>
    [image.naturalWidth, image.naturalHeight])).toEqual([16, 12])
  const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  const accepted = page.waitForResponse((response) => response.request().method() === "POST"
    && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
  await send(page, "Use the attached material")
  const response = await accepted
  expect(response.status()).toBe(202)
  expect(response.request().postDataJSON().images).toEqual([
    { name: "shot.png", mime: "image/png", data: tinyPngB64 },
    { name: "photo.jpg", mime: "image/jpeg", data: jpegB64 },
  ])
  await waitForIdle(page)

  const thumbs = page.getByTestId("user-message").getByTestId("input-images").locator("img")
  await expect(thumbs).toHaveCount(2)
  await expect(thumbs.nth(0)).toHaveAttribute("src", /\/input-images\/img_/)
  await expect(thumbs.nth(1)).toHaveAttribute("src", /\/input-images\/img_/)
  const photo = page.getByTestId("user-message").getByAltText("photo.jpg")
  await expect.poll(() => photo.evaluate((image: HTMLImageElement) =>
    [image.naturalWidth, image.naturalHeight])).toEqual([16, 12])
  const imageURL = await photo.getAttribute("src")
  expect(imageURL).toBeTruthy()
  const stored = await request.get(imageURL!)
  expect(stored.status()).toBe(200)
  expect(stored.headers()["content-type"]).toBe("image/jpeg")
  expect(stored.headers()["x-content-type-options"]).toBe("nosniff")
  expect(await stored.body()).toEqual(jpegBytes)
  const { turn } = await response.json()
  const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
  expect(trace.events.find((event: { kind: string }) => event.kind === "user_message").images)
    .toContainEqual({ id: imageURL!.split("/").at(-1), name: "photo.jpg", mime: "image/jpeg" })
  await page.screenshot({ path: info.outputPath("stored-generic-photo.png"), animations: "disabled" })

  await reloadOpenConversation(page)
  await expect.poll(() => page.getByTestId("user-message").getByAltText("photo.jpg")
    .evaluate((image: HTMLImageElement) => [image.naturalWidth, image.naturalHeight])).toEqual([16, 12])

  await page.getByRole("tab", { name: "Files" }).click()
  const listed = await filterFiles(page, "notes.txt")
  await expect(listed.getByRole("treeitem", { name: "notes.txt" })).toBeVisible()
  await expect(listed.getByText("shot.png")).toBeHidden()
  await expect(listed.getByText("photo.jpg")).toBeHidden()
})

test("pastes a UTI photo as decodable vision input", async ({ page, request }) => {
  await freshConversation(page)
  await composer(page).evaluate((el, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
    const data = new DataTransfer()
    data.items.add(new File([bytes], "pasted.jpeg", { type: "public.jpeg" }))
    const event = new Event("paste", { bubbles: true, cancelable: true })
    Object.defineProperty(event, "clipboardData", { value: data })
    el.dispatchEvent(event)
  }, jpegB64)
  await expect.poll(() => page.getByAltText("pasted.jpeg").evaluate((image: HTMLImageElement) =>
    [image.naturalWidth, image.naturalHeight])).toEqual([16, 12])
  await expect(page.getByTestId("composer-attachments")).toHaveCount(0)
  await send(page, "Inspect the attached image")
  await waitForIdle(page)
  const photo = page.getByTestId("user-message").getByAltText("pasted.jpeg")
  await expect.poll(() => photo.evaluate((image: HTMLImageElement) =>
    [image.naturalWidth, image.naturalHeight])).toEqual([16, 12])
  const response = await request.get((await photo.getAttribute("src"))!)
  expect(response.headers()["content-type"]).toBe("image/jpeg")
  expect(await response.body()).toEqual(jpegBytes)
})

test("enlarges the selected sent image within the viewport and after reload", async ({ page, request }, info) => {
  await freshConversation(page)
  await dropFilesOnComposer(page, [
    { name: "landscape.png", mime: "image/png", b64: previewPngBytes.toString("base64") },
    { name: "detail.jpg", mime: "image/jpeg", b64: jpegB64 },
  ], "drop")
  await send(page, "Inspect the images")
  await waitForIdle(page)
  const trigger = page.getByRole("button", { name: "Enlarge landscape.png", exact: true })
  const thumbnail = await trigger.locator("img").boundingBox()
  expect(thumbnail).not.toBeNull()
  for (const viewport of [{ width: 1440, height: 900 }, { width: 720, height: 500 }, { width: 390, height: 650 }]) {
    // Open in the desktop layout, then resize the preview independently of
    // the three-pane shell's minimum usable width.
    await page.setViewportSize({ width: 1440, height: 900 })
    await trigger.click()
    await page.setViewportSize(viewport)
    const dialog = page.getByRole("dialog", { name: "landscape.png", exact: true })
    const image = dialog.getByAltText("landscape.png")
    await expect(image).toBeVisible()
    await dialog.evaluate((element) => Promise.all(element.getAnimations().map((animation) => animation.finished)))
    await expect.poll(() => image.evaluate((img: HTMLImageElement) =>
      [img.naturalWidth, img.naturalHeight])).toEqual([1200, 800])
    const box = (await image.boundingBox())!
    const frame = (await dialog.boundingBox())!
    expect(box.height).toBeGreaterThan(thumbnail!.height)
    expect(box.width / box.height).toBeCloseTo(1.5, 1)
    expect(frame.x).toBeGreaterThanOrEqual(0)
    expect(frame.y).toBeGreaterThanOrEqual(0)
    expect(frame.x + frame.width).toBeLessThanOrEqual(viewport.width)
    expect(frame.y + frame.height).toBeLessThanOrEqual(viewport.height)
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width)
    expect(box.y + box.height).toBeLessThanOrEqual(viewport.height)
    expect(await (await request.get((await image.getAttribute("src"))!)).body()).toEqual(previewPngBytes)
    await page.screenshot({ path: info.outputPath(`image-preview-${viewport.width}.png`), animations: "disabled" })
    await page.keyboard.press("Escape")
    await expect(dialog).toHaveCount(0)
    await expect(trigger).toBeFocused()
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  const second = page.getByRole("button", { name: "Enlarge detail.jpg", exact: true })
  await second.focus()
  await page.keyboard.press("Enter")
  const detail = page.getByRole("dialog", { name: "detail.jpg", exact: true })
  await expect(detail.getByAltText("detail.jpg")).toBeVisible()
  await expect(detail.getByAltText("landscape.png")).toHaveCount(0)
  await page.keyboard.press("Tab")
  await expect(detail.getByRole("button", { name: "Close image preview" })).toBeFocused()
  await page.keyboard.press("Shift+Tab")
  await expect(detail.getByRole("button", { name: "Close image preview" })).toBeFocused()
  await detail.getByRole("button", { name: "Close image preview" }).click()
  await expect(detail).toHaveCount(0)
  await trigger.click()
  await page.mouse.click(8, 8)
  await expect(page.getByRole("dialog")).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await reloadOpenConversation(page)
  await trigger.click()
  await expect(page.getByRole("dialog", { name: "landscape.png", exact: true }).getByAltText("landscape.png")).toBeVisible()
})

test("Escape closes a sent image preview without interrupting the active task", async ({ page }) => {
  await freshConversation(page)
  await dropFilesOnComposer(page, [
    { name: "working.png", mime: "image/png", b64: previewPngBytes.toString("base64") },
  ], "drop")
  const interruptions: string[] = []
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.endsWith("/interrupt")) interruptions.push(request.url())
  })
  await send(page, "Inspect the image")
  await page.getByRole("button", { name: "Enlarge working.png", exact: true }).click()
  await expect(page.getByRole("dialog", { name: "working.png", exact: true })).toBeVisible()
  await expect(page.getByTestId("status-badge")).toContainText("Working")
  await page.keyboard.press("Escape")
  await expect(page.getByRole("dialog")).toHaveCount(0)
  expect(interruptions).toEqual([])
  await waitForIdle(page)
  expect(interruptions).toEqual([])
})
