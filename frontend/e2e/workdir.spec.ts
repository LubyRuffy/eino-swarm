import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import path from "node:path"
import { expect, test, type Page } from "@playwright/test"

let directory: string
let selected: string

test.beforeAll(() => {
  directory = mkdtempSync(path.join(tmpdir(), "zwai-workdir-e2e-"))
  selected = path.join(directory, "目录 with spaces")
  mkdirSync(selected)
  writeFileSync(path.join(directory, "ordinary-file.txt"), "fixture")
})
test.afterAll(() => rmSync(directory, { recursive: true, force: true }))

async function nativeBridge(page: Page) {
  await page.addInitScript(() => {
    const host = window as Window & {
      _wails?: { invoke: (message: string) => void }
      directoryRequest?: { requestId: string; action: string }
    }
    host._wails = {
      invoke: (message) => {
        if (message.startsWith("{")) host.directoryRequest = JSON.parse(message)
      },
    }
  })
}

async function finishChoice(page: Page, result: { path?: string; error?: string }) {
  await expect.poll(() => page.evaluate(() =>
    Boolean((window as Window & { directoryRequest?: unknown }).directoryRequest),
  )).toBe(true)
  await page.evaluate((response) => {
    const host = window as Window & { directoryRequest?: { requestId: string } }
    window.dispatchEvent(new CustomEvent("zwai:directory-result", {
      detail: { ...host.directoryRequest, ...response },
    }))
    delete host.directoryRequest
  }, result)
}

test("selecting a folder creates and edits a project with the real path", async ({ page, request }) => {
  await nativeBridge(page)
  await page.goto("/")
  await page.getByRole("button", { name: "New project" }).click()
  const name = `Directory choice ${Date.now()}`
  await page.getByLabel("Name", { exact: true }).fill(name)
  await page.getByRole("button", { name: "Choose directory", exact: true }).click()
  await expect(page.getByRole("button", { name: "Create project", exact: true })).toBeDisabled()
  await finishChoice(page, { path: selected })
  await expect(page.getByLabel("Working directory", { exact: true })).toHaveValue(selected)
  await page.getByRole("button", { name: "Create project", exact: true }).click()
  await expect(page.getByRole("dialog")).toBeHidden()
  await expect(page.getByRole("button", { name, exact: true })).toBeVisible()
  const projects = await (await request.get("/api/projects")).json()
  expect(projects.projects.find((p: { name: string }) => p.name === name)).toMatchObject({ workdir: selected, resolved_workdir: selected })
  await page.getByRole("button", { name: `Project options for ${name}` }).click()
  await page.getByRole("menuitem", { name: "Edit", exact: true }).click()
  await page.getByRole("button", { name: "Choose directory", exact: true }).click()
  await finishChoice(page, { path: directory })
  await page.getByRole("button", { name: "Save", exact: true }).click()
  await expect(page.getByRole("dialog")).toBeHidden()
  const edited = await (await request.get("/api/projects")).json()
  expect(edited.projects.find((p: { name: string }) => p.name === name)).toMatchObject({ workdir: directory })
})

test("cancel and a native error preserve the path, and files are still refused by the server", async ({ page }) => {
  await nativeBridge(page)
  await page.goto("/")
  await page.getByRole("button", { name: "New project" }).click()
  await page.getByLabel("Name", { exact: true }).fill(`Directory errors ${Date.now()}`)
  const input = page.getByLabel("Working directory", { exact: true })
  await input.fill(selected)
  await page.getByRole("button", { name: "Choose directory", exact: true }).click()
  await finishChoice(page, { path: "" })
  await expect(input).toHaveValue(selected)
  await page.getByRole("button", { name: "Choose directory", exact: true }).click()
  await finishChoice(page, { error: "selection unavailable" })
  await expect(page.getByRole("alert")).toContainText("selection unavailable")
  await expect(input).toHaveValue(selected)
  await input.fill(path.join(directory, "ordinary-file.txt"))
  await page.getByRole("button", { name: "Create project", exact: true }).click()
  await expect(input).toHaveAttribute("aria-invalid", "true")
  await expect(page.getByRole("dialog")).toBeVisible()
})

test("a browser does not expose a directory picker even with a desktop query", async ({ page }) => {
  await page.goto("/?shell=desktop")
  await page.getByRole("button", { name: "New project" }).click()
  await expect(page.getByRole("button", { name: "Choose directory", exact: true })).toHaveCount(0)
  await expect(page.getByLabel("Working directory", { exact: true })).toBeVisible()
})

