import { Button } from "@/components/ui/button"
import type { ComposerDraftSession, ComposerDraftSnapshot } from "@/lib/composer-draft"
import { useT } from "@/lib/use-t"

export function ComposerDraftStatus({ session, snapshot }: { session: ComposerDraftSession; snapshot: ComposerDraftSnapshot }) {
  const t = useT()
  const uncertain = snapshot.draft.state === "uncertain"
  const hasDraft = Boolean(snapshot.draft.text || snapshot.draft.quotes.length || snapshot.draft.files.length)
  return <div data-testid="composer-draft-status" className="pointer-events-auto px-4 py-1 text-xs text-muted-foreground" aria-live="polite">
    <p role={snapshot.error || uncertain ? "alert" : "status"}>
      {uncertain ? t("draft.uncertain") : snapshot.loading ? t("draft.loading") : snapshot.error ? t("draft.failed") : snapshot.saved ? t("draft.saved") : t("draft.saving")}
    </p>
    {snapshot.error ? <p className="break-words text-destructive">{snapshot.error}</p> : null}
    {snapshot.conflict ? <><p>{t("draft.conflict")}</p>
      <Button type="button" variant="link" size="sm" disabled={snapshot.saving} onClick={() => void session.useStoredDraft().catch(() => undefined)}>{t("draft.useStored")}</Button>
      <Button type="button" variant="link" size="sm" disabled={snapshot.saving} onClick={() => void session.saveCurrentDraft().catch(() => undefined)}>{t("draft.useCurrent")}</Button>
    </> : null}
    {snapshot.error && !snapshot.conflict ? <Button type="button" variant="link" size="sm" disabled={snapshot.saving || snapshot.loading}
      onClick={() => void (snapshot.loaded ? session.flush() : session.load()).catch(() => undefined)}>
      {t(snapshot.loaded ? "draft.retry" : "draft.retryLoad")}
    </Button> : null}
    {uncertain ? <Button type="button" variant="link" size="sm" disabled={snapshot.saving}
      onClick={() => void session.resumeEditing().catch(() => undefined)}>{t("draft.resume")}</Button> : null}
    {hasDraft ? <Button type="button" variant="link" size="sm" disabled={snapshot.saving || snapshot.loading || snapshot.draft.state === "sending"}
      onClick={() => void session.discard().catch(() => undefined)}>{t("draft.discard")}</Button> : null}
  </div>
}
