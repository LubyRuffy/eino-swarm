import { expect, test } from "@playwright/test"
import { mkdir, readFile, writeFile } from "node:fs/promises"
import { composer, freshConversation, reloadOpenConversation } from "./session"
import { startGenerationInterruptionFixture, startManagerMidturnInterruptionFixture } from "./generation-interruption-fixture"

type Event = { seq: number; kind: string; agent_id?: string; message_id?: string; text?: string }

test("interrupted reasoning retains its cause while the same worker recovers and replays", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_GENERATION_INTERRUPTION !== "1", "requires the keyless generation fixture")
  const fixture = await startGenerationInterruptionFixture()
  let turnID = ""
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "fixture-provider", providers: [{ id: "fixture-provider", label: "Offline generation interruption regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false, composer_predictions: false },
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    await freshConversation(page)
    const accepted = page.waitForResponse((response) => response.request().method() === "POST" && /\/api\/threads\/[^/]+\/steer$/.test(new URL(response.url()).pathname))
    await composer(page).fill("Inspect the assigned inputs.")
    await composer(page).press("Enter")
    turnID = (await (await accepted).json()).turn.id
    await expect.poll(() => fixture.state().workerRequests).toBe(1)
    await page.getByRole("tab", { name: /^Agents(?: \d+)?$/ }).click()
    const workerID = fixture.state().workerID
    expect(workerID).toBeTruthy()
    const worker = page.getByTestId("agent-roster").locator(`[data-agent-id="${workerID}"]`)
    await worker.click()
    const pane = page.getByTestId("agent-scroller")
    await expect(pane.getByTestId("thought-toggle")).toHaveCount(1)
    await expect(pane.locator(".animate-spin")).not.toHaveCount(0)
    fixture.interruptFirst()
    await expect.poll(() => fixture.state().workerRequests).toBe(2)
    const trace = async () => (await (await request.get(`/api/trace/${turnID}`)).json())
    // Fail on missing durable lifecycle before UI assertions so red evidence
    // distinguishes a backend omission from a merely absent notice component.
    await expect.poll(async () => (await trace()).events.filter((event: Event) => event.kind === "generation_interrupted").length).toBe(1)
    await expect(pane.getByTestId("generation-notice")).toHaveCount(1)
    await expect(pane.getByTestId("generation-notice")).toContainText(/output|budget/i)
    await expect(pane.getByTestId("generation-notice")).toContainText(/recover|resum/i)
    const thoughts = pane.getByTestId("thought-toggle")
    await expect(thoughts).toHaveCount(2)
    await expect(thoughts.first().locator(".animate-spin")).toHaveCount(0)
    await expect(thoughts.first()).not.toContainText("Thinking…")
    await expect(page.getByTestId("agent-failure")).toHaveCount(0)
    await page.screenshot({ path: info.outputPath("same-worker-recovering.png"), animations: "disabled" })
    await page.getByRole("button", { name: "Back to agents" }).click()
    await expect(worker.getByLabel("running", { exact: true })).toBeVisible()
    await expect(worker.getByLabel("failed", { exact: true })).toHaveCount(0)
    fixture.finishRecovery()
    await expect.poll(async () => (await trace()).turn.status).toBe("done")
    for (let replay = 0; replay < 2; replay++) {
      await page.getByRole("tab", { name: /^Agents(?: \d+)?$/ }).click()
      await worker.click()
      await expect(pane.getByTestId("generation-notice")).toHaveCount(1)
      await expect(pane.getByTestId("generation-notice")).toContainText(/output|budget/i)
      await expect(pane.getByTestId("generation-notice")).toContainText(/recover|resum/i)
      await expect(pane).toContainText("The assigned inputs were verified after recovery.")
      await expect(pane.locator(".animate-spin")).toHaveCount(0)
      await expect(page.getByTestId("agent-failure")).toHaveCount(0)
      await page.screenshot({ path: info.outputPath(`same-worker-recovered-${replay}.png`), animations: "disabled" })
      if (replay === 0) await reloadOpenConversation(page)
    }
    const saved = await trace()
    const interruptions = saved.events.filter((event: Event) => event.kind === "generation_interrupted") as Event[]
    expect(interruptions).toHaveLength(1)
    expect(interruptions[0].message_id).toBeTruthy()
    expect(JSON.parse(interruptions[0].text ?? "{}")).toMatchObject({ reason: "output_limit" })
    expect(saved.events.some((event: Event) => event.kind === "generation_recovered" && event.agent_id === workerID && event.message_id === interruptions[0].message_id)).toBe(true)
    expect(new Set(saved.events.filter((event: Event) => event.kind === "spawned").map((event: Event) => event.agent_id))).toEqual(new Set([workerID]))
    expect(fixture.errors).toEqual([])
  } catch (error) {
    await page.screenshot({ path: info.outputPath("first-failure.png") }).catch((failure) => info.annotations.push({ type: "artifact", description: String(failure) }))
    throw error
  } finally {
    try {
      if (turnID) await writeFile(info.outputPath("generation-trace.json"), JSON.stringify(await (await request.get(`/api/trace/${turnID}`, { timeout: 5000 })).json(), null, 2))
      await writeFile(info.outputPath("generation-fixture.json"), JSON.stringify({ ...fixture.state(), errors: fixture.errors }, null, 2))
    } catch (error) {
      info.annotations.push({ type: "artifact", description: String(error) })
    } finally {
      await fixture.close()
    }
  }
})

