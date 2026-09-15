import { ArrowUp, Paperclip, ShieldCheck, Square, X } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import type { ModelInfo } from "@/lib/types"
import { formatBytes } from "@/lib/utils"

/** The composer. While a turn is running, Enter steers instead of queuing:
 *  the server decides, so there is no state to get wrong here. */
export function Composer({
  running,
  models,
  provider,
  onProviderChange,
  onSend,
  onStop,
  onUpload,
  disabled,
  text,
  onTextChange,
  focusSignal,
}: {
  running: boolean
  models: ModelInfo[]
  provider?: string
  onProviderChange: (id: string) => void
  onSend: (text: string) => void
  onStop: () => void
  onUpload: (files: File[]) => Promise<void>
  disabled?: boolean
  /** Held above so a keyboard shortcut or a starter idea can fill it in. */
  text: string
  onTextChange: (text: string) => void
  focusSignal: number
}) {
  const [pending, setPending] = useState<File[]>([])
  const [uploading, setUploading] = useState(false)
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  // Grow with the text, but stop before the composer eats the conversation.
  useEffect(() => {
    const el = areaRef.current
    if (!el) return
    el.style.height = "auto"
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [text])

  useEffect(() => {
    const el = areaRef.current
    if (!el) return
    el.focus()
    el.setSelectionRange(el.value.length, el.value.length)
  }, [focusSignal])

  const submit = async () => {
    const value = text.trim()
    if (!value && pending.length === 0) return
    if (pending.length > 0) {
      setUploading(true)
      try {
        await onUpload(pending)
        setPending([])
      } finally {
        setUploading(false)
      }
    }
    if (value) {
      onSend(value)
      onTextChange("")
    }
  }

  return (
    <div className="border-t border-border bg-background px-4 pb-4 pt-3 sm:px-8">
      <div className="mx-auto max-w-3xl">
        {pending.length > 0 ? (
          <div className="mb-2 flex flex-wrap gap-1.5">
            {pending.map((f, i) => (
              <Badge key={`${f.name}-${i}`} variant="outline" className="gap-1.5 py-1">
                <Paperclip className="size-3" />
                <span className="max-w-48 truncate">{f.name}</span>
                <span className="text-muted-foreground">{formatBytes(f.size)}</span>
                <button
                  type="button"
                  aria-label={`Remove ${f.name}`}
                  onClick={() => setPending((prev) => prev.filter((_, j) => j !== i))}
                  className="ml-0.5 rounded hover:text-foreground"
                >
                  <X className="size-3" />
                </button>
              </Badge>
            ))}
          </div>
        ) : null}

        <div className="rounded-2xl border border-input bg-card shadow-sm transition-colors focus-within:border-ring">
          <Textarea
            ref={areaRef}
            value={text}
            rows={1}
            disabled={disabled}
            data-testid="composer-input"
            placeholder={
              running
                ? "Working… type to steer it"
                : "Describe what you want done. It will delegate as needed."
            }
            className="max-h-[200px] min-h-[44px] px-4 py-3 text-[15px]"
            onChange={(e) => onTextChange(e.target.value)}
            onKeyDown={(e) => {
              // Enter sends; Shift+Enter is a newline. That is what every chat
              // app does, and muscle memory is not negotiable.
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                void submit()
              }
            }}
          />

          <div className="flex items-center gap-1.5 px-2 pb-2">
            <input
              ref={fileRef}
              type="file"
              multiple
              className="hidden"
              data-testid="file-input"
              onChange={(e) => {
                const files = Array.from(e.target.files ?? [])
                if (files.length > 0) setPending((prev) => [...prev, ...files])
                e.target.value = ""
              }}
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  disabled={disabled}
                  onClick={() => fileRef.current?.click()}
                  aria-label="Attach files"
                >
                  <Paperclip />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Attach files to the workspace</TooltipContent>
            </Tooltip>

            {models.length > 1 ? (
              <Select value={provider} onValueChange={onProviderChange}>
                <SelectTrigger className="h-7 w-auto gap-1.5 border-none bg-transparent px-2 shadow-none hover:bg-accent">
                  <SelectValue placeholder="Model" />
                </SelectTrigger>
                <SelectContent>
                  {models.map((m) => (
                    <SelectItem key={m.id} value={m.id}>
                      {m.label || m.model || m.id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : models.length === 1 ? (
              <span className="px-1 text-xs text-muted-foreground">
                {models[0].label || models[0].model || models[0].id}
              </span>
            ) : null}

            <Tooltip>
              <TooltipTrigger asChild>
                <Badge variant="outline" className="gap-1">
                  <ShieldCheck className="size-3" />
                  Full access
                </Badge>
              </TooltipTrigger>
              <TooltipContent>
                Agents can read and write files in this conversation's workspace
                and run commands.
              </TooltipContent>
            </Tooltip>

            <div className="ml-auto">
              {running ? (
                <Button
                  size="icon"
                  variant="secondary"
                  onClick={onStop}
                  aria-label="Stop"
                  title="Stop"
                >
                  <Square className="size-3.5 fill-current" />
                </Button>
              ) : (
                <Button
                  size="icon"
                  disabled={
                    disabled || uploading || (!text.trim() && pending.length === 0)
                  }
                  onClick={() => void submit()}
                  aria-label="Send"
                  title="Send (Enter)"
                >
                  <ArrowUp />
                </Button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
