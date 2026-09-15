import { BookOpen, FolderPlus, Inbox, MoreHorizontal, Pencil, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import type { Project, SkillInfo } from "@/lib/types"
import { cn } from "@/lib/utils"

/** How many skills a project row lists before it points at the Memory tab.
 *  The sidebar is a directory, not a catalogue: past this the names stop
 *  fitting, and the full list is one click away. */
const sidebarSkillsCap = 8

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
  onOpenSkill,
}: {
  projects: Project[]
  selectedId?: string
  onSelect: (id?: string) => void
  onNew: () => void
  onEdit: (project: Project) => void
  onDelete: (project: Project) => void
  onOpenSkill: (project: Project, skill?: SkillInfo) => void
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
          onOpenSkill={onOpenSkill}
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
  onOpenSkill,
}: {
  project: Project
  active: boolean
  onSelect: (id: string) => void
  onEdit: (project: Project) => void
  onDelete: (project: Project) => void
  onOpenSkill: (project: Project, skill?: SkillInfo) => void
}) {
  const skills = project.skills ?? []
  return (
    <div className="mb-0.5">
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
          {skills.length > 0 ? (
            <span className="shrink-0 text-[11px] text-sidebar-foreground/50" aria-hidden="true">
              {skills.length}
            </span>
          ) : null}
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
      {skills.length > 0 ? (
        <ul className="ml-2 border-l border-sidebar-border py-0.5 pl-2" data-testid="project-skills">
          {skills.slice(0, sidebarSkillsCap).map((skill) => (
            <li key={skill.name}>
              <button
                type="button"
                onClick={() => onOpenSkill(project, skill)}
                title={skill.description}
                aria-label={`Open skill ${skill.name}`}
                className="flex w-full items-center gap-1.5 rounded-md px-1.5 py-1 text-left text-xs text-sidebar-foreground/80 transition-colors hover:bg-sidebar-accent/60 hover:text-foreground"
              >
                <BookOpen className="size-3 shrink-0" />
                <span className="min-w-0 truncate">{skill.name}</span>
              </button>
            </li>
          ))}
          {skills.length > sidebarSkillsCap ? (
            <li>
              <button
                type="button"
                onClick={() => onOpenSkill(project)}
                aria-label={`Show remaining skills for ${project.name}`}
                className="w-full rounded-md px-1.5 py-1 text-left text-[11px] text-sidebar-foreground/60 hover:text-foreground"
              >
                {skills.length - sidebarSkillsCap} more
              </button>
            </li>
          ) : null}
        </ul>
      ) : null}
    </div>
  )
}
