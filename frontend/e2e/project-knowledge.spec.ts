import { appendFile, mkdir, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { expect, test, type Page } from "@playwright/test"
import { freshConversation, send, waitForIdle } from "./session"

async function selectProjectConversation(page: Page, name: string, threadId: string) {
  const folder = page.getByRole("button", { name, exact: true })
  await expect(folder).toBeVisible()
  // On reload the folder may already be expanded while its rows are loading.
  if (await folder.getAttribute("aria-expanded") === "false") await folder.click()
  const row = page.locator(`[data-testid="thread-row"][data-id="${threadId}"]`)
  await expect(row).toBeVisible()
  await row.getByTestId("row-label").click()
}

async function openProjectConversation(page: Page, name: string, memoryEnabled = false) {
  const projectResponse = await page.request.post("/api/projects", { data: { name, memory_enabled: memoryEnabled } })
  expect(projectResponse.status()).toBe(201)
  const { project } = await projectResponse.json() as { project: { id: string } }
  const title = "Original conversation"
  const threadResponse = await page.request.post("/api/threads", { data: { project_id: project.id, title } })
  expect(threadResponse.status()).toBe(201)
  const { thread } = await threadResponse.json() as { thread: { id: string } }
  await page.goto("/")
  await selectProjectConversation(page, name, thread.id)
  await expect(page.getByTestId("thread-title")).toHaveText(title)
  return { projectId: project.id, threadId: thread.id, title }
}

const knowledgeTab = (page: Page) => page.getByRole("tab", { name: "Knowledge", exact: true })

test("a conversation outside projects keeps its shared library and explains knowledge admission", async ({ page }) => {
  await freshConversation(page)
  const tabs = page.getByTestId("side-panel").getByRole("tablist")
  await expect(tabs.locator('[data-scope="thread"]')).toHaveText(["Agents", "Files", "Trace", "Work records"])
  await expect(tabs.getByRole("tab", { name: "Memory", exact: true })).toHaveAttribute("data-scope", "project")
  await tabs.getByRole("tab", { name: "Knowledge", exact: true }).click()
  await expect(page.getByTestId("knowledge-panel")).toContainText("Move this conversation into a project")
  await expect(page.getByTestId("knowledge-enable")).toHaveCount(0)
  await expect(page.getByTestId("knowledge-distill")).toHaveCount(0)
})

test("enable, browse, follow links, and return through the project wiki", async ({ page }, info) => {
  const name = `Wiki navigation ${Date.now()}`
  const { projectId, threadId } = await openProjectConversation(page, name)
  const kb = `/api/projects/${projectId}/knowledge`
  const tabs = page.getByTestId("side-panel").getByRole("tablist")
  await expect(tabs.getByRole("tab", { name: "Memory", exact: true })).toHaveAttribute("data-scope", "project")
  await tabs.getByRole("tab", { name: "Knowledge", exact: true }).click()
  const enabled = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === kb)
  await page.getByTestId("knowledge-enable").click()
  expect((await enabled).status()).toBe(200)
  const { data_dir: dataDir } = await (await page.request.get("/api/meta")).json() as { data_dir: string }
  const knowledgeDir = join(dataDir, "projects", projectId, "knowledge")
  const dir = join(knowledgeDir, "pages", "entries")
  await mkdir(dir, { recursive: true })
  await appendFile(join(knowledgeDir, "schema.yaml"), [
    "\nviews:", "  - id: linked", "    root_type: entry", "    follow: [related]", "    depth: 2", "",
  ].join("\n"))
  await writeFile(join(dir, "entry-one.md"), [
    "---", "id: entry-one", "type: entry", "title: First entry", "links:", "  - rel: related", "    to: entry-two",
    "sources:", "  - kind: event", "    thread_id: th_missing", "    seq: 1", "    classification: primary", "---",
    "Read [[entry-two|the next entry]] and [[entry-missing]].", "",
  ].join("\n"))
  await writeFile(join(dir, "entry-two.md"), [
    "---", "id: entry-two", "type: entry", "title: Second entry", "---", "Second entry body.", "",
  ].join("\n"))
  // A tab switch reads the files again, including edits made outside the app.
  await page.getByRole("tab", { name: "Agents", exact: true }).click()
  await knowledgeTab(page).click()
  const tree = page.getByTestId("knowledge-tree")
  await expect(tree.getByRole("button", { name: "First entry" })).toBeVisible()
  await page.getByTestId("knowledge-filter").fill("second")
  await expect(tree.getByRole("button", { name: "First entry" })).toHaveCount(0)
  await expect(tree.getByRole("button", { name: "Second entry" })).toBeVisible()
  await page.getByTestId("knowledge-filter").clear()
  await page.getByTestId("knowledge-view").click()
  await page.getByRole("option", { name: "linked", exact: true }).click()
  await expect(tree.getByText("related", { exact: true })).toBeVisible()
  await expect(tree.getByRole("button", { name: "Second entry", exact: true })).toHaveCount(2)
  await page.getByTestId("knowledge-view").click()
  await page.getByRole("option", { name: "Pending", exact: true }).click()
  await expect(tree).toContainText("entry-missing")
  await tree.getByRole("button", { name: "entry-one" }).click()
  const detail = page.getByTestId("knowledge-page")
  await expect(detail.getByRole("heading", { name: "First entry" })).toBeVisible()
  await expect(detail.getByText("Primary material", { exact: true })).toBeVisible()
  await expect(detail.getByText("Event #1 · Source unavailable", { exact: true })).toBeVisible()
  await detail.getByRole("button", { name: "the next entry" }).click()
  await expect(detail.getByRole("heading", { name: "Second entry" })).toBeVisible()
  await expect(detail.getByRole("heading", { name: "Backlinks" })).toBeVisible()
  await detail.getByRole("button", { name: "Expand preview" }).click()
  await expect(page.getByTestId("knowledge-preview")).toContainText("Second entry body.")
  const screenshot = info.outputPath("knowledge-preview.png")
  await page.screenshot({ path: screenshot, animations: "disabled" })
  await info.attach("knowledge-preview", { path: screenshot, contentType: "image/png" })
  await page.keyboard.press("Escape")
  await expect(page.getByTestId("knowledge-preview")).toHaveCount(0)
  await page.getByTestId("knowledge-back").click()
  await expect(detail.getByRole("heading", { name: "First entry" })).toBeVisible()
  await page.getByTestId("knowledge-back").click()
  await expect(tree).toBeVisible()

  await page.getByRole("tab", { name: "Agents", exact: true }).click()
  const started = page.waitForResponse((response) => response.request().method() === "POST" &&
    new URL(response.url()).pathname === `/api/threads/${threadId}/deliver`)
  await send(page, "Refer to [[entry-two|the wiki reference]].")
  const { turn } = await (await started).json() as { turn: { id: string } }
  await waitForIdle(page)
  await writeFile(join(dir, "entry-two.md"), [
    "---", "id: entry-two", "type: entry", "title: Second entry", `updated_turn: ${turn.id}`,
    `updated_thread: ${threadId}`, "updated_at: 2026-01-01T00:00:00Z", "---", "Second entry body.", "",
  ].join("\n"))
  await page.getByTestId("transcript").getByTestId("knowledge-wikilink").filter({ hasText: "the wiki reference" }).first().click()
  await expect(knowledgeTab(page)).toHaveAttribute("aria-selected", "true")
  await expect(detail.getByRole("heading", { name: "Second entry" })).toBeVisible()
  await detail.getByRole("button", { name: "Open Trace", exact: true }).click()
  await expect(page.getByRole("tab", { name: "Trace", exact: true })).toHaveAttribute("aria-selected", "true")
  expect((await page.request.delete(`/api/projects/${projectId}`)).status()).toBe(204)
})


