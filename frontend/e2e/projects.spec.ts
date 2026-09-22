import { expect, test, type Page } from "@playwright/test"

/** The whole point of a project: two conversations, one directory, one
 *  instruction, and what the first one learned available to the second. */

const composer = (page: Page) => page.getByTestId("composer-input")
const statusBadge = (page: Page) => page.getByTestId("status-badge")
const notes = (page: Page) => page.getByLabel("Project notes")

async function send(page: Page, text: string) {
  await composer(page).fill(text)
  await composer(page).press("Enter")
  await expect(statusBadge(page)).toContainText("Working")
  await expect(statusBadge(page)).toContainText("Idle", { timeout: 60_000 })

  // Hermes-style: a write that landed is named in the transcript, not only
  // in a panel the user may not have open.
  await expect(page.getByTestId("memory-notice")).toBeVisible({ timeout: 60_000 })
}

async function startInProject(page: Page, name: string) {
  await page.getByRole("button", { name: `New conversation in ${name}` }).click()
}

async function createProject(page: Page, name: string) {
  await page.goto("/")
  await page.getByRole("button", { name: "New project" }).click()
  await page.getByLabel("Name").fill(name)
  await page.getByRole("button", { name: "Create project" }).click()
  await expect(projectRow(page, name)).toBeVisible()
}

/** The row itself, not the menu button beside it that is named after it. */
const projectRow = (page: Page, name: string) =>
  page.getByRole("button", { name, exact: true })

async function openMemory(page: Page) {
  await page.getByRole("tab", { name: "Memory" }).click()
}

test("a project carries what one conversation learned into the next", async ({
  page,
}) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)

  // A conversation started from the project row belongs to it, and
  // says so: which directory the tools are pointed at is otherwise invisible.
  await startInProject(page, project)
  await expect(page.getByTestId("thread-project")).toHaveText(project)
  // Project name prefixes the title on one line. Stacking them in the 48px
  // bar made the chrome look cramped; a second copy of the model name used to
  // sit on the right of the same row.
  const projectBox = await page.getByTestId("thread-project").boundingBox()
  const titleBox = await page.getByTestId("thread-title").boundingBox()
  expect(projectBox).not.toBeNull()
  expect(titleBox).not.toBeNull()
  expect(projectBox!.x).toBeLessThan(titleBox!.x)
  expect(Math.abs(projectBox!.y - titleBox!.y)).toBeLessThan(4)

  const first = `Trace the ${Date.now()} material and summarise it`
  await send(page, first)

  // The review runs after the turn, on its own. Skills live behind the
  // project menu so the folder stays a directory, not a catalogue.
  await page.getByRole("button", { name: `Project options for ${project}` }).click()
  await page.getByRole("menuitem", { name: "View skills" }).click()
  await expect(page.getByRole("tab", { name: "Memory", selected: true })).toBeVisible()
  await expect(notes(page)).toHaveValue(new RegExp(escape(first)), {
    timeout: 60_000,
  })
  const skillCard = page.getByTestId("skill-card")
  await expect(skillCard).toBeVisible({ timeout: 60_000 })
  const skillName = ((await skillCard.locator("button").first().textContent()) ?? "").trim()
  expect(skillName).not.toBe("")
  await skillCard.locator("button").first().click()
  const skillToggle = skillCard.locator("button").first()
  await expect(skillToggle).toHaveAttribute("aria-expanded", "true")
  await expect(skillToggle).toBeInViewport()
  const skillBody = page.getByTestId("skill-body")
  await expect(skillBody).toBeVisible()
  const bodyBox = await skillBody.boundingBox()
  const cardBox = await skillCard.boundingBox()
  expect(bodyBox).not.toBeNull()
  expect(cardBox).not.toBeNull()
  // The body used to paint over the Files tree (and the next skill). It has
  // to stay inside its card.
  expect(bodyBox!.x).toBeGreaterThanOrEqual(cardBox!.x - 1)
  expect(bodyBox!.x + bodyBox!.width).toBeLessThanOrEqual(cardBox!.x + cardBox!.width + 1)
  expect(bodyBox!.y).toBeGreaterThanOrEqual(cardBox!.y - 1)
  expect(bodyBox!.y + bodyBox!.height).toBeLessThanOrEqual(cardBox!.y + cardBox!.height + 1)

  // One id still reaches the whole run: the review is recorded on the turn it
  // reviewed, so it shows up in the same trace as the work — behind Full log.
  await page.getByRole("tab", { name: "Trace" }).click()
  await page.getByTestId("trace-log-toggle").click()
  await expect(page.getByTestId("trace-log").getByText(/Memory updated/)).toBeVisible()

  // A second conversation in the same project starts with the first one's
  // skill already there — that is the feature.
  await startInProject(page, project)
  await expect(page.getByTestId("thread-project")).toHaveText(project)
  await openMemory(page)
  await expect(notes(page)).toHaveValue(new RegExp(escape(first)))
  expect(skillName).not.toBe("")
  await expect(page.getByText(skillName.slice(0, 12), { exact: false }).first()).toBeVisible()

  // A note the user corrects must stay corrected: memory nobody can fix is
  // memory that repeats its mistake in every later conversation. Save is not
  // on the pane until there is something to write.
  await expect(page.getByRole("button", { name: "Save notes", exact: true })).toHaveCount(0)
  await notes(page).fill("A note the user wrote by hand")
  await page.getByRole("button", { name: "Save notes", exact: true }).click()
  await page.reload()
  await openMemory(page)
  await expect(notes(page)).toHaveValue("A note the user wrote by hand")

  // Deleting the project takes its conversations with it, and says so first.
  await page.getByRole("button", { name: `Project options for ${project}` }).click()
  await page.getByRole("menuitem", { name: "Delete" }).click()
  await expect(page.getByText(/conversations and everything it remembered/)).toBeVisible()
  await page.getByRole("button", { name: "Delete project" }).click()

  await expect(projectRow(page, project)).toBeHidden()
})

