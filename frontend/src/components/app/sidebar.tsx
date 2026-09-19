import { MessageSquarePlus, Search, Settings } from "lucide-react"
import { useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { ProjectList } from "@/components/app/project-list"
import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { ResizeHandle } from "@/components/app/resize-handle"
import { SidebarSection } from "@/components/app/sidebar-section"
import { SidebarThreadGroup } from "@/components/app/sidebar-thread-group"
import { SidebarThreadRow } from "@/components/app/sidebar-thread-row"
import { ScheduleInbox, ScheduleInboxTrigger } from "@/components/app/schedule-inbox"
import {
  isProjectExpanded,
  readProjectExpanded,
  readSectionExpanded,
  runningProjectIds,
  writeProjectExpanded,
  writeSectionExpanded,
  type SectionId,
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
  const [sections, setSections] = useState(readSectionExpanded)
  const toggleSection = (id: SectionId) => {
    const next = { ...sections, [id]: !sections[id] }
    setSections(next)
    writeSectionExpanded(next)
  }
  const activeProjectId = threads.find((th) => th.id === activeId)?.project_id
  const busyProjects = useMemo(
    () => runningProjectIds(threads, runningId),
    [threads, runningId],
  )
  const openByProject = useMemo(() => {
    const next: Record<string, boolean> = {}
    for (const project of projects) {
      next[project.id] = isProjectExpanded(project.id, {
        activeProjectId,
        selectedId: selectedProjectId,
        runningProjectIds: busyProjects,
        overrides: expanded,
      })
    }
    return next
  }, [projects, activeProjectId, selectedProjectId, expanded, busyProjects])
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
          <SidebarSection
            testId="pinned-list"
            label={t("sidebar.pinned")}
            open={sections.pinned}
            onToggle={() => toggleSection("pinned")}
          >
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
          </SidebarSection>
        ) : null}

        <ProjectList
          projects={projects}
          threadsByProject={buckets.byProject}
          expanded={openByProject}
          activeId={activeId}
          runningId={runningId}
          sectionOpen={sections.projects}
          onToggleSection={() => toggleSection("projects")}
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
          <SidebarSection
            testId="recents-list"
            label={t("sidebar.recents")}
            open={sections.recents}
            onToggle={() => toggleSection("recents")}
          >
            <SidebarThreadGroup
              threads={buckets.recents}
              activeId={activeId}
              runningId={runningId}
              onOpen={onOpen}
              onRename={onRename}
              onDelete={askDelete}
              onReorder={onReorder}
            />
          </SidebarSection>
        ) : threads.length === 0 ? (
          <p className="px-2 py-6 text-xs text-sidebar-foreground/70">
            {t("sidebar.empty")}
          </p>
        ) : null}

        <ScheduleInboxTrigger />
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
      <ScheduleInbox />
    </aside>
  )
}
