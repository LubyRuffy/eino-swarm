import { expect, test, type Page } from "@playwright/test"
import { readFileSync } from "node:fs"
import { composer, freshConversation, reloadOpenConversation, waitForDraftSaved, waitForIdle } from "./session"

const image = readFileSync(new URL("./fixtures/vision-photo.jpg", import.meta.url))
const attachment = Buffer.from([0, 255, 13, 42, 0, 1])
const selectedThread = (page: Page) => page.locator('[data-testid="thread-row"][aria-current="true"]')
const row = (page: Page, id: string) => page.locator(`[data-testid="thread-row"][data-id="${id}"]`)
const saved = waitForDraftSaved

async function attachImage(page: Page) {
  await composer(page).evaluate((el, b64) => {
    const data = new DataTransfer()
    data.items.add(new File([Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))], "draft-photo.jpg", { type: "image/jpeg", lastModified: 1234 }))
    const event = new Event("paste", { bubbles: true, cancelable: true })
    Object.defineProperty(event, "clipboardData", { value: data })
    el.dispatchEvent(event)
  }, image.toString("base64"))
}

test("autosaves quietly and shows a failed save only until retry succeeds", async ({ page, request }, info) => {
  await freshConversation(page)
  const id = (await selectedThread(page).getAttribute("data-id"))!
  const path = `/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`
  let release!: () => void
  const held = new Promise<void>((done) => { release = done })
  let entered!: () => void
  const saving = new Promise<void>((done) => { entered = done })
  let failed = false
  await page.route(`**${path}`, async (route) => {
    if (route.request().method() !== "PUT" || failed) { await route.continue(); return }
    entered()
    await held
    failed = true
    await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "fixture draft storage unavailable" }) })
  })
  await expect(page.getByTestId("composer-draft-status")).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Delete draft", exact: true })).toHaveCount(0)
  await composer(page).fill("Retain this input without persistent status chrome.")
  await saving
  try {
    await expect(page.getByTestId("composer-draft-status")).toHaveCount(0)
    await expect(page.getByRole("button", { name: "Delete draft", exact: true })).toHaveCount(0)
  } finally { release() }
  const status = page.getByTestId("composer-draft-status")
  await expect(status.getByRole("alert")).toContainText("Draft not saved")
  await expect(status).toContainText("fixture draft storage unavailable")
  await expect(composer(page)).toHaveValue("Retain this input without persistent status chrome.")
  await page.screenshot({ path: info.outputPath("save-failure-with-retry.png"), animations: "disabled" })
  await status.getByRole("button", { name: "Retry saving", exact: true }).click()
  await saved(page)
  expect((await (await request.get(path)).json()).draft.text).toBe("Retain this input without persistent status chrome.")
  await expect(status).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Delete draft", exact: true })).toHaveCount(0)
  await reloadOpenConversation(page)
  await expect(composer(page)).toHaveValue("Retain this input without persistent status chrome.")
  await saved(page)
  await expect(status).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Delete draft", exact: true })).toHaveCount(0)
  await page.screenshot({ path: info.outputPath("saved-draft-without-status-chrome.png"), animations: "disabled" })
})

test("shows a conflicting window's draft until an explicit version choice resolves it", async ({ page, context, request }, info) => {
  await freshConversation(page)
  const id = (await selectedThread(page).getAttribute("data-id"))!
  await composer(page).fill("Shared draft before either window changes it.")
  await saved(page)
  const other = await context.newPage()
  try {
    await other.goto("/")
    await other.getByTestId("dest-chats").click()
    await row(other, id).getByTestId("row-label").click()
    await expect(composer(other)).toHaveValue("Shared draft before either window changes it.")
    await composer(other).fill("A newer draft saved by the other window.")
    await saved(other)
    await composer(page).fill("The current window's unsaved input.")
    const status = page.getByTestId("composer-draft-status")
    await expect(status).toContainText("Another window changed this draft")
    await expect(composer(page)).toHaveValue("The current window's unsaved input.")
    await expect(composer(page)).toBeDisabled()
    await expect(status.getByRole("button", { name: "Save this input (replace the saved version)", exact: true })).toBeEnabled()
    await page.screenshot({ path: info.outputPath("conflicting-draft-with-version-choice.png"), animations: "disabled" })
    await status.getByRole("button", { name: "Use the saved version (replace this input)", exact: true }).click()
    await expect(composer(page)).toHaveValue("A newer draft saved by the other window.")
    await expect(composer(page)).toBeEnabled()
    await saved(page)
    expect((await (await request.get(`/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`)).json()).draft.text).toBe("A newer draft saved by the other window.")
    await expect(status).toHaveCount(0)
    await expect(page.getByRole("button", { name: "Delete draft", exact: true })).toHaveCount(0)
  } finally { await other.close() }
})