test("the Memory tab does not leave a blank Agents pane above the notes", async ({
  page,
}) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)
  await startInProject(page, project)
  await openMemory(page)

  const tablist = page.getByRole("tablist")
  const heading = page.getByRole("heading", { name: "Notes" })
  await expect(heading).toBeVisible()
  const tabBox = await tablist.boundingBox()
  const notesBox = await heading.boundingBox()
  expect(tabBox).not.toBeNull()
  expect(notesBox).not.toBeNull()
  // Project name + padding sit between the tabs and Notes. A leaked Agents
  // shell was half the column — hundreds of pixels.
  expect(notesBox!.y - (tabBox!.y + tabBox!.height)).toBeLessThan(120)
  // Notes used to eat the pane; Skills were clipped at the window with no
  // way to scroll them into view.
  await expect(page.getByRole("heading", { name: "Skills" })).toBeInViewport()
  await expect(page.getByTestId("skills-list")).toBeInViewport()
  // Tailwind .flex used to beat [hidden], so Files sat beside Memory and a
  // skill body covered the file names. Inactive panes must not paint.
  await expect(
    page.locator('[role="tabpanel"][data-state="inactive"]').first(),
  ).toHaveCSS("display", "none")
})

test("Review now says when there is nothing to review", async ({ page }) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)
  await startInProject(page, project)
  await openMemory(page)
  await page.getByRole("button", { name: "Review this conversation now" }).click()
  await expect(page.getByTestId("review-status")).toContainText(/nothing to review/i)
})

test("Tidy skills says when the catalog is empty", async ({ page }) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)
  await startInProject(page, project)
  await openMemory(page)
  await page.getByRole("button", { name: "Tidy skills" }).click()
  await expect(page.getByTestId("tidy-status")).toContainText(/No skills to curate/i)
  await expect(page.getByTestId("tidy-stats")).toContainText(/0 scanned/)
})

test("Tidy skills asks the model when the catalog has a skill", async ({
  page,
}) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)
  await startInProject(page, project)
  await send(page, `Trace the ${Date.now()} material and summarise it`)
  await openMemory(page)
  await expect(page.getByTestId("skill-card")).toBeVisible({ timeout: 60_000 })
  await page.getByRole("button", { name: "Tidy skills" }).click()
  await expect(page.getByTestId("tidy-status")).toContainText(
    /The model reviewed the catalog|Skills curated/i,
    { timeout: 60_000 },
  )
  await expect(page.getByTestId("tidy-stats")).toContainText(/[1-9]\d* scanned/)
})

test("a conversation outside a project has no memory to show", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(page.getByTestId("thread-project")).toBeHidden()
  await expect(page.getByRole("tab", { name: "Memory" })).toBeHidden()
})

test("hovering a project starts a conversation in it, not in Recents", async ({
  page,
}) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)
  const wrap = page.getByTestId("project-wrap").filter({ hasText: project })

  await expect(wrap.getByTestId("project-folder")).toHaveAttribute("data-open", "true")
  await expect(wrap.getByTestId("project-fold")).toHaveCount(0)
  await expect(wrap.locator("[data-drag-handle]")).toHaveCount(0)

  const startIn = page.getByRole("button", { name: `New conversation in ${project}` })
  await expect(startIn).toHaveCSS("opacity", "0")
  await projectRow(page, project).hover()
  await expect(startIn).toHaveCSS("opacity", "1")
  await expect(wrap.getByTestId("project-folder")).toHaveAttribute("data-open", "true")
  await startIn.click()

  await expect(page.getByTestId("thread-project")).toHaveText(project)
  await expect(projectRow(page, project)).not.toHaveAttribute("aria-pressed")
  await expect(
    wrap.getByTestId("thread-row").first(),
  ).toHaveAttribute("aria-current", "true")

  const folder = await wrap.getByTestId("project-kind").boundingBox()
  const topicKind = await wrap.getByTestId("row-kind").boundingBox()
  const projectName = await wrap.getByTestId("project-row").getByTestId("row-label").boundingBox()
  const topicName = await wrap.getByTestId("thread-row").getByTestId("row-label").boundingBox()
  expect(folder).not.toBeNull()
  expect(topicKind).not.toBeNull()
  expect(projectName).not.toBeNull()
  expect(topicName).not.toBeNull()
  expect(Math.abs(folder!.x - topicKind!.x)).toBeLessThan(2)
  expect(Math.abs(projectName!.x - topicName!.x)).toBeLessThan(2)

  await projectRow(page, project).click()
  await expect(wrap.getByTestId("project-folder")).toHaveAttribute("data-open", "false")
  await expect(wrap.getByTestId("project-threads")).toHaveCount(0)
  await projectRow(page, project).click()
  await expect(wrap.getByTestId("project-folder")).toHaveAttribute("data-open", "true")
})

