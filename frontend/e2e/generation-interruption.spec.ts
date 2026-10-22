import { expect, test } from "@playwright/test"
import { writeFile } from "node:fs/promises"
import { composer, freshConversation, reloadOpenConversation } from "./session"
import { startGenerationInterruptionFixture } from "./generation-interruption-fixture"

type Event = { seq: number; kind: string; agent_id?: string; message_id?: string; text?: string }

test("interrupted reasoning retains its cause while the same worker recovers and replays", async ({ page, request }, info) => {
  test.skip(process.env.ZWAI_E2E_GENERATION_INTERRUPTION !== "1", "requires the keyless generation fixture")
  const fixture = await startGenerationInterruptionFixture()
  let turnID = ""
  try {
    const { settings } = await (await request.get("/api/settings")).json()
    expect((await request.put("/api/settings", { data: {
      models: { default: "fixture-provider", providers: [{ id: "fixture-provider", label: "Offline generation interruption regression", base_url: fixture.url, model: "fixture-model", api_key: "", context_window: 128000 }] },
      swarm: { ...settings.swarm, auto_title: false },
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
