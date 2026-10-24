import { expect, request as createRequest, test, type APIRequestContext, type Page, type TestInfo } from "@playwright/test"
import { writeFile } from "node:fs/promises"

import { composer, freshConversation, reloadOpenConversation } from "./session"

const selectedThread = (page: Page) => page.locator('[data-testid="thread-row"][aria-current="true"]')
const threadPath = (id: string) => `/api/threads/${id}`
const draftPath = (id: string) => `/api/composer/drafts/${encodeURIComponent(`thread:${id}`)}`
type TraceEvent = { seq: number; kind: string; text: string; tool_call_id?: string }
type Trace = { turn: { id: string; user_text: string }; events: TraceEvent[] }

function observeCommands(page: Page, id: string) {
  const requests: { path: string; body: unknown }[] = []
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname
    if (request.method() === "POST" && ["turns", "steer", "answers", "followups"].some((action) => path === `${threadPath(id)}/${action}`)) {
      requests.push({ path, body: request.postDataJSON() })
    }
  })
  return requests
}

async function startQuestion(request: APIRequestContext, id: string) {
  const response = await request.post(`${threadPath(id)}/turns`, {
    data: { text: "/plan Inspect the original assigned operation" },
  })
  expect(response.status()).toBe(202)
  const { turn } = await response.json()
  await expect.poll(async () => (await (await request.get(threadPath(id))).json()).status.awaiting_answer).toBe(true)
  const trace = await (await request.get(`/api/trace/${turn.id}`)).json() as Trace
  const ask = trace.events.find((event) => event.kind === "tool_call" && event.text.startsWith("ask_user("))
  expect(ask?.tool_call_id).toBeTruthy()
  return { turn, trace, ask: ask! }
}

async function expectOriginalQuestion(request: APIRequestContext, id: string, original: Awaited<ReturnType<typeof startQuestion>>) {
  const snapshot = await (await request.get(threadPath(id))).json()
  expect(snapshot.status).toMatchObject({ running: true, awaiting_answer: true, turn_id: original.turn.id })
  const { turns } = await (await request.get(`${threadPath(id)}/turns`)).json()
  expect(turns).toHaveLength(1)
  expect(turns[0].id).toBe(original.turn.id)
  const trace = await (await request.get(`/api/trace/${original.turn.id}`)).json() as Trace
  expect(trace.events.find((event) => event.seq === original.ask.seq)).toEqual(original.ask)
  expect(trace.events.filter((event) => event.kind === "tool_result" && event.tool_call_id === original.ask.tool_call_id)).toEqual([])
  expect(trace.events.filter((event) => event.kind === "user_message")).toHaveLength(1)
  return { snapshot, trace }
}

async function retainEvidence(info: TestInfo, value: unknown) {
  await writeFile(info.outputPath("command-admission-evidence.json"), JSON.stringify(value, null, 2), { mode: 0o600 })
}

async function cleanupQuestion(request: APIRequestContext, id: string, info: TestInfo) {
  const receipt: Record<string, unknown> = { thread_id: id }
  try {
    const before = await (await request.get(threadPath(id))).json()
    receipt.before = before.status
    if (before.status.running) {
      const response = await request.post(`${threadPath(id)}/interrupt`)
      receipt.interrupt_status = response.status()
      expect(response.status()).toBe(202)
    }
    await expect.poll(async () => (await (await request.get(threadPath(id))).json()).status.running).toBe(false)
    receipt.after = (await (await request.get(threadPath(id))).json()).status
  } catch (error) {
    receipt.error = String(error)
    throw error
  } finally {
    await writeFile(info.outputPath("command-cleanup.json"), JSON.stringify(receipt, null, 2), { mode: 0o600 })
    await request.dispose()
  }
}

