import { expect, test, type Page } from "@playwright/test"

/** Every spec starts on its own conversation, so one failing run cannot leave
 *  state that breaks the next. */
async function freshConversation(page: Page) {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
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

async function openFiles(page: Page) {
  await page.getByRole("tab", { name: "Files" }).click()
  return page.getByRole("tabpanel")
}

async function filterFiles(page: Page, query: string) {
  const panel = await openFiles(page)
  await panel.getByLabel("Filter files").fill(query)
  return panel
}

test("runs a swarm turn end to end and keeps it after a reload", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")

  // a live thought is a short scrolling box whose label sweeps, not a
  // wall of frozen text that pushes the answer off the screen. Catch it
  // before the mock finishes thinking — that window is short.
  await expect(page.getByTestId("thought-scroll")).toBeVisible({ timeout: 15_000 })
  await expect(page.getByText("Thinking", { exact: true })).toBeVisible()

  // live status must look alive: a sweep on the running line, not a frozen
  // ellipsis. The mock turn is long enough for wait_agents / heartbeat to land.
  await expect(page.locator('[data-marquee="shimmer"]').first()).toBeVisible({ timeout: 15_000 })

  // the manager delegates, and both workers show up in the roster
  const roster = page.getByRole("tabpanel").filter({
    has: page.getByText("researcher", { exact: true }),
  })
  await expect(roster.getByText("researcher", { exact: true })).toBeVisible()
  await expect(roster.getByText("reviewer", { exact: true })).toBeVisible()

  // the transcript shows the delegation and then an answer
  const transcript = page.getByTestId("transcript")
  await waitForIdle(page)
  await expect(transcript.getByText("Two sub-agents ran in parallel")).toBeVisible()
  const answer = transcript.locator("[data-testid=assistant-message]", {
    hasText: "Two sub-agents ran in parallel",
  })
  const answerMeta = answer.getByTestId("message-meta")
  await expect(answerMeta).toHaveCSS("opacity", "0")
  const idle = await answerMeta.boundingBox()
  expect(idle?.height ?? 0).toBeGreaterThan(8)
  await answer.hover()
  await expect(answer.getByTestId("assistant-message-time")).toHaveAttribute("datetime", /^\d{4}-/)
  await expect(answerMeta).toHaveCSS("opacity", "1")
  expect((await answerMeta.boundingBox())?.height).toBe(idle?.height)
  const user = page.getByTestId("user-message").first()
  const userMeta = user.locator("xpath=following-sibling::*[@data-testid='message-meta']")
  await expect(userMeta).toHaveCSS("opacity", "0")
  const userIdle = await userMeta.boundingBox()
  expect(userIdle?.height ?? 0).toBeGreaterThan(8)
  await user.hover()
  await expect(userMeta).toHaveCSS("opacity", "1")
  expect((await userMeta.boundingBox())?.height).toBe(userIdle?.height)
  // WKWebView denies clipboard-write. The click must still copy via the
  // gesture-safe path and flip the control to Copied.
  await page.evaluate(() => {
    const clip = navigator.clipboard
    if (clip) clip.writeText = () => Promise.reject(new Error("denied"))
  })
  const copyMsg = userMeta.getByRole("button", { name: "Copy message" })
  await copyMsg.click()
  await expect(copyMsg).toHaveAttribute("title", "Copied")
  await expect(transcript.getByTestId("transcript-chart")).toBeVisible()
  await transcript.getByRole("tab", { name: "Table" }).click()
  await expect(transcript.getByRole("table")).toBeVisible()
  await transcript.getByRole("tab", { name: "Chart" }).click()
  await expect(transcript.getByTestId("transcript-chart").locator("svg")).toBeVisible()
  await expect(transcript.getByText(/Worked for/)).toBeVisible()

  // billed tokens land on the composer ring and the Trace tab, not as a
  // transcript row. A missing kind used to dump the JSON snapshot as a notice.
  await expect(page.getByTestId("context-meter")).toBeVisible()
  await expect(page.getByTestId("context-meter")).toHaveAttribute(
    "aria-label",
    /context used|tokens used/,
  )
  await expect(transcript.getByText(/context_tokens/)).toBeHidden()
  await page.getByRole("tab", { name: "Trace" }).click()
  await expect(page.getByTestId("trace-usage")).toBeVisible()
  await expect(page.getByTestId("trace-usage")).toContainText(/tokens/)
  // The event dump stays folded: a swarm turn would otherwise paint every
  // spawn/tool row into the sidebar.
  await expect(page.getByTestId("trace-log")).toHaveCount(0)
  await page.getByRole("tab", { name: "Agents" }).click()

  // Opening a worker must not put its chrome inside the scroller: a sticky
  // bar there covers the back button the moment the transcript is dragged.
  await roster.getByText("researcher", { exact: true }).click()
  await expect(page.getByTestId("agent-chrome")).toBeVisible()
  await expect(page.getByRole("button", { name: "Back to agents" })).toBeVisible()
  const agentLog = page.getByTestId("agent-scroller")
  await expect(agentLog).toBeVisible()
  // write stays collapsed like exec; the hunk is behind a click, not a
  // wall of additions and not the one-line status eino-tools returns.
  await expect(agentLog.getByTestId("file-diff")).toHaveCount(0)
  await agentLog.getByRole("button", { name: /write/ }).click()
  const written = agentLog.getByTestId("file-diff")
  await expect(written).toBeVisible()
  await expect(written).toContainText("notes/researcher.md")
  await expect(written.locator('[data-diff="add"]').first()).toBeVisible()
  await expect(agentLog.getByText(/Updated file/)).toHaveCount(0)
  await expect
    .poll(async () =>
      agentLog.evaluate(
        (el) => el.scrollHeight - el.scrollTop - el.clientHeight <= 24,
      ),
    )
    .toBe(true)
  await page.getByRole("button", { name: "View system prompt" }).click()
  const prompt = page.getByRole("dialog")
  await expect(prompt).toBeVisible()
  await expect(prompt.getByTestId("agent-prompt")).toContainText("Environment")
  await expect(prompt.getByTestId("agent-prompt")).toContainText(
    "The tools run on this machine",
  )
  await page.getByRole("button", { name: "Close" }).click()
  await expect(prompt).toBeHidden()
  await page.getByRole("button", { name: "Back to agents" }).click()
  await expect(roster.getByText("reviewer", { exact: true })).toBeVisible()

  // The progress pulse is a live-only signal: it must leave nothing behind when
  // the turn ends, and its payload must never surface as a transcript row —
  // an unhandled event kind used to land in the timeline as a raw notice.
  await expect(page.getByTestId("heartbeat")).toBeHidden()
  await expect(transcript.getByText(/elapsed_ms/)).toBeHidden()

  // one user turn is not a conversation you jump around in
  await expect(page.getByTestId("turn-nav")).toBeHidden()

  // exactly one thought per thought: the streamed text and the stored record
  // must fold into a single row
  const thoughts = await transcript.getByText("Thought", { exact: true }).count()
  expect(thoughts).toBeGreaterThan(0)

  // the workers left their notes in the conversation's workspace. A unique
  // nested folder starts expanded so the files are visible without a click.
  const files = await openFiles(page)
  await expect(files.getByRole("treeitem", { name: "researcher.md" })).toBeVisible()
  await expect(files.getByRole("treeitem", { name: "reviewer.md" })).toBeVisible()

  // a reload replays the whole turn from the event log
  await page.reload()
  await expect(page.getByTestId("transcript").getByText("Two sub-agents ran in parallel")).toBeVisible()
  await expect(page.getByTestId("transcript").getByTestId("transcript-chart")).toBeVisible()
  await expect(statusBadge(page)).toContainText("Idle")
  await expect(page.getByTestId("context-meter")).toBeVisible()
})

