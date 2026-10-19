import { expect, test, type Page } from "@playwright/test"
import { readFileSync } from "node:fs"

import { composer, filterFiles, freshConversation, reloadOpenConversation, send, waitForIdle } from "./session"

const tinyPngB64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
const jpegBytes = readFileSync(new URL("./fixtures/vision-photo.jpg", import.meta.url))
const jpegB64 = jpegBytes.toString("base64")

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