test("a plan arriving after another turn starts keeps its draft and never answers that turn", async ({ page, request }, info) => {
  await freshConversation(page)
  const id = (await selectedThread(page).getAttribute("data-id"))!
  const command = "/plan Inspect the later assigned operation"
  const observed = observeCommands(page, id)
  const cleanup = await createRequest.newContext({ baseURL: info.project.use.baseURL })
  let original: Awaited<ReturnType<typeof startQuestion>> | undefined
  let occupied = false
  await page.route(`**${threadPath(id)}/turns`, async (route) => {
    if (route.request().method() !== "POST" || occupied) { await route.continue(); return }
    occupied = true
    original = await startQuestion(request, id)
    await route.continue()
  })
  const refused = page.waitForResponse((response) => response.request().method() === "POST"
    && new URL(response.url()).pathname === `${threadPath(id)}/turns`)
  try {
    await composer(page).fill(command)
    await composer(page).press("Enter")
    const response = await refused
    expect(response.status()).toBe(409)
    expect(response.request().postDataJSON()).toEqual({ text: command })
    await expect(composer(page)).toHaveValue(command)
    await expect.poll(async () => (await (await request.get(draftPath(id))).json()).draft.state).toBe("uncertain")
    const draft = (await (await request.get(draftPath(id))).json()).draft
    expect(draft.text).toBe(command)
    const after = await expectOriginalQuestion(request, id, original!)
    expect(after.snapshot.thread).toMatchObject({ plan_mode: true, goal: "" })
    expect(observed).toEqual([{ path: `${threadPath(id)}/turns`, body: { text: command } }])
    await reloadOpenConversation(page)
    await expect(composer(page)).toHaveValue(command)
    await expect(page.getByTestId("ask-card")).toBeVisible()
    await page.screenshot({ path: info.outputPath("plan-race-draft-retained.png"), animations: "disabled" })
    await retainEvidence(info, { id, command, observed, original, draft, after, rejection: await response.json() })
  } finally {
    await cleanupQuestion(cleanup, id, info)
  }
})

test("a captured goal target entering a question keeps that question and leaves the selected conversation idle", async ({ page, request }, info) => {
  await freshConversation(page)
  const id = (await selectedThread(page).getAttribute("data-id"))!
  const objective = "Inspect the newly assigned objective"
  const command = `/goal ${objective}`
  const observed = observeCommands(page, id)
  const cleanup = await createRequest.newContext({ baseURL: info.project.use.baseURL })
  let releaseDraft!: () => void
  let sawSending!: () => void
  const sending = new Promise<void>((resolve) => { sawSending = resolve })
  const release = new Promise<void>((resolve) => { releaseDraft = resolve })
  let held = false
  await page.route(`**${draftPath(id)}`, async (route) => {
    if (!held && route.request().method() === "PUT" && route.request().postData()?.includes('"state":"sending"')) {
      held = true
      const response = await route.fetch()
      sawSending()
      await release
      await route.fulfill({ response })
    } else await route.continue()
  })
  let original: Awaited<ReturnType<typeof startQuestion>> | undefined
  const metadata: unknown[] = []
  try {
    await composer(page).fill(command)
    await expect(page.getByTestId("composer-draft-status")).toContainText("Draft saved on this computer")
    await page.getByRole("button", { name: "Send", exact: true }).click()
    await sending
    await page.getByRole("button", { name: "New conversation", exact: true }).click()
    await expect(selectedThread(page)).not.toHaveAttribute("data-id", id)
    const selected = (await selectedThread(page).getAttribute("data-id"))!
    expect(selected).not.toBe(id)
    await page.route(`**${threadPath(id)}`, async (route) => {
      if (route.request().method() !== "GET") { await route.continue(); return }
      const response = await route.fetch()
      const snapshot = await response.json()
      metadata.push(snapshot)
      if (metadata.length === 1) {
        expect(snapshot.status.running).toBe(false)
        original = await startQuestion(request, id)
      }
      await route.fulfill({ response, json: snapshot })
    })
    const accepted = page.waitForResponse((response) => response.request().method() === "POST"
      && new URL(response.url()).pathname === `${threadPath(id)}/steer`)
    releaseDraft()
    const response = await accepted
    expect(response.status()).toBe(202)
    await expect.poll(async () => (await (await request.get(draftPath(id))).json()).draft.text).toBe("")
    const after = await expectOriginalQuestion(request, id, original!)
    expect(after.snapshot.thread.goal).toBe(objective)
    expect(metadata.length).toBeGreaterThan(0)
    expect(observed).toEqual([{ path: `${threadPath(id)}/steer`, body: { text: command } }])
    const other = await (await request.get(threadPath(selected))).json()
    expect(other.status.running).toBe(false)
    await expect(selectedThread(page)).toHaveAttribute("data-id", selected)
    await expect(page.getByTestId("status-badge")).toContainText("Idle")
    await page.locator(`[data-testid="thread-row"][data-id="${id}"]`).getByTestId("row-label").click()
    await expect(page.getByTestId("ask-card")).toBeVisible()
    await expect(page.getByTestId("goal-banner")).toContainText(objective)
    await expect(composer(page)).toHaveValue("")
    await page.screenshot({ path: info.outputPath("goal-race-question-retained.png"), animations: "disabled" })
    await retainEvidence(info, { id, selected, command, observed, metadata, original, after, other })
  } finally {
    releaseDraft?.()
    await cleanupQuestion(cleanup, id, info)
  }
})
