import {
  ArrowLeft,
  Download,
  File as FileIcon,
  Folder,
  FolderOpen,
  RefreshCw,
  Trash2,
  Upload,
} from "lucide-react"
import { useMemo, useRef } from "react"

import { AgentTranscript, CopyButton, StatusDot } from "@/components/app/transcript"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import { MANAGER_ID, type TranscriptState } from "@/lib/transcript"
import type { FileEntry, Meta, Turn } from "@/lib/types"
import { formatBytes, formatDuration, formatTime } from "@/lib/utils"

export type PanelTab = "agents" | "files" | "trace"

/** The right-hand panel: who is working, what they produced, and what
 *  happened — the three questions the transcript alone cannot answer. */
export function RightPanel({
  tab,
  onTabChange,
  transcript,
  selectedAgent,
  onSelectAgent,
  files,
  workspace,
  turns,
  meta,
  threadId,
  onUpload,
  onDeleteFile,
  onRefreshFiles,
  onReveal,
  width,
  onWidthChange,
}: {
  tab: PanelTab
  onTabChange: (tab: PanelTab) => void
  transcript: TranscriptState
  selectedAgent?: string
  onSelectAgent: (id?: string) => void
  files: FileEntry[]
  workspace: string
  turns: Turn[]
  meta?: Meta
  threadId?: string
  onUpload: (files: File[]) => Promise<void>
  onDeleteFile: (path: string) => void
  onRefreshFiles: () => void
  onReveal: (path?: string) => void
  width: number
  onWidthChange: (width: number) => void
}) {
  return (
    <aside
      className="relative flex h-full shrink-0 flex-col border-l border-border bg-card"
      style={{ width }}
    >
      <ResizeHandle width={width} onWidthChange={onWidthChange} />
      <Tabs
        value={tab}
        onValueChange={(v) => onTabChange(v as PanelTab)}
        className="flex h-full flex-col"
      >
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <TabsList>
            <TabsTrigger value="agents">
              Agents
              {countRunning(transcript) > 0 ? (
                <Badge variant="warning" className="ml-1 px-1 py-0">
                  {countRunning(transcript)}
                </Badge>
              ) : null}
            </TabsTrigger>
            <TabsTrigger value="files">Files</TabsTrigger>
            <TabsTrigger value="trace">Trace</TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value="agents" className="thin-scrollbar overflow-y-auto">
          <AgentsTab
            transcript={transcript}
            selected={selectedAgent}
            onSelect={onSelectAgent}
          />
        </TabsContent>

        <TabsContent value="files" className="thin-scrollbar overflow-y-auto">
          <FilesTab
            files={files}
            workspace={workspace}
            threadId={threadId}
            canReveal={Boolean(meta?.capabilities?.reveal)}
            onUpload={onUpload}
            onDelete={onDeleteFile}
            onRefresh={onRefreshFiles}
            onReveal={onReveal}
          />
        </TabsContent>

        <TabsContent value="trace" className="thin-scrollbar overflow-y-auto">
          <TraceTab turns={turns} transcript={transcript} />
        </TabsContent>
      </Tabs>
    </aside>
  )
}

/** Dragging the border is the discoverable way to resize a panel, and the
 *  keyboard arrows are the accessible one. */
function ResizeHandle({
  width,
  onWidthChange,
}: {
  width: number
  onWidthChange: (width: number) => void
}) {
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the side panel"
      tabIndex={0}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId)
        const startX = e.clientX
        const startWidth = width
        const move = (ev: PointerEvent) =>
          onWidthChange(clampWidth(startWidth - (ev.clientX - startX)))
        const up = () => {
          window.removeEventListener("pointermove", move)
          window.removeEventListener("pointerup", up)
        }
        window.addEventListener("pointermove", move)
        window.addEventListener("pointerup", up)
      }}
      onKeyDown={(e) => {
        if (e.key === "ArrowLeft") onWidthChange(clampWidth(width + 24))
        if (e.key === "ArrowRight") onWidthChange(clampWidth(width - 24))
      }}
      className="absolute inset-y-0 -left-1 z-10 w-2 cursor-col-resize hover:bg-ring/40 focus-visible:bg-ring/60 focus-visible:outline-none"
    />
  )
}

