import { useEffect, useState } from "react"

import { MemoMarkdown } from "@/components/app/markdown"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"
import { updateOpenClient } from "@/lib/client-open"
import { contentTypeClass } from "@/lib/chrome-type"
import type { ClientTranscript } from "@/lib/local-clients"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

/** The same column as a conversation. The composer stays mounted above this
 *  and is locked by the shell; this view never sends. */
export function ClientChat({ id }: { id: string }) {
  const t = useT()
  const [doc, setDoc] = useState<ClientTranscript | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let stop = false
    let busy = false
    const load = () => {
      if (busy) return
      busy = true
      api
        .clientTask(id)
        .then((next) => {
          if (stop) return
          setDoc(next)
          setFailed(false)
          updateOpenClient({ title: next.title, status: next.status })
        })
        .catch(() => {
          if (!stop) setFailed(true)
        })
        .finally(() => {
          busy = false
        })
    }
    setDoc(null)
    setFailed(false)
    load()
    const timer = window.setInterval(load, 2000)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [id])

  return (
    <div
      data-testid="client-chat"
      className="min-h-0 flex-1 overflow-y-auto pb-composer"
    >
      <div className="content-column content-gutter flex flex-col gap-4 py-6">
        {failed && !doc ? (
          <p className={cn(contentTypeClass, "text-muted-foreground")}>{t("sidebar.clientMissing")}</p>
        ) : null}
        {!doc && !failed
          ? [0, 1, 2].map((i) => <Skeleton key={i} className="h-12 w-2/3" />)
          : null}
        {doc?.entries.map((entry, i) =>
          entry.role === "user" ? (
            <div key={`${entry.role}-${i}`} className="flex justify-end">
              <div
                data-testid="user-message"
                className={cn(
                  contentTypeClass,
                  "max-w-[85%] rounded-2xl rounded-br-md bg-secondary px-4 py-2.5 text-secondary-foreground",
                )}
              >
                {entry.text}
              </div>
            </div>
          ) : entry.role === "tool" ? (
            <p
              key={`${entry.role}-${i}`}
              data-testid="client-tool"
              className={cn(contentTypeClass, "text-xs text-muted-foreground")}
            >
              {entry.text}
            </p>
          ) : (
            <div key={`${entry.role}-${i}`} data-testid="assistant-message" className={contentTypeClass}>
              <MemoMarkdown text={entry.text} />
            </div>
          ),
        )}
        {doc?.truncated ? (
          <p className={cn(contentTypeClass, "text-xs text-muted-foreground")}>
            {t("sidebar.clientTruncated")}
          </p>
        ) : null}
      </div>
    </div>
  )
}
