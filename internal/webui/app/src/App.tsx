// App.tsx — Codex-desktop-style three-pane layout.
// Left: sessions sidebar. Center: thread (agent text plain, tool lines as
// icon+verb summaries, thinking as "Worked for…" collapsibles, user messages
// as right-aligned pills). Right: sub-agents grouped Active/Done.
import { useEffect, useRef, useState } from "react"
import { useSwarm, startTask, connect } from "./store"
import { TranscriptLine, durationText } from "./codex"
import { AgentStatus } from "./Block"
import { cn } from "./lib/utils"
import { Plus, Clock, AtSign, Send, Flower } from "lucide-react"
import type { AgentState } from "./state"

export default function App() {
  const { state, connected } = useSwarm()
  const [selected, setSelected] = useState<number | -1>(-1)
  const [task, setTask] = useState("")
  const threadRef = useRef<HTMLDivElement>(null)

  useEffect(() => { connect() }, [])
  useEffect(() => {
    if (threadRef.current) threadRef.current.scrollTop = threadRef.current.scrollHeight
  })

  const viewingWorker = selected !== -1 ? (state.agents[selected] ?? null) : null
  const current: AgentState = viewingWorker ?? state.manager
  const active = state.agents.filter(a => !a.finished)
  const done = state.agents.filter(a => a.finished)

  function submit() {
    const t = task.trim()
    if (!t) return
    startTask(t)
    setTask("")
  }

  return (
    <div className="flex h-full">
      {/* left sidebar */}
      <aside className="hidden w-[280px] shrink-0 flex-col border-r border-[#1e3a40] bg-[#0d171a] md:flex">
        <div className="flex items-center justify-between px-4 py-3">
          <span className="text-[15px] font-bold text-[#e4e8e6]">zwai</span>
          <span className={cn("text-[11px]", connected ? "text-emerald-400" : "text-[#51707a]")}>
            {connected ? "connected" : "connecting…"}
          </span>
        </div>
        <nav className="space-y-0.5 px-2">
          <SideItem icon={<Plus className="size-4" />} label="New chat" />
          <SideItem icon={<Clock className="size-4" />} label="Scheduled" />
          <SideItem icon={<AtSign className="size-4" />} label="Plugins" />
        </nav>
        <div className="mt-5 px-4 text-[11px] font-semibold uppercase tracking-wide text-[#51707a]">
          Sub-agents
        </div>
        <div className="px-2">
          {state.agents.map(a => (
            <button
              key={a.id}
              onClick={() => {
                const idx = state.agents.indexOf(a)
                setSelected(selected === idx ? -1 : idx)
              }}
              className={cn(
                "flex w-full items-center gap-2 truncate rounded-md px-2 py-1.5 text-left text-[13px]",
                selected === state.agents.indexOf(a)
                  ? "bg-[#132429] text-[#bfe0e4]"
                  : "text-[#8aa0a6] hover:bg-[#101f23]",
              )}
            >
              <span className={cn("size-1.5 rounded-full", a.finished ? "bg-emerald-400" : "bg-[#39c5cf] animate-pulse")} />
              <span className="truncate">{a.id}</span>
            </button>
          ))}
          {state.agents.length === 0 && (
            <div className="px-2 py-1.5 text-[12px] italic text-[#51707a]">no agents yet</div>
          )}
        </div>
        <div className="mt-auto flex items-center gap-2 px-4 py-3 text-[12px] text-[#51707a]">
          <span className="inline-flex size-6 items-center justify-center rounded-full bg-emerald-500/80 text-[10px] font-bold text-black">Z</span>
          zwai session
        </div>
      </aside>

      {/* center thread */}
      <main className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-center justify-between border-b border-[#1e3a40] px-5 py-2.5">
          <span className="text-[13px] font-semibold text-[#bfe0e4]">
            {viewingWorker ? viewingWorker.id : "MANAGER"}
          </span>
          <span className="text-[11px] text-[#51707a]">
            {state.manager.blocks.length > 0
              ? `worked for ${durationText(workedMs(state.manager))}`
              : "idle"}
          </span>
        </div>
        <div ref={threadRef} className="flex-1 overflow-y-auto px-6 py-4">
          <div className="mx-auto max-w-[760px] space-y-1">
            {current.blocks.map((b, i) => (
              <TranscriptLine key={i} b={b} />
            ))}
            {current.blocks.length === 0 && (
              <div className="py-10 text-center text-[13px] italic text-[#51707a]">
                {selected === -1
                  ? "Give the swarm a goal. Simple questions get a direct answer; complex tasks fan out to sub-agents in parallel."
                  : "waiting for activity…"}
              </div>
            )}
          </div>
        </div>

        {/* floating input card, Codex-style */}
        <div className="px-6 pb-5">
          <div className="mx-auto max-w-[760px] rounded-2xl border border-[#22424a] bg-[#101c20] shadow-lg">
            <textarea
              value={task}
              onChange={e => setTask(e.target.value)}
              onKeyDown={e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit() } }}
              rows={2}
              placeholder="Do anything…（简单问题直接回答；复杂任务自动 fan-out sub-agents）"
              className="w-full resize-none bg-transparent px-4 pt-3 text-[14px] text-[#e4e8e6] outline-none placeholder:text-[#51707a]"
            />
            <div className="flex items-center gap-3 px-3 pb-2.5">
              <button className="rounded-md p-1 text-[#51707a] hover:text-[#b8b8bc]">＋</button>
              <span className="text-[12px] font-medium text-amber-400">Full access</span>
              <span className="ml-auto text-[12px] text-[#8aa0a6]">
                {modelLabel()} <span className="opacity-60">⌄</span>
              </span>
              <button
                onClick={submit}
                className="inline-flex size-8 items-center justify-center rounded-full bg-black text-white ring-1 ring-[#39c5cf]/60"
                aria-label="run"
              >
                <Send className="size-3.5" />
              </button>
            </div>
          </div>
        </div>
      </main>

      {/* right: sub-agents panel */}
      <aside className="hidden w-[380px] shrink-0 flex-col border-l border-[#1e3a40] bg-[#0d171a] lg:flex">
        <div className="px-4 py-2.5">
          <div className="text-[12px] text-[#51707a]">
            Active · {state.agents.filter(a => !a.finished).length}
          </div>
          {active.length === 0 && (
            <div className="py-1 text-[12px] italic text-[#51707a]">No active subagents</div>
          )}
          {active.map(a => (
            <button
              key={a.id}
              onClick={() => {
                const idx = state.agents.indexOf(a)
                setSelected(selected === idx ? -1 : idx)
              }}
              className="flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left hover:bg-[#101f23]"
            >
              <span className="text-[#39c5cf]"><Flower className="size-4" /></span>
              <span className="flex-1 truncate text-[14px] text-[#d8e4e6]">{a.id}</span>
              <span className="text-[12px] text-[#51707a]"><AgentStatus a={a} /></span>
            </button>
          ))}
        </div>
        <div className="px-4 pb-2 pt-4 text-[12px] text-[#51707a]">
          Done · {done.length}
        </div>
        <div className="flex-1 overflow-y-auto px-4">
          {done.map(a => (
            <button
              key={a.id}
              onClick={() => {
                const idx = state.agents.indexOf(a)
                setSelected(selected === idx ? -1 : idx)
              }}
              className="flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left hover:bg-[#101f23]"
            >
              <span className={cn("size-3 rounded-full", a.finErr ? "bg-[#ff6b6b]" : "bg-emerald-400")} />
              <span className="flex-1 truncate text-[14px] text-[#d8e4e6]">{a.id}</span>
              {a.activityTail && (
                <span className="max-w-[40%] truncate text-[12px] text-[#51707a]">{a.activityTail}</span>
              )}
            </button>
          ))}
          {done.length === 0 && (
            <div className="py-1 text-[12px] italic text-[#51707a]">nothing finished yet</div>
          )}
        </div>
      </aside>
    </div>
  )
}

function workedMs(a: AgentState): number {
  // rough: from first block start to last finish
  const first = a.blocks[0]?.startedAt
  if (!first) return 0
  return Date.now() - first
}

function modelLabel(): string {
  return "DeepSeek V4 Flash"
}

function SideItem({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <button className="flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-[13px] text-[#8aa0a6] hover:bg-[#101f23]">
      {icon}
      {label}
    </button>
  )
}
