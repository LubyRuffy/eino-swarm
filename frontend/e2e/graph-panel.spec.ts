import { expect, test, type Locator, type Page } from "@playwright/test"
import type { GraphSubject, GraphView } from "../src/lib/graph/types"

type Scene = { id: string; ring: string; x: number; y: number }
type Stage = HTMLDivElement & { __graphScene?: () => Scene[] }

const subject = (id: string, kind: string, name: string, extra: Partial<GraphSubject> = {}): GraphSubject =>
  ({ id, kind, name, attrs: {}, status: "confirmed", version: 1, updated_at: "2026-01-01T00:00:00Z", ...extra })

/** One revision of a graph the way the API serves it. */
function graphAt(owner: string, revision: number): GraphView {
  const later = revision > 1
  return {
    owner_id: owner, revision, total: 4, more: false,
    subjects: [
      subject("s-org", "company", "Example Org"),
      subject("s-ip", "ip", "203.0.113.7", { found_by_todo: "t-find" }),
      subject("s-host", "domain", "host.example.test", { found_by_todo: "t-find" }),
      subject("s-old", "domain", "old.example.test", { status: "retracted" }),
    ],
    links: [
      { id: "l1", from: "s-org", to: "s-ip", rel: "owns", found_by_todo: "t-find" },
      { id: "l2", from: "s-org", to: "s-host", rel: "owns", found_by_todo: "t-find" },
      { id: "l3", from: "s-org", to: "s-old", rel: "was" },
    ],
    todos: [
      { id: "t-find", title: "Find the company's assets", about: "s-org", status: "done", position: 0, version: 1 },
      { id: "t-check", title: "Check the address", about: "s-ip", status: later ? "done" : "open", position: 1, version: later ? 2 : 1 },
      { id: "t-fail", title: "Verify the old name", about: "s-old", status: "failed", result: "host unreachable", position: 2, version: 1 },
    ],
  }
}

/** Serve a mutable graph and let the test push `graph_updated` over the real
 *  SSE listener, as the engine's poll does. */
async function serveGraph(page: Page, thread: string) {
  let revision = 1
  await page.addInitScript(() => {
    const Native = window.EventSource
    const active = new Set<EventSource>()
    class TrackedSource extends Native {
      constructor(url: string | URL, options?: EventSourceInit) { super(url, options); active.add(this) }
      close() { active.delete(this); super.close() }
    }
    window.EventSource = TrackedSource
    Object.assign(window, {
      emitGraphUpdated: (id: string, rev: number) => {
        for (const source of active) if (new URL(source.url).pathname === `/api/threads/${id}/events`)
          source.dispatchEvent(new MessageEvent("graph_updated", { data: JSON.stringify({ thread_id: id, revision: rev }) }))
      },
    })
  })
  await page.route(`**/api/threads/${thread}/graph?preview=1`, (route) => route.fulfill({ json: graphAt(thread, revision) }))
  return async (next: number) => {
    revision = next
    await page.evaluate(([id, rev]) => (window as unknown as { emitGraphUpdated: (a: string, b: number) => void }).emitGraphUpdated(id as string, rev as number), [thread, next])
  }
}

const scene = (stage: Locator) => stage.evaluate((el) => (el as Stage).__graphScene?.() ?? [])

async function ringOf(stage: Locator, id: string) {
  return (await scene(stage)).find((node) => node.id === id)?.ring
}