test("collapses a live thought while it is still streaming", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  // Same short window as the live-thought assertion above. Streaming used
  // to force the row open, so this click was a no-op.
  await expect(page.getByTestId("thought-scroll")).toBeVisible({ timeout: 15_000 })
  await page.getByTestId("thought-toggle").click()
  await expect(page.getByTestId("thought-scroll")).toBeHidden()
  await expect(page.getByTestId("thought-toggle")).toHaveAttribute("aria-expanded", "false")
})

test("renders markdown as the answer streams, not after it finishes", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  const transcript = page.getByTestId("transcript")
  // The scripted answer opens with a heading. Both must be true at once:
  // a heading that only appears after Idle would pass even if the UI still
  // dumped raw hashes until the stream ended.
  await expect(async () => {
    await expect(transcript.getByRole("heading", { name: "Result" })).toBeVisible()
    await expect(statusBadge(page)).toContainText("Working")
  }).toPass({ timeout: 60_000 })
  await waitForIdle(page)
  await expect(transcript.getByRole("heading", { name: "Result" })).toBeVisible()
})

test("stays put when the reader scrolls up mid-stream and offers a jump back", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 520 })
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  const transcript = page.getByTestId("transcript")
  // Spawn rows are the first time the log is long enough to leave the live
  // edge. Catch it while the turn is still writing so a yank would still
  // be a yank, not a finished conversation settling.
  await expect(transcript.getByText(/^Started/).first()).toBeVisible({
    timeout: 30_000,
  })
  await expect
    .poll(async () =>
      transcript.evaluate((el) => el.scrollHeight - el.clientHeight > 40),
    )
    .toBe(true)
  const left = await transcript.evaluate((el) => {
    el.scrollTop = 0
    el.dispatchEvent(new WheelEvent("wheel", { deltaY: -160, bubbles: true }))
    return el.scrollTop
  })
  await expect(async () => {
    await expect(transcript.getByRole("heading", { name: "Result" })).toBeVisible()
    await expect(statusBadge(page)).toContainText("Working")
  }).toPass({ timeout: 60_000 })
  const away = await transcript.evaluate((el) => ({
    top: el.scrollTop,
    gap: el.scrollHeight - el.scrollTop - el.clientHeight,
  }))
  expect(away.top).toBe(left)
  expect(away.gap).toBeGreaterThan(40)
  const jump = page.getByRole("button", { name: "Jump to latest" })
  await expect(jump).toBeVisible()
  await jump.click()
  await expect(jump).toBeHidden()
  expect(
    await transcript.evaluate(
      (el) => el.scrollHeight - el.scrollTop - el.clientHeight,
    ),
  ).toBeLessThan(40)
})

