import { MessageSquarePlus, Search } from "lucide-react"
import { useMemo, useState } from "react"

import { Button } from "@/components/ui/button"
import { ProjectList } from "@/components/app/project-list"
import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { ResizeHandle } from "@/components/app/resize-handle"
import { SidebarSection } from "@/components/app/sidebar-section"
import { SidebarThreadGroup } from "@/components/app/sidebar-thread-group"
import { SidebarThreadRow } from "@/components/app/sidebar-thread-row"
import { DestRail, useVisibleDest } from "@/components/app/dest-rail"
import { LocalClientsSection } from "@/components/app/local-clients"
import { ScheduleListPane } from "@/components/app/schedule-inbox"
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

function SidebarVersion() {
  const t = useT()
  const version = useApp((s) => s.meta?.version ?? "")
  if (!version) return null
  return (
    <p data-testid="sidebar-version" className="truncate px-2 pb-1 text-xs text-muted-foreground">
      {t("sidebar.version", { version })}
    </p>
  )
}

function ListToolbar({
  onNew,
  onSearch,
  newLabel,
  searchLabel,
}: {
  onNew?: () => void
  onSearch: () => void
  newLabel?: string
  searchLabel: string
}) {
  return (
    <div className="flex items-center gap-1.5 px-[var(--sidebar-list-px)] pb-2 pt-2">
      {onNew ? (
        <Button
          variant="secondary"
          size="sm"
          className={cn(chromeTypeClass, "min-w-0 flex-1 justify-start gap-2 overflow-hidden")}
          style={{ height: "var(--sidebar-row-height)" }}
          onClick={onNew}
        >
          <MessageSquarePlus className="shrink-0" />
          <span className="min-w-0 truncate">{newLabel}</span>
        </Button>
      ) : null}
      <Button
        variant="ghost"
        size="icon-sm"
        className={cn("shrink-0", !onNew && "ml-auto")}
        style={{
          width: "var(--sidebar-row-height)",
          height: "var(--sidebar-row-height)",
        }}
        onClick={onSearch}
        title={searchLabel}
      >
        <Search />
      </Button>
    </div>
  )
}

/** Projects are the list. Conversations with no project, waits, and
 *  local clients each take the same column when their rail icon is current. */
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
  onToggleTheme,
  onToggleLocale,
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
  listOpen = true,
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
  onToggleTheme: () => void
  onToggleLocale: () => void
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
  listOpen?: boolean
}) {
  const t = useT()
  const pane = useVisibleDest()
  const currentId = pane === "projects" || pane === "chats" ? activeId : undefined
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
      className="relative z-10 flex h-full shrink-0 border-r border-sidebar-border bg-sidebar"
      style={{
        width: listOpen
          ? `var(${SIDEBAR_WIDTH_VAR}, ${startWidth}px)`
          : "var(--dest-rail-width)",
      }}
    >
      {listOpen ? (
        <ResizeHandle
          width={startWidth}
          onWidthChange={paintSidebarWidth}
          onWidthCommit={applySidebarWidth}
          edge="right"
          label={t("sidebar.resize")}
          min={SIDEBAR_WIDTH_MIN}
          max={SIDEBAR_WIDTH_MAX}
        />
      ) : null}
      <DestRail
        edge={listOpen}
        onSettings={onSettings}
        onToggleTheme={onToggleTheme}
        onToggleLocale={onToggleLocale}
      />
      {listOpen ? (
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {pane === "projects" ? (
          <>
            <ListToolbar
              onSearch={onSearch}
              searchLabel={t("sidebar.search")}
            />
            <div className="thin-scrollbar min-h-0 flex-1 overflow-y-auto px-[var(--sidebar-list-px)] pb-3 pt-1">
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
            </div>
          </>
        ) : null}
        {pane === "chats" ? (
          <>
            <ListToolbar
              onNew={onNew}
              onSearch={onSearch}
              newLabel={t("sidebar.newConversation")}
              searchLabel={t("sidebar.search")}
            />
            <div className="thin-scrollbar min-h-0 flex-1 overflow-y-auto px-[var(--sidebar-list-px)] pb-3 pt-1">
              {buckets.recents.length > 0 ? (
                <SidebarSection
                  testId="recents-list"
                  label={t("sidebar.recents")}
                  open={sections.recents}
                  onToggle={() => toggleSection("recents")}
                  actions={
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      data-testid="recents-new"
                      onClick={onNew}
                      aria-label={t("sidebar.newInRecents")}
                      title={t("sidebar.newInRecents")}
                    >
                      <MessageSquarePlus />
                    </Button>
                  }
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
              ) : (
                <p className={cn(chromeTypeClass, "px-[var(--sidebar-row-px)] py-6 text-sidebar-foreground/70")}>
                  {t("sidebar.empty")}
                </p>
              )}
            </div>
          </>
        ) : null}
        {pane === "scheduled" ? <ScheduleListPane /> : null}
        {pane === "clients" ? (
          <div className="thin-scrollbar min-h-0 flex-1 overflow-y-auto px-[var(--sidebar-list-px)] py-2">
            <LocalClientsSection bare />
          </div>
        ) : null}

        <div className="mt-auto px-[var(--sidebar-list-px)] pb-2">
          <SidebarVersion />
        </div>
      </div>
      ) : null}
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