test("restores the complete draft and exact attachment bytes without crossing conversation scopes", async ({ page, request }, info) => {
  await freshConversation(page)
  const id = (await selectedThread(page).getAttribute("data-id"))!
  await composer(page).fill("earlier context")
  await composer(page).press("Enter")
  await waitForIdle(page)
  await page.getByTestId("user-message").getByText("earlier context", { exact: true }).selectText()
  await page.getByRole("menuitem", { name: "Add to chat" }).click()
  await expect(page.getByLabel("1 annotation")).toBeVisible()
  await composer(page).fill("  preserved 中文 draft  ")
  await page.getByTestId("file-input").setInputFiles({ name: "draft-bytes.bin", mimeType: "application/octet-stream", buffer: attachment })
  await attachImage(page)
  await saved(page)
  const path = `/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`
  const { draft } = await (await request.get(path)).json()
  expect(draft).toMatchObject({ text: "  preserved 中文 draft  ", quotes: [{ text: "earlier context" }], state: "editing" })
  expect(draft.files).toHaveLength(2)
  for (const file of draft.files) {
    const response = await request.get(`${path}/files/${file.id}`)
    expect(await response.body()).toEqual(file.kind === "image" ? image : attachment)
    expect(response.headers()["x-content-type-options"]).toBe("nosniff")
  }
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(composer(page)).toHaveValue("")
  await composer(page).fill("a different scope")
  await saved(page)
  await row(page, id).getByTestId("row-label").click()
  await expect(composer(page)).toHaveValue("  preserved 中文 draft  ")
  await expect(page.getByTestId("composer-attachments")).toContainText("draft-bytes.bin")
  await reloadOpenConversation(page)
  await expect(composer(page)).toHaveValue("  preserved 中文 draft  ")
  await expect(page.getByLabel("1 annotation")).toBeVisible()
  await expect(page.getByTestId("composer-attachments")).toContainText("draft-bytes.bin")
  const photo = page.getByAltText("draft-photo.jpg")
  await expect.poll(() => photo.evaluate((el: HTMLImageElement) => [el.naturalWidth, el.naturalHeight])).toEqual([16, 12])
  expect(await photo.evaluate(async (el: HTMLImageElement) => [...new Uint8Array(await (await fetch(el.src)).arrayBuffer())])).toEqual([...image])
  await page.getByLabel("1 annotation").hover()
  await expect(page.getByTestId("quote-snippet")).toContainText("earlier context")
  await page.screenshot({ path: info.outputPath("complete-restored-draft.png"), animations: "disabled" })
})

