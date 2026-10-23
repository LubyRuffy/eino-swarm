import { expect, test } from "@playwright/test"
import fs from "node:fs/promises"
import path from "node:path"

// Denying removal only in this case's skill parent lets the real tool save
// the destination note, then fail source cleanup through the offline graph.
test("Trace preserves a saved destination note when source cleanup fails", async ({ page, request }, info) => {
  test.skip(process.platform === "win32" || process.getuid?.() === 0, "requires an ordinary POSIX user for the owned-directory permission fixture")
  const { settings } = await (await request.get("/api/settings")).json()
  const name = `Partial tidy ${Date.now()}`
  const created = await request.post("/api/projects", { data: { name } })
  expect(created.ok()).toBe(true)
  const { project } = await created.json()
  const skillsDir = path.join(project.memory_dir, "skills")
  const retained = "The work environment preserves the selected locale."
  try {
    const configured = await request.put("/api/settings", { data: {
      memory: { ...settings.memory, auto_review: false }, ui: { ...settings.ui, locale: "en" },
    } })
    expect(configured.ok()).toBe(true)
    await page.goto("/")
    await page.getByRole("button", { name: `New conversation in ${name}` }).click()
    const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
    const accepted = page.waitForResponse((response) => response.request().method() === "POST"
      && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
    await page.getByTestId("composer-input").fill("Inspect the available inputs and summarize the result.")
    await page.getByTestId("composer-input").press("Enter")
    const { turn } = await (await accepted).json()
    await expect.poll(async () => (await (await request.get(`/api/trace/${turn.id}`)).json()).turn.status).toBe("done")
    await expect(page.getByTestId("status-badge")).toContainText("Idle")

    const source = path.join(skillsDir, "stored-fact")
    await fs.mkdir(source, { recursive: true })
    await fs.writeFile(path.join(source, "SKILL.md"), `---\nname: stored-fact\ndescription: When the saved environment fact is relevant\n---\n\n${retained}\n`)
    await fs.chmod(skillsDir, 0o555)
    await page.getByRole("tab", { name: "Memory", exact: true }).click()
    await page.getByRole("button", { name: "Reload memory" }).click()
    await page.getByRole("button", { name: "Tidy memory" }).click()
    await expect(page.getByTestId("tidy-status")).toContainText("could not be removed")
    await expect(page.getByTestId("tidy-status")).toContainText("Saved changes")
    await expect(page.getByTestId("tidy-status")).toContainText("Added note")
    await expect(page.getByTestId("tidy-stats")).toContainText("0 demoted")
    await expect(page.getByLabel("Project notes")).toHaveValue(retained)
    await page.screenshot({ path: info.outputPath("partial-tidy-saved-note.png"), animations: "disabled" })

    const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
    const receipts = trace.events.filter((event: { kind: string }) => event.kind === "memory_review").map((event: { text: string }) => JSON.parse(event.text))
    const partial = receipts.find((receipt: { err?: string; changes?: { target: string; action: string }[] }) => receipt.err && receipt.changes?.some((change) => change.target === "memory" && change.action === "add"))
    expect(partial).toBeDefined()
    expect(partial.notes ?? {}).toEqual({})
    expect(partial.changes.some((change: { action: string }) => change.action === "demote")).toBe(false)
    await info.attach("partial-write-trace", { body: JSON.stringify(trace, null, 2), contentType: "application/json" })
    await page.getByRole("tab", { name: "Trace", exact: true }).click()
    await page.getByTestId("trace-log-toggle").click()
    await expect(page.getByTestId("trace-log")).toContainText("Memory review failed")
    await expect(page.getByTestId("trace-log")).not.toContainText("Notes were not saved")
    await page.screenshot({ path: info.outputPath("partial-tidy-trace-en.png"), animations: "disabled" })
    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch language" }).click()
    await expect(page.getByTestId("trace-log")).toContainText("记忆复盘失败")
    await expect(page.getByTestId("trace-log")).not.toContainText("本次笔记未保存")
    await page.screenshot({ path: info.outputPath("partial-tidy-trace-zh.png"), animations: "disabled" })
  } finally {
    await fs.chmod(skillsDir, 0o755).catch(() => {})
    await request.put("/api/settings", { data: { memory: settings.memory, ui: settings.ui } })
    await request.delete(`/api/projects/${project.id}`)
  }
})
