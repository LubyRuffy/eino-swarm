import { ArrowLeft, ChevronDown } from "lucide-react"
import { useRef, useState } from "react"

import { AgentPromptButton } from "@/components/app/agent-prompt"
import { FilesTab } from "@/components/app/files-tab"
import { MemoryPanel, type MemoryPanelProps } from "@/components/app/memory-panel"
import { ResizeHandle } from "@/components/app/resize-handle"
import { TraceTab } from "@/components/app/trace-tab"
import { AgentTranscript } from "@/components/app/agent-transcript"
import { StatusDot } from "@/components/app/transcript"
import { MarqueeText } from "@/components/app/marquee"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useTranscriptFollow } from "@/lib/follow-scroll"
import { MANAGER_ID, type AgentState, type TranscriptState } from "@/lib/transcript"
import type { FileEntry, Meta, Turn, UsageSnapshot } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

export type PanelTab = "agents" | "files" | "trace" | "memory"

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
  memory,
  onUpload,
  onDeleteFile,
  onRefreshFiles,
  onReveal,
  usage,
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
  /** Absent when there is no project to show memory for: the open
   *  conversation is not in one, and the sidebar has not asked to open a
   *  project's skill either. */
  memory?: MemoryPanelProps
  onUpload: (files: File[]) => Promise<unknown>
  onDeleteFile: (path: string) => void
  onRefreshFiles: () => void
  onReveal: (path?: string) => void
  usage?: UsageSnapshot | null
}) {
  const t = useT()
  const [width, setWidth] = useState(352)
  return (
    <aside
      className="relative flex h-full min-w-0 shrink-0 flex-col overflow-hidden border-l border-border bg-card"
      style={{ width }}
    >
      <ResizeHandle
        width={width}
        onWidthChange={setWidth}
        edge="left"
        label={t("panel.resize")}
        min={260}
        max={640}
      />
      <Tabs
        value={tab}
        onValueChange={(v) => onTabChange(v as PanelTab)}
        className="flex h-full flex-col"
      >
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <TabsList>
            <TabsTrigger value="agents">
              {t("panel.agents")}
              {countRunning(transcript) > 0 ? (
                <Badge variant="warning" className="ml-1 px-1 py-0">
                  {countRunning(transcript)}
                </Badge>
              ) : null}
            </TabsTrigger>
            <TabsTrigger value="files">{t("panel.files")}</TabsTrigger>
            <TabsTrigger value="trace">{t("panel.trace")}</TabsTrigger>
            {memory ? (
              <TabsTrigger value="memory">
                {t("panel.memory")}
                {memory.unread ? (
                  <Badge variant="warning" className="ml-1 px-1 py-0">
                    {t("panel.memoryNew")}
                  </Badge>
                ) : null}
              </TabsTrigger>
            ) : null}
          </TabsList>
        </div>

        <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
          <TabsContent
            value="agents"
            className="flex min-h-0 flex-1 flex-col overflow-hidden bg-card"
          >
            <AgentsTab
              transcript={transcript}
              selected={selectedAgent}
              onSelect={onSelectAgent}
            />
          </TabsContent>

          <TabsContent
            value="files"
            className="flex min-h-0 flex-1 flex-col overflow-hidden bg-card"
          >
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

          <TabsContent
            value="trace"
            className="thin-scrollbar min-h-0 flex-1 overflow-auto bg-card"
          >
            <TraceTab turns={turns} transcript={transcript} usage={usage} />
          </TabsContent>

          {memory ? (
            <TabsContent
              value="memory"
              className="flex min-h-0 flex-1 flex-col overflow-hidden bg-card"
            >
              <MemoryPanel {...memory} />
            </TabsContent>
          ) : null}
        </div>
      </Tabs>
    </aside>
  )
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
  const t = useT()
  const workers = transcript.agentOrder.filter((id) => id !== MANAGER_ID)

  if (selected && transcript.agents[selected]) {
    const agent = transcript.agents[selected]
    // Chrome sits outside the scroller: a sticky bar inside it lets the
    // transcript paint through the back button the moment you drag-scroll.
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div
          data-testid="agent-chrome"
          className="flex shrink-0 items-center gap-2 border-b border-border bg-card px-2 py-2"
        >
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => onSelect(undefined)}
            aria-label={t("panel.back")}
          >
            <ArrowLeft />
          </Button>
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <StatusDot status={agent.status} />
            <span className="truncate text-sm font-medium">{agent.role}</span>
            <span className="truncate text-xs text-muted-foreground">{agent.id}</span>
          </div>
          {agent.instruction ? (
            <AgentPromptButton instruction={agent.instruction} />
          ) : null}
        </div>
        <AgentLog key={agent.id} agent={agent} />
      </div>
    )
  }

  if (workers.length === 0) {
    return (
      <p className="px-4 py-8 text-center text-xs text-muted-foreground">
        {t("panel.noAgents")}
      </p>
    )
  }

  const active = workers.filter((id) => transcript.agents[id].status === "running")
  const finished = workers.filter((id) => transcript.agents[id].status !== "running")

  return (
    <div className="thin-scrollbar min-h-0 flex-1 overflow-y-auto p-2">
      {active.length > 0 ? (
        <Section title={t("panel.active", { n: active.length })}>
          {active.map((id) => (
            <AgentRow key={id} agent={transcript.agents[id]} onSelect={onSelect} />
          ))}
        </Section>
      ) : null}
      {finished.length > 0 ? (
        <Section title={t("panel.done", { n: finished.length })}>
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

/** One worker's log. Same live-edge contract as the conversation: open at
 *  the latest line, follow while the reader stays there, freeze on wheel-up. */
function AgentLog({ agent }: { agent: AgentState }) {
  const t = useT()
  const loading = useApp((s) => s.agentLogLoading === agent.id)
  const scrollerRef = useRef<HTMLDivElement>(null)
  const last = agent.blocks.at(-1)
  const { showJump, jumpToLatest } = useTranscriptFollow({
    scrollerRef,
    loaded: true,
    threadId: agent.id,
    growthKey: `${agent.blocks.length}:${last?.text.length ?? 0}`,
  })
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={scrollerRef}
        data-testid="agent-scroller"
        data-quote-source=""
        className="thin-scrollbar min-h-0 flex-1 overflow-y-auto [overflow-anchor:none]"
      >
        {agent.error ? (
          <p className="mx-2 mt-2 rounded border border-destructive/40 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">
            {agent.error}
          </p>
        ) : null}
        {loading && agent.blocks.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-foreground">
            {t("transcript.agentLoading")}
          </p>
        ) : (
          <AgentTranscript agent={agent} />
        )}
      </div>
      {showJump ? (
        <Button
          type="button"
          variant="outline"
          size="icon"
          data-testid="agent-jump-to-latest"
          aria-label={t("transcript.jumpLatest")}
          className="absolute right-3 bottom-3 z-20 rounded-full bg-background shadow-md"
          onClick={jumpToLatest}
        >
          <ChevronDown />
        </Button>
      ) : null}
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
      <span className="shrink-0 truncate text-xs text-muted-foreground">{agent.id}</span>
      <MarqueeText
        text={agent.activity}
        active={agent.status === "running"}
        className="text-xs text-muted-foreground"
      />
    </button>
  )
}

function countRunning(transcript: TranscriptState): number {
  return transcript.agentOrder.filter(
    (id) => id !== MANAGER_ID && transcript.agents[id].status === "running",
  ).length
}
