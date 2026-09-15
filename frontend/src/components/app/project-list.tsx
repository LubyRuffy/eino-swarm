import { FolderPlus, Inbox, MoreHorizontal, Pencil, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { Project } from "@/lib/types"
import { cn } from "@/lib/utils"

/** The projects section at the top of the sidebar.
 *
 *  "All conversations" is a row rather than a way to clear the filter, because
 *  the selected project is also where a new conversation lands: leaving it
 *  selected by accident would put work in the wrong directory. */
export function ProjectList({
  projects,
  selectedId,
  onSelect,
  onNew,
  onEdit,
  onDelete,
}: {
  projects: Project[]
  selectedId?: string
  onSelect: (id?: string) => void
  onNew: () => void
  onEdit: (project: Project) => void
  onDelete: (project: Project) => void
}) {
  return (
    <div className="px-2 pb-2">
      <div className="flex items-center justify-between px-2 py-1">
        <p className="text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/60">
          Projects
        </p>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onNew}
          aria-label="New project"
          title="New project"
        >
          <FolderPlus />
        </Button>
      </div>

      <button
        type="button"
        onClick={() => onSelect(undefined)}
        aria-pressed={!selectedId}
        className={cn(
          "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors",
          !selectedId
            ? "bg-sidebar-accent text-foreground"
            : "text-sidebar-foreground hover:bg-sidebar-accent/60",
        )}
      >
        <Inbox className="size-4 shrink-0" />
        <span className="truncate">All conversations</span>
      </button>

      {projects.map((project) => (
        <ProjectRow
          key={project.id}
          project={project}
          active={project.id === selectedId}
          onSelect={onSelect}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}

      {projects.length === 0 ? (
        <p className="px-2 py-2 text-xs text-sidebar-foreground/70">
          A project gives its conversations one working directory, one
          instruction and a shared memory.
        </p>
      ) : null}
    </div>
  )
}

function ProjectRow({
  project,
  active,
  onSelect,
  onEdit,
  onDelete,
}: {
  project: Project
  active: boolean
  onSelect: (id: string) => void
  onEdit: (project: Project) => void
  onDelete: (project: Project) => void
}) {
  return (
    <div
      className={cn(
        "group flex items-center gap-1 rounded-md px-2 py-1.5 text-sm transition-colors",
        active
          ? "bg-sidebar-accent text-foreground"
          : "text-sidebar-foreground hover:bg-sidebar-accent/60",
      )}
    >
      <button
        type="button"
        onClick={() => onSelect(project.id)}
        aria-pressed={active}
        className="flex min-w-0 flex-1 items-center gap-2 text-left"
      >
        <span className="truncate">{project.name}</span>
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            className="opacity-0 transition-opacity group-hover:opacity-100 data-[state=open]:opacity-100"
            aria-label={`Project options for ${project.name}`}
            title="More"
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => onEdit(project)}>
            <Pencil />
            Edit
          </DropdownMenuItem>
          <DropdownMenuItem destructive onSelect={() => onDelete(project)}>
            <Trash2 />
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