function clampWidth(px: number): number {
  return Math.min(640, Math.max(260, Math.round(px)))
}

function AgentsTab({
  transcript,
  selected,
  onSelect,
}: {
  transcript: TranscriptState
  selected?: string
  onSelect: (id?: string) => void
}) {
  const workers = transcript.agentOrder.filter((id) => id !== MANAGER_ID)

  if (selected && transcript.agents[selected]) {
    const agent = transcript.agents[selected]
    return (
      <div>
        <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-card px-2 py-2">
          <Button variant="ghost" size="icon-sm" onClick={() => onSelect(undefined)}>
            <ArrowLeft />
          </Button>
          <StatusDot status={agent.status} />
          <span className="truncate text-sm font-medium">{agent.role}</span>
          <span className="truncate text-xs text-muted-foreground">{agent.id}</span>
        </div>
        {agent.error ? (
          <p className="mx-2 mt-2 rounded border border-destructive/40 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
            {agent.error}
          </p>
        ) : null}
        <AgentTranscript agent={agent} />
      </div>
    )
  }

  if (workers.length === 0) {
    return (
      <p className="px-4 py-8 text-center text-xs text-muted-foreground">
        No sub-agents yet. The manager starts them when a task is worth
        splitting up.
      </p>
    )
  }

  const active = workers.filter((id) => transcript.agents[id].status === "running")
  const finished = workers.filter((id) => transcript.agents[id].status !== "running")

  return (
    <div className="p-2">
      {active.length > 0 ? (
        <Section title={`Active (${active.length})`}>
          {active.map((id) => (
            <AgentRow key={id} agent={transcript.agents[id]} onSelect={onSelect} />
          ))}
        </Section>
      ) : null}
      {finished.length > 0 ? (
        <Section title={`Done (${finished.length})`}>
          {finished.map((id) => (
            <AgentRow key={id} agent={transcript.agents[id]} onSelect={onSelect} />
          ))}
        </Section>
      ) : null}
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-3">
      <p className="px-2 py-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {title}
      </p>
      {children}
    </div>
  )
}

function AgentRow({
  agent,
  onSelect,
}: {
  agent: { id: string; role: string; status: "running" | "done" | "failed"; activity: string }
  onSelect: (id: string) => void
}) {
  return (
    <button
      type="button"
      onClick={() => onSelect(agent.id)}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-accent/60"
    >
      <StatusDot status={agent.status} />
      <span className="shrink-0 text-sm">{agent.role}</span>
      <span className="truncate text-xs text-muted-foreground">{agent.activity}</span>
    </button>
  )
}

