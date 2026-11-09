import { useState } from "react"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

/** Repair is explicit and does not resend input or replay uncertain tools. */
export function WorkerModelRecovery({ turnId }: { turnId?: string }) {
  const t = useT()
  const id = useApp((s) => s.activeId)
  const thread = useApp((s) => s.threads.find((row) => row.id === s.activeId))
  const latest = useApp((s) => s.transcript.turns.at(-1))
  const unavailable = useApp((s) => !s.loaded || s.status.running || s.transcript.running || s.status.awaiting_answer || s.meta?.startup === "restoring")
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<{ ok: boolean; text: string }>()
  const selected = useApp((s) => s.models.find((row) => row.ready && row.provider_id === thread?.provider_id && (!thread?.model || row.model === thread.model)))
  if (!id || !thread || !turnId || latest?.id !== turnId || latest.status !== "error") return null
  async function repair() {
    if (pending || unavailable || !selected || !id) return
    setPending(true)
    setResult(undefined)
    try {
      await api.recoverWorkerModels(id, selected.provider_id, selected.model)
      setResult({ ok: true, text: t("transcript.workerRecoverySaved") })
    } catch (error) {
      setResult({ ok: false, text: error instanceof Error ? error.message : String(error) })
    } finally {
      setPending(false)
    }
  }
  return (
    <div className="mt-2" data-testid="worker-model-recovery">
      <p>{t("transcript.workerRecoveryHint")}</p>
      <Button type="button" size="sm" className="mt-2" disabled={unavailable || pending || !selected || result?.ok} onClick={() => void repair()}>
        {pending ? t("transcript.workerRecoverySaving") : t("transcript.workerRecoveryAction", { model: selected?.model ?? t("transcript.workerRecoverySelect") })}
      </Button>
      {result ? <p role="status" className="mt-1">{result.text}</p> : null}
    </div>
  )
}