for (const [scope, value, tab, fullText] of [
  ["Latest turn", "latest_turn", "Memory", false],
  ["Current task", "thread", "Knowledge", false],
  ["Current project", "project", "Knowledge", false],
  ["Current task · Full text", "thread", "Knowledge", true],
  ["Current project · Full text", "project", "Knowledge", true],
] as const) {
  test(`Distill ${scope.toLowerCase()} in place and share its receipt across panels`, async ({ page }) => {
    const { projectId, threadId, title } = await openProjectConversation(page, `Distillation ${value} ${Date.now()}`, true)
    const { settings } = await (await page.request.get("/api/settings")).json()
    expect((await page.request.put("/api/settings", { data: {
      memory: { ...settings.memory, auto_review: false },
    } })).ok()).toBe(true)
    try {
      await send(page, "Keep the observation and retain the reusable procedure.")
      await waitForIdle(page)
      const endpoint = `/api/projects/${projectId}/distill`
      await page.getByRole("tab", { name: tab === "Memory" ? /^Memory/ : "Knowledge" }).click()
      const admitted = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === endpoint)
      await page.getByTestId("knowledge-distill").click()
      await page.getByRole("menuitem", { name: scope, exact: true }).click()
      const reply = await admitted
      expect(reply.status()).toBe(202)
      expect(reply.request().postDataJSON()).toEqual({
        scope: value,
        ...(value === "project" ? {} : { thread_id: threadId }),
        ...(fullText ? { full_text: true } : {}),
      })
      const job = await reply.json()
      const status = page.getByTestId("project-distill-status")
      await expect(status).toContainText("Distillation finished.", { timeout: 45_000 })
      if (fullText) await expect(status.getByRole("status")).toContainText("Full text")
      expect(await (await page.request.get(endpoint)).json()).toMatchObject({
        review_id: job.review_id, state: "completed", thread_id: threadId,
        knowledge: [expect.objectContaining({ action: "write_page" })],
      })
      await expect(page.getByTestId("thread-title")).toHaveText(title)
      await page.getByRole("tab", { name: tab === "Memory" ? "Knowledge" : /^Memory/ }).click()
      await expect(status).toContainText("1 knowledge changes")
      await status.getByRole("button", { name: "Open Trace" }).click()
      await expect(page.getByRole("tab", { name: "Trace", exact: true })).toHaveAttribute("aria-selected", "true")
      expect((await page.request.delete(`/api/projects/${projectId}`)).status()).toBe(204)
    } finally {
      await page.request.put("/api/settings", { data: { memory: settings.memory } })
    }
  })
}

