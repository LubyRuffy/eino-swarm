import { useEffect } from "react"
import { RightPanel, type PanelTab } from "./panel"
import { api } from "@/lib/api"
import { LIBRARY_ID } from "@/lib/library"
import { useApp } from "@/store/app"
import { projectOf, useProjects } from "@/store/projects"
import { useCoveredTranscript } from "@/store/settings-sheet"

export function AppPanel({
  tab,
  onTabChange,
  focusSkill,
}: {
  tab: PanelTab
  onTabChange: (tab: PanelTab) => void
  focusSkill?: { projectId: string; name?: string }
}) {
  const transcript = useCoveredTranscript()
  const selectedAgent = useApp((s) => s.selectedAgent)
  const selectAgent = useApp((s) => s.selectAgent)
  const files = useApp((s) => s.files)
  const loadedFileDirs = useApp((s) => s.loadedFileDirs)
  const fileDirErrors = useApp((s) => s.fileDirErrors)
  const workspace = useApp((s) => s.workspace)
  const fileScope = useApp((s) => s.fileScope)
  const fileView = useApp((s) => s.fileView)
  const setFileScope = useApp((s) => s.setFileScope)
  const turns = useApp((s) => s.turns)
  const meta = useApp((s) => s.meta)
  const usage = useApp((s) => s.usage)
  const activeId = useApp((s) => s.activeId)
  const scheduled = useApp((s) => s.scheduleInboxOpen)
  const upload = useApp((s) => s.upload)
  const removeFile = useApp((s) => s.removeFile)
  const refreshFiles = useApp((s) => s.refreshFiles)
  const ensureFileDir = useApp((s) => s.ensureFileDir)
  const searchFiles = useApp((s) => s.searchFiles)
  const threads = useApp((s) => s.threads)
  const reviewNow = useApp((s) => s.reviewNow)
  const projects = useProjects((s) => s.projects)
  const memory = useProjects((s) => s.memory)
  const selectedProjectId = useProjects((s) => s.selectedId)
  const memoryLoading = useProjects((s) => s.memoryLoading)
  const loadMemory = useProjects((s) => s.loadMemory)
  const saveMemory = useProjects((s) => s.saveMemory)
  const removeSkill = useProjects((s) => s.removeSkill)
  const tidySkills = useProjects((s) => s.tidySkills)
  const clearTidy = useProjects((s) => s.clearTidy)
  const tidying = useProjects((s) => s.tidying)
  const tidyReport = useProjects((s) => s.tidyReport)
  const tidyError = useProjects((s) => s.tidyError)
  const tidyLive = useProjects((s) => s.tidyLive)
  const memoryUnread = useProjects((s) => s.memoryUnread)
  const seeMemory = useProjects((s) => s.seeMemory)
  const memoryProjectId = useProjects((s) => s.memoryProjectId)
  const reviewing = useProjects((s) => s.reviewing)
  const reviewHint = useProjects((s) => s.reviewHint)
  const reviewOutcome = useProjects((s) => s.reviewOutcome)
  const reviewTurnId = useProjects((s) => s.reviewTurnId)
  const reviewThreadId = useProjects((s) => s.reviewThreadId)
  const reviewProjectId = useProjects((s) => s.reviewProjectId)
  const activeThread = threads.find(({ id }) => id === activeId)
  const conversationProject = projectOf(projects, activeThread?.project_id)
  const scopeProject = activeId ? conversationProject : projectOf(projects, selectedProjectId)
  useEffect(() => {
    if (tab === "files") void refreshFiles()
    return () => setFileScope("task")
  }, [tab, activeId, conversationProject?.id, conversationProject?.workdir, refreshFiles, setFileScope])
  // A click on a sidebar skill is a request to look at that project's
  // memory, even when the open conversation belongs to another one — or
  // to none. The conversation's project is the default the rest of the
  // time, so merely filtering the list does not swap the notes under you.
  const project =
    (focusSkill ? projectOf(projects, focusSkill.projectId) : undefined) ??
    scopeProject
  // No project means the shared library: procedures from conversations that
  // belong to none, kept so they can be copied into an agent later.
  const memoryTarget = project?.id ?? (activeId ? LIBRARY_ID : undefined)
  const reviewForTarget = (!reviewThreadId || reviewThreadId === activeId) && (!reviewProjectId || reviewProjectId === memoryTarget)
  const memoryForTarget =
    memoryTarget && memoryProjectId === memoryTarget ? memory : undefined
  // A project-row click loads that project's catalog. This panel follows the
  // open conversation, or the shared library when there is no project, so a
  // mismatched read would blank the list until something opened the thread again.
  useEffect(() => {
    if (!memoryTarget || memoryProjectId === memoryTarget) return
    void loadMemory(memoryTarget)
  }, [memoryTarget, memoryProjectId, loadMemory])
  if (scheduled) return null
  return (
    <RightPanel
      tab={tab}
      onTabChange={(next) => {
        if (next === "memory") seeMemory()
        onTabChange(next)
      }}
      transcript={transcript}
      selectedAgent={selectedAgent}
      onSelectAgent={selectAgent}
      files={files}
      loadedDirs={loadedFileDirs}
      dirErrors={fileDirErrors}
      onOpenDir={ensureFileDir}
      onSearch={searchFiles}
      workspace={workspace}
      fileScope={fileScope}
      fileView={fileView}
      onFileScope={setFileScope}
      turns={turns}
      meta={meta}
      threadId={activeId}
      scopeProject={scopeProject}
      memory={
        memoryTarget
          ? {
              project,
              library: !project,
              memory: memoryForTarget,
              loading: memoryLoading || memoryProjectId !== memoryTarget,
              onSave: saveMemory,
              onDeleteSkill: (name) => void removeSkill(name),
              onRefresh: () => void loadMemory(memoryTarget),
              onReview: () => void reviewNow(),
              distill: project ? {
                projectId: project.id,
                threadId: conversationProject?.id === project.id ? activeId : undefined,
              } : undefined,
              onTidySkills: () => void tidySkills(),
              reviewing: reviewForTarget && reviewing,
              reviewHint: reviewForTarget ? reviewHint : undefined,
              reviewOutcome: reviewForTarget ? reviewOutcome : undefined,
              reviewTurnId: reviewForTarget ? reviewTurnId : undefined,
              tidying,
              tidyReport,
              tidyError,
              tidyLive,
              onDismissTidy: clearTidy,
              unread: memoryUnread,
              onSeen: seeMemory,
              focusSkill: focusSkill?.name,
            }
          : undefined
      }
      onUpload={async (picked) => {
        await upload(picked)
      }}
      onDeleteFile={(path) => void removeFile(path)}
      onRefreshFiles={() => void refreshFiles()}
      onReveal={(path) => {
        if (activeId) void api.reveal(activeId, path, { scope: fileScope, root: fileView?.root }).catch(() => undefined)
      }}
      usage={usage}
    />
  )
}