test("releases saved previews while navigating and reloads each scope's unchanged attachment bytes", async ({ page }, info) => {
  await page.addInitScript(() => {
    const released: string[] = []
    ;(window as Window & { __draftReleasedPreviews?: string[] }).__draftReleasedPreviews = released
    const revoke = URL.revokeObjectURL.bind(URL)
    URL.revokeObjectURL = (url) => { released.push(url); revoke(url) }
  })
  let attachmentReads = 0
  page.on("request", (req) => { if (/\/api\/composer\/drafts\/[^/]+\/files\//.test(req.url())) attachmentReads++ })
  await freshConversation(page)
  const scopes: { id: string; preview: string; text: string }[] = []
  for (let index = 0; index < 3; index++) {
    const id = (await selectedThread(page).getAttribute("data-id"))!
    const text = `Saved attachment scope ${index}`
    await composer(page).fill(text)
    await page.getByTestId("file-input").setInputFiles({ name: "retained.bin", mimeType: "application/octet-stream", buffer: attachment })
    await attachImage(page)
    await saved(page)
    const preview = (await page.getByAltText("draft-photo.jpg").getAttribute("src"))!
    scopes.push({ id, preview, text })
    await page.getByRole("button", { name: "New conversation", exact: true }).click()
    await expect(composer(page)).toBeEnabled()
    await expect(composer(page)).toHaveValue("")
    await expect.poll(() => page.evaluate((url) =>
      (window as Window & { __draftReleasedPreviews?: string[] }).__draftReleasedPreviews?.includes(url), preview)).toBe(true)
  }
  for (const scope of scopes.reverse()) {
    const before = attachmentReads
    await row(page, scope.id).getByTestId("row-label").click()
    await expect(composer(page)).toHaveValue(scope.text)
    const photo = page.getByAltText("draft-photo.jpg")
    await expect.poll(() => photo.evaluate((el: HTMLImageElement) => [el.naturalWidth, el.naturalHeight])).toEqual([16, 12])
    expect(await photo.getAttribute("src")).not.toBe(scope.preview)
    expect(await photo.evaluate(async (el: HTMLImageElement) => [...new Uint8Array(await (await fetch(el.src)).arrayBuffer())])).toEqual([...image])
    await expect(page.getByTestId("composer-attachments")).toContainText("retained.bin")
    expect(attachmentReads - before).toBe(2)
  }
  await page.screenshot({ path: info.outputPath("inactive-previews-released-and-restored.png"), animations: "disabled" })
})

for (const command of ["goal", "plan"] as const) {
  test(`/${command} keeps files, images and quotations until they can be sent as a complete message`, async ({ page, request }, info) => {
    await freshConversation(page)
    const id = (await selectedThread(page).getAttribute("data-id"))!
    const accepted = page.waitForResponse((response) => response.request().method() === "POST"
      && new URL(response.url()).pathname === `/api/threads/${id}/steer`)
    await composer(page).fill("context for the pending command")
    await composer(page).press("Enter")
    expect((await accepted).status()).toBe(202)
    await waitForIdle(page)
    await page.getByTestId("user-message").getByText("context for the pending command", { exact: true }).selectText()
    await page.getByRole("menuitem", { name: "Add to chat" }).click()
    await expect(page.getByLabel("1 annotation")).toBeVisible()
    const text = `/${command} Inspect the assigned operation.`
    await composer(page).fill(text)
    await page.getByTestId("file-input").setInputFiles({ name: "command-bytes.bin", mimeType: "application/octet-stream", buffer: attachment })
    await attachImage(page)
    await saved(page)
    const path = `/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`
    const before = (await (await request.get(path)).json()).draft
    expect(before.files).toHaveLength(2)
    expect(before.quotes).toHaveLength(1)
    let mutations = 0
    page.on("request", (req) => {
      const url = new URL(req.url()).pathname
      if ((url === `/api/threads/${id}` && req.method() === "PATCH")
        || (url === `/api/threads/${id}/steer` && req.method() === "POST")
        || (url === `/api/threads/${id}/files` && req.method() === "POST")
        || (url.startsWith("/api/composer/drafts/") && req.method() === "DELETE")) mutations++
    })
    await composer(page).press("Enter")
    await expect(page.getByRole("alert")).toContainText("Your draft was kept")
    await expect(composer(page)).toHaveValue(text)
    await expect(composer(page)).toBeEnabled()
    expect((await (await request.get(path)).json()).draft).toEqual(before)
    expect(mutations).toBe(0)
    await expect(page.getByLabel("1 annotation")).toBeVisible()
    for (const file of before.files) {
      expect(await (await request.get(`${path}/files/${file.id}`)).body()).toEqual(file.kind === "image" ? image : attachment)
    }
    await expect.poll(() => page.getByAltText("draft-photo.jpg").evaluate((el: HTMLImageElement) => [el.naturalWidth, el.naturalHeight])).toEqual([16, 12])
    await page.screenshot({ path: info.outputPath(`${command}-complete-draft-kept.png`), animations: "disabled" })

    // A normal message supports this same complete snapshot. Removing the
    // command is enough; the user does not have to reselect any original bytes.
    await composer(page).fill("Inspect the attached context.")
    await expect(page.getByRole("alert")).toHaveCount(0)
    const sent = page.waitForResponse((response) => response.request().method() === "POST"
      && new URL(response.url()).pathname === `/api/threads/${id}/steer`)
    await composer(page).press("Enter")
    expect((await sent).status()).toBe(202)
    await expect(composer(page)).toHaveValue("")
    await expect(page.getByTestId("composer-attachments")).toHaveCount(0)
    const turns = (await (await request.get(`/api/threads/${id}/turns`)).json()).turns
    expect(turns).toHaveLength(2)
  })
}

for (const failure of ["refused", "ack-lost"] as const) {
  test(`keeps the draft after a ${failure} send and never resends it on reload`, async ({ page, request }, info) => {
    await freshConversation(page)
    const id = (await selectedThread(page).getAttribute("data-id"))!
    const url = `**/api/threads/${id}/steer`
    let sends = 0
    await page.route(url, async (route) => {
      sends++
      if (failure === "ack-lost") {
        const accepted = await route.fetch()
        expect(accepted.status()).toBe(202)
        await route.abort("failed")
      } else await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "fixture refusal" }) })
    })
    await composer(page).fill("keep this request until acknowledged")
    await page.getByTestId("file-input").setInputFiles({ name: "kept.bin", mimeType: "application/octet-stream", buffer: attachment })
    await attachImage(page)
    await saved(page)
    await composer(page).press("Enter")
    await expect(page.getByTestId("composer-draft-status")).toContainText("send status is unknown")
    await expect(composer(page)).toHaveValue("keep this request until acknowledged")
    await expect(page.getByTestId("composer-attachments")).toContainText("kept.bin")
    await expect(page.getByTestId("composer-images").getByAltText("draft-photo.jpg")).toBeVisible()
    if (failure === "ack-lost") await waitForIdle(page)
    expect(sends).toBe(1)
    await reloadOpenConversation(page)
    await expect(page.getByTestId("composer-draft-status")).toContainText("send status is unknown")
    await expect(composer(page)).toHaveValue("keep this request until acknowledged")
    await expect(composer(page)).toBeDisabled()
    await expect(page.getByTestId("composer-attachments")).toContainText("kept.bin")
    await expect.poll(() => page.getByAltText("draft-photo.jpg").last().evaluate((el: HTMLImageElement) => [el.naturalWidth, el.naturalHeight])).toEqual([16, 12])
    const { turns } = await (await request.get(`/api/threads/${id}/turns`)).json()
    expect(turns).toHaveLength(failure === "ack-lost" ? 1 : 0)
    expect(sends).toBe(1)
    await page.screenshot({ path: info.outputPath(`${failure}-restored.png`), animations: "disabled" })
  })
}

