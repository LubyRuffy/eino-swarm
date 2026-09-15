import { memo } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"

/** remark plugins are module-level so a memoised markdown block is not
 *  invalidated by a new array on every parent render. */
const PLUGINS = [remarkGfm]

/** Completed markdown is immutable. Without this memo, every streamed token
 *  would re-parse every finished answer in the conversation. */
export const MemoMarkdown = memo(function MemoMarkdown({ text }: { text: string }) {
  return <Markdown remarkPlugins={PLUGINS}>{text}</Markdown>
})