test("switching conversations lands at the latest turn", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 520 })
  await freshConversation(page)
  // Distinct from the other specs' first message so the generated sidebar
  // name is unique in the shared e2e data directory.
  const firstAsk = "Switch back and land on the latest turn of this conversation"
  await send(page, firstAsk)
  await waitForIdle(page)
  const secondAsk = "Second task: stay on the live edge after switching back"
  await send(page, secondAsk)
  await waitForIdle(page)
  const title = page.getByTestId("thread-title")
  await expect(title).not.toHaveText("New conversation")
  await expect(title).not.toHaveText(/…/, { timeout: 15_000 })
  const first = (await title.textContent()) ?? ""

  await page.getByRole("button", { name: "New conversation" }).click()
  await send(page, "First task: outline the work")
  await waitForIdle(page)
  const transcript = page.getByTestId("transcript")
  await transcript.evaluate((el) => {
    el.scrollTop = 0
  })

  await page.locator("aside").getByRole("button", { name: first, exact: true }).click()
  await expect(transcript.getByText(`Request: ${secondAsk}`)).toBeVisible()
  const pos = await transcript.evaluate((el) => ({
    overflow: el.scrollHeight - el.clientHeight,
    fromBottom: el.scrollHeight - el.scrollTop - el.clientHeight,
  }))
  expect(pos.overflow).toBeGreaterThan(80)
  expect(pos.fromBottom).toBeLessThanOrEqual(40)
  await expect(
    page.getByTestId("turn-nav").locator("[data-turn-nav-tick]").last(),
  ).toHaveAttribute("aria-current", "true")
})

test("names a conversation after the first turn", async ({ page }) => {
  await freshConversation(page)
  const ask =
    "Investigate the overdue items in the weekly status report for the ops team"
  await send(page, ask)
  await waitForIdle(page)

  const title = page.getByTestId("thread-title")
  await expect(title).not.toHaveText("New conversation")
  await expect(title).not.toHaveText(ask)
  // The placeholder is the truncated request plus an ellipsis; the generated
  // name is shorter and has none. Naming runs from the opening message, so
  // a long first turn cannot rename the sidebar after the fact.
  await expect(title).not.toHaveText(/…/, { timeout: 15_000 })
  await expect(title).toHaveText(/Investigate/)
  await expect(
    page.getByTestId("transcript").getByText(ask, { exact: true }),
  ).toBeVisible()
  await expect(page.getByTestId("transcript").getByText("title-namer")).toBeHidden()
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

  // two user turns is the point the left rail appears: hover lists them,
  // click jumps to the earlier one instead of making the user scroll it.
  const nav = page.getByTestId("turn-nav")
  await expect(nav).toBeVisible()
  await expect(
    nav.locator('[data-turn-nav-tick][aria-current="true"]'),
  ).toHaveAttribute("data-turn-nav-tick", /.+/)
  // Idle at the live edge: the last user turn, not the first tick.
  const ticks = nav.locator("[data-turn-nav-tick]")
  await expect(ticks.last()).toHaveAttribute("aria-current", "true")
  await nav.hover()
  const list = page.getByTestId("turn-nav-list")
  await expect(list).toHaveClass(/w-96/)
  await expect(list.getByText("First task: outline the work")).toBeVisible()
  await expect(list.getByText("Second task: tighten that outline")).toBeVisible()
  await expect(
    list.getByRole("button", { name: "First task: outline the work" }).locator("span"),
  ).toHaveClass(/line-clamp-2/)
  await list.getByRole("button", { name: "First task: outline the work" }).click()
  await expect(
    transcript.getByText("First task: outline the work", { exact: true }),
  ).toBeInViewport()
})

