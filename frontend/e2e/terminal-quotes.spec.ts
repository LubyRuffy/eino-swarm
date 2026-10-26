import { expect, test, type Locator, type Page } from "@playwright/test"

import { composer, freshConversation, reloadOpenConversation, waitForDraftSaved, waitForIdle } from "./session"

const selectedThread = (page: Page) => page.locator('[data-testid="thread-row"][aria-current="true"]')
const addToChat = (page: Page) => page.getByRole("menuitem", { name: "Add to chat" })

async function openTerminal(page: Page) {
  const ready = page.waitForEvent("websocket").then(async (socket) => {
    const frame = await socket.waitForEvent("framereceived", {
      predicate: ({ payload }) => typeof payload === "string" && payload.includes('"type":"ready"'),
    })
    return JSON.parse(frame.payload as string) as { cwd: string; shell: string }
  })
  await page.getByRole("banner").getByRole("button", { name: "Open terminal" }).click()
  const source = await ready
  const active = page.locator('[data-testid^="terminal-session-"]:visible')
  await expect(active).toHaveCount(1)
  const testId = (await active.getAttribute("data-testid"))!
  return { terminal: page.getByTestId(testId), id: testId.slice("terminal-session-".length), ...source }
}

async function printOutput(page: Page, terminal: Locator, marker: string) {
  await terminal.locator(".xterm-helper-textarea").focus()
  // Clear the echoed command and previous prompts, leaving one known output
  // row to select with real pointer input. No terminal implementation API is used.
  await page.keyboard.type(`printf '\\033[2J\\033[H'; printf '%s\\n' '${marker}'`)
  await page.keyboard.press("Enter")
  const output = terminal.locator(".xterm-rows > div").filter({ hasText: marker })
  await expect(output).toHaveCount(1)
  await expect(output).toHaveText(marker)
  return output
}

async function selectOutput(page: Page, output: Locator) {
  const rect = await output.boundingBox()
  expect(rect).not.toBeNull()
  await page.mouse.move(rect!.x + 1, rect!.y + rect!.height / 2)
  await page.mouse.down()
  await page.mouse.move(rect!.x + rect!.width - 1, rect!.y + rect!.height / 2, { steps: 10 })
  await page.mouse.up()
  await expect(addToChat(page)).toBeVisible()
}

test("selected terminal output keeps its source through draft reload and send", async ({ page, request }, info) => {
  await freshConversation(page)
  const threadId = (await selectedThread(page).getAttribute("data-id"))!
  const question = "Explain the selected terminal result."
  await composer(page).fill(question)
  const source = await openTerminal(page)
  const marker = "terminal-context-ready"
  const output = await printOutput(page, source.terminal, marker)
  await selectOutput(page, output)
  await addToChat(page).click()

  await expect(composer(page)).toBeFocused()
  await expect(composer(page)).toHaveValue(question)
  await page.getByLabel("1 annotation").hover()
  const snippet = page.getByTestId("quote-snippet")
  await expect(snippet).toContainText("Source: integrated terminal")
  await expect(snippet).toContainText(`Session: ${JSON.stringify(source.id)}`)
  await expect(snippet).toContainText(`Conversation: ${JSON.stringify(threadId)}`)
  await expect(snippet).toContainText(`Initial directory: ${JSON.stringify(source.cwd)}`)
  await expect(snippet).toContainText(`Shell: ${JSON.stringify(source.shell)}`)
  await expect(snippet).toContainText(marker)
  await page.screenshot({ path: info.outputPath("terminal-quote-draft.png"), animations: "disabled" })
  await waitForDraftSaved(page)
  const { draft } = await (await request.get(`/api/composer/drafts/${encodeURIComponent(`thread:${threadId}`)}`)).json()
  expect(draft.quotes).toHaveLength(1)
  const quote = draft.quotes[0].text as string
  expect(quote).toContain(marker)

  await reloadOpenConversation(page)
  await expect(composer(page)).toHaveValue(question)
  await expect(page.getByLabel("1 annotation")).toBeVisible()
  const sent = page.waitForRequest((req) => req.method() === "POST" && new URL(req.url()).pathname === `/api/threads/${threadId}/steer`)
  await composer(page).press("Enter")
  const payload = (await sent).postDataJSON() as { text: string }
  expect(payload.text).toBe(`<selected_text>\n${quote}\n</selected_text>\n\n<user_request>\n${question}\n</user_request>`)
  await waitForIdle(page)
  const message = page.getByTestId("user-message").last()
  await expect(message).toContainText(marker)
  await expect(message).toContainText("Source: integrated terminal")
  await expect(message).toContainText(question)
  await expect(message).not.toContainText("<selected_text>")
  await expect(message).not.toContainText("<user_request>")
  await expect(page.getByLabel("1 annotation")).toHaveCount(0)
  await page.screenshot({ path: info.outputPath("terminal-quote-sent.png"), animations: "disabled" })
})

test("terminal selection clears on tab and panel changes and retains its original conversation", async ({ page }) => {
  await freshConversation(page)
  const originalThread = (await selectedThread(page).getAttribute("data-id"))!
  const source = await openTerminal(page)
  const marker = "terminal-original-conversation"
  await selectOutput(page, await printOutput(page, source.terminal, marker))

  const panel = page.getByTestId("terminal-panel")
  await panel.getByRole("button", { name: "New terminal", exact: true }).click()
  await expect(source.terminal).toBeHidden()
  await expect(addToChat(page)).toHaveCount(0)
  const tabLabel = source.cwd.replace(/[\\/]+$/, "").split(/[\\/]/).pop()!
  await panel.getByRole("button", { name: tabLabel, exact: true }).first().click()
  await expect(source.terminal).toBeVisible()
  await expect(addToChat(page)).toHaveCount(0)
  await selectOutput(page, source.terminal.locator(".xterm-rows > div").filter({ hasText: marker }))
  await panel.getByRole("button", { name: "Close terminal", exact: true }).click()
  await expect(panel).toBeHidden()
  await expect(addToChat(page)).toHaveCount(0)
  await page.keyboard.press("ControlOrMeta+j")
  await expect(panel).toBeVisible()
  await expect(addToChat(page)).toHaveCount(0)

  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(selectedThread(page)).not.toHaveAttribute("data-id", originalThread)
  await expect(composer(page)).toBeVisible()
  await expect(composer(page)).toBeEnabled()
  const currentThread = (await selectedThread(page).getAttribute("data-id"))!
  expect(currentThread).not.toBe(originalThread)
  await composer(page).fill("Use the output from the other conversation.")
  await selectOutput(page, source.terminal.locator(".xterm-rows > div").filter({ hasText: marker }))
  await addToChat(page).click()
  await expect(composer(page)).toBeFocused()
  await page.getByLabel("1 annotation").hover()
  await expect(page.getByTestId("quote-snippet")).toContainText(`Conversation: ${JSON.stringify(originalThread)}`)
  await expect(page.getByTestId("quote-snippet")).not.toContainText(`Conversation: ${JSON.stringify(currentThread)}`)
  await expect(composer(page)).toHaveValue("Use the output from the other conversation.")
})
