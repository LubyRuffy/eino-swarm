import { MessageSquarePlus, Search, Settings } from "lucide-react"
import { useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { ProjectList } from "@/components/app/project-list"
import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { ResizeHandle } from "@/components/app/resize-handle"
import { SidebarThreadRow } from "@/components/app/sidebar-thread-row"
import { reorderById } from "@/lib/reorder"
import {
  isProjectExpanded,
  readProjectExpanded,
  writeProjectExpanded,
} from "@/lib/sidebar-collapse"
import { sidebarBuckets } from "@/lib/sidebar-groups"
import {
  SIDEBAR_WIDTH_MAX,
  SIDEBAR_WIDTH_MIN,
  SIDEBAR_WIDTH_VAR,
  applySidebarWidth,
  hydrateSidebarWidth,
  paintSidebarWidth,
} from "@/lib/sidebar-width"
import { useSortableList } from "@/lib/sortable"
import type { Project, SkillInfo, Thread } from "@/lib/types"
import { useT } from "@/lib/use-t"

/** Conversations, grouped the way people remember them: pins to watch,
 *  project folders, Recents for everything else. */
export function Sidebar({
  threads,
  activeId,
  runningId,
  onNew,
  onOpen,
  onRename,
  onDelete,
  onSearch,
  onSettings,
  projects,
  selectedProjectId,
  onSelectProject,
  onNewProject,
  onNewInProject,
  onEditProject,
  onDeleteProject,
  onOpenSkill,
  onReorder,
  onReorderProjects,
  onPin,
}: {
  threads: Thread[]
  activeId?: string
  runningId?: string
  onNew: () => void
  onOpen: (id: string) => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
  onSearch: () => void
  onSettings: () => void
  projects: Project[]
  selectedProjectId?: string
  onSelectProject: (id?: string) => void
  onNewProject: () => void
  onNewInProject: (project: Project) => void
  onEditProject: (project: Project) => void
  onDeleteProject: (project: Project) => void
  onOpenSkill: (project: Project, skill?: SkillInfo) => void
  onReorder: (ids: string[]) => void
  onReorderProjects: (ids: string[]) => void
  onPin: (id: string, pinned: boolean) => void
}) {
  const t = useT()
  const buckets = useMemo(() => sidebarBuckets(threads), [threads])
  const [startWidth] = useState(hydrateSidebarWidth)
  const [doomed, setDoomed] = useState<Thread>()
  const [expanded, setExpanded] = useState(readProjectExpanded)
  const activeProjectId = threads.find((th) => th.id === activeId)?.project_id
  const openByProject = useMemo(() => {
    const next: Record<string, boolean> = {}
    for (const project of projects) {
      next[project.id] = isProjectExpanded(project.id, {
        activeProjectId,
        selectedId: selectedProjectId,
        overrides: expanded,
      })
    }
    return next
  }, [projects, activeProjectId, selectedProjectId, expanded])
  const recentsSortable = useSortableList((from, to) => {
    onReorder(reorderById(buckets.recents, from, to).map((th) => th.id))
  })
  const askDelete = (id: string) => {
    const hit = threads.find((th) => th.id === id)
    if (hit) setDoomed(hit)
  }

  return (
    // The resize strip hangs 4px into the transcript. This column is the
    // earlier flex sibling; without a stacking context the main column
    // paints over that overlap and the drag dies.
    <aside
      data-testid="conversation-list"
      className="relative z-10 flex h-full shrink-0 flex-col border-r border-sidebar-border bg-sidebar"
      style={{ width: `var(${SIDEBAR_WIDTH_VAR}, ${startWidth}px)` }}
    >
      <ResizeHandle
        width={startWidth}
        onWidthChange={paintSidebarWidth}
        onWidthCommit={applySidebarWidth}
        edge="right"
        label={t("sidebar.resize")}
        min={SIDEBAR_WIDTH_MIN}
        max={SIDEBAR_WIDTH_MAX}
      />
      <div className="flex items-center gap-1 px-3 pb-2 pt-3">
        <Button
          variant="secondary"
          size="sm"
          className="min-w-0 flex-1 justify-start gap-2 overflow-hidden"
          onClick={onNew}
        >
          <MessageSquarePlus className="shrink-0" />
          <span className="min-w-0 truncate">{t("sidebar.newConversation")}</span>
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          onClick={onSearch}
          title={t("sidebar.search")}
        >
          <Search />
        </Button>
      </div>

      <div className="thin-scrollbar flex-1 overflow-y-auto px-2 pb-2">
        {buckets.pinned.length > 0 ? (
          <section className="mb-2" data-testid="pinned-list">
            <p className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/60">
              {t("sidebar.pinned")}
            </p>
            {buckets.pinned.map((thread) => (
              <SidebarThreadRow
                key={thread.id}
                thread={thread}
                active={thread.id === activeId}
                running={thread.running || thread.id === runningId}
                onOpen={onOpen}
                onRename={onRename}
                onDelete={askDelete}
                onPin={onPin}
              />
            ))}
          </section>
        ) : null}

        <ProjectList
          projects={projects}
          threadsByProject={buckets.byProject}
          expanded={openByProject}
          selectedId={selectedProjectId}
          activeId={activeId}
          runningId={runningId}
          onSelect={onSelectProject}
          onToggle={(id) => {
            const next = { ...expanded, [id]: !openByProject[id] }
            setExpanded(next)
            writeProjectExpanded(next)
          }}
          onNew={onNewProject}
          onNewConversation={onNewInProject}
          onEdit={onEditProject}
          onDelete={onDeleteProject}
          onOpenSkill={onOpenSkill}
          onReorder={onReorderProjects}
          onOpenThread={onOpen}
          onRenameThread={onRename}
          onDeleteThread={askDelete}
          onReorderThreads={onReorder}
          onPinThread={onPin}
        />

        {buckets.recents.length > 0 ? (
          <section className="mb-2" data-testid="recents-list">
            <p className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/60">
              {t("sidebar.recents")}
            </p>
            {buckets.recents.map((thread) => (
              <SidebarThreadRow
                key={thread.id}
                thread={thread}
                active={thread.id === activeId}
                running={thread.running || thread.id === runningId}
                drag={recentsSortable.bind(thread.id)}
                onOpen={onOpen}
                onRename={onRename}
                onDelete={askDelete}
              />
            ))}
          </section>
        ) : threads.length === 0 ? (
          <p className="px-2 py-6 text-xs text-sidebar-foreground/70">
            {t("sidebar.empty")}
          </p>
        ) : null}
      </div>

      <div className="flex items-center justify-between border-t border-sidebar-border px-3 py-2">
        <Button variant="ghost" size="sm" className="gap-2" onClick={onSettings}>
          <Settings />
          {t("sidebar.settings")}
        </Button>
      </div>
      <ConfirmDeleteDialog
        open={Boolean(doomed)}
        title={t("thread.deleteTitle", {
          name: doomed?.title?.trim() || t("sidebar.untitled"),
        })}
        description={t("thread.deleteDesc")}
        confirmLabel={t("thread.deleteConfirm")}
        cancelLabel={t("confirm.cancel")}
        onOpenChange={(open) => {
          if (!open) setDoomed(undefined)
        }}
        onConfirm={() => {
          if (doomed) onDelete(doomed.id)
        }}
      />
    </aside>
  )
}