test("an upload followed by navigation sends to its captured thread", async ({ page, request }, info) => {
  await freshConversation(page)
  const original = (await selectedThread(page).getAttribute("data-id"))!
  const { thread: other } = await (await request.post("/api/threads", { data: { title: "Other draft scope" } })).json()
  await expect(row(page, other.id)).toBeVisible()
  let release!: () => void
  const blocked = new Promise<void>((done) => { release = done })
  let entered!: () => void
  const uploadStarted = new Promise<void>((done) => { entered = done })
  await page.route(`**/api/threads/${original}/files`, async (route) => {
    if (route.request().method() !== "POST") { await route.continue(); return }
    entered()
    await blocked
    await route.continue()
  })
  await composer(page).fill("keep the original send target")
  await page.getByTestId("file-input").setInputFiles({ name: "original.bin", mimeType: "application/octet-stream", buffer: attachment })
  await saved(page)
  await composer(page).press("Enter")
  await uploadStarted
  await row(page, other.id).getByTestId("row-label").click()
  await expect(selectedThread(page)).toHaveAttribute("data-id", other.id)
  release()
  await expect.poll(async () => (await (await request.get(`/api/threads/${original}/turns`)).json()).turns.length).toBe(1)
  expect((await (await request.get(`/api/threads/${other.id}/turns`)).json()).turns).toHaveLength(0)
  await expect(composer(page)).toHaveValue("")
  await row(page, original).getByTestId("row-label").click()
  await expect(page.getByTestId("user-message")).toContainText("keep the original send target")
  await expect(page.getByTestId("user-message")).toContainText("original.bin")
  await page.screenshot({ path: info.outputPath("captured-upload-target.png"), animations: "disabled" })
})

test("keeps the original new draft when first-send migration is interrupted", async ({ page, request }) => {
  await page.goto("/")
  await expect(composer(page)).toBeEnabled()
  await composer(page).fill("first draft before a conversation exists")
  await page.getByTestId("file-input").setInputFiles({ name: "first.bin", mimeType: "application/octet-stream", buffer: attachment })
  await saved(page)
  let migration!: () => void
  const migrating = new Promise<void>((done) => { migration = done })
  let release!: () => void
  const blocked = new Promise<void>((done) => { release = done })
  let handled!: () => void
  const handledMigration = new Promise<void>((done) => { handled = done })
  await page.route("**/api/composer/drafts/thread%3A*", async (route) => {
    if (route.request().method() !== "PUT") { await route.continue(); return }
    migration()
    await blocked
    await route.abort()
    handled()
  })
  await composer(page).press("Enter")
  await migrating
  await expect(selectedThread(page)).toHaveCount(0)
  const original = (await (await request.get("/api/composer/drafts/new%3Ahome")).json()).draft
  expect(original.text).toBe("first draft before a conversation exists")
  expect(original.files).toHaveLength(1)
  release()
  await handledMigration
  await page.unroute("**/api/composer/drafts/thread%3A*")
  await page.reload()
  await expect(composer(page)).toHaveValue("first draft before a conversation exists")
  await expect(page.getByTestId("composer-attachments")).toContainText("first.bin")
  await expect(selectedThread(page)).toHaveCount(0)
})

