// Codex-desktop-style transcript components.
// Tool lines become icon + verb-phrase summaries (never raw JSON);
// thinking collapses into a "Worked for Xm Ys" summary line.
import { useState } from "react"
import { Brain, ChevronDown, ChevronRight, Wrench, FileText, MessageSquare, FolderOpen, Sparkles, Search } from "lucide-react"
import { firstWords, toolNameOf } from "./state"
import type { Block } from "./state"

// toolSummary turns "tool_name({json})" into a Codex-style verb phrase.
export function toolSummary(text: string): { icon: React.ReactNode; phrase: string } {
  const name = toolNameOf(text)
  switch (name) {
    case "spawn_agent":
      return { icon: <Sparkles className="size-3.5" />, phrase: "Spawned a researcher sub-agent" }
    case "send_message":
      return { icon: <MessageSquare className="size-3.5" />, phrase: "Sent a message to a sub-agent" }
    case "wait_agents":
      return { icon: <FolderOpen className="size-3.5" />, phrase: "Waited for sub-agent results" }
    case "close_agent":
      return { icon: <FolderOpen className="size-3.5" />, phrase: "Closed a finished sub-agent" }
    case "web_search":
      return { icon: <Search className="size-3.5" />, phrase: "Searched the web" }
    case "web_fetch":
      return { icon: <FileText className="size-3.5" />, phrase: "Fetched a web page" }
    default:
      return { icon: <Wrench className="size-3.5" />, phrase: name.replace(/_/g, " ") }
  }
}

// durationText formats ms like Codex: "1h 12m 14s".
export function durationText(ms: number): string {
  if (!ms || ms <= 0) return "0s"
  const s = Math.floor(ms / 1000)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return `${h}h ${m}m ${sec}s`
  if (m > 0) return `${m}m ${sec}s`
  return `${sec}s`
}

// TranscriptLine renders one block in Codex style.
export function TranscriptLine({ b }: { b: Block }) {
  const [open, setOpen] = useState(false)

  if (b.kind === "thinking") {
    return (
      <div>
        <button
          onClick={() => setOpen(!open)}
          className="flex w-full items-center gap-1.5 py-1 text-left text-[13px] text-[#8a8a8e] hover:text-[#b8b8bc]"
        >
          {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
          Worked for {durationText(b.elapsedMs ?? 0)}
          {b.live && (
            <span className="ml-1 inline-flex items-center gap-1 text-[#39c5cf]">
              <Brain className="size-3 animate-pulse" /> thinking…
            </span>
          )}
        </button>
        {open && (
          <div className="ml-1 my-1 border-l-2 border-[#2a4a52] pl-3 text-[13px] italic text-[#8a9aa0] whitespace-pre-wrap max-h-56 overflow-y-auto">
            {b.text}
          </div>
        )}
      </div>
    )
  }

  if (b.kind === "tool") {
    const { icon, phrase } = toolSummary(b.name + "(" + (b.args || "") + ")")
    return (
      <div>
        <button
          onClick={() => setOpen(!open)}
          className="flex w-full items-center gap-2 py-1.5 text-left text-[13px] text-[#8a8a8e] hover:text-[#b8b8bc]"
        >
          <span className="shrink-0">{icon}</span>
          <span className="truncate">{b.res ? phrase + " · " + firstWords(b.res, 8) : phrase}</span>
          {b.res && (open ? <ChevronDown className="size-3 shrink-0" /> : <ChevronRight className="size-3 shrink-0" />)}
        </button>
        {open && b.res && (
          <div className="ml-6 my-1 max-h-40 overflow-y-auto rounded-md border border-[#22424a] bg-[#0f2024] p-2 text-[12px] text-[#9ab] whitespace-pre-wrap">
            {b.res}
          </div>
        )}
      </div>
    )
  }

  // answer: plain text on background (no bubble), Codex-style
  return (
    <div className="my-2 text-[15px] leading-[1.65] text-[#e4e8e6] whitespace-pre-wrap">
      {b.text}
    </div>
  )
}
