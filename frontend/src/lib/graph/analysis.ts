import type { GraphLink, GraphSubject, GraphTodo } from "./types"

/** How the ring around a subject is drawn.
 *  unanalyzed / pending: static dashed; doing: flowing dashed; settled: solid;
 *  retracted: greyed. */
export type Ring = "unanalyzed" | "pending" | "doing" | "settled" | "retracted"

/** Analysis state is derived, never stored: a new to-do about a settled
 *  subject reopens its ring without anyone writing a flag. A `doing` to-do
 *  left behind by a turn that is no longer running is pending, not active. */
export function ringState(subject: Pick<GraphSubject, "id" | "status">, todos: readonly GraphTodo[], running: boolean): Ring {
  if (subject.status === "retracted") return "retracted"
  let any = false
  let doing = false
  let unsettled = false
  for (const todo of todos) {
    if (todo.about !== subject.id) continue
    any = true
    if (todo.status === "doing") doing = true
    if (todo.status === "open" || todo.status === "doing") unsettled = true
  }
  if (!any) return "unanalyzed"
  if (doing && running) return "doing"
  return unsettled ? "pending" : "settled"
}

export function ringStates(subjects: readonly GraphSubject[], todos: readonly GraphTodo[], running: boolean): Map<string, Ring> {
  return new Map(subjects.map((subject) => [subject.id, ringState(subject, todos, running)]))
}

/** The subjects and links a to-do touched: the one it is about plus everything
 *  its settlement brought in. */
export function todoFootprint(todo: GraphTodo, subjects: readonly GraphSubject[], links: readonly GraphLink[]) {
  const nodes = new Set<string>()
  if (todo.about) nodes.add(todo.about)
  for (const subject of subjects) if (subject.found_by_todo === todo.id) nodes.add(subject.id)
  for (const link of links) {
    if (link.found_by_todo !== todo.id) continue
    nodes.add(link.from)
    nodes.add(link.to)
  }
  return nodes
}

/** To-dos that explain a subject: the ones about it and the one that found it. */
export function todosForSubject(subject: Pick<GraphSubject, "id" | "found_by_todo">, todos: readonly GraphTodo[]) {
  return todos.filter((todo) => todo.about === subject.id || todo.id === subject.found_by_todo)
}
