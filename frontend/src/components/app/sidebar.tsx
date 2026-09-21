import { MessageSquarePlus, Search, Settings } from "lucide-react"
import { useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { ProjectList } from "@/components/app/project-list"
import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { ResizeHandle } from "@/components/app/resize-handle"
import { SidebarSection } from "@/components/app/sidebar-section"
import { SidebarThreadGroup } from "@/components/app/sidebar-thread-group"
import { SidebarThreadRow } from "@/components/app/sidebar-thread-row"
import { ScheduleInboxTrigger } from "@/components/app/schedule-inbox"
import { useApp } from "@/store/app"
import {
  isProjectExpanded,
  readProjectExpanded,
  readSectionExpanded,
  runningProjectIds,
  writeProjectExpanded,
  writeSectionExpanded,
  type SectionId,
} from "@/lib/sidebar-collapse"
import { chromeTypeClass } from "@/lib/chrome-type"
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
import { cn } from "@/lib/utils"

/** Conversations, grouped the way people remember them: pins to watch,
 *  project folders, Recents for everything else. */
export function Sidebar({
  threads,
  activeId,
  runningId,
  waitingIds,
  askingIds,
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
  waitingIds?: ReadonlySet<string>
  askingIds?: ReadonlySet<string>
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
  const inboxOpen = useApp((s) => s.scheduleInboxOpen)
  const currentId = inboxOpen ? undefined : activeId
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
  const activeProjectId = threads.find((th) => th.id === currentId)?.project_id
  const busyProjects = useMemo(
    () => runningProjectIds(threads, runningId, waitingIds),
    [threads, runningId, waitingIds],
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
      <div className="flex items-center gap-1.5 px-[var(--sidebar-list-px)] pb-2 pt-2">
        <Button
          variant="secondary"
          size="sm"
          className={cn(
            chromeTypeClass,
            "min-w-0 flex-1 justify-start gap-2 overflow-hidden",
          )}
          style={{ height: "var(--sidebar-row-height)" }}
          onClick={onNew}
        >
          <MessageSquarePlus className="shrink-0" />
          <span className="min-w-0 truncate">{t("sidebar.newConversation")}</span>
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          className="shrink-0"
          style={{
            width: "var(--sidebar-row-height)",
            height: "var(--sidebar-row-height)",
          }}
          onClick={onSearch}
          title={t("sidebar.search")}
        >
          <Search />
        </Button>
      </div>

      <div className="thin-scrollbar flex-1 overflow-y-auto px-[var(--sidebar-list-px)] pb-3 pt-1">
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
                active={thread.id === currentId}
                running={thread.running || thread.id === runningId}
                waiting={waitingIds?.has(thread.id)}
                asking={Boolean(askingIds?.has(thread.id) || thread.awaiting_answer)}
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
          activeId={currentId}
          runningId={runningId}
          waitingIds={waitingIds}
          askingIds={askingIds}
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
              activeId={currentId}
              runningId={runningId}
              waitingIds={waitingIds}
              askingIds={askingIds}
              onOpen={onOpen}
              onRename={onRename}
              onDelete={askDelete}
              onReorder={onReorder}
            />
          </SidebarSection>
        ) : threads.length === 0 ? (
          <p className={cn(chromeTypeClass, "px-[var(--sidebar-row-px)] py-6 text-sidebar-foreground/70")}>
            {t("sidebar.empty")}
          </p>
        ) : null}

        <ScheduleInboxTrigger />
      </div>

      <div className="border-t border-sidebar-border px-[var(--sidebar-list-px)] py-2.5">
        <Button
          variant="ghost"
          size="sm"
          className={cn(
            chromeTypeClass,
            "w-full justify-between rounded-full px-3 text-sidebar-foreground hover:bg-sidebar-accent hover:text-foreground",
          )}
          style={{ height: "var(--sidebar-row-height)" }}
          onClick={onSettings}
        >
          <span>{t("sidebar.settings")}</span>
          <Settings />
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
