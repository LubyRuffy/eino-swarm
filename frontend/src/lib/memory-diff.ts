import { diffText, type EditDiff } from "./edit-diff"
import type { MessageKey } from "./i18n"

type MemoryChangeView = { diff?: EditDiff; noticeKey?: MessageKey }

/** A selector is only a substring of a note. Prefer the tool's stored images;
 * older calls can show their request, but cannot reconstruct the full old note. */
export function memoryChangeView(args: string, result?: string, failed = false): MemoryChangeView {
  const call = parseObject(args)
  const action = text(call?.action).trim().toLowerCase()
  if (!["add", "replace", "remove"].includes(action)) return {}
  const receipt = parseObject(result)
  const saved = receipt?.success === true && !failed
  if (saved && receipt.changed === false) return { noticeKey: "tool.memoryUnchanged" }
  if (saved && typeof receipt.before === "string" && typeof receipt.after === "string") {
    return { diff: diffText("MEMORY.md", receipt.before, receipt.after) }
  }
  const before = action === "add" ? "" : text(call?.old_text)
  const after = action === "remove" ? "" : text(call?.content)
  if ((action !== "add" && !before.trim()) || (action !== "remove" && !after.trim())) return {}
  return {
    diff: diffText("MEMORY.md", before, after),
    noticeKey: saved ? "tool.memoryPreview" : "tool.requestedChange",
  }
}

function parseObject(raw?: string): Record<string, unknown> | undefined {
  try {
    const value: unknown = JSON.parse(raw ?? "")
    return value && typeof value === "object" && !Array.isArray(value)
      ? value as Record<string, unknown> : undefined
  } catch {
    return undefined
  }
}

function text(value: unknown): string {
  return typeof value === "string" ? value : ""
}
