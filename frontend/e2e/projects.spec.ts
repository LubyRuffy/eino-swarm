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
}

/** Creates a project and leaves it selected. The name is unique per run so a
 *  leftover data directory cannot make a later run pass for the wrong reason. */
async function createProject(page: Page, name: string) {
  await page.goto("/")
  await page.getByRole("button", { name: "New project" }).click()
  await page.getByLabel("Name").fill(name)
  await page.getByRole("button", { name: "Create project" }).click()
  await expect(projectRow(page, name)).toHaveAttribute("aria-pressed", "true")
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

  // A conversation started while a project is selected belongs to it, and
  // says so: which directory the tools are pointed at is otherwise invisible.
  await page.getByRole("button", { name: "New conversation" }).click()
  await expect(page.getByTestId("thread-project")).toHaveText(project)

  const first = `Trace the ${Date.now()} material and summarise it`
  await send(page, first)

  // The review runs after the turn, on its own, and the panel re-reads the
  // files when its event arrives.
  await openMemory(page)
  await expect(notes(page)).toHaveValue(new RegExp(escape(first)), {
    timeout: 60_000,
  })
  const skill = page.getByRole("button", { expanded: false }).last()
  await expect(skill).toBeVisible()
  const skillName = (await skill.textContent()) ?? ""

  // Opening a skill fetches its body, which is deliberately not in the list.
  await skill.click()
  await expect(page.getByRole("button", { expanded: true })).toBeVisible()

  // One id still reaches the whole run: the review is recorded on the turn it
  // reviewed, so it shows up in the same trace as the work.
  await page.getByRole("tab", { name: "Trace" }).click()
  await expect(page.getByRole("tabpanel").getByText(/Memory updated/)).toBeVisible()

  // A second conversation in the same project starts with the first one's
  // skill already there — that is the feature.
  await page.getByRole("button", { name: "New conversation" }).click()
  await expect(page.getByTestId("thread-project")).toHaveText(project)
  await openMemory(page)
  await expect(notes(page)).toHaveValue(new RegExp(escape(first)))
  expect(skillName).not.toBe("")
  await expect(page.getByText(skillName.slice(0, 12), { exact: false }).first()).toBeVisible()

  // A note the user corrects must stay corrected: memory nobody can fix is
  // memory that repeats its mistake in every later conversation.
  await notes(page).fill("A note the user wrote by hand")
  await page.getByRole("button", { name: "Save notes" }).click()
  await page.reload()
  await openMemory(page)
  await expect(notes(page)).toHaveValue("A note the user wrote by hand")

  // Deleting the project takes its conversations with it, and says so first.
  await page.getByRole("button", { name: `Project options for ${project}` }).click()
  await page.getByRole("menuitem", { name: "Delete" }).click()
  await expect(page.getByText(/conversations and everything it remembered/)).toBeVisible()
  await page.getByRole("button", { name: "Delete project" }).click()

  await expect(projectRow(page, project)).toBeHidden()
  await expect(page.getByRole("button", { name: "All conversations" })).toHaveAttribute(
    "aria-pressed",
    "true",
  )
})

test("a conversation outside a project has no memory to show", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "All conversations" }).click()
  await page.getByRole("button", { name: "New conversation" }).click()
  await expect(page.getByTestId("thread-project")).toBeHidden()
  await expect(page.getByRole("tab", { name: "Memory" })).toBeHidden()
})

function escape(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
}