test("Full-text progress shows a partially reviewed record without counting it as complete", async ({ page }) => {
  const { projectId, threadId } = await openProjectConversation(page, `Segment progress ${Date.now()}`, true)
  const path = `/api/projects/${projectId}/distill`
  let started = false
  await page.route(path, async (route) => {
    if (route.request().method() === "POST") {
      expect(route.request().postDataJSON()).toEqual({ scope: "thread", thread_id: threadId, full_text: true })
      started = true
    }
    await route.fulfill({
      json: started ? {
        state: "running", review_id: "saved-full-text", scope: "thread", full_text: true,
        progress: { total: 1, processed: 0, remaining: 1, skipped: 0, batches: 1,
          coverage_known: true, segment_bytes: 24000, segment_total_bytes: 96000 },
      } : { state: "idle" },
    })
  })
  try {
    await knowledgeTab(page).click()
    await page.getByTestId("knowledge-distill").click()
    await page.getByRole("menuitem", { name: "Current task · Full text", exact: true }).click()
    const status = page.getByTestId("project-distill-status")
    await expect(status.getByRole("status")).toHaveText("Distilling history… · Task scope · Full text")
    await expect(status).toContainText("Current record · 23 KB of 94 KB reviewed")
    await expect(status.getByTestId("project-distill-progress")).toHaveText("0 of 1 history records processed · 1 remaining")
    await expect(status.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "0")
  } finally {
    await page.request.delete(`/api/projects/${projectId}`)
  }
})

test("Wiki build shows saved source batches before a long record finishes", async ({ page }) => {
  const { projectId, threadId } = await openProjectConversation(page, `Wiki batch progress ${Date.now()}`, true)
  const endpoint = `/api/projects/${projectId}/distill`
  let started = false
  let accepted = false
  await page.route((url) => url.pathname === endpoint, async (route) => {
    const request = route.request()
    if (request.method() === "POST") {
      expect(request.postDataJSON()).toEqual({ scope: "thread", thread_id: threadId, wiki_graph: true })
      started = true
    } else if (new URL(request.url()).searchParams.get("wiki_graph") !== "true") {
      await route.continue()
      return
    }
    await route.fulfill({ json: started ? {
      state: "running", review_id: "wiki-progress", scope: "thread", wiki_graph: true, wiki_phase: "graph",
      progress: {
        total: 1, processed: 0, remaining: 1, skipped: 0, batches: accepted ? 1 : 0,
        ...(accepted ? { segment_bytes: 1800, segment_total_bytes: 96000 } : {}),
      },
    } : { state: "idle" } })
  })
  try {
    await knowledgeTab(page).click()
    await page.getByTestId("knowledge-build").click()
    await page.getByRole("menuitem", { name: "Current task", exact: true }).click()
    const status = page.getByTestId("project-wiki-build-status")
    await expect(status.getByTestId("wiki-source-batches")).toHaveText("The first source batch is awaiting verification.")
    accepted = true
    await expect(status.getByTestId("wiki-source-batches")).toHaveText("Source batches verified and saved · 1")
    await expect(status.getByTestId("project-distill-progress")).toHaveText("0 of 1 history records processed · 1 remaining")
  } finally {
    await page.request.delete(`/api/projects/${projectId}`)
  }
})