async function queuedAfterStop(page: Page) {
  await freshConversation(page)
  const id = (await selectedThread(page).getAttribute("data-id"))!
  await composer(page).fill("start a bounded offline task")
  await composer(page).press("Enter")
  await expect(page.getByTestId("status-badge")).toContainText("Working")
  await composer(page).fill("queued draft copy")
  await composer(page).press("Enter")
  await expect(page.getByTestId("followup-queue")).toContainText("queued draft copy")
  await page.getByRole("button", { name: "Stop", exact: true }).click()
  await waitForIdle(page)
  await saved(page)
  return id
}

test("a failed draft save leaves the queued message intact", async ({ page, request }) => {
  const id = await queuedAfterStop(page)
  let deletes = 0
  await page.route(`**/api/threads/${id}/followups/*`, async (route) => {
    if (route.request().method() === "DELETE") deletes++
    await route.continue()
  })
  await page.route(`**/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`, async (route) => {
    if (route.request().method() === "PUT") await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "fixture draft save unavailable" }) })
    else await route.continue()
  })
  await page.getByRole("button", { name: "Edit queued message: queued draft copy" }).click()
  await expect(page.getByText("fixture draft save unavailable").first()).toBeVisible()
  await expect(page.getByTestId("followup-queue")).toContainText("queued draft copy")
  expect((await (await request.get(`/api/threads/${id}/followups`)).json()).followups).toHaveLength(1)
  expect(deletes).toBe(0)
})

test("a failed queue removal keeps both copies visible and forbids automatic resending", async ({ page, request }, info) => {
  const id = await queuedAfterStop(page)
  await page.route(`**/api/threads/${id}/followups/*`, async (route) => {
    if (route.request().method() === "DELETE") await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "fixture queue removal unavailable" }) })
    else await route.continue()
  })
  await page.getByRole("button", { name: "Edit queued message: queued draft copy" }).click()
  await expect(composer(page)).toHaveValue("queued draft copy")
  await expect(page.getByTestId("followup-queue")).toContainText("queued draft copy")
  await expect(page.getByTestId("composer-draft-status")).toContainText("send status is unknown")
  await expect(composer(page)).toBeDisabled()
  const { draft } = await (await request.get(`/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`)).json()
  expect(draft).toMatchObject({ text: "queued draft copy", state: "uncertain" })
  expect((await (await request.get(`/api/threads/${id}/followups`)).json()).followups).toHaveLength(1)
  await page.screenshot({ path: info.outputPath("queue-and-draft-retained.png"), animations: "disabled" })
})

// A scope change may take a real HTTP round trip. Input must not remain bound
// to the previous conversation while the New action is still in flight.
test("creating a conversation blocks edits in the previous draft scope", async ({ page, request }) => {
  await freshConversation(page)
  const previous = (await selectedThread(page).getAttribute("data-id"))!
  await expect(composer(page)).toBeEnabled()
  await composer(page).fill("keep the previous draft")
  await saved(page)
  let release!: () => void
  const held = new Promise<void>((done) => { release = done })
  let started!: () => void
  const requested = new Promise<void>((done) => { started = done })
  await page.route("**/api/threads", async (route) => {
    if (route.request().method() !== "POST") { await route.continue(); return }
    started()
    await held
    await route.continue()
  })
  try {
    await page.getByRole("button", { name: "New conversation", exact: true }).click()
    await requested
    await expect(composer(page)).toBeDisabled({ timeout: 1000 })
  } finally { release() }
  await expect(selectedThread(page)).not.toHaveAttribute("data-id", previous)
  await expect(composer(page)).toBeEnabled()
  await expect(composer(page)).toHaveValue("")
  await composer(page).fill("the new scope")
  await saved(page)
  const original = await (await request.get(`/api/composer/drafts/${encodeURIComponent(`thread:${previous}`)}`)).json()
  expect(original.draft.text).toBe("keep the previous draft")
})
