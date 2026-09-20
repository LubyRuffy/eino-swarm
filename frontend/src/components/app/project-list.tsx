import {
  BookOpen,
  Folder,
  FolderOpen,
  FolderPlus,
  MessageSquarePlus,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react"

import { SidebarSection } from "@/components/app/sidebar-section"
import { SidebarKindSlot, sidebarRowClass } from "@/components/app/sidebar-slots"
import { SidebarThreadGroup } from "@/components/app/sidebar-thread-group"
import { StatusDot } from "@/components/app/transcript"
import { WaitMark } from "@/components/app/wait-mark"
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
  activeId,
  runningId,
  waitingIds,
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
  sectionOpen = true,
  onToggleSection,
}: {
  projects: Project[]
  threadsByProject: Record<string, Thread[]>
  expanded: Record<string, boolean>
  activeId?: string
  runningId?: string
  waitingIds?: ReadonlySet<string>
  sectionOpen?: boolean
  onToggleSection?: () => void
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
    <SidebarSection
      testId="project-list"
      label={t("projects.title")}
      open={sectionOpen}
      onToggle={() => onToggleSection?.()}
      actions={
        <Button
          variant="ghost"
          size="icon-xs"
          onClick={onNew}
          aria-label={t("projects.new")}
          title={t("projects.new")}
        >
          <FolderPlus />
        </Button>
      }
    >
      {projects.map((project) => (
        <ProjectRow
          key={project.id}
          project={project}
          threads={threadsByProject[project.id] ?? []}
          open={Boolean(expanded[project.id])}
          activeId={activeId}
          runningId={runningId}
          waitingIds={waitingIds}
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
    </SidebarSection>
  )
}

function ProjectRow({
  project,
  threads,
  open,
  activeId,
  runningId,
  waitingIds,
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
  activeId?: string
  runningId?: string
  waitingIds?: ReadonlySet<string>
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
  const busy = threads.some((thread) => thread.running || thread.id === runningId)
  const waiting =
    !busy && threads.some((thread) => Boolean(waitingIds?.has(thread.id)))
  return (
    <div className="mb-0.5" data-testid="project-wrap" data-id={project.id}>
      <div
        {...drag}
        data-testid="project-row"
        data-id={project.id}
        className={cn(
          sidebarRowClass,
          "text-sidebar-foreground hover:bg-sidebar-accent/60",
          "data-[dragging=true]:cursor-grabbing data-[dragging=true]:opacity-60 data-[over=true]:bg-sidebar-accent",
        )}
      >
        <button
          type="button"
          onClick={() => {
            onToggle(project.id)
            onSelect(project.id)
          }}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-1 text-left"
        >
          <SidebarKindSlot testId="project-kind">
            {open ? (
              <FolderOpen
                data-testid="project-folder"
                data-open="true"
                className="size-4 shrink-0"
                aria-hidden="true"
              />
            ) : (
              <>
                <Folder
                  data-testid="project-folder"
                  data-open="false"
                  className="size-4 shrink-0"
                  aria-hidden="true"
                />
                {busy ? (
                  <span className="absolute right-0 top-0">
                    <StatusDot status="running" />
                  </span>
                ) : waiting ? (
                  <span className="absolute right-0 top-0">
                    <WaitMark className="size-2.5" />
                  </span>
                ) : null}
              </>
            )}
          </SidebarKindSlot>
          <span data-testid="row-label" className="truncate">
            {project.name}
          </span>
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
              size="icon-xs"
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
          size="icon-xs"
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
          <SidebarThreadGroup
            threads={threads}
            activeId={activeId}
            runningId={runningId}
            waitingIds={waitingIds}
            onOpen={onOpenThread}
            onRename={onRenameThread}
            onDelete={onDeleteThread}
            onPin={onPinThread}
            onReorder={onReorderThreads}
          />
        </div>
      ) : null}
    </div>
  )
}