test("Wiki build opens and cites a node from the local graph while review is still pending", async ({ page }) => {
  const { projectId, threadId } = await openProjectConversation(page, `Wiki local scan ${Date.now()}`, true)
  const project = `/api/projects/${projectId}`
  const endpoint = `${project}/distill`
  const node = { id: "found-observation", title: "Retained observation", type: "wiki-node" }
  const quote = "Retained observation appeared in the first source."
  const source = { kind: "event", seq: 114, thread_id: threadId, classification: "primary", available: true }
  const nodes = [
    ...Array.from({ length: 3_000 }, (_, i) => ({ id: `entry-${i}`, title: `Entry ${i}`, type: "wiki-node" })),
    node,
  ]
  let started = false
  let ready = false
  await page.route((url) => url.pathname === endpoint, async (route) => {
    const request = route.request()
    if (request.method() === "POST") {
      expect(request.postDataJSON()).toEqual({ scope: "thread", thread_id: threadId, wiki_graph: true })
      started = true
    } else if (new URL(request.url()).searchParams.get("wiki_graph") !== "true") {
      await route.continue()
      return
    }
    await route.fulfill({ json: started ? {
      state: "running", review_id: "local-scan", scope: "thread", wiki_graph: true, knowledge_enabled: true,
      wiki_phase: ready ? "graph" : "scan", wiki_scan_processed: ready ? 23_252 : 128,
      wiki_scan_complete: ready,
      wiki_scan_deferred: ready ? 1 : 0,
      wiki_documents: ready ? nodes.length : 0, wiki_documented: ready ? 64 : 0,
      progress: { total: 23_252, processed: 0, remaining: 23_252, skipped: 0, batches: 0 },
    } : { state: "idle" } })
  })
  await page.route((url) => url.pathname === `${project}/knowledge/graph`,
    (route) => route.fulfill({ json: { nodes: ready ? nodes : [], edges: [] } }))
  await page.route((url) => url.pathname === `${project}/knowledge/pages/${node.id}`, (route) => route.fulfill({ json: {
    ...node,
    body: "",
    wiki: { kind: "entity", category: "observation", document_pending: true },
    sources: [source],
    wiki_observations: [{ source, quote }],
  } }))
  try {
    expect((await page.request.post(`${project}/knowledge`)).ok()).toBe(true)
    await knowledgeTab(page).click()
    await page.getByTestId("knowledge-build").click()
    await page.getByRole("menuitem", { name: "Current task", exact: true }).click()
    const status = page.getByTestId("project-wiki-build-status")
    await expect(status.getByTestId("wiki-scan-progress")).toHaveText("Scanned 128 of 23252 records")
    await page.getByRole("button", { name: "Graph", exact: true }).click()
    const graph = page.getByRole("dialog", { name: "Knowledge graph" })
    await expect(graph).toContainText("No pages yet.")
    ready = true
    await expect(graph.getByRole("searchbox")).toBeVisible()
    await expect(graph).toContainText("Pages 3001 · Connections 0")
    await graph.getByRole("searchbox").fill(node.title)
    await graph.getByRole("button", { name: new RegExp(node.title) }).click()
    await graph.getByRole("button", { name: `Open ${node.title}` }).click()
    const detail = page.getByTestId("knowledge-page")
    await expect(detail.getByRole("heading", { name: node.title })).toBeVisible()
    await detail.getByText("View cited excerpt").click()
    await expect(detail.getByText(quote)).toBeVisible()
    await expect(status.getByRole("status")).toContainText("Graph ready · review in progress")
    await expect(status.getByTestId("wiki-scan-progress")).toContainText("Large records queued for review · 1")
    await expect(status.getByTestId("project-distill-progress")).toContainText("Reviewed 0 of 23252 records")
    await expect(status.getByTestId("wiki-document-progress")).toHaveText("Documents updated · 64 of 3001")
  } finally {
    await page.request.delete(project)
  }
})

