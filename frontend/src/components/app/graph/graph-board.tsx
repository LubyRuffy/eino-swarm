import { Maximize2, X } from "lucide-react"
import { useMemo, useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../../ui/dialog"
import { ringStates, todoFootprint, todosForSubject } from "../../../lib/graph/analysis"
import { kindSlot } from "../../../lib/graph/kinds"
import { explanationName } from "../../../lib/graph/names"
import type { GraphSubject, GraphView as GraphData } from "../../../lib/graph/types"
import type { GraphTranslate } from "../../../lib/messages-graph"
import { GraphView, type ViewLink, type ViewNode } from "./graph-view"
import { SubjectDetail } from "./subject-detail"
import { TodoList } from "./todo-list"

/** The graph with its details and to-dos, drawn from one snapshot. The desktop
 *  panel and the phone screen differ only in how they fetch it. An other
 *  subject whose name explains the work is not a node; that text belongs on
 *  a to-do. A subject with no stored link is not drawn. A real subject stays
 *  when its only link ends at one of those names. Full screen opens the same
 *  picture large enough to read. */
export function GraphBoard({ view, running, visible = true, connected = true, t }: {
  view: GraphData
  running: boolean
  visible?: boolean
  connected?: boolean
  t: GraphTranslate
}) {
  const [selected, setSelected] = useState<string | null>(null)
  const [focusedTodo, setFocusedTodo] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(false)
  const subjects = useMemo(() => {
    const named = view.subjects.filter((subject) => subject.kind !== "other" || !explanationName(subject.name))
    const stored = new Set(view.subjects.map((subject) => subject.id))
    const kept = new Set(named.map((subject) => subject.id))
    const linked = new Set<string>()
    for (const link of view.links) {
      if (!stored.has(link.from) || !stored.has(link.to) || link.from === link.to) continue
      if (kept.has(link.from)) linked.add(link.from)
      if (kept.has(link.to)) linked.add(link.to)
    }
    return named.filter((subject) => linked.has(subject.id))
  }, [view.subjects, view.links])
  const hidden = subjects.length !== view.subjects.length
  const links = useMemo(() => {
    const ids = new Set(subjects.map((subject) => subject.id))
    return view.links.filter((link) => ids.has(link.from) && ids.has(link.to))
  }, [subjects, view.links])
  const todos = useMemo(() => [...view.todos].sort((a, b) => a.position - b.position), [view.todos])
  const picture = usePicture(subjects, links, todos, running, focusedTodo)
  const selectedSubject = subjects.find((subject) => subject.id === selected)
  const shownTodos = selectedSubject ? todosForSubject(selectedSubject, todos) : todos
  const select = (id: string | null) => { setSelected(id); if (id) setFocusedTodo(null) }
  const focus = (id: string | null) => { setFocusedTodo(id); if (id) setSelected(null) }

  const canvas = (large: boolean) => (
    <GraphView nodes={picture.nodes} links={picture.edges} highlighted={picture.highlighted} selected={selected}
      active={visible && (large ? expanded : !expanded)} fill={large} labelAll={large}
      text={{ graph: t("graph.graph"), empty: t("graph.empty"), fit: t("graph.fit"), zoomIn: t("graph.zoomIn"), zoomOut: t("graph.zoomOut") }}
      onSelect={select} />
  )
  const details = (
    <>
      {selectedSubject ? (
        <SubjectDetail subject={selectedSubject} ring={picture.rings.get(selectedSubject.id) ?? "unanalyzed"} t={t} onClose={() => setSelected(null)} />
      ) : null}
      <TodoSection todos={shownTodos} names={picture.names} selected={selectedSubject} focused={focusedTodo} running={running} t={t}
        onClear={() => setSelected(null)} onFocus={focus} />
    </>
  )

  return (
    <div className="flex min-h-0 flex-col gap-3 overflow-y-auto p-3" data-testid="graph-panel">
      <div className="flex items-center justify-between gap-2">
        <p className="m-0 text-[11px] text-muted-foreground">
          {t("graph.counts", { subjects: hidden ? subjects.length : view.total, links: hidden ? links.length : view.links.length })}
          {view.more ? ` · ${t("graph.truncated", { shown: subjects.length, total: view.total })}` : ""}
        </p>
        <button type="button" className="grid size-7 place-items-center rounded-md border bg-card text-muted-foreground hover:border-primary hover:text-primary"
          aria-label={t("graph.expand")} title={t("graph.expand")} onClick={() => setExpanded(true)}>
          <Maximize2 className="size-4" aria-hidden />
        </button>
      </div>
      {!connected ? <p role="status" className="m-0 text-[11px] text-muted-foreground">{t("graph.disconnected")}</p> : null}
      {canvas(false)}
      {!expanded && details}
      <Dialog open={expanded} onOpenChange={setExpanded}>
        <DialogContent className="flex h-[94dvh] w-[96vw] max-w-none flex-col gap-0 overflow-hidden p-0">
          <header className="shrink-0 border-b px-5 py-4 pr-12">
            <DialogTitle>{t("graph.graph")}</DialogTitle>
            <DialogDescription className="sr-only">{t("graph.expand")}</DialogDescription>
          </header>
          <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 overflow-y-auto p-4 md:grid-cols-[minmax(0,1.7fr)_minmax(280px,0.9fr)] md:overflow-hidden">
            <div className="min-h-[70dvh] md:min-h-0">{canvas(true)}</div>
            <div className="flex min-h-0 flex-col gap-3 overflow-y-auto">{details}</div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function usePicture(subjects: GraphSubject[], links: GraphData["links"], todos: GraphData["todos"], running: boolean, focusedTodo: string | null) {
  const rings = useMemo(() => ringStates(subjects, todos, running), [subjects, todos, running])
  const nodes = useMemo<ViewNode[]>(() => subjects.map((subject) => ({
    id: subject.id, label: subject.name, slot: kindSlot(subject.kind), ring: rings.get(subject.id) ?? "unanalyzed",
  })), [subjects, rings])
  const edges = useMemo<ViewLink[]>(() => {
    const retracted = new Set(subjects.filter((subject) => subject.status === "retracted").map((subject) => subject.id))
    return links.map((link) => ({ id: link.id, from: link.from, to: link.to, retracted: retracted.has(link.from) || retracted.has(link.to) }))
  }, [subjects, links])
  const names = useMemo(() => new Map(subjects.map((subject) => [subject.id, subject.name])), [subjects])
  const highlighted = useMemo(() => {
    const todo = todos.find((candidate) => candidate.id === focusedTodo)
    return todo ? todoFootprint(todo, subjects, links) : new Set<string>()
  }, [todos, focusedTodo, subjects, links])
  return { rings, nodes, edges, names, highlighted }
}

function TodoSection({ todos, names, selected, focused, running, t, onClear, onFocus }: {
  todos: GraphData["todos"]
  names: Map<string, string>
  selected?: GraphSubject
  focused: string | null
  running: boolean
  t: GraphTranslate
  onClear: () => void
  onFocus: (id: string | null) => void
}) {
  return (
    <section aria-label={t("graph.todos")} className="flex flex-col gap-1.5">
      <h3 className="m-0 flex items-center gap-2 text-xs font-medium">
        {t("graph.todos")}
        {selected ? (
          <button type="button" onClick={onClear} title={t("graph.filterClear")}
            className="inline-flex max-w-full items-center gap-1 rounded-full border bg-muted px-2 py-0.5 text-[11px] font-normal">
            <span className="truncate">{t("graph.filterOn", { name: selected.name })}</span>
            <X className="size-3 shrink-0" aria-hidden />
          </button>
        ) : null}
      </h3>
      <TodoList todos={todos} subjectNames={names} focused={focused} running={running} t={t} onFocus={onFocus} />
    </section>
  )
}