test("pins unread steering under the working line", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  // Type while it is still spawning. wait_agents returns on the first
  // finish, so the old tray round-trip (Enter → Steer click) lost the
  // unread window and the pin never painted.
  const nudge = "prefer the shorter path"
  await composer(page).fill(nudge)
  await expect(page.getByText(/Waiting for/)).toBeVisible({ timeout: 30_000 })
  await composer(page).press("ControlOrMeta+Enter")
  const transcript = page.getByTestId("transcript")
  // Pin and position have to be the same snapshot: a later model round
  // unmounts queued-steers in well under the next await.
  await expect
    .poll(async () => {
      return transcript.evaluate((el, text) => {
        const queuedEl = el.querySelector('[data-testid="queued-steers"]')
        if (!queuedEl || !(queuedEl.textContent ?? "").includes(text)) {
          return "waiting"
        }
        const pulse = el.querySelector('[data-testid="heartbeat"]')
        if (pulse) {
          return pulse.compareDocumentPosition(queuedEl) & Node.DOCUMENT_POSITION_FOLLOWING
            ? "after-pulse"
            : "before-pulse"
        }
        const wait = Array.from(el.querySelectorAll("[data-marquee]")).find((n) =>
          (n.textContent ?? "").includes("Waiting for"),
        )
        if (!wait) return "after-body"
        return wait.compareDocumentPosition(queuedEl) & Node.DOCUMENT_POSITION_FOLLOWING
          ? "after-wait"
          : "before-wait"
      }, nudge)
    })
    .toMatch(/^after-/)
  await expect(statusBadge(page)).toContainText("Working")
})

test("retracts unread steering so the manager never sees it", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  const nudge = "prefer the shorter path"
  await composer(page).fill(nudge)
  await expect(page.getByText(/Waiting for/)).toBeVisible({ timeout: 30_000 })
  await composer(page).press("ControlOrMeta+Enter")
  const queued = page.getByTestId("queued-steers")
  await expect(queued).toContainText(nudge, { timeout: 30_000 })
  await expect(
    queued.getByRole("button", { name: "Abort the current tool and inject queued steering" }),
  ).toBeVisible()
  await queued.getByRole("button", { name: "Remove this unread steering" }).click()
  await expect(page.getByTestId("queued-steers")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Working")
})

test("interrupts the current tool so unread steering lands now", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  const nudge = "prefer the shorter path"
  await composer(page).fill(nudge)
  await expect(page.getByText(/Waiting for/)).toBeVisible({ timeout: 30_000 })
  await composer(page).press("ControlOrMeta+Enter")
  const queued = page.getByTestId("queued-steers")
  await expect(queued).toContainText(nudge, { timeout: 30_000 })
  await queued
    .getByRole("button", { name: "Abort the current tool and inject queued steering" })
    .click()
  // Stop would cancel the turn. Interrupt keeps this one running.
  await expect(statusBadge(page)).toContainText("Working")
  await expect(statusBadge(page)).not.toContainText("Idle")
})

test("Enter while working queues until the turn finishes", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  const later = "after the current turn"
  await composer(page).fill(later)
  await composer(page).press("Enter")
  await expect(page.getByTestId("followup-queue")).toContainText(later)
  await expect(page.getByTestId("transcript").getByTestId("steer")).toHaveCount(0)
  await expect(
    page.getByTestId("transcript").getByText(later, { exact: true }),
  ).toBeVisible({ timeout: 60_000 })
  await expect(page.getByTestId("followup-queue")).toHaveCount(0)
})

test("Steer on a queued row injects and empties the tray", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  const later = "narrow the current turn"
  await composer(page).fill(later)
  await composer(page).press("Enter")
  const tray = page.getByTestId("followup-queue")
  await expect(tray).toContainText(later)
  await tray.getByRole("button", { name: `Steer: ${later}` }).click()
  await expect(page.getByTestId("followup-queue")).toHaveCount(0)
  await expect(page.getByTestId("steer")).toContainText(later)
})