test("Wiki build publishes its graph, documents, and sources independently of distillation", async ({ page }) => {
  const { projectId, threadId } = await openProjectConversation(page, `Wiki build ${Date.now()}`, true)
  const endpoint = `/api/projects/${projectId}/distill`
  const original = "Retained observation for the graph."
  try {
    await send(page, original)
    await waitForIdle(page)
    await knowledgeTab(page).click()
    const admitted = page.waitForResponse((response) => response.request().method() === "POST" &&
      new URL(response.url()).pathname === endpoint)
    await page.getByTestId("knowledge-build").click()
    await page.getByRole("menuitem", { name: "Current task", exact: true }).click()
    const response = await admitted
    expect(response.status()).toBe(202)
    expect(response.request().postDataJSON()).toEqual({ scope: "thread", thread_id: threadId, wiki_graph: true })
    const status = page.getByTestId("project-wiki-build-status")
    await expect(status.getByRole("status")).toContainText("Wiki graph and documents are ready.")
    await expect(status.getByRole("progressbar", { name: "Wiki document progress" })).toHaveAttribute("aria-valuenow", "100")
    const graph = await (await page.request.get(`/api/projects/${projectId}/knowledge/graph`)).json()
    // An automatic review may already own the unqualified title. The
    // graph build preserves that page and gives its own node a distinct ID.
    const node = graph.nodes.find((entry: { title: string; type: string }) =>
      entry.type === "wiki-node" && (entry.title === original || entry.title.startsWith(`${original} (`)))
    expect(node).toBeTruthy()
    const article = await (await page.request.get(`/api/projects/${projectId}/knowledge/pages/${node.id}`)).json()
    expect(article).toMatchObject({
      wiki: { kind: "entity", document_pending: false },
      body: expect.stringContaining(original),
      sources: [expect.objectContaining({ available: true, thread_id: threadId })],
    })
    expect(await (await page.request.get(endpoint)).json()).toEqual({ state: "idle" })
  } finally {
    await page.request.delete(`/api/projects/${projectId}`)
  }
})

test("automatic review updates the open knowledge panel and the original Trace", async ({ page }) => {
  const { settings } = await (await page.request.get("/api/settings")).json()
  const { projectId, threadId } = await openProjectConversation(page, `Automatic knowledge ${Date.now()}`, true)
  try {
    expect((await page.request.put("/api/settings", { data: {
      memory: { ...settings.memory, enabled: true, auto_review: true, notifications: "on" },
    } })).ok()).toBe(true)
    await knowledgeTab(page).click()
    await page.getByTestId("knowledge-enable").click()
    const tree = page.getByTestId("knowledge-tree")
    await expect(tree).toContainText("No pages yet.")
    const observed = "Record this observation with its verification procedure."
    const delivery = page.waitForResponse((response) => response.request().method() === "POST" &&
      new URL(response.url()).pathname === `/api/threads/${threadId}/deliver`)
    await send(page, observed)
    const { turn } = await (await delivery).json() as { turn: { id: string } }
    // Observe live delivery and file refresh before a history or Trace query can hide a missing event.
    await expect(page.getByTestId("memory-notice")).toContainText("Knowledge updated", { timeout: 30_000 })
    await expect(tree.getByRole("button", { name: observed, exact: true })).toBeVisible()
    const receipt = await (await page.request.get(`/api/trace/${turn.id}`)).json()
    const reviews = receipt.events.filter((event: { kind: string }) => event.kind === "memory_review")
    expect(reviews).toHaveLength(1)
    expect(JSON.parse(reviews[0].text)).toMatchObject({
      project_id: projectId, changed: true, notes: { add: 1 },
      skills: [expect.objectContaining({ action: "create" })],
      knowledge: [expect.objectContaining({ action: "write_page", title: observed })],
    })
  } finally {
    await page.request.put("/api/settings", { data: { memory: settings.memory } })
    await page.request.delete(`/api/projects/${projectId}`)
  }
})