test("the work-record panel draws the graph, its rings and to-dos, and follows revisions", async ({ page, request }) => {
  const response = await request.post("/api/threads", { data: { title: "Graph panel fixture" } })
  await expect(response).toBeOK()
  const { thread } = await response.json()
  const publish = await serveGraph(page, thread.id)
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: "Graph panel fixture", exact: true }).click()
  await page.getByRole("tab", { name: "Work records", exact: true }).click()

  const panel = page.getByTestId("graph-panel")
  await expect(panel).toBeVisible()
  const stage = panel.getByTestId("graph-stage")
  await expect.poll(async () => (await scene(stage)).length).toBe(4)
  // A real canvas, not an empty placeholder: force-graph painted something.
  await expect(stage.locator("canvas")).toBeVisible()
  const painted = await stage.locator("canvas").evaluate((canvas) => {
    const c = canvas as HTMLCanvasElement
    const data = c.getContext("2d")!.getImageData(0, 0, c.width, c.height).data
    for (let i = 3; i < data.length; i += 4) if (data[i] !== 0) return true
    return false
  })
  expect(painted).toBe(true)

  // Rings come from the to-dos: settled by a done to-do, pending by an open one,
  // retracted greyed. Nothing runs here, so nothing flows.
  await expect.poll(() => ringOf(stage, "s-org")).toBe("settled")
  expect(await ringOf(stage, "s-ip")).toBe("pending")
  expect(await ringOf(stage, "s-host")).toBe("unanalyzed")
  expect(await ringOf(stage, "s-old")).toBe("retracted")
  await expect(stage).toHaveAttribute("data-flowing", "false")

  const todos = panel.getByRole("list", { name: "To-dos" })
  await expect(todos.getByRole("button")).toHaveCount(3)
  await expect(todos.getByText("Find the company's assets")).toHaveClass(/line-through/)
  await expect(todos.getByText("Check the address")).not.toHaveClass(/line-through/)
  await expect(todos.getByText("host unreachable")).toBeVisible()

  // Clicking a to-do marks what it touched; clicking again clears it.
  const find = todos.getByRole("button", { name: /Find the company's assets/ })
  await find.click()
  await expect(find).toHaveAttribute("aria-pressed", "true")
  await find.click()
  await expect(find).toHaveAttribute("aria-pressed", "false")

  // Clicking a subject on the canvas filters the list to it and shows its details.
  // The layout keeps moving while it cools; click once the node holds still,
  // after a hover so the canvas has picked it.
  const box = (await stage.boundingBox())!
  const at = async () => (await scene(stage)).find((node) => node.id === "s-ip")!
  let last = await at()
  await expect.poll(async () => {
    const now = await at()
    const still = Math.hypot(now.x - last.x, now.y - last.y) < 0.5
    last = now
    return still
  }, { intervals: [300] }).toBe(true)
  await page.mouse.move(box.x + last.x - 2, box.y + last.y - 2)
  await page.mouse.move(box.x + last.x, box.y + last.y)
  // force-graph repaints its hit-test canvas on a throttle; the pointer cursor
  // is the sign that it has found the node under the mouse.
  await expect.poll(() => stage.evaluate((el) => (el as HTMLElement).style.cursor)).toBe("pointer")
  await page.mouse.click(box.x + last.x, box.y + last.y)
  await expect(panel.getByRole("region", { name: "203.0.113.7" })).toBeVisible()
  await expect(todos.getByRole("button")).toHaveCount(2)
  await panel.getByRole("button", { name: /To-dos for 203.0.113.7/ }).click()
  await expect(todos.getByRole("button")).toHaveCount(3)

  // A new revision arrives over SSE: the open to-do settles and its ring with it.
  await publish(2)
  await expect.poll(() => ringOf(stage, "s-ip")).toBe("settled")
  await expect(todos.getByText("Check the address")).toHaveClass(/line-through/)
})

test("an open work-record panel follows a project move and never clears the new owner on old confirmation", async ({ page, request }) => {
  const projectResponse = await request.post("/api/projects", { data: { name: "Graph destination" } })
  const threadResponse = await request.post("/api/threads", { data: { title: "Moving graph fixture" } })
  await expect(projectResponse).toBeOK()
  await expect(threadResponse).toBeOK()
  const { project } = await projectResponse.json()
  const { thread } = await threadResponse.json()
  const threadPath = `/api/threads/${thread.id}`
  const owners = new Map<string, number>()
  await page.route(`**${threadPath}/graph?preview=1`, async (route) => {
    const response = await route.fetch()
    if (!response.ok()) {
      await route.fulfill({ response })
      return
    }
    // Keep the live server's owner and revision. A label makes the change of
    // owner visible on two otherwise empty graphs. The SSE frames are real.
    const graph: GraphView = await response.json()
    owners.set(graph.owner_id, graph.revision)
    const title = graph.owner_id === thread.id ? "Review standalone findings" : "Review project findings"
    await route.fulfill({ response, json: {
      ...graph, todos: [{ id: graph.owner_id, title, status: "open", position: 0, version: 1 }],
    } })
  })
  await page.goto("/")
  await page.getByTestId("dest-chats").click()
  await page.getByRole("button", { name: "Moving graph fixture", exact: true }).click()
  await page.getByRole("tab", { name: "Work records", exact: true }).click()
  const panel = page.getByTestId("graph-panel")
  await expect(panel.getByText("Review standalone findings")).toBeVisible()
  await page.getByRole("button", { name: "Replay into the graph" }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toBeVisible()
  await page.route(`**${threadPath}/graph/extract?*`, async (route) => {
    // Move after the click has chosen its target and before the server admits it.
    await expect(await request.patch(threadPath, { data: { project_id: project.id } })).toBeOK()
    await route.continue()
  }, { times: 1 })
  const clearResponse = page.waitForResponse((response) =>
    response.request().method() === "POST" && new URL(response.url()).pathname === `${threadPath}/graph/extract`)
  await dialog.getByRole("button", { name: "Clear, then write" }).click()
  const refusal = await clearResponse
  const clearQuery = new URL(refusal.url()).searchParams
  expect(clearQuery.get("clear")).toBe("1")
  expect(clearQuery.get("owner_id")).toBe(thread.id)
  expect(refusal.status()).toBe(409)
  expect(await refusal.json()).toMatchObject({ code: "graph_conflict" })
  await expect(panel.getByText("Review project findings")).toBeVisible()
  await expect(panel.getByText("Review standalone findings")).toHaveCount(0)
  expect(owners.get(thread.id)).toBe(0)
  expect(owners.get(project.id)).toBe(0)
})