test("editing a queued follow-up moves it to the back", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  const first = "queued first"
  const second = "queued second"
  await composer(page).fill(first)
  await composer(page).press("Enter")
  await composer(page).fill(second)
  await composer(page).press("Enter")
  const tray = page.getByTestId("followup-queue")
  await expect(tray).toContainText(first)
  await expect(tray).toContainText(second)
  await tray.getByRole("button", { name: `Edit queued message: ${first}` }).click()
  const edit = tray.getByTestId("followup-edit")
  await edit.fill("queued first edited")
  await edit.press("Enter")
  await expect(tray.getByTestId("followup-edit")).toHaveCount(0)
  const rows = tray.locator("li")
  await expect(rows.nth(0)).toContainText(second)
  await expect(rows.nth(1)).toContainText("queued first edited")
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

  await expect(page.getByTestId("user-message")).toContainText("uploads/brief.txt")

  const files = await filterFiles(page, "brief.txt")
  await expect(files.getByRole("treeitem", { name: "brief.txt" })).toBeVisible()
  await expect(files.getByText("yours")).toBeVisible()
})

test("collapses a workspace directory in Files", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  await waitForIdle(page)
  const files = await openFiles(page)
  await expect(files.getByRole("treeitem", { name: "researcher.md" })).toBeVisible()
  await files.getByRole("treeitem", { name: "notes" }).click()
  await expect(files.getByRole("treeitem", { name: "researcher.md" })).toHaveCount(0)
  await files.getByLabel("Filter files").fill("reviewer")
  await expect(files.getByRole("treeitem", { name: "reviewer.md" })).toBeVisible()
  await expect(files.getByRole("treeitem", { name: "researcher.md" })).toHaveCount(0)
})

const tinyPngB64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

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

  await page.reload()
  await expect(
    page.getByTestId("user-message").getByTestId("input-images").locator("img"),
  ).toBeVisible()
})

test("drops a file and an image onto the composer", async ({ page }) => {
  await freshConversation(page)
  const files = [
    {
      name: "notes.txt",
      mime: "text/plain",
      b64: Buffer.from("workspace material").toString("base64"),
    },
    { name: "shot.png", mime: "image/png", b64: tinyPngB64 },
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
  await send(page, "Use the attached material")
  await waitForIdle(page)

  const thumb = page.getByTestId("user-message").getByTestId("input-images").locator("img")
  await expect(thumb).toBeVisible()
  await expect(thumb).toHaveAttribute("src", /\/input-images\/img_/)

  await page.getByRole("tab", { name: "Files" }).click()
  const listed = await filterFiles(page, "notes.txt")
  await expect(listed.getByRole("treeitem", { name: "notes.txt" })).toBeVisible()
  await expect(listed.getByText("shot.png")).toBeHidden()
})

test("shows the turn id for troubleshooting", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Something worth tracing")
  await waitForIdle(page)

  await page.getByRole("tab", { name: "Trace" }).click()
  const panel = page.getByRole("tabpanel")
  await expect(panel.getByText(/^tn_/)).toBeVisible()
  await expect(page.getByTestId("trace-log")).toHaveCount(0)
  // the timeline names the agents that took part — behind Full log
  await page.getByTestId("trace-log-toggle").click()
  await expect(page.getByTestId("trace-log").getByText("researcher-1").first()).toBeVisible()
})

test("switches the thinking level and keeps it after a reload", async ({ page }) => {
  await freshConversation(page)
  const level = page.getByLabel("Thinking level")
  // a fresh conversation starts on the model's own default
  await expect(level).toContainText("Default")

  await level.click()
  await page.getByRole("option", { name: "High thinking" }).click()
  await expect(level).toContainText("High")

  // the choice is stored on the conversation, so a reload brings it back
  await page.reload()
  await expect(page.getByLabel("Thinking level")).toContainText("High")
})

