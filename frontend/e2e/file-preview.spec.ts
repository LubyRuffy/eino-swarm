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

test("previews a decoded workspace image and downloads its unchanged bytes", async ({ page }, info) => {
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
  const downloaded = page.waitForEvent("download")
  await preview.locator("a[download]").click()
  const download = await downloaded
  expect(download.suggestedFilename()).toBe("landscape.png")
  const downloadedPath = await download.path()
  expect(downloadedPath).toBeTruthy()
  expect(readFileSync(downloadedPath!)).toEqual(imageBytes)
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
