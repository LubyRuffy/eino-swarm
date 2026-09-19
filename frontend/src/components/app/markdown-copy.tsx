import { useState } from "react"
import { Check, Copy } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useT } from "@/lib/use-t"

/** Copy the fence or formula source. Icon-only so the toolbar does not
 *  become a second caption; the accessible name says what it copies. */
export function MarkdownCopy({
  text,
  kind,
}: {
  text: string
  kind: "code" | "formula"
}) {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const name = kind === "formula" ? t("markdown.copyFormula") : t("markdown.copyCode")
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          setTimeout(() => setCopied(false), 1200)
        } catch {
          // Clipboard access can be denied; the button just does nothing
          // rather than throwing an error at the user.
        }
      }}
      title={copied ? t("transcript.copied") : name}
      aria-label={name}
    >
      {copied ? <Check /> : <Copy />}
    </Button>
  )
}
