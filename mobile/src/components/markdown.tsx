import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"

const PLUGINS = [remarkGfm]

export function PhoneMarkdown({ text }: { text: string }) {
  return (
    <div className="md-body text-sm">
      <Markdown remarkPlugins={PLUGINS}>{text}</Markdown>
    </div>
  )
}
