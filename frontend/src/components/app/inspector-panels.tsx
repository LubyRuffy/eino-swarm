import { useEffect, type ReactNode } from "react"
import type { Project, Turn } from "@/lib/types"
import type { Translate } from "@/lib/use-t"
import { useTaskPanelConnection, useTaskPanels } from "@/store/task-panels"
import { panelText } from "@/task-panels/messages"
import type { TaskPanel } from "@/task-panels/types"
import { GraphPanel } from "./graph/graph-panel"
import { TaskPanels } from "./task-panels"

interface TaskPanelProjection {
  threadId?: string
  panels: readonly TaskPanel[]
}

interface InspectorContext {
  threadId?: string
  project?: Project
  dataGeneration?: number
  graph?: boolean
  taskPanels: TaskPanelProjection
}

interface InspectorView {
  visible: boolean
  latestTurn?: Pick<Turn, "id" | "status">
  openFiles: () => void
  selectAgent: (id: string) => void
}

interface InspectorBinding {
  ownerId: string
  scope: "project" | "thread"
  activation: "project_mode" | "thread_open" | "agent_open"
  render: (view: InspectorView) => ReactNode
}

type InspectorRegistration = {
  id: string
  label: (t: Translate) => string
  bind: (context: InspectorContext) => InspectorBinding | undefined
} & (
  | { renderer: "react"; mount: "active" }
  | { renderer: "iframe"; mount: "retained" }
)

const registrations = [
  {
    id: "task-panels",
    renderer: "iframe",
    mount: "retained",
    label: (t) => panelText(t.locale, "tab"),
    bind: ({ threadId, taskPanels }) => {
      if (!threadId || taskPanels.threadId !== threadId || !taskPanels.panels.some((panel) => panel.thread_id === threadId)) return
      return {
        ownerId: threadId,
        scope: "thread",
        activation: "agent_open",
        render: ({ visible }) => <TaskPanels threadId={threadId} visible={visible} />,
      }
    },
  },
  {
    id: "graph",
    renderer: "react",
    mount: "active",
    label: (t) => t("graph.tab"),
    bind: ({ project, threadId, dataGeneration, graph }) => {
      if (graph === false || (!threadId && !project)) return
      return {
        ownerId: threadId ?? project!.id,
        scope: threadId ? "thread" : "project",
        activation: threadId ? "thread_open" : "project_mode",
        render: ({ visible, latestTurn }) => (
          <GraphPanel
            key={JSON.stringify([threadId, project?.id, dataGeneration])}
            threadId={threadId}
            projectId={threadId ? undefined : project?.id}
            running={latestTurn?.status === "running"}
            visible={visible}
          />
        ),
      }
    },
  },
] as const satisfies readonly InspectorRegistration[]

export type RegisteredInspectorTab = (typeof registrations)[number]["id"]

export function isRegisteredInspectorTab(tab: string): tab is RegisteredInspectorTab {
  return registrations.some((registration) => registration.id === tab)
}

export function resolveInspectorPanels(context: InspectorContext) {
  return registrations.flatMap((registration) => {
    const binding = registration.bind(context)
    return binding ? [{ ...registration, ...binding }] : []
  })
}

export function useRegisteredInspectorAutoOpen(threadId: string | undefined, open: (tab: RegisteredInspectorTab) => void) {
  const signal = useTaskPanelConnection(threadId)
  useEffect(() => {
    const taskPanels = useTaskPanels.getState()
    if (!threadId || signal.threadId !== threadId || taskPanels.threadId !== threadId || !signal.automaticPanelId ||
      taskPanels.automaticPanelId !== signal.automaticPanelId ||
      !taskPanels.panels.some((panel) => panel.id === signal.automaticPanelId && panel.thread_id === threadId)) return
    const automatic = resolveInspectorPanels({ threadId, taskPanels }).find(
      (panel) => panel.activation === "agent_open" && panel.ownerId === threadId,
    )
    if (automatic) open(automatic.id)
  }, [threadId, open, signal.threadId, signal.automaticPanelId])
}
