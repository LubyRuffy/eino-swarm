import { act, renderHook } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { useTaskPanels } from "@/store/task-panels"
import catalog from "@/task-panels/catalog.json"
import type { TaskPanel } from "@/task-panels/types"
import { useRegisteredInspectorAutoOpen } from "./inspector-panels"

vi.mock("@/lib/task-panel-events", () => ({
  subscribeTaskPanelEvents: () => () => {},
}))

const definition = {
  id: "progress", version: "1", name: "Progress", description: "", entry: "", schema: {},
}
const panel: TaskPanel = {
  id: "progress", thread_id: "first", plugin_id: definition.id, plugin_version: definition.version,
  revision: 1, state: {}, definition,
}

afterEach(() => vi.unstubAllGlobals())

it("automatically opens only a present instance owned by the selected conversation", () => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 404 })))
  const open = vi.fn()
  const view = renderHook(({ threadId }) => useRegisteredInspectorAutoOpen(threadId, open), {
    initialProps: { threadId: "first" },
  })
  const update = (state: Partial<ReturnType<typeof useTaskPanels.getState>>) => act(() => useTaskPanels.setState(state))

  update({ panels: [panel], automaticPanelId: "missing" })
  update({ threadId: "second", panels: [panel], automaticPanelId: panel.id })
  update({ threadId: "first", panels: [{ ...panel, id: "foreign", thread_id: "second" }], automaticPanelId: "foreign" })
  expect(open).not.toHaveBeenCalled()

  update({ panels: [panel], automaticPanelId: panel.id })
  expect(open.mock.calls).toEqual([["task-panels"]])
  update({ panels: [{ ...panel, revision: 2 }], connected: false })
  update({ connected: true })
  update({ panels: [] })
  update({ panels: [panel] })
  expect(open).toHaveBeenCalledTimes(1)

  view.rerender({ threadId: "second" })
  update({ threadId: "first", panels: [panel], automaticPanelId: panel.id })
  expect(open).toHaveBeenCalledTimes(1)
  view.unmount()
})

it("keeps host-only project collaboration out of the embedded agent plugin catalog", () => {
  expect(catalog.map((entry) => entry.id)).not.toContain("graph")
})