test("Distillation shows the saved batch progress and continues after reopening", async ({ page }) => {
  const name = `Distillation progress ${Date.now()}`
  const { projectId, threadId } = await openProjectConversation(page, name, true)
  const path = `/api/projects/${projectId}/distill`
  const reviewId = "saved-review"
  let state = "idle"
  let processed = 0
  const requests: unknown[] = []
  await page.route(path, async (route) => {
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON()
      requests.push(body)
      if (body.scope === "thread") {
        state = "running"
        processed = 4
      } else if (body.pause_review_id === reviewId) {
        state = "pausing"
      } else if (body.resume_review_id === reviewId) {
        state = "running"
      }
    }
    await route.fulfill({
      json: state === "idle" ? { state } : {
        state, review_id: reviewId, scope: "thread",
        ...(state === "failed" ? { error: "the model sent no first byte within 30s", error_code: "first_byte_timeout" } : {}),
        progress: { total: 9, processed, remaining: 9 - processed, skipped: 2, batches: processed === 9 ? 2 : 1,
          coverage_known: true, excerpted: 1, oversized_skipped: 1 },
      },
    })
  })
  const status = page.getByTestId("project-distill-status")
  const panelAction = page.getByTestId("knowledge-distill")
  const progress = page.getByTestId("project-distill-progress")
  const bar = status.getByRole("progressbar", { name: "Distillation progress" })
  try {
    await knowledgeTab(page).click()
    await page.getByTestId("knowledge-distill").click()
    await page.getByRole("menuitem", { name: "Current task", exact: true }).click()
    await expect(progress).toHaveText("4 of 9 history records processed · 5 remaining")
    await expect(status.getByRole("status")).toHaveText("Distilling history… · Task scope")
    await expect(status).toContainText("Records reviewed as excerpts only · 1")
    await expect(status).toContainText("Records skipped above the excerpt mode read limit · 1")
    await expect(status).toContainText("1 processed records were unavailable or ineligible as evidence.")
    await expect(bar).toHaveAttribute("aria-valuenow", "44.44")
    await expect(status).not.toContainText("Distillation finished")
    await status.getByRole("button", { name: "Pause distillation" }).click()
    await expect(status.getByRole("status")).toHaveText("Pausing distillation… · Task scope")
    await expect(panelAction).toBeDisabled()
    state = "paused"
    await expect(status.getByRole("status")).toHaveText("Distillation paused. · Task scope")
    await expect(panelAction).toBeEnabled()
    await page.reload()
    await selectProjectConversation(page, name, threadId)
    if (await knowledgeTab(page).getAttribute("aria-selected") !== "true") await knowledgeTab(page).click()
    await expect(progress).toHaveText("4 of 9 history records processed · 5 remaining")
    await expect(panelAction).toHaveText("Resume distillation")
    await panelAction.click()
    await expect(status.getByRole("status")).toHaveText("Distilling history… · Task scope")
    await expect(progress).toHaveText("4 of 9 history records processed · 5 remaining")
    state = "failed"
    await expect(status.getByRole("alert")).toContainText("Increase the provider’s first-byte timeout")
    await expect(bar).toHaveAttribute("aria-valuenow", "44.44")
    await status.getByRole("button", { name: "Configure first-byte timeout" }).click()
    await page.getByRole("dialog").getByRole("button", { name: "Back to app" }).click()
    await status.getByRole("button", { name: "Resume distillation" }).click()
    await expect(status.getByRole("status")).toHaveText("Distilling history… · Task scope")
    processed = 9
    state = "completed"
    await expect(progress).toHaveText("9 of 9 history records processed · 0 remaining")
    await expect(status).toContainText("Distillation finished")
    await expect(bar).toHaveAttribute("aria-valuenow", "100")
    expect(requests).toEqual([
      { scope: "thread", thread_id: threadId },
      { pause_review_id: reviewId },
      { resume_review_id: reviewId },
      { resume_review_id: reviewId },
    ])
  } finally {
    await page.unroute(path)
    await page.request.delete(`/api/projects/${projectId}`)
  }
})

