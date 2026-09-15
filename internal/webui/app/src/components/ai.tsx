// Components mirror shadcn.io/ai families (chat / reasoning / agent / dev),
// adapted to consume swarm.Notification SSE events.

import { cn } from "../lib/utils"
import type { Block } from "../state"



function truncate(s: string, n: number) {
  return s.length <= n ? s : s.slice(0, n) + "…"
}
function firstWords(s: string, n: number) {
  const f = s.split(/\s+/)
  return f.length <= n ? s : f.slice(0, n).join(" ") + "…"
}
function lastLine(s: string) {
  const i = s.lastIndexOf("\n")
  return i >= 0 ? s.slice(i + 1) : s
}

/* ─────────── reasoning family ─────────── */

// Reasoning: collapsible thinking block. Streams open, auto-collapses when done.
export function Reasoning({ block }: { block: Block }) {
  const live = !!block.live
  if (!block.open && !live) {
    return (
      <div className="text-[#7a9aa0] text-[12px] italic">
        ▸ 💭 thought ({block.text.length}) {firstWords(lastLine(block.text), 14)}
      </div>
    )
  }
  return (
    <div className="my-1">
      <div className="flex items-center gap-1.5 text-[12px] text-[#7a9aa0]">
        <span className="animate-pulse">💭</span> thinking… <span className="opacity-50">(t to fold)</span>
      </div>
      <div className="ml-3 mt-1 pl-2.5 border-l-2 border-[#22424a] text-[#8fa8ad] italic text-[12px] whitespace-pre-wrap max-h-48 overflow-y-auto">
        {block.text}
      </div>
    </div>
  )
}

export function ReasoningCollapsed({ block }: { block: Block }) {
  return (
    <div className="text-[#7a9aa0] text-[12px] italic">
      ▸ 💭 thought ({block.text.length}) {firstWords(lastLine(block.text), 14)}
    </div>
  )
}

/* ─────────── agent family ─────────── */

// Tool: one tool invocation with args + result, collapsible.
export function Tool({ block }: { block: Block }) {
  return (
    <div className="ml-1 my-1">
      <div className="text-[12px]">
        <span className="text-[#ffb86c]">⚙ {block.name}</span>
        {block.args && <span className="text-[#c9b48a] text-[11px] ml-2 break-all">{truncate(block.args, 160)}</span>}
      </div>
      {block.res && <div className="ml-3 text-[#9dd0a5] text-[11px]">← {truncate(block.res, 120)}</div>}
    </div>
  )
}

// Task: a spawned sub-agent rendered as a task card in the manager transcript.
export function Task({ name, status }: { name: string; status: string }) {
  return (
    <div className="my-1 flex items-center gap-2 text-[12px]">
      <span className={status === "✓ done" ? "text-[#42d392]" : "text-[#39c5cf]"}>{status.startsWith("✗") ? "✗" : "●"}</span>
      <span className="text-[#bfe0e4]">{name}</span>
      <span className="text-[#51707a] text-[11px]">{status}</span>
    </div>
  )
}

/* ─────────── chat family ─────────── */

// Message: one row in the conversation with role alignment.
export function Message({ role, children }: { role: "user" | "manager" | "worker"; children: React.ReactNode }) {
  return (
    <div className={cn("flex w-full", role === "user" ? "justify-end" : "justify-start")}>
      <div className={cn(
        "max-w-[85%] rounded-xl px-3.5 py-2",
        role === "user"
          ? "bg-[#173c44] text-[#c7ecef]"
          : "bg-[#122428] text-[#d8e4e6]",
      )}>
        {children}
      </div>
    </div>
  )
}

// PromptInput: bottom composer.
export function PromptInput({ value, onChange, onSubmit, placeholder }: {
  value: string; onChange: (v: string) => void; onSubmit: () => void; placeholder?: string
}) {
  return (
    <div className="flex gap-3">
      <input
        value={value}
        onChange={e => onChange(e.target.value)}
        onKeyDown={e => { if (e.key === "Enter") onSubmit() }}
        placeholder={placeholder}
        className="flex-1 bg-[#0f2024] border border-[#1e3a40] rounded-lg px-4 py-2 outline-none focus:border-[#39c5cf]"
      />
      <button
        onClick={onSubmit}
        className="bg-[#173c44] hover:bg-[#1d4a54] text-[#39c5cf] rounded-lg px-5 font-medium transition-colors"
      >
        Run
      </button>
    </div>
  )
}

// Loader: inline streaming indicator.
export function Loader({ label }: { label: string }) {
  return (
    <div className="text-[#39c5cf] text-[12px] animate-pulse">{label}</div>
  )
}