test("a project topic can be pinned to the top and stays there after reload", async ({
  page,
}) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)
  await startInProject(page, project)
  const row = page.getByTestId("project-threads").getByTestId("thread-row").first()
  await row.hover()
  await row.getByRole("button", { name: "More" }).click()
  await page.getByRole("menuitem", { name: "Pin" }).click()
  await expect(page.getByTestId("pinned-list")).toBeVisible()
  await page.reload()
  await expect(page.getByTestId("pinned-list")).toBeVisible()
})

test("dragging a project pins that order across reload", async ({ page }) => {
  const older = `Zebra ${Date.now()}`
  const newer = `Alpha ${Date.now()}`
  await createProject(page, older)
  await createProject(page, newer)
  await expect(page.getByTestId("project-row").nth(0)).toContainText(newer)
  await page.evaluate(() => {
    const rows = Array.from(document.querySelectorAll('[data-testid="project-row"]'))
    const source = rows[1]
    const target = rows[0]
    if (!(source instanceof HTMLElement) || !(target instanceof HTMLElement)) {
      throw new Error("missing project row")
    }
    source.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }))
    const dt = new DataTransfer()
    source.dispatchEvent(
      new DragEvent("dragstart", { bubbles: true, cancelable: true, dataTransfer: dt }),
    )
    target.dispatchEvent(
      new DragEvent("dragover", { bubbles: true, cancelable: true, dataTransfer: dt }),
    )
    target.dispatchEvent(
      new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: dt }),
    )
    source.dispatchEvent(new DragEvent("dragend", { bubbles: true, dataTransfer: dt }))
  })
  await expect(page.getByTestId("project-row").nth(0)).toContainText(older)
  await page.reload()
  await expect(page.getByTestId("project-row").nth(0)).toContainText(older)
})

test("a long project folder hides extra topics behind Show more", async ({
  page,
}) => {
  const project = `Project ${Date.now()}`
  await createProject(page, project)
  const wrap = page.getByTestId("project-wrap").filter({ hasText: project })
  for (let i = 0; i < 6; i++) {
    await startInProject(page, project)
    if (i < 5) {
      await expect(wrap.getByTestId("thread-row")).toHaveCount(i + 1)
    }
  }
  await expect(wrap.getByTestId("thread-row")).toHaveCount(5)
  await expect(wrap.getByRole("button", { name: "Show more" })).toBeVisible()
  await wrap.getByRole("button", { name: "Show more" }).click()
  await expect(wrap.getByTestId("thread-row")).toHaveCount(6)
  await expect(wrap.getByRole("button", { name: "Show less" })).toBeVisible()
  await wrap.getByRole("button", { name: "Show less" }).click()
  await expect(wrap.getByTestId("thread-row")).toHaveCount(5)
})

test("a running conversation keeps its progress after switching away", async ({
  page,
}) => {
  const project = `Busy ${Date.now()}`
  await createProject(page, project)
  await startInProject(page, project)
  const wrap = page.getByTestId("project-wrap").filter({ hasText: project })
  const row = wrap.getByTestId("thread-row").first()
  const id = await row.getAttribute("data-id")
  expect(id).toBeTruthy()
  await composer(page).fill("Look at this from two angles and merge the findings")
  await composer(page).press("Enter")
  await expect(statusBadge(page)).toContainText("Working")
  await expect(row.getByLabel("running")).toBeVisible()
  await page.getByRole("button", { name: "New conversation", exact: true }).click()
  await expect(statusBadge(page)).toContainText("Idle")
  const left = wrap.locator(`[data-testid="thread-row"][data-id="${id}"]`)
  await expect(left).toBeVisible()
  await expect(left.getByLabel("running")).toBeVisible()

  await wrap.getByRole("button", { name: project, exact: true }).click()
  await expect(wrap.getByTestId("thread-row")).toHaveCount(0)
  const mark = wrap.getByTestId("folder-live-mark")
  await expect(mark).toBeVisible()
  await expect(mark.getByLabel("running")).toBeVisible()
  await expect(mark).toHaveClass(/overflow-hidden/)
})

function escape(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
}