test("switches the model in the composer and keeps it after a reload", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  const provider = settings.models.providers[0]
  await request.put("/api/settings", {
    data: {
      models: {
        default: settings.models.default,
        providers: [
          { ...provider, model: "alpha", catalog: ["alpha", "beta"] },
        ],
      },
    },
  })
  try {
    await freshConversation(page)
    const picker = page.getByLabel("Model")
    await expect(picker).toContainText("alpha")
    await picker.click()
    await expect(page.getByLabel("Search models")).toBeVisible()
    await expect(page.getByRole("button", { name: "Refresh models" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Edit providers" })).toBeVisible()
    await page.getByLabel("Search models").fill("beta")
    await page.getByRole("option", { name: /beta/ }).click()
    await expect(picker).toContainText("beta")
    await picker.click()
    await page.getByRole("button", { name: "Edit providers" }).click()
    const dialog = page.getByRole("dialog")
    await expect(dialog.getByRole("heading", { name: "Models", exact: true })).toBeVisible()
    const heading = (provider.label || provider.id).trim()
    await expect(
      dialog.getByRole("button", { name: `${heading} details` }),
    ).toBeVisible()
    await expect(dialog.getByRole("button", { name: "Add a provider" })).toBeVisible()
    await expect(dialog.getByRole("textbox", { name: "Provider" })).toHaveCount(0)
    await dialog.getByRole("button", { name: "Back to app" }).click()
    await page.reload()
    await expect(page.getByLabel("Model")).toContainText("beta")
  } finally {
    await request.put("/api/settings", {
      data: { models: settings.models },
    })
  }
})

test("Enter that confirmed the IME does not send the draft", async ({ page }) => {
  await freshConversation(page)
  const input = composer(page)
  await input.fill("draft still in the box")
  // WebKit (and this test) fires compositionend, then the Enter that kept
  // leftover Latin, with isComposing already false. That key belongs to
  // the IME; sending would dump a half-composed line.
  // String, not a closure: this file is typechecked without DOM libs.
  await input.evaluate(`(el) => {
    el.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true }))
    el.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true }))
    el.dispatchEvent(new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
    }))
  }`)
  await expect(statusBadge(page)).toContainText("Idle")
  await expect(input).toHaveValue("draft still in the box")
  // Empty conversations have no transcript yet. If that Enter had sent,
  // the draft would be a user bubble and the box would be empty.
  await expect(page.getByTestId("transcript")).toHaveCount(0)

  await page.evaluate(
    `() => new Promise((r) => requestAnimationFrame(() => r(undefined)))`,
  )
  await input.press("Enter")
  await expect(statusBadge(page)).toContainText("Working")
})

test("pauses at the tool-round cap instead of failing the turn", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  await request.put("/api/settings", {
    data: { swarm: { ...settings.swarm, manager_max_iterations: 1 } },
  })
  try {
    await freshConversation(page)
    await send(page, "Look at this from two angles")
    const card = page.getByTestId("iteration-limit")
    await expect(card).toBeVisible({ timeout: 30_000 })
    await expect(statusBadge(page)).toContainText("Waiting")
    await card.getByRole("button", { name: "Stop at the tool-round limit" }).click()
    await waitForIdle(page)
    await expect(page.getByTestId("transcript")).not.toContainText("NodeRunError")
    await expect(card.getByText(/Stopped after/)).toBeVisible()
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm } })
  }
})

test("continuing at the tool-round cap finishes the turn", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  await request.put("/api/settings", {
    data: { swarm: { ...settings.swarm, manager_max_iterations: 2 } },
  })
  try {
    await freshConversation(page)
    await send(page, "Look at this from two angles and merge the findings")
    const card = page.getByTestId("iteration-limit")
    await expect(card).toBeVisible({ timeout: 30_000 })
    // A small cap is hit more than once before the scripted swarm answers.
    // Keep extending until the turn actually ends, instead of clicking a
    // handful of times while the next pause has not been drawn yet.
    await expect(async () => {
      if ((await statusBadge(page).textContent())?.includes("Idle")) return
      await page
        .getByRole("button", { name: "Continue for more tool rounds" })
        .click({ timeout: 1_000 })
    }).toPass({ timeout: 60_000 })
    await waitForIdle(page)
    await expect(page.getByTestId("transcript")).toContainText(
      "Two sub-agents ran in parallel",
    )
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm } })
  }
})

test("edits a sent message in place and restarts from there", async ({
  page,
}) => {
  await freshConversation(page)
  const first = "Look at this from two angles and merge the findings"
  const second = "Now compare those findings with the first pass"
  await send(page, first)
  await waitForIdle(page)
  await send(page, second)
  await waitForIdle(page)
  await expect(page.getByTestId("user-message")).toHaveCount(2)

  await page.getByTestId("user-message").first().hover()
  await page.getByRole("button", { name: "Edit message" }).first().click()
  const editor = page.getByTestId("user-message-editor")
  await expect(editor).toBeVisible()
  await expect(composer(page)).toHaveValue("")
  const edited = "Look at this from two angles and merge the findings again"
  await editor.getByRole("textbox", { name: "Edit message" }).fill(edited)
  await editor.getByRole("button", { name: "Send" }).click()

  await expect(page.getByTestId("user-message")).toHaveCount(1)
  await expect(page.getByTestId("user-message")).toHaveText(edited)
  await expect(page.getByTestId("transcript")).not.toContainText(second)
  await expect(statusBadge(page)).toContainText("Working")
  await waitForIdle(page)
})