for (const siblingExists of [false, true]) {
  test(`the selected directory keeps its trailing whitespace when trimmed sibling exists=${siblingExists}`, async ({ page, request }) => {
    test.skip(process.platform === "win32", "Windows aliases trailing spaces instead of exposing distinct directories")
    const sibling = path.join(directory, siblingExists ? "with-sibling" : "without-sibling")
    const exact = `${sibling}  `
    mkdirSync(exact)
    if (siblingExists) mkdirSync(sibling)
    await nativeBridge(page)
    await page.goto("/")
    await page.getByRole("button", { name: "New project" }).click()
    const name = `Exact directory ${siblingExists} ${Date.now()}`
    await page.getByLabel("Name", { exact: true }).fill(name)
    await page.getByRole("button", { name: "Choose directory", exact: true }).click()
    await finishChoice(page, { path: exact })
    await expect(page.getByLabel("Working directory", { exact: true })).toHaveValue(exact)
    await page.getByRole("button", { name: "Create project", exact: true }).click()
    await expect(page.getByRole("dialog")).toBeHidden()
    const projects = await (await request.get("/api/projects")).json()
    const created = projects.projects.find((p: { name: string }) => p.name === name)
    expect(created).toMatchObject({ workdir: exact, resolved_workdir: exact })

    await page.getByRole("button", { name: `Project options for ${name}` }).click()
    await page.getByRole("menuitem", { name: "Edit", exact: true }).click()
    await expect(page.getByLabel("Working directory", { exact: true })).toHaveValue(exact)
    await page.getByLabel("Working directory", { exact: true }).fill(directory)
    await page.getByRole("button", { name: "Choose directory", exact: true }).click()
    await finishChoice(page, { path: exact })
    await page.getByRole("button", { name: "Save", exact: true }).click()
    await expect(page.getByRole("dialog")).toBeHidden()
    const edited = await (await request.get(`/api/projects/${created.id}`)).json()
    expect(edited.project).toMatchObject({ workdir: exact, resolved_workdir: exact })
  })
}

test("a missing exact selection is refused without falling back or discarding the draft", async ({ page, request }) => {
  test.skip(process.platform === "win32", "Windows aliases trailing spaces instead of exposing a distinct sibling")
  // A directory can disappear between native selection and Save. An existing
  // similarly named sibling must not turn that race into a successful save.
  const existingSibling = path.join(directory, "missing-selection")
  mkdirSync(existingSibling)
  const missingSelection = `${existingSibling} `
  mkdirSync(missingSelection)
  await nativeBridge(page)
  await page.goto("/")
  await page.getByRole("button", { name: "New project" }).click()
  const name = `Missing exact directory ${Date.now()}`
  await page.getByLabel("Name", { exact: true }).fill(name)
  await page.getByRole("button", { name: "Choose directory", exact: true }).click()
  await finishChoice(page, { path: missingSelection })
  rmSync(missingSelection, { recursive: true })
  await page.getByRole("button", { name: "Create project", exact: true }).click()
  await expect(page.getByLabel("Working directory", { exact: true })).toHaveAttribute("aria-invalid", "true")
  await expect(page.getByLabel("Working directory", { exact: true })).toHaveValue(missingSelection)
  await expect(page.getByRole("dialog")).toBeVisible()
  const projects = await (await request.get("/api/projects")).json()
  expect(projects.projects.find((p: { name: string }) => p.name === name)).toBeUndefined()
})

test("manual paths and blank managed workspaces remain available in the browser", async ({ page, request }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "New project" }).click()
  const name = `Manual workspace ${Date.now()}`
  await page.getByLabel("Name", { exact: true }).fill(name)
  await page.getByLabel("Working directory", { exact: true }).fill(selected)
  await page.getByRole("button", { name: "Create project", exact: true }).click()
  await expect(page.getByRole("dialog")).toBeHidden()
  const projects = await (await request.get("/api/projects")).json()
  const created = projects.projects.find((p: { name: string }) => p.name === name)
  expect(created).toMatchObject({ workdir: selected, resolved_workdir: selected })
  await page.getByRole("button", { name: `Project options for ${name}` }).click()
  await page.getByRole("menuitem", { name: "Edit", exact: true }).click()
  await page.getByLabel("Working directory", { exact: true }).fill(" \t ")
  await page.getByRole("button", { name: "Save", exact: true }).click()
  await expect(page.getByRole("dialog")).toBeHidden()
  const edited = await (await request.get(`/api/projects/${created.id}`)).json()
  expect(edited.project.workdir).toBe("")
  expect(edited.project.resolved_workdir).not.toBe(selected)
  expect(edited.project.resolved_workdir).not.toBe("")
})
