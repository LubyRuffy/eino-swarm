import {
  BookOpen,
  ChevronRight,
  Folder,
  FolderPlus,
  GripVertical,
  MessageSquarePlus,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react"

import { SidebarThreadRow } from "@/components/app/sidebar-thread-row"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { reorderById } from "@/lib/reorder"
import { useSortableList } from "@/lib/sortable"
import type { Project, SkillInfo, Thread } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

/** Hover/focus chrome on a project row. Hidden until the row is the one
 *  being used, so the name stays the thing you read. */
const rowActionClass =
  "opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100 data-[state=open]:opacity-100"

/** Project folders in the sidebar. Each folder nests its conversations and
 *  collapses on click. Skills live behind the row menu — listing them under
 *  the name turns a directory into a catalogue. */
export function ProjectList({
  projects,
  threadsByProject,
  expanded,
  selectedId,
  activeId,
  runningId,
  onSelect,
  onToggle,
  onNew,
  onNewConversation,
  onEdit,
  onDelete,
  onOpenSkill,
  onReorder,
  onOpenThread,
  onRenameThread,
  onDeleteThread,
  onReorderThreads,
  onPinThread,
}: {
  projects: Project[]
  threadsByProject: Record<string, Thread[]>
  expanded: Record<string, boolean>
  selectedId?: string
  activeId?: string
  runningId?: string
  onSelect: (id: string) => void
  onToggle: (id: string) => void
  onNew: () => void
  onNewConversation: (project: Project) => void
  onEdit: (project: Project) => void
  onDelete: (project: Project) => void
  onOpenSkill: (project: Project, skill?: SkillInfo) => void
  onReorder: (ids: string[]) => void
  onOpenThread: (id: string) => void
  onRenameThread: (id: string, title: string) => void
  onDeleteThread: (id: string) => void
  onReorderThreads: (ids: string[]) => void
  onPinThread: (id: string, pinned: boolean) => void
}) {
  const t = useT()
  const sortable = useSortableList((from, to) => {
    onReorder(reorderById(projects, from, to).map((p) => p.id))
  })
  return (
    <div className="pb-2" data-testid="project-list">
      <div className="flex items-center justify-between pr-2">
        <p className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/60">
          {t("projects.title")}
        </p>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onNew}
          aria-label={t("projects.new")}
          title={t("projects.new")}
        >
          <FolderPlus />
        </Button>
      </div>

      {projects.map((project) => (
        <ProjectRow
          key={project.id}
          project={project}
          threads={threadsByProject[project.id] ?? []}
          open={Boolean(expanded[project.id])}
          active={project.id === selectedId}
          activeId={activeId}
          runningId={runningId}
          drag={sortable.bind(project.id)}
          onSelect={onSelect}
          onToggle={onToggle}
          onNewConversation={onNewConversation}
          onEdit={onEdit}
          onDelete={onDelete}
          onOpenSkill={onOpenSkill}
          onOpenThread={onOpenThread}
          onRenameThread={onRenameThread}
          onDeleteThread={onDeleteThread}
          onReorderThreads={onReorderThreads}
          onPinThread={onPinThread}
        />
      ))}

      {projects.length === 0 ? (
        <p className="px-2 py-2 text-xs text-sidebar-foreground/70">
          {t("projects.empty")}
        </p>
      ) : null}
    </div>
  )
}

function ProjectRow({
  project,
  threads,
  open,
  active,
  activeId,
  runningId,
  drag,
  onSelect,
  onToggle,
  onNewConversation,
  onEdit,
  onDelete,
  onOpenSkill,
  onOpenThread,
  onRenameThread,
  onDeleteThread,
  onReorderThreads,
  onPinThread,
}: {
  project: Project
  threads: Thread[]
  open: boolean
  active: boolean
  activeId?: string
  runningId?: string
  drag: ReturnType<ReturnType<typeof useSortableList>["bind"]>
  onSelect: (id: string) => void
  onToggle: (id: string) => void
  onNewConversation: (project: Project) => void
  onEdit: (project: Project) => void
  onDelete: (project: Project) => void
  onOpenSkill: (project: Project, skill?: SkillInfo) => void
  onOpenThread: (id: string) => void
  onRenameThread: (id: string, title: string) => void
  onDeleteThread: (id: string) => void
  onReorderThreads: (ids: string[]) => void
  onPinThread: (id: string, pinned: boolean) => void
}) {
  const t = useT()
  const sortable = useSortableList((from, to) => {
    onReorderThreads(reorderById(threads, from, to).map((th) => th.id))
  })
  return (
    <div className="mb-0.5">
      <div
        {...drag}
        data-testid="project-row"
        data-id={project.id}
        className={cn(
          "group flex cursor-grab select-none items-center gap-1 rounded-md px-2 py-1.5 text-sm transition-colors",
          "data-[dragging=true]:cursor-grabbing data-[dragging=true]:opacity-60 data-[over=true]:bg-sidebar-accent",
          active
            ? "bg-sidebar-accent text-foreground"
            : "text-sidebar-foreground hover:bg-sidebar-accent/60",
        )}
      >
        <span
          data-drag-handle
          className="flex cursor-grab items-center self-stretch opacity-0 group-hover:opacity-50"
          aria-hidden="true"
          onClick={() => {
            onToggle(project.id)
            onSelect(project.id)
          }}
        >
          <GripVertical className="size-3.5 shrink-0" />
        </span>
        <button
          type="button"
          onClick={() => {
            onToggle(project.id)
            onSelect(project.id)
          }}
          aria-pressed={active}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
        >
          <ChevronRight
            className={cn(
              "size-3 shrink-0 text-sidebar-foreground/50 transition-transform",
              open && "rotate-90",
            )}
            aria-hidden="true"
          />
          <Folder className="size-4 shrink-0" aria-hidden="true" />
          <span className="truncate">{project.name}</span>
          {threads.length > 0 ? (
            <span className="shrink-0 text-[11px] text-sidebar-foreground/50" aria-hidden="true">
              {threads.length}
            </span>
          ) : null}
        </button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              data-no-drag
              className={rowActionClass}
              aria-label={t("projects.options", { name: project.name })}
              title={t("projects.more")}
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => onOpenSkill(project)}>
              <BookOpen />
              {t("projects.viewSkills")}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => onEdit(project)}>
              <Pencil />
              {t("projects.edit")}
            </DropdownMenuItem>
            <DropdownMenuItem destructive onSelect={() => onDelete(project)}>
              <Trash2 />
              {t("projects.delete")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          variant="ghost"
          size="icon-sm"
          data-no-drag
          className={rowActionClass}
          aria-label={t("projects.newIn", { name: project.name })}
          title={t("projects.newIn", { name: project.name })}
          onClick={() => onNewConversation(project)}
        >
          <MessageSquarePlus />
        </Button>
      </div>
      {open ? (
        <div data-testid="project-threads">
          {threads.map((thread) => (
            <SidebarThreadRow
              key={thread.id}
              thread={thread}
              active={thread.id === activeId}
              running={thread.running || thread.id === runningId}
              drag={sortable.bind(thread.id)}
              indent
              onOpen={onOpenThread}
              onRename={onRenameThread}
              onDelete={onDeleteThread}
              onPin={onPinThread}
            />
          ))}
        </div>
      ) : null}
    </div>
  )
}