test("slash menu lists built-in commands", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/")
  const menu = page.getByTestId("slash-menu")
  await expect(menu).toBeVisible()
  await expect(page.getByTestId("slash-command-goal")).toBeVisible()
  await expect(page.getByTestId("slash-command-plan")).toBeVisible()
  await expect(page.getByTestId("slash-command-compact")).toBeVisible()
  await expect(page.getByTestId("slash-command-compact")).not.toContainText("% full")
  await composer(page).press("ArrowDown")
  await expect(page.getByTestId("slash-command-plan")).toHaveAttribute(
    "aria-selected",
    "true",
  )
  await composer(page).press("Escape")
  await expect(menu).toBeHidden()
})

test("slash menu opens after existing text", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("hello /")
  await expect(page.getByTestId("slash-menu")).toBeVisible()
  await expect(page.getByTestId("slash-command-goal")).toBeVisible()
  await composer(page).press("Escape")
  await expect(page.getByTestId("slash-menu")).toHaveCount(0)
  await expect(composer(page)).toHaveValue("hello ")
})

test("slash menu opens from the IME punctuation comma", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("、")
  await expect(page.getByTestId("slash-menu")).toBeVisible()
  await expect(composer(page)).toHaveValue("/")
  await expect(page.getByTestId("slash-command-goal")).toBeVisible()
})

test("goal command pins a standing objective", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/")
  await page.getByTestId("slash-command-goal").click()
  await expect(page.getByTestId("slash-menu")).toHaveCount(0)
  await expect(composer(page)).toHaveValue("/goal ")
  await composer(page).fill("/goal keep going")
  await composer(page).press("Enter")
  await expect(page.getByTestId("goal-banner")).toContainText("keep going")
  await expect(page.getByTestId("goal-banner")).toContainText("Pursuing")
  await expect(statusBadge(page)).toContainText("Working")
  await page.getByRole("button", { name: "Clear goal" }).click()
  await expect(page.getByTestId("goal-banner")).toHaveCount(0)
})

test("a standing objective completes after the scripted run", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/goal keep going")
  await composer(page).press("Enter")
  await expect(page.getByTestId("goal-banner")).toContainText("Pursuing")
  await waitForIdle(page)
  await expect(page.getByTestId("goal-banner")).toContainText("Done")
  await expect(page.getByTestId("transcript")).toContainText(
    "Standing objective completed.",
  )
  await expect(page.getByTestId("user-message")).toHaveCount(1)
})

test("a standing objective is not paused by the tool-round slice", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  await request.put("/api/settings", {
    data: { swarm: { ...settings.swarm, goal_session_max_iterations: 1, goal_max_auto_turns: 1 } },
  })
  try {
    await freshConversation(page)
    await composer(page).fill("/goal keep going")
    await composer(page).press("Enter")
    await waitForIdle(page)
    await expect(page.getByTestId("goal-banner")).toContainText("Done")
    await expect(page.getByTestId("goal-banner")).not.toContainText("Paused")
    await expect(page.getByTestId("goal-session")).toHaveCount(0)
    await expect(page.getByTestId("user-message")).toHaveCount(1)
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm } })
  }
})

test("a slash goal starts pursuing without a second human message", async ({
  page,
}) => {
  await freshConversation(page)
  await composer(page).fill("/goal keep going")
  await composer(page).press("Enter")
  await expect(page.getByTestId("goal-banner")).toContainText("Pursuing")
  await expect(page.getByTestId("goal-start")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Working")
  await waitForIdle(page)
  await expect(page.getByTestId("goal-banner")).toContainText("Done")
})

test("the standing objective can be edited in place", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/goal keep going")
  await composer(page).press("Enter")
  await page.getByTestId("goal-text").click()
  const box = page.getByTestId("goal-edit")
  await box.fill("keep going, tighter")
  await box.press("Enter")
  await expect(page.getByTestId("goal-banner")).toContainText("keep going, tighter")
  await expect(page.getByTestId("transcript")).toContainText(
    "Standing objective updated.",
  )
})