test("a middle-turn manager cap preserves one real tool effect across recovery and fresh client replay", async ({ page, request, browser }, info) => {
  test.skip(process.env.ZWAI_E2E_GENERATION_INTERRUPTION !== "1", "requires the keyless generation fixture")
  const workspace = info.outputPath("manager-workspace")
  await mkdir(workspace, { recursive: true, mode: 0o700 })
  const receiptPath = `${workspace}/effect-receipt.txt`
  const fixture = await startManagerMidturnInterruptionFixture({ receiptPath })
  let threadID = "", turnID = ""
  let previousGroups: Record<string, unknown> | undefined
  const originalStorage = await page.context().storageState()
  const origin = new URL(String(info.project.use.baseURL)).origin
  const originalLocalStorage = originalStorage.origins.find((entry) => entry.origin === origin)?.localStorage ?? []
  let caseFailed = false
  let freshClient: Awaited<ReturnType<typeof browser.newContext>> | undefined
  type ManagerEvent = Event & { tool_call_id?: string; err?: string }
  const trace = async () => (await (await request.get(`/api/trace/${turnID}`)).json())
  const screenshot = async (viewed: typeof page, name: string) => {
    const path = info.outputPath(name)
    await writeFile(path, new Uint8Array(), { mode: 0o600, flag: "wx" })
    await viewed.screenshot({ path, animations: "disabled" })
  }
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    previousGroups = { models: settings.models, swarm: settings.swarm, memory: settings.memory, ui: settings.ui }
    expect((await request.put("/api/settings", { data: {
      models: { default: "manager-recovery-fixture", providers: [{ id: "manager-recovery-fixture", label: "Offline manager generation recovery", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false, composer_predictions: false,
        recover_reasoning_output_limit: true, max_completion_tokens: 16384, policy_reasoning_effort: "high" },
      memory: { ...settings.memory, enabled: false, auto_review: false },
      ui: { ...settings.ui, locale: "en", transcript_mode: "developer" },
    } })).ok()).toBe(true)
    const projectResponse = await request.post("/api/projects", { data: { name: "Manager generation evidence", workdir: workspace } })
    expect(projectResponse.ok()).toBe(true)
    const { project } = await projectResponse.json()
    const threadResponse = await request.post("/api/threads", { data: { title: "Manager generation recovery", project_id: project.id } })
    expect(threadResponse.ok()).toBe(true)
    const { thread } = await threadResponse.json()
    threadID = thread.id
    expect((await request.patch(`/api/threads/${threadID}`, { data: { reasoning_effort: "high" } })).ok()).toBe(true)
    const openProjectThread = async (viewed: typeof page) => {
      await viewed.getByTestId("dest-projects").click()
      const projectButton = viewed.getByRole("button", { name: project.name, exact: true })
      await expect(projectButton).toBeVisible()
      if (await projectButton.getAttribute("aria-expanded") !== "true") await projectButton.click()
      const row = viewed.locator(`[data-testid="thread-row"][data-id="${threadID}"]`)
      await expect(row).toBeVisible()
      await row.getByTestId("row-label").click()
      await expect(viewed.getByTestId("thread-title")).toHaveText(thread.title)
    }
    await page.goto("/")
    await openProjectThread(page)
    const accepted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === `/api/threads/${threadID}/steer`)
    await composer(page).fill("Make one workspace change and preserve its verification evidence.")
    await composer(page).press("Enter")
    turnID = (await (await accepted).json()).turn.id
    await expect.poll(() => fixture.state().managerRequests).toBe(2)
    await expect.poll(() => readFile(receiptPath, "utf8")).toBe(`${fixture.marker}\n`)
    const transcript = page.getByTestId("transcript")
    await expect(transcript.getByTestId("thought-toggle")).toHaveCount(1)
    fixture.interruptCapped()
    await expect.poll(() => fixture.state().managerRequests).toBe(3)
    await expect.poll(async () => (await trace()).events.filter((event: ManagerEvent) => event.kind === "generation_interrupted").length).toBe(1)
    await expect(transcript.getByTestId("generation-notice")).toHaveCount(1)
    await expect(transcript.getByTestId("generation-notice")).toContainText(/output|budget/i)
    await expect(transcript.getByTestId("generation-notice")).toContainText(/recover|resum/i)
    await expect(transcript.getByTestId("thought-toggle")).toHaveCount(2)
    await expect(transcript.getByTestId("thought-toggle").first().locator(".animate-spin")).toHaveCount(0)
    await expect(transcript.getByTestId("output-budget-error")).toHaveCount(0)
    await expect(transcript.getByTestId("transcript-error-retry")).toHaveCount(0)
    const recovering = await trace()
    const interrupted = recovering.events.filter((event: ManagerEvent) => event.kind === "generation_interrupted") as ManagerEvent[]
    const starts = recovering.events.filter((event: ManagerEvent) => event.kind === "generation_started") as ManagerEvent[]
    expect(starts).toHaveLength(3)
    expect(new Set(starts.map((event) => event.message_id)).size).toBe(3)
    expect(interrupted[0].message_id).toBe(starts[1].message_id)
    expect(JSON.parse(interrupted[0].text ?? "{}")).toMatchObject({ reason: "output_limit" })
    expect(recovering.events.filter((event: ManagerEvent) => event.kind === "model_retry")).toHaveLength(1)
    expect(recovering.events.some((event: ManagerEvent) => event.kind === "generation_recovered" && event.message_id === interrupted[0].message_id)).toBe(true)
    await screenshot(page, "manager-recovering.png")
    fixture.finishRecovery()
    await expect.poll(async () => (await trace()).turn.status).toBe("done")
    const saved = await trace()
    const originalTool = saved.events.filter((event: ManagerEvent) => event.kind === "tool_result" && event.tool_call_id === fixture.callID)
    expect(originalTool).toHaveLength(1)
    expect(originalTool[0].text).toContain(fixture.marker)
    const pages: { before: number; events: ManagerEvent[]; has_more: boolean }[] = []
    for (const replay of ["live", "reload", "fresh-client-paged"] as const) {
      let viewed = page
      if (replay === "reload") {
        await page.reload()
        await expect(page.getByRole("heading", { name: "What should we work on?" })).toBeVisible()
        await openProjectThread(page)
      }
      if (replay === "fresh-client-paged") {
        freshClient = await browser.newContext({ baseURL: new URL(page.url()).origin, locale: "en-US", viewport: { width: 1440, height: 900 } })
        viewed = await freshClient.newPage()
        await viewed.route(new RegExp(`/api/threads/${threadID}/log(?:\\?|$)`), async (route) => {
          const url = new URL(route.request().url())
          const before = Number(url.searchParams.get("before") ?? 0)
          url.searchParams.set("limit", "2")
          const response = await route.fetch({ url: url.toString() })
          const body = await response.json()
          pages.push({ before, events: body.events, has_more: body.has_more })
          await route.fulfill({ response })
        })
        await viewed.goto("/")
        await openProjectThread(viewed)
        await expect.poll(() => pages.some((entry) => entry.before > 0 && entry.events.some((event) => event.seq === originalTool[0].seq))).toBe(true)
      }
      const visible = viewed.getByTestId("transcript")
      await expect(visible).toContainText(fixture.answer)
      await expect(visible.getByTestId("generation-notice")).toHaveCount(1)
      await expect(visible.getByTestId("generation-notice")).toContainText(/output|budget/i)
      await expect(visible.getByTestId("generation-notice")).toContainText(/recover|resum/i)
      await expect(visible.getByTestId("thought-toggle")).toHaveCount(2)
      await expect(visible.locator(".animate-spin")).toHaveCount(0)
      await expect(visible.getByTestId("output-budget-error")).toHaveCount(0)
      await expect(visible.getByTestId("transcript-error-retry")).toHaveCount(0)
      if (!(await visible.getByTestId("tool-output").isVisible())) await visible.getByRole("button", { name: /^exec/ }).click()
      await expect(visible.getByTestId("tool-output")).toBeVisible()
      await expect(visible.getByTestId("tool-output")).toContainText(fixture.marker)
      await expect.poll(() => readFile(receiptPath, "utf8")).toBe(`${fixture.marker}\n`)
      await screenshot(viewed, `manager-${replay}.png`)
      const after = await trace()
      expect(after.turn.id).toBe(turnID)
      expect(after.events.filter((event: ManagerEvent) => event.kind === "tool_result" && event.tool_call_id === fixture.callID)).toEqual(originalTool)
      expect(after.events.filter((event: ManagerEvent) => event.kind === "generation_interrupted")).toEqual(saved.events.filter((event: ManagerEvent) => event.kind === "generation_interrupted"))
      expect(fixture.state()).toMatchObject({ managerRequests: 3, originalToolEvidencePreserved: true, exhaustedReasoningAbsent: true })
    }
    expect((await (await request.get(`/api/threads/${threadID}/turns`)).json()).turns).toHaveLength(1)
    expect(fixture.errors).toEqual([])
    await writeFile(info.outputPath("manager-recovery-evidence.json"), JSON.stringify({ saved, fixture: fixture.state(), pages, receipt: await readFile(receiptPath, "utf8") }, null, 2), { mode: 0o600 })
  } catch (error) {
    caseFailed = true
    await screenshot(page, "manager-first-failure.png").catch((failure) => info.annotations.push({ type: "artifact", description: String(failure) }))
    if (turnID) {
      try { await writeFile(info.outputPath("manager-failed-trace.json"), JSON.stringify(await trace(), null, 2), { mode: 0o600 }) }
      catch (failure) { info.annotations.push({ type: "artifact", description: String(failure) }) }
    }
    throw error
  } finally {
    const cleanupErrors: unknown[] = []
    for (const cleanup of [
      async () => {
        if (threadID && turnID) {
          const terminal = ["done", "error", "cancelled"]
          if (!terminal.includes((await trace()).turn.status)) {
            expect((await request.post(`/api/threads/${threadID}/interrupt`, { timeout: 5000 })).ok()).toBe(true)
            await expect.poll(async () => terminal.includes((await trace()).turn.status)).toBe(true)
          }
        }
      },
      async () => { await freshClient?.close() },
      async () => {
        if (new URL(page.url()).origin === origin) {
          const restored = await page.evaluate((entries) => {
            localStorage.clear()
            for (const { name, value } of entries) localStorage.setItem(name, value)
            return Object.entries(localStorage).sort(([a], [b]) => a.localeCompare(b))
          }, originalLocalStorage)
          expect(restored).toEqual(originalLocalStorage.map(({ name, value }) => [name, value]).sort(([a], [b]) => a.localeCompare(b)))
        }
        // Stop this disposable client's effects before restoring background
        // preferences/providers. The suite's independent presence remains held.
        await page.goto("about:blank")
      },
      async () => {
        if (previousGroups) {
          // Settings PATCH semantics omit false booleans from GET; explicitly
          // restore this enabled-only-for-the-case switch to its prior value.
          const swarm = previousGroups.swarm as Record<string, unknown>
          const restoreGroups = { ...previousGroups, swarm: { ...swarm, recover_reasoning_output_limit: swarm.recover_reasoning_output_limit ?? false } }
          expect((await request.put("/api/settings", { data: restoreGroups, timeout: 5000 })).ok()).toBe(true)
          const { settings } = await (await request.get("/api/settings", { timeout: 5000 })).json()
          for (const group of ["models", "swarm", "memory", "ui"]) expect(settings[group]).toEqual(previousGroups[group])
        }
      },
      () => fixture.close(),
    ]) {
      try { await cleanup() }
      catch (failure) { cleanupErrors.push(failure); info.annotations.push({ type: "cleanup", description: String(failure) }) }
    }
    await writeFile(info.outputPath("manager-fixture-final.json"), JSON.stringify({ ...fixture.state(), errors: fixture.errors, settingsAndClientCleanupVerified: cleanupErrors.length === 0, cleanupErrors: cleanupErrors.map(String) }, null, 2), { mode: 0o600 })
    if (!caseFailed && cleanupErrors.length > 0) throw cleanupErrors[0]
  }
})
