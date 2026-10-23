import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { composer, freshConversation } from "./session"

test("a trailing goal marker starts a new objective after the old one completed", async ({ page, request }, info) => {
  await freshConversation(page)
  await composer(page).fill("/goal Inspect the previous operation.")
  await composer(page).press("Enter")
  await expect(page.getByTestId("goal-banner")).toContainText("Done", { timeout: 60_000 })
  await expect(page.getByTestId("status-badge")).toContainText("Idle", { timeout: 60_000 })
  const threadID = await page.locator('[data-testid="thread-row"][aria-current="true"]').getAttribute("data-id")
  expect(threadID).toBeTruthy()
  const objective = "检查新的 assigned operation"
  const activated = page.waitForResponse((response) => response.request().method() === "PATCH" && new URL(response.url()).pathname === `/api/threads/${threadID}`)
  await composer(page).fill(`${objective} /goal  \n`)
  await expect(page.getByRole("button", { name: "Send", exact: true })).toBeEnabled()
  await composer(page).press("Enter")
  const activation = await (await activated).json()
  expect(activation.thread).toMatchObject({ goal: objective, goal_complete: false, goal_blocked: false, goal_capped: false, goal_idle: false })
  await expect(page.getByTestId("goal-banner")).toContainText(objective)
  await expect(page.getByTestId("goal-banner")).toContainText("Done", { timeout: 60_000 })
  await expect(page.getByTestId("status-badge")).toContainText("Idle", { timeout: 60_000 })
  const { turns } = await (await request.get(`/api/threads/${threadID}/turns`)).json()
  const turn = turns.at(-1)
  expect(turn).toMatchObject({ user_text: objective, status: "done" })
  const trace = await (await request.get(`/api/trace/${turn.id}`)).json()
  expect(trace.events.some((event: { kind: string }) => event.kind === "goal_complete")).toBe(true)
  await page.screenshot({ path: info.outputPath("trailing-goal-completed-objective.png"), animations: "disabled" })
  await writeFile(info.outputPath("trailing-goal-evidence.json"), JSON.stringify({ activation, turn, trace }, null, 2), { mode: 0o600 })
})
