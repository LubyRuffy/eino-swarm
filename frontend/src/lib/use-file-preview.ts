import { useEffect, useReducer, useState } from "react"

import { readFilePreview, type FilePreview } from "@/lib/file-preview"

type PreviewState = { key: string; file?: FilePreview; url?: string; error?: string }

/** Each selection owns its request and image URL. Closing or changing the
 * conversation must not let a late file replace the currently visible one. */
export function useFilePreview(threadId: string, path: string) {
  const [revision, refresh] = useReducer((n: number) => n + 1, 0)
  const key = JSON.stringify([threadId, path, revision])
  const [state, setState] = useState<PreviewState>()

  useEffect(() => {
    const controller = new AbortController()
    let url: string | undefined
    void readFilePreview(threadId, path, controller.signal).then((file) => {
      if (controller.signal.aborted) return
      if (file.kind === "image") url = URL.createObjectURL(file.blob)
      setState({ key, file, url })
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) {
        setState({ key, error: cause instanceof Error ? cause.message : String(cause) })
      }
    })
    return () => {
      controller.abort()
      if (url) URL.revokeObjectURL(url)
    }
  }, [threadId, path, key])

  const current = state?.key === key ? state : undefined
  return { ...current, loading: !current, refresh }
}
