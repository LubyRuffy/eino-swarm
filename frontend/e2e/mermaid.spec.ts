import { writeFileSync } from "node:fs"
import { expect, test, type Locator, type Page } from "@playwright/test"

import { reloadOpenConversation } from "./session"

async function openDocument(page: Page, title: string, source: string) {
  const created = await page.request.post("/api/threads", { data: { title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const uploaded = await page.request.post(`/api/threads/${thread.id}/files`, {
    multipart: { files: { name: "diagrams.md", mimeType: "text/markdown", buffer: Buffer.from(source) } },
  })
  expect(uploaded.ok()).toBe(true)
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: title, exact: true }).click()
  await page.getByRole("tab", { name: "Files", exact: true }).click()
  const row = page.getByTestId("file-tree").getByRole("treeitem", { name: "diagrams.md", exact: true })
  await row.click()
  return { preview: page.getByTestId("file-preview"), row }
}

async function decodedDiagram(figure: Locator) {
  const image = figure.getByRole("img", { name: "Mermaid", exact: true })
  await expect(image).toBeVisible()
  await expect.poll(() => image.evaluate((element: HTMLImageElement) =>
    element.complete && element.naturalWidth > 0 && element.naturalHeight > 0,
  )).toBe(true)
  const source = await image.getAttribute("src")
  expect(source).toMatch(/^data:image\/svg\+xml[,;]/)
  const diagram = await image.evaluate((element: HTMLImageElement) => {
    const source = element.src
    const comma = source.indexOf(",")
    const svg = source.slice(0, comma).endsWith(";base64")
      ? atob(source.slice(comma + 1)) : decodeURIComponent(source.slice(comma + 1))
    const document = new DOMParser().parseFromString(svg, "image/svg+xml")
    return {
      source, text: document.documentElement.textContent ?? "",
      id: document.documentElement.id,
      presentation: Array.from(document.querySelectorAll("style"), (style) => style.textContent ?? "").join("\n").replaceAll(document.documentElement.id, "diagram"),
      mathCount: document.querySelectorAll("math").length,
      width: element.naturalWidth, height: element.naturalHeight,
      viewBox: (document.documentElement.getAttribute("viewBox") ?? "").trim().split(/[\s,]+/).map(Number),
      malformed: document.querySelector("parsererror") !== null,
    }
  })
  expect(diagram.viewBox).toHaveLength(4)
  expect(Math.abs(diagram.width - diagram.viewBox[2])).toBeLessThanOrEqual(1)
  expect(Math.abs(diagram.height - diagram.viewBox[3])).toBeLessThanOrEqual(1)
  return diagram
}

test("renders distinct Mermaid diagrams in Markdown files with original source copy and both themes", async ({ page, context }, info) => {
  const origin = new URL(String(info.project.use.baseURL)).origin
  const remote: string[] = []
  page.on("request", (request) => {
    const url = new URL(request.url())
    if (/^https?:$/.test(url.protocol) && url.origin !== origin) remote.push(url.href)
  })
  const graph = 'graph TB\n  subgraph Layer["服务层"]\n    A["Client<br/>请求"] --> B["API"]\n  end'
  const sequence = "sequenceDiagram\n  participant A as Intake\n  participant B as Planning\n  participant C as Execution\n  participant D as Verification\n  participant E as Collection\n  participant F as Completion\n  A->>F: Review\n  A->>F: $$x^2$$\n  F-->>A: Accepted"
  const source = ["# Architecture", "", "```mermaid", graph, "```", "", "```mermaid", sequence, "```"].join("\n")
  const { preview, row } = await openDocument(page, info.title, source)
  if (await page.locator("html").evaluate((element) => element.classList.contains("dark"))) {
    await page.keyboard.press("Escape")
    await page.getByRole("button", { name: "App menu" }).click()
    await page.getByRole("menuitem", { name: "Switch theme" }).click()
    await row.click()
  }
  const diagrams = preview.getByTestId("markdown-mermaid")
  await expect(diagrams).toHaveCount(2)
  const first = await decodedDiagram(diagrams.nth(0))
  const second = await decodedDiagram(diagrams.nth(1))
  expect(first.malformed).toBe(false)
  expect(first.text).toContain("服务层")
  expect(first.text).toContain("Client")
  expect(first.text).toContain("请求")
  expect(second.mathCount).toBeGreaterThan(0)
  expect(second.text).not.toContain("$$x^2$$")
  expect(second.text).toContain("Accepted")
  expect(first.id).toBeTruthy()
  expect(first.presentation).toBeTruthy()
  expect(second.id).not.toBe(first.id)
  await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin: new URL(page.url()).origin })
  await diagrams.first().getByRole("button", { name: "Copy code", exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(graph)
  await diagrams.first().locator("summary").click()
  await expect(diagrams.first().locator("code")).toHaveText(graph)
  await diagrams.first().locator("summary").click()
  for (const width of [1000, 640]) {
    await page.setViewportSize({ width, height: 760 })
    await expect.poll(() => preview.evaluate((element) => element.getAnimations().every((animation) => animation.playState !== "running"))).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
    const bounds = await diagrams.first().boundingBox()
    const outer = await preview.boundingBox()
    expect(bounds!.x).toBeGreaterThanOrEqual(outer!.x)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(outer!.x + outer!.width + 1)
  }
  const scrollport = diagrams.nth(1).getByRole("img", { name: "Mermaid", exact: true }).locator("..")
  expect(await scrollport.evaluate((element) => element.scrollWidth)).toBeGreaterThan(await scrollport.evaluate((element) => element.clientWidth))
  await scrollport.evaluate((element) => { element.scrollLeft = element.scrollWidth })
  expect(await scrollport.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
  writeFileSync(info.outputPath("mermaid-light.png"), await page.screenshot(), { mode: 0o600 })
  await page.keyboard.press("Escape")
  await page.getByRole("button", { name: "App menu" }).click()
  await page.getByRole("menuitem", { name: "Switch theme" }).click()
  await expect(page.locator("html")).toHaveClass(/dark/)
  await row.click()
  const themed = await decodedDiagram(preview.getByTestId("markdown-mermaid").first())
  expect(themed.source).not.toBe(first.source)
  expect(themed.presentation).not.toBe(first.presentation)
  expect(themed.text).toContain("Client")
  expect(remote).toEqual([])
  writeFileSync(info.outputPath("mermaid-dark.png"), await page.screenshot(), { mode: 0o600 })
})

test("keeps invalid Mermaid and ordinary fences readable and isolates diagram HTML and clicks", async ({ page }, info) => {
  const invalid = "graph TB\n  A[unfinished"
  const ordinary = "graph TB\n  Plain --> Text"
  const malicious = 'graph LR\n  A["<img src=\'https://mermaid.invalid/leak\' onerror=\'window.__mermaidExecuted=true\'>"] --> B["Safe"]\n  click B "javascript:window.__mermaidExecuted=true"'
  const source = ["```mermaid", invalid, "```", "", "```text", ordinary, "```", "", "```mermaid", malicious, "```"].join("\n")
  const external: string[] = []
  page.on("request", (request) => { if (new URL(request.url()).hostname === "mermaid.invalid") external.push(request.url()) })
  const { preview } = await openDocument(page, info.title, source)
  await expect(preview.getByTestId("markdown-code")).toHaveCount(2)
  await expect(preview.getByTestId("markdown-code").nth(0).locator("code")).toHaveText(invalid)
  await expect(preview.getByTestId("markdown-code").nth(1).locator("code")).toHaveText(ordinary)
  const diagram = preview.getByTestId("markdown-mermaid")
  await expect(diagram).toHaveCount(1)
  expect((await decodedDiagram(diagram)).text).toContain("Safe")
  await expect(diagram.locator("a, script, iframe, object")).toHaveCount(0)
  await expect(diagram.getByRole("img", { name: "Mermaid", exact: true }).locator("..").locator("svg")).toHaveCount(0)
  expect(await page.evaluate(() => (window as Window & { __mermaidExecuted?: boolean }).__mermaidExecuted)).toBeUndefined()
  expect(external).toEqual([])
})

test("renders a Mermaid label whose identifier ends in url", async ({ page }, info) => {
  const graph = 'flowchart LR\n  A["itemurl(group)"] --> B["Shown"]'
  const { preview } = await openDocument(page, info.title, ["```mermaid", graph, "```"].join("\n"))
  expect((await decodedDiagram(preview.getByTestId("markdown-mermaid"))).text).toContain("Shown")
  await expect(preview.getByTestId("markdown-code")).toHaveCount(0)
})

test("keeps optional Mermaid image metadata as source without fetching its resources", async ({ page }, info) => {
  const resources = [
    'flowchart LR\n  A@{img: "https://mermaid.invalid/plain", label: "Image"}',
    'flowchart LR\n  A@{"img": "https://mermaid.invalid/double", label: "Image"}',
    "flowchart LR\n  A@{'img': 'https://mermaid.invalid/single', label: 'Image'}",
    String.raw`flowchart LR
  A@{"\u0069mg": "https://mermaid.invalid/escaped", label: "Image"}`,
  ]
  const witness = 'flowchart LR\n  A["Checked"] --> B["Ready"]'
  const source = [...resources, witness].map((body) => ["```mermaid", body, "```"].join("\n")).join("\n\n")
  const external: string[] = []
  page.on("request", (request) => { if (new URL(request.url()).hostname === "mermaid.invalid") external.push(request.url()) })
  const { preview } = await openDocument(page, info.title, source)
  // The safe last diagram settles only after the preceding queue tasks ran.
  expect((await decodedDiagram(preview.getByTestId("markdown-mermaid"))).text).toContain("Checked")
  const code = preview.getByTestId("markdown-code")
  await expect(code).toHaveCount(resources.length)
  for (const [index, body] of resources.entries()) await expect(code.nth(index).locator("code")).toHaveText(body)
  expect(external).toEqual([])
})

test("renders an assistant Mermaid answer from named SSE and preserves it through history replay", async ({ page }, info) => {
  const graph = 'flowchart LR\n  Request["Request"] --> Response["Response"]'
  const answer = ["The request flow:", "", "```mermaid", graph, "```"].join("\n")
  const created = await page.request.post("/api/threads", { data: { title: info.title } })
  expect(created.ok()).toBe(true)
  const { thread } = await created.json()
  const base = { thread_id: thread.id, turn_id: "diagram-answer", agent_id: "manager", created_at: "2026-01-01T00:00:00Z" }
  const events = [
    { ...base, seq: 1, kind: "agent_message", text: answer },
    { ...base, seq: 2, kind: "done", text: answer },
  ]
  let released = false, historyReads = 0, streamCount = 0
  let release!: () => void
  const ready = new Promise<void>((resolve) => { release = resolve })
  await page.route(new RegExp(`/api/threads/${thread.id}/log(?:\\?|$)`), async (route) => {
    historyReads++
    const response = await route.fetch()
    const body = await response.json()
    await route.fulfill({ response, json: { ...body, events: released ? events : [], has_more: false } })
  })
  await page.route(new RegExp(`/api/threads/${thread.id}/events(?:\\?|$)`), async (route) => {
    const first = ++streamCount === 1
    if (first) await ready
    await route.fulfill({ contentType: "text/event-stream", body: "retry: 3600000\n\n" + (first ? events.map((event) =>
      `id: ${event.seq}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`,
    ).join("") : "") })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: info.title, exact: true }).click()
  await expect.poll(() => historyReads).toBeGreaterThan(0)
  const beforeLive = historyReads
  await expect(page.getByTestId("transcript").getByTestId("markdown-mermaid")).toHaveCount(0)
  released = true
  release()
  const message = page.getByTestId("assistant-message").filter({ hasText: "The request flow:" })
  expect((await decodedDiagram(message.getByTestId("markdown-mermaid"))).text).toContain("Response")
  expect(historyReads).toBe(beforeLive)
  await reloadOpenConversation(page)
  expect((await decodedDiagram(page.getByTestId("assistant-message").filter({ hasText: "The request flow:" }).getByTestId("markdown-mermaid"))).text).toContain("Response")
})
