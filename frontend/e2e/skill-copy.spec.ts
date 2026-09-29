import { expect, test, type APIRequestContext, type Page } from "@playwright/test"
import fs from "node:fs/promises"
import path from "node:path"

/** A copied skill is a separate file. The source can move on, the copy can
 *  grow its own steps, and an update is a click — not a live link. */

const skillFile = (memoryDir: string, name: string) =>
  path.join(memoryDir, "skills", name, "SKILL.md")

async function writeSkill(memoryDir: string, name: string, description: string, body: string) {
  const file = skillFile(memoryDir, name)
  await fs.mkdir(path.dirname(file), { recursive: true })
  const raw = await fs.readFile(file, "utf8").catch(() => "")
  const kept = raw.match(/^---\n([\s\S]*?)\n---/)
  let front = kept?.[1] ?? `name: ${name}`
  if (/^description:/m.test(front)) {
    front = front.replace(/^description:.*$/m, `description: ${description}`)
  } else {
    front += `\ndescription: ${description}`
  }
  if (!/^name:/m.test(front)) front = `name: ${name}\n${front}`
  await fs.writeFile(file, `---\n${front}\n---\n\n${body}\n`)
}

async function createProject(request: APIRequestContext, name: string) {
  const res = await request.post("/api/projects", { data: { name } })
  expect(res.ok()).toBeTruthy()
  const body = (await res.json()) as { project: { id: string; memory_dir: string; name: string } }
  return body.project
}

async function openSkills(page: Page, projectName: string) {
  await page.getByRole("button", { name: `Project options for ${projectName}` }).click()
  await page.getByRole("menuitem", { name: "View skills" }).click()
  await expect(page.getByRole("tab", { name: "Memory", selected: true })).toBeVisible()
}

test("a copied skill stays independent and can still take an update", async ({ page, request }) => {
  const stamp = Date.now()
  const libraryName = `Library ${stamp}`
  const workName = `Work ${stamp}`
  const skill = `steps-${stamp}`
  const library = await createProject(request, libraryName)
  const work = await createProject(request, workName)
  await writeSkill(
    library.memory_dir,
    skill,
    "when a recorded procedure should be followed again",
    "1. follow the recorded steps",
  )

  await page.goto("/")
  await openSkills(page, libraryName)
  await expect(page.getByText(skill, { exact: true })).toBeVisible()
  await page.getByRole("button", { name: `Copy skill ${skill}` }).click()
  await page.getByLabel("Destination project").click()
  await page.getByRole("option", { name: workName, exact: true }).click()
  await page.getByRole("button", { name: "Copy skill", exact: true }).click()
  await expect(page.getByTestId("skill-copy-notice")).toContainText(workName)

  await openSkills(page, workName)
  await expect(page.getByTestId("skill-origin")).toContainText(libraryName)
  await expect(page.getByText("when a recorded procedure should be followed again")).toBeVisible()

  const marker = `marker-${stamp}`
  await writeSkill(
    library.memory_dir,
    skill,
    `when a recorded procedure should be followed again ${marker}`,
    `1. follow the recorded steps\n2. ${marker}`,
  )
  await page.getByRole("button", { name: "Reload memory" }).click()
  await page.getByRole("button", { name: `Update ${skill} from its source` }).click()
  await expect(page.getByText(new RegExp(marker))).toBeVisible()
  await expect(page.getByTestId("skill-origin")).toContainText(libraryName)

  const local = `local-${stamp}`
  const widened = `widened-${stamp}`
  await writeSkill(
    work.memory_dir,
    skill,
    `when a recorded procedure should be followed again ${local}`,
    `1. follow the recorded steps\n2. ${local}`,
  )
  await writeSkill(
    library.memory_dir,
    skill,
    `when a recorded procedure should be followed again ${widened}`,
    `1. follow the recorded steps\n2. ${widened}`,
  )
  await page.getByRole("button", { name: "Reload memory" }).click()
  await expect(page.getByText(new RegExp(local))).toBeVisible()
  await page.getByRole("button", { name: `Update ${skill} from its source` }).click()
  await expect(page.getByRole("heading", { name: "Replace local edits?" })).toBeVisible()
  await page.getByRole("button", { name: "Cancel" }).click()
  await expect(page.getByText(new RegExp(local))).toBeVisible()
  await page.getByRole("button", { name: `Update ${skill} from its source` }).click()
  await page.getByRole("button", { name: "Replace with source" }).click()
  await expect(page.getByText(new RegExp(widened))).toBeVisible()
  await expect(page.getByText(new RegExp(local))).toHaveCount(0)

  const libraryRaw = await fs.readFile(skillFile(library.memory_dir, skill), "utf8")
  expect(libraryRaw).toContain(widened)
  expect(libraryRaw).not.toContain(local)
})