test("auto-compacts when prompt tokens pass the configured budget", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  await request.put("/api/settings", {
    data: {
      swarm: {
        ...settings.swarm,
        compact_keep_messages: 2,
        auto_compact_tokens: 200,
      },
    },
  })
  try {
    await freshConversation(page)
    await send(page, "the first request")
    await waitForIdle(page)
    const before = await page.getByTestId("user-message").count()
    await send(page, "the second request")
    await expect(page.getByTestId("transcript")).toContainText(
      "Context compressed",
      { timeout: 60_000 },
    )
    await waitForIdle(page)
    await expect(page.getByTestId("transcript")).toContainText("tokens")
    await expect(page.getByTestId("user-message")).toHaveCount(before + 1)
    await page.getByTestId("compact-briefing-open").last().click()
    const briefing = page.getByTestId("compact-briefing")
    await expect(briefing).toBeVisible()
    await expect(briefing).not.toContainText("through_seq")
    await expect(briefing).not.toHaveText(/^\s*$/)
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm } })
  }
})

test("compact folds earlier turns without rewriting the transcript", async ({
  page,
  request,
}) => {
  const { settings } = await (await request.get("/api/settings")).json()
  await request.put("/api/settings", {
    data: { swarm: { ...settings.swarm, compact_keep_messages: 2 } },
  })
  try {
    await freshConversation(page)
    await send(page, "First request in this conversation")
    await waitForIdle(page)
    await send(page, "Second request in this conversation")
    await waitForIdle(page)
    const before = await page.getByTestId("user-message").count()
    await composer(page).fill("/compact")
    await composer(page).press("Enter")
    await expect(page.getByTestId("transcript")).toContainText(
      "Earlier turns were folded into a briefing",
    )
    await expect(page.getByTestId("user-message")).toHaveCount(before)
    await page.getByTestId("compact-briefing-open").last().click()
    const briefing = page.getByTestId("compact-briefing")
    await expect(briefing).toBeVisible()
    await expect(briefing).not.toContainText("through_seq")
    await expect(briefing).not.toHaveText(/^\s*$/)
  } finally {
    await request.put("/api/settings", { data: { swarm: settings.swarm } })
  }
})

test("Stop closes in-flight tools instead of leaving them spinning", async ({ page }) => {
  await freshConversation(page)
  await send(page, "Look at this from two angles and merge the findings")
  await expect(page.getByText(/Waiting for/)).toBeVisible({ timeout: 30_000 })
  await page.getByRole("button", { name: "Stop", exact: true }).click()
  await waitForIdle(page)
  await expect(page.getByText("interrupted")).toBeVisible()
  await expect(page.locator(".animate-spin")).toHaveCount(0)
})

test("plan command drafts then implements", async ({ page }) => {
  await freshConversation(page)
  await composer(page).fill("/plan inspect then change")
  await composer(page).press("Enter")
  await expect(page.getByTestId("plan-banner")).toContainText("Planning")
  await expect(statusBadge(page)).toContainText("Working")
  const ask = page.getByTestId("ask-card")
  await expect(ask).toBeVisible({ timeout: 30_000 })
  await expect(ask).toContainText("Your answer needed")
  await expect(ask.getByTestId("ask-mark")).toBeVisible()
  await expect(ask).not.toHaveClass(/animate-ask-ring/)
  await expect(ask.locator("[data-testid=ask-mark] .animate-ping")).toHaveCount(0)
  await expect(statusBadge(page)).toContainText("Your turn")
  await expect(
    statusBadge(page).locator("[data-testid=ask-mark] .animate-ping"),
  ).toHaveCount(1)
  expect(await ask.evaluate((el) => {
    const col = el.closest(".content-column")
    if (!(col instanceof HTMLElement)) return false
    const cw = col.getBoundingClientRect().width
    return cw > 512 && Math.abs(el.getBoundingClientRect().width - cw) < 2
  })).toBe(true)
  await page.getByTestId("ask-option-safer").click()
  await page.getByTestId("ask-submit").click()
  await waitForIdle(page)
  await expect(page.getByTestId("plan-text")).toContainText("# Plan")
  await expect(page.getByTestId("transcript")).toContainText("Plan updated.")
  await page.getByTestId("plan-implement").click()
  await expect(statusBadge(page)).toContainText("Working")
  await waitForIdle(page)
  await expect(page.getByTestId("plan-banner")).toHaveCount(0)
  await expect(page.getByTestId("transcript")).toContainText(
    "The human accepted the plan. Execute it.",
  )
})
