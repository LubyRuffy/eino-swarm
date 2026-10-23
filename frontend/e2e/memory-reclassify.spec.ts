import { expect, test } from "@playwright/test"
import fs from "node:fs/promises"
import path from "node:path"

// Both moves run through the real scripted model, memory tools, SSE and Trace.
for (const direction of ["promote", "demote"] as const) {
  test(`memory tidy can ${direction} existing information without losing it`, async ({ page, request }, info) => {
    const { settings } = await (await request.get("/api/settings")).json()
    const projectName = `${direction} ${Date.now()}`
    const created = await request.post("/api/projects", { data: { name: projectName } })
    expect(created.ok()).toBe(true)
    const { project } = await created.json()
    let releaseResponse = () => {}
    try {
      const configured = await request.put("/api/settings", { data: {
        memory: { ...settings.memory, auto_review: false }, ui: { ...settings.ui, locale: "en" },
      } })
      expect(configured.ok()).toBe(true)
      await page.goto("/")
      await page.getByRole("button", { name: `New conversation in ${projectName}` }).click()
      const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
      expect(threadID).toBeTruthy()
      const accepted = page.waitForResponse((response) => response.request().method() === "POST"
        && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
      await page.getByTestId("composer-input").fill("Inspect the available inputs and summarize the result.")
      await page.getByTestId("composer-input").press("Enter")
      const { turn } = await (await accepted).json()
      await expect.poll(async () => {
        const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
        return trace.turn.status
      }).toBe("done")
      await expect(page.getByTestId("status-badge")).toContainText("Idle")

      const retainedText = direction === "promote"
        ? "1. Inspect the supplied inputs.\n2. Verify the visible result."
        : "The work environment preserves the selected locale."
      if (direction === "promote") {
        const initial = await (await request.get(`/api/projects/${project.id}/memory`)).json()
        const saved = await request.put(`/api/projects/${project.id}/memory`, { data: { text: retainedText, rev: initial.memory.memory.rev } })
        expect(saved.ok()).toBe(true)
      } else {
        const dir = path.join(project.memory_dir, "skills", "stored-fact")
        await fs.mkdir(dir, { recursive: true })
        await fs.writeFile(path.join(dir, "SKILL.md"), `---\nname: stored-fact\ndescription: When the saved environment fact is relevant\n---\n\n${retainedText}\n`)
      }
      await page.getByRole("tab", { name: "Memory", exact: true }).click()
      await page.getByRole("button", { name: "Reload memory" }).click()
      await expect(page.getByText(/move misplaced information between notes and skills/)).toBeVisible()

      let markReady = () => {}
      let markFailed = (_error: unknown) => {}
      const responseReady = new Promise<void>((resolve, reject) => { markReady = resolve; markFailed = reject })
      const responseGate = new Promise<void>((resolve) => { releaseResponse = resolve })
      let streamBody = ""
      const tidyPath = `**/api/projects/${project.id}/memory/tidy-skills`
      await page.route(tidyPath, async (route) => {
        // Hold only delivery to the browser. The server/model result is real
        // and fulfilled unchanged after the pending state has been inspected.
        try {
          const response = await route.fetch()
          streamBody = await response.text()
          markReady()
          await responseGate
          await route.fulfill({ response })
        } catch (error) {
          markFailed(error)
          await route.abort()
        }
      })
      await page.getByRole("button", { name: "Tidy memory" }).click()
      await responseReady
      await expect(page.getByRole("progressbar", { name: "Tidy progress" })).toBeVisible()
      await expect(page.getByTestId("tidy-stream")).toContainText("Waiting for the model")
      await expect(page.getByText(direction === "promote" ? "Scanning 0 skills" : "Scanning 1 skills", { exact: true })).toBeVisible()
      await page.screenshot({ path: info.outputPath(`${direction}-pending.png`), animations: "disabled" })
      releaseResponse()
      await expect(page.getByTestId("tidy-status")).toContainText("Memory curated")
      await expect(page.getByTestId("tidy-stats")).toContainText(direction === "promote" ? "1 promoted" : "1 demoted")
      await page.unroute(tidyPath)
      const liveWrites = streamBody.split(/\r?\n/).filter((line) => line.startsWith("data:")).map((line) => JSON.parse(line.slice(5)))
      expect(liveWrites.some((event) => event.phase === "change" && event.action === direction)).toBe(true)

      const { memory } = await (await request.get(`/api/projects/${project.id}/memory`)).json()
      if (direction === "promote") {
        expect(memory.memory.text).toBe("")
        expect(memory.skills).toHaveLength(1)
        const { skill } = await (await request.get(`/api/projects/${project.id}/skills/${memory.skills[0].name}`)).json()
        expect(skill.body.trim()).toBe(retainedText)
      } else {
        expect(memory.memory.text).toBe(retainedText)
        expect(memory.skills).toHaveLength(0)
      }
      await page.screenshot({ path: info.outputPath(`${direction}-result-en.png`), animations: "disabled" })
      const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
      expect(trace.turn).toMatchObject({ id: turn.id, thread_id: threadID, status: "done" })
      const receipts = trace.events.filter((event: { kind: string }) => event.kind === "memory_review").map((event: { text: string }) => JSON.parse(event.text))
      expect(receipts.some((receipt: { changes?: { action: string }[] }) => receipt.changes?.some((change) => change.action === direction))).toBe(true)
      expect(trace.llm_calls.some((call: { agent_id: string }) => call.agent_id === "memory-reviewer")).toBe(true)
      await info.attach(`${direction}-trace`, { body: JSON.stringify(trace, null, 2), contentType: "application/json" })
      await page.getByRole("tab", { name: "Trace", exact: true }).click()
      await page.getByTestId("trace-log-toggle").click()
      await expect(page.getByTestId("trace-log")).toContainText(direction === "promote" ? "promoted from a note" : "demoted to a note")
      await page.screenshot({ path: info.outputPath(`${direction}-trace-en.png`), animations: "disabled" })
      await page.getByRole("button", { name: "App menu" }).click()
      await page.getByRole("menuitem", { name: "Switch language" }).click()
      await expect(page.getByTestId("trace-log")).toContainText(direction === "promote" ? "笔记已提升为技能" : "已降为笔记")
      await page.getByRole("tab", { name: "记忆", exact: true }).click()
      await expect(page.getByRole("button", { name: "整理记忆" })).toBeVisible()
      await page.screenshot({ path: info.outputPath(`${direction}-result-zh.png`), animations: "disabled" })
    } finally {
      releaseResponse()
      await request.put("/api/settings", { data: { memory: settings.memory, ui: settings.ui } })
      await request.delete(`/api/projects/${project.id}`)
    }
  })
}
