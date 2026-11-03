import { foldPhoneItems, managerBlocks, type CompactBlock } from "./transcript"
import { dropPackedJson } from "./tool-preview"

/** A boundary or a filtered empty bubble cannot give a reader conversation
 * context. Opening should keep paging until content can actually be painted. */
export function hasPhoneHistoryContent(blocks: CompactBlock[]): boolean {
  return foldPhoneItems(managerBlocks(blocks)).some((item) => {
    if (item.type === "work") return true
    const block = item.block
    if (block.kind === "turn_end") return false
    if (block.kind === "user" && block.hasImages) return true
    if (["user", "steer", "answer"].includes(block.kind)) {
      return Boolean(dropPackedJson(block.text).trim())
    }
    return true
  })
}