test("The project graph connects titled pages and opens a node found by alias", async ({ page }) => {
  const titles = ["Release checklist", "Review criteria", "Standalone note"]
  await page.addInitScript((labels) => {
    const visible: Record<string, boolean> = {}
    Object.assign(window, { visibleKnowledgeLabels: visible })
    const fill = CanvasRenderingContext2D.prototype.fillText
    CanvasRenderingContext2D.prototype.fillText = function (text, x, y, maxWidth) {
      if (maxWidth === undefined) fill.call(this, text, x, y)
      else fill.call(this, text, x, y, maxWidth)
      if (!labels.includes(text) || !this.canvas.closest('[data-testid="knowledge-graph-canvas"]')) return
      const { a, d, e, f } = this.getTransform()
      const textWidth = this.measureText(text).width * a
      const left = x * a + e - (this.textAlign === "right" ? textWidth : 0)
      visible[text] = left >= 0 && left + textWidth <= this.canvas.width && y * d + f > 0 && y * d + f < this.canvas.height
    }
  }, titles)
  const { projectId } = await openProjectConversation(page, `Wiki graph ${Date.now()}`)
  const base = `/api/projects/${projectId}/knowledge`
  try {
    expect((await page.request.post(base)).ok()).toBe(true)
    const { data_dir: dataDir } = await (await page.request.get("/api/meta")).json() as { data_dir: string }
    const entries = join(dataDir, "projects", projectId, "knowledge", "pages", "entries")
    await mkdir(entries, { recursive: true })
    await Promise.all([
      writeFile(join(entries, "first.md"), "---\nid: first\ntype: entry\ntitle: Release checklist\naliases: [Ready list]\nlinks:\n  - rel: related\n    to: second\n---\n[[second]] and [[missing]].\n"),
      writeFile(join(entries, "second.md"), "---\nid: second\ntype: entry\ntitle: Review criteria\n---\n[[first]]\n"),
      writeFile(join(entries, "isolated.md"), "---\nid: isolated\ntype: entry\ntitle: Standalone note\n---\nAn independent page.\n"),
    ])
    await knowledgeTab(page).click()
    const response = page.waitForResponse((reply) => new URL(reply.url()).pathname === base + "/graph")
    await page.getByRole("button", { name: "Graph", exact: true }).click()
    const { nodes, edges } = await (await response).json()
    expect(nodes.map((node: { title: string }) => node.title).sort()).toEqual(titles)
    expect(edges).toEqual([
      { source: "first", target: "second", relations: ["related"], wikilink: true },
    ])
    const dialog = page.getByRole("dialog", { name: "Knowledge graph" })
    await expect(dialog).toContainText("Pages 3 · Connections 1")
    const host = dialog.getByTestId("knowledge-graph-canvas")
    await expect(host.locator("canvas")).toBeVisible()
    await dialog.getByRole("button", { name: "Fit the whole knowledge graph" }).click()
    await expect.poll(() => page.evaluate((labels) => {
      const shown = (window as Window & { visibleKnowledgeLabels?: Record<string, boolean> }).visibleKnowledgeLabels
      return labels.every((label) => shown?.[label])
    }, titles)).toBe(true)
    await dialog.screenshot({ path: test.info().outputPath("knowledge-graph.png"), animations: "disabled" })

    await dialog.getByRole("searchbox", { name: "Find a page in the graph" }).fill("ready list")
    await dialog.getByRole("button", { name: /Release checklist.*entry/ }).click()
    await expect(dialog.getByRole("button", { name: "Open Release checklist" })).toBeVisible()

    const box = await host.boundingBox()
    if (!box) throw new Error("graph canvas has no layout bounds")
    const center = { x: box.x + box.width / 2, y: box.y + box.height / 2 }
    await expect.poll(async () => {
      await page.mouse.move(center.x - 4, center.y)
      await page.mouse.move(center.x, center.y)
      return host.evaluate((element) => element.style.cursor)
    }).toBe("pointer")
    await page.mouse.click(center.x, center.y)

    await expect(dialog).toBeHidden()
    await expect(page.getByTestId("knowledge-page").getByRole("heading", { name: "Release checklist" })).toBeVisible()
  } finally {
    await page.request.delete(`/api/projects/${projectId}`)
  }
})
