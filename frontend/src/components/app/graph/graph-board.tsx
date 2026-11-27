import { X } from "lucide-react"
import { useMemo, useState } from "react"
import { ringStates, todoFootprint, todosForSubject } from "../../../lib/graph/analysis"
import { kindSlot } from "../../../lib/graph/kinds"
import type { GraphView as GraphData } from "../../../lib/graph/types"
import type { GraphTranslate } from "../../../lib/messages-graph"
import { GraphView, type ViewLink, type ViewNode } from "./graph-view"
import { SubjectDetail } from "./subject-detail"
import { TodoList } from "./todo-list"

/** The graph with its details and to-dos, drawn from one snapshot. The desktop
 *  panel and the phone screen differ only in how they fetch it. */
export function GraphBoard({ view, running, visible = true, connected = true, t }: {
  view: GraphData
  running: boolean
  visible?: boolean
  connected?: boolean
  t: GraphTranslate
}) {
  const [selected, setSelected] = useState<string | null>(null)
  const [focusedTodo, setFocusedTodo] = useState<string | null>(null)
  const subjects = view.subjects
  const todos = useMemo(() => [...view.todos].sort((a, b) => a.position - b.position), [view.todos])
  const rings = useMemo(() => ringStates(subjects, todos, running), [subjects, todos, running])
  const nodes = useMemo<ViewNode[]>(() => subjects.map((subject) => ({
    id: subject.id, label: subject.name, slot: kindSlot(subject.kind), ring: rings.get(subject.id) ?? "unanalyzed",
  })), [subjects, rings])
  const links = useMemo<ViewLink[]>(() => {
    const retracted = new Set(subjects.filter((subject) => subject.status === "retracted").map((subject) => subject.id))
    return view.links.map((link) => ({ id: link.id, from: link.from, to: link.to, retracted: retracted.has(link.from) || retracted.has(link.to) }))
  }, [subjects, view.links])
  const names = useMemo(() => new Map(subjects.map((subject) => [subject.id, subject.name])), [subjects])
  const highlighted = useMemo(() => {
    const todo = todos.find((candidate) => candidate.id === focusedTodo)
    return todo ? todoFootprint(todo, subjects, view.links) : new Set<string>()
  }, [todos, focusedTodo, subjects, view.links])
  const selectedSubject = subjects.find((subject) => subject.id === selected)
  const shownTodos = selectedSubject ? todosForSubject(selectedSubject, todos) : todos

  return (
    <div className="flex min-h-0 flex-col gap-3 overflow-y-auto p-3" data-testid="graph-panel">
      <p className="m-0 text-[11px] text-muted-foreground">
        {t("graph.counts", { subjects: view.total, links: view.links.length })}
        {view.more ? ` · ${t("graph.truncated", { shown: subjects.length, total: view.total })}` : ""}
      </p>
      {!connected ? <p role="status" className="m-0 text-[11px] text-muted-foreground">{t("graph.disconnected")}</p> : null}
      <GraphView nodes={nodes} links={links} highlighted={highlighted} selected={selected} active={visible}
        text={{ graph: t("graph.graph"), empty: t("graph.empty"), fit: t("graph.fit"), zoomIn: t("graph.zoomIn"), zoomOut: t("graph.zoomOut") }}
        onSelect={(id) => { setSelected(id); if (id) setFocusedTodo(null) }} />
      {selectedSubject ? (
        <SubjectDetail subject={selectedSubject} ring={rings.get(selectedSubject.id) ?? "unanalyzed"} t={t} onClose={() => setSelected(null)} />
      ) : null}
      <section aria-label={t("graph.todos")} className="flex flex-col gap-1.5">
        <h3 className="m-0 flex items-center gap-2 text-xs font-medium">
          {t("graph.todos")}
          {selectedSubject ? (
            <button type="button" onClick={() => setSelected(null)} title={t("graph.filterClear")}
              className="inline-flex max-w-full items-center gap-1 rounded-full border bg-muted px-2 py-0.5 text-[11px] font-normal">
              <span className="truncate">{t("graph.filterOn", { name: selectedSubject.name })}</span>
              <X className="size-3 shrink-0" aria-hidden />
            </button>
          ) : null}
        </h3>
        <TodoList todos={shownTodos} subjectNames={names} focused={focusedTodo} running={running} t={t}
          onFocus={(id) => { setFocusedTodo(id); if (id) setSelected(null) }} />
      </section>
    </div>
  )
}
