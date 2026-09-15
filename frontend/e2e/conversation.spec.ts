import { expect, test, type Page } from "@playwright/test"

/** Every spec starts on its own conversation, so one failing run cannot leave
 *  state that breaks the next. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation" }).click()
  await expect(composer(page)).toBeVisible()
}

const composer = (page: Page) => page.getByTestId("composer-input")
const statusBadge = (page: Page) => page.getByTestId("status-badge")

async function send(page: Page, text: string) {
  await composer(page).fill(text)
  await composer(page).press("Enter")
  await expect(statusBadge(page)).toContainText("Working")
}

async function waitForIdle(page: Page) {
  await expect(statusBadge(page)).toContainText("Idle", { timeout: 60_000 })
}

test("runs a swarm turn end to end and keeps it after a reload", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")

  // the manager delegates, and both workers show up in the roster
  const roster = page.getByRole("tabpanel").first()
  await expect(roster.getByText("researcher")).toBeVisible()
  await expect(roster.getByText("reviewer")).toBeVisible()

  // the transcript shows the delegation and then an answer
  const transcript = page.getByTestId("transcript")
  await expect(transcript.getByText(/^Started/).first()).toBeVisible()
  await waitForIdle(page)
  await expect(transcript.getByText("Two sub-agents ran in parallel")).toBeVisible()
  await expect(transcript.getByText(/Worked for/)).toBeVisible()

  // exactly one thought per thought: the streamed text and the stored record
  // must fold into a single row
  const thoughts = await transcript.getByText("Thought", { exact: true }).count()
  expect(thoughts).toBeGreaterThan(0)

  // the workers left their notes in the conversation's workspace
  await page.getByRole("tab", { name: "Files" }).click()
  const files = page.getByRole("tabpanel")
  await expect(files.getByText("researcher.md", { exact: true })).toBeVisible()
  await expect(files.getByText("reviewer.md", { exact: true })).toBeVisible()

  // a reload replays the whole turn from the event log
  await page.reload()
  await expect(page.getByTestId("transcript").getByText("Two sub-agents ran in parallel")).toBeVisible()
  await expect(statusBadge(page)).toContainText("Idle")
})

test("carries context across turns", async ({ page }) => {
  await freshConversation(page)
  await send(page, "First task: outline the work")
  await waitForIdle(page)
  await send(page, "Second task: tighten that outline")
  await waitForIdle(page)

  const transcript = page.getByTestId("transcript")
  await expect(
    transcript.getByText("First task: outline the work", { exact: true }),
  ).toBeVisible()
  await expect(
    transcript.getByText("Second task: tighten that outline", { exact: true }),
  ).toBeVisible()
  // one footer per turn, not one for the conversation
  await expect(transcript.getByText(/Worked for/)).toHaveCount(2)
})

test("uploads a file into the workspace and offers it back", async ({ page }) => {
  await freshConversation(page)
  await page.getByTestId("file-input").setInputFiles({
    name: "brief.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("the material to work from"),
  })
  // it shows as a chip in the composer before it is sent anywhere
  await expect(page.getByText("brief.txt", { exact: true })).toBeVisible()
  await send(page, "Use the attached material")
  await waitForIdle(page)

  await page.getByRole("tab", { name: "Files" }).click()
  const files = page.getByRole("tabpanel")
  await expect(files.getByText("brief.txt", { exact: true })).toBeVisible()
  await expect(files.getByText("yours")).toBeVisible()
})

test("shows the turn id for troubleshooting", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Something worth tracing")
  await waitForIdle(page)

  await page.getByRole("tab", { name: "Trace" }).click()
  const panel = page.getByRole("tabpanel")
  await expect(panel.getByText(/^tn_/)).toBeVisible()
  // the timeline names the agents that took part
  await expect(panel.getByText("researcher-1").first()).toBeVisible()
})