function FilesTab({
  files,
  workspace,
  threadId,
  canReveal,
  onUpload,
  onDelete,
  onRefresh,
  onReveal,
}: {
  files: FileEntry[]
  workspace: string
  threadId?: string
  canReveal: boolean
  onUpload: (files: File[]) => Promise<void>
  onDelete: (path: string) => void
  onRefresh: () => void
  onReveal: (path?: string) => void
}) {
  const inputRef = useRef<HTMLInputElement>(null)
  const sorted = useMemo(
    () =>
      // Directory before its contents, then alphabetical, so the tree reads
      // top-down the way the paths are nested.
      [...files].sort((a, b) => a.path.localeCompare(b.path)),
    [files],
  )

  return (
    <div className="p-2">
      <div className="flex items-center gap-1 px-1 pb-2">
        <input
          ref={inputRef}
          type="file"
          multiple
          className="hidden"
          onChange={async (e) => {
            const picked = Array.from(e.target.files ?? [])
            e.target.value = ""
            if (picked.length > 0) await onUpload(picked)
          }}
        />
        <Button variant="ghost" size="sm" className="gap-1.5" onClick={() => inputRef.current?.click()}>
          <Upload />
          Upload
        </Button>
        <Button variant="ghost" size="icon-sm" onClick={onRefresh} title="Refresh">
          <RefreshCw />
        </Button>
        {canReveal ? (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => onReveal()}
            title="Show the workspace in the file manager"
          >
            <FolderOpen />
          </Button>
        ) : null}
        {workspace ? (
          <CopyButton text={workspace} className="ml-auto" />
        ) : null}
      </div>

      {sorted.length === 0 ? (
        <p className="px-3 py-8 text-center text-xs text-muted-foreground">
          Nothing here yet. Upload files for the agents to work on, or wait for
          them to produce some.
        </p>
      ) : (
        <ul>
          {sorted.map((f) => (
            <li
              key={f.path}
              className="group flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent/60"
              // Nesting is shown by indentation rather than by repeating the
              // parent directory on every row.
              style={{ paddingLeft: 8 + depthOf(f.path) * 14 }}
            >
              {f.dir ? (
                <Folder className="size-3.5 shrink-0 text-muted-foreground" />
              ) : (
                <FileIcon className="size-3.5 shrink-0 text-muted-foreground" />
              )}
              <span className="min-w-0 flex-1 truncate text-[13px]" title={f.path}>
                {f.name}
              </span>
              {f.uploaded ? (
                <Badge variant="outline" className="shrink-0 px-1 py-0 text-[10px]">
                  yours
                </Badge>
              ) : null}
              {!f.dir ? (
                <span className="shrink-0 text-[11px] text-muted-foreground">
                  {formatBytes(f.size)}
                </span>
              ) : null}
              {!f.dir && threadId ? (
                <div className="flex shrink-0 opacity-0 transition-opacity group-hover:opacity-100">
                  <Button variant="ghost" size="icon-sm" asChild title="Download">
                    <a href={api.downloadURL(threadId, f.path)} download>
                      <Download />
                    </a>
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title="Delete"
                    onClick={() => onDelete(f.path)}
                  >
                    <Trash2 />
                  </Button>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function depthOf(path: string): number {
  return path.split("/").length - 1
}

function TraceTab({
  turns,
  transcript,
}: {
  turns: Turn[]
  transcript: TranscriptState
}) {
  const latest = turns.at(-1)
  const current = transcript.turns.at(-1)
  const turnId = current?.id ?? latest?.id

  if (!turnId) {
    return (
      <p className="px-4 py-8 text-center text-xs text-muted-foreground">
        Nothing has run in this conversation yet.
      </p>
    )
  }

  const rows = collectRows(transcript, turnId)

  return (
    <div className="p-2">
      <div className="flex items-center gap-1 rounded-md bg-muted/60 px-2 py-1.5">
        <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
          Turn
        </span>
        <code className="min-w-0 flex-1 truncate font-mono text-[11px]">{turnId}</code>
        {/* The id is the whole troubleshooting story: `zwai trace <id>`. */}
        <CopyButton text={turnId} label="Copy" />
      </div>

      {latest ? (
        <p className="px-2 py-2 text-[11px] text-muted-foreground">
          {latest.model ? `${latest.model} · ` : ""}
          {latest.reasoning_effort ? `${latest.reasoning_effort} thinking · ` : ""}
          {latest.status}
          {latest.duration_ms ? ` · ${formatDuration(latest.duration_ms)}` : ""}
        </p>
      ) : null}

      <ul className="space-y-0.5">
        {rows.map((row, i) => (
          <li
            key={`${row.at}-${i}`}
            className="flex items-start gap-2 rounded px-2 py-1 text-[12px] hover:bg-accent/60"
          >
            <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
              {formatTime(row.at)}
            </span>
            <span className="shrink-0 font-medium">{row.kind}</span>
            <span className="shrink-0 text-muted-foreground">{row.agent}</span>
            <span className="min-w-0 flex-1 truncate text-muted-foreground">
              {row.text}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function collectRows(transcript: TranscriptState, turnId: string) {
  const rows: { at: string; kind: string; agent: string; text: string }[] = []
  for (const id of transcript.agentOrder) {
    for (const b of transcript.agents[id].blocks) {
      if (b.turnId !== turnId) continue
      rows.push({
        at: b.at,
        kind: b.kind === "tool" ? (b.tool?.name ?? "tool") : b.kind,
        agent: id,
        text: b.kind === "tool" ? (b.tool?.args ?? "") : b.text,
      })
    }
  }
  return rows.sort((a, b) => a.at.localeCompare(b.at))
}

function countRunning(transcript: TranscriptState): number {
  return transcript.agentOrder.filter(
    (id) => id !== MANAGER_ID && transcript.agents[id].status === "running",
  ).length
}
