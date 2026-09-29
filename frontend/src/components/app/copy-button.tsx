import { Check, Copy } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { copyText } from "@/lib/copy-text"
import { useT } from "@/lib/use-t"

export function CopyButton({
  text,
  className,
  label,
  size = "icon-sm",
  "aria-label": ariaLabel,
}: {
  text: string
  className?: string
  label?: string
  size?: "icon-sm" | "icon-xs" | "icon-2xs"
  "aria-label"?: string
}) {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const name = ariaLabel ?? label ?? t("transcript.copy")
  const glyph = size === "icon-2xs" ? "size-3" : "size-3.5"
  return (
    <Button
      type="button"
      variant="ghost"
      size={label ? "sm" : size}
      className={className}
      onClick={() => {
        void copyText(text).then((ok) => {
          if (!ok) return
          setCopied(true)
          setTimeout(() => setCopied(false), 1200)
        })
      }}
      title={copied ? t("transcript.copied") : name}
      aria-label={name}
    >
      {copied ? <Check className={glyph} /> : <Copy className={glyph} />}
      {label ? <span>{copied ? t("transcript.copied") : label}</span> : null}
    </Button>
  )
}
