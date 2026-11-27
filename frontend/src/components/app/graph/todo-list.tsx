import { Check, Circle, Loader2, X } from "lucide-react"
import type { GraphTodo, TodoStatus } from "../../../lib/graph/types"
import type { GraphTranslate } from "../../../lib/messages-graph"
import { cn } from "../../../lib/cn"

const STATUS_KEY = {
  open: "graph.todoOpen",
  doing: "graph.todoDoing",
  done: "graph.todoDone",
  failed: "graph.todoFailed",
} as const satisfies Record<TodoStatus, string>

function StatusIcon({ status, running }: { status: TodoStatus; running: boolean }) {
  const common = "mt-0.5 size-4 shrink-0"
  if (status === "done") return <Check className={cn(common, "text-primary")} aria-hidden />
  if (status === "failed") return <X className={cn(common, "text-destructive")} aria-hidden />
  // A `doing` to-do whose turn is gone is waiting, not working.
  if (status === "doing" && running) return <Loader2 className={cn(common, "text-primary motion-safe:animate-spin")} aria-hidden />
  return <Circle className={cn(common, "text-muted-foreground")} aria-hidden />
}

/** Ordered to-dos. Clicking one marks what it touched in the graph; done is a
 *  tick and a strikethrough, failed shows why. */
export function TodoList({ todos, subjectNames, focused, running, t, onFocus }: {
  todos: readonly GraphTodo[]
  subjectNames: ReadonlyMap<string, string>
  focused: string | null
  running: boolean
  t: GraphTranslate
  onFocus: (id: string | null) => void
}) {
  if (!todos.length) return <p className="m-0 px-1 text-xs text-muted-foreground">{t("graph.todosEmpty")}</p>
  return (
    <ol aria-label={t("graph.todos")} className="m-0 flex list-none flex-col gap-1 p-0">
      {todos.map((todo) => {
        const about = todo.about ? subjectNames.get(todo.about) : undefined
        const pressed = focused === todo.id
        return (
          <li key={todo.id}>
            <button type="button" aria-pressed={pressed} title={t("graph.todoFocus")}
              data-status={todo.status}
              onClick={() => onFocus(pressed ? null : todo.id)}
              className={cn(
                "flex w-full items-start gap-2 rounded-md border border-transparent px-2 py-1.5 text-left text-xs hover:bg-muted/60",
                pressed && "border-primary bg-muted/60",
              )}>
              <StatusIcon status={todo.status} running={running} />
              <span className="min-w-0 flex-1">
                <span className={cn("block break-words", todo.status === "done" && "text-muted-foreground line-through")}>{todo.title}</span>
                <span className="sr-only">{t(STATUS_KEY[todo.status])}</span>
                {about ? <span className="block truncate text-[11px] text-muted-foreground">{t("graph.todoAbout", { name: about })}</span> : null}
                {todo.status === "failed" && todo.result ? <span className="block break-words text-[11px] text-destructive">{todo.result}</span> : null}
              </span>
            </button>
          </li>
        )
      })}
    </ol>
  )
}
