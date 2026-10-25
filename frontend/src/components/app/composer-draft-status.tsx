import { Button } from "@/components/ui/button"
import type { ComposerDraftSession, ComposerDraftSnapshot } from "@/lib/composer-draft"
import { useT } from "@/lib/use-t"

export function ComposerDraftStatus({ session, snapshot, disabled = false }: { session: ComposerDraftSession; snapshot: ComposerDraftSnapshot; disabled?: boolean }) {
  const t = useT()
  const uncertain = snapshot.draft.state === "uncertain"
  if (!snapshot.error && !snapshot.conflict && !uncertain) return null
  const hasDraft = Boolean(snapshot.draft.text || snapshot.draft.quotes.length || snapshot.draft.files.length)
  return <div data-testid="composer-draft-status" className="pointer-events-auto px-4 py-1 text-xs text-muted-foreground" aria-live="polite">
    <p role="alert">
      {uncertain ? t("draft.uncertain") : snapshot.conflict ? t("draft.conflict") : t("draft.failed")}
    </p>
    {snapshot.error ? <p className="break-words text-destructive">{snapshot.error}</p> : null}
    {snapshot.conflict ? <>
      <Button type="button" variant="link" size="sm" disabled={disabled || snapshot.saving} onClick={() => void session.useStoredDraft().catch(() => undefined)}>{t("draft.useStored")}</Button>
      <Button type="button" variant="link" size="sm" disabled={disabled || snapshot.saving} onClick={() => void session.saveCurrentDraft().catch(() => undefined)}>{t("draft.useCurrent")}</Button>
    </> : null}
    {snapshot.error && !snapshot.conflict ? <Button type="button" variant="link" size="sm" disabled={disabled || snapshot.saving || snapshot.loading}
      onClick={() => void (snapshot.loaded ? session.flush() : session.load()).catch(() => undefined)}>
      {t(snapshot.loaded ? "draft.retry" : "draft.retryLoad")}
    </Button> : null}
    {uncertain ? <Button type="button" variant="link" size="sm" disabled={disabled || snapshot.saving}
      onClick={() => void session.resumeEditing().catch(() => undefined)}>{t("draft.resume")}</Button> : null}
    {hasDraft ? <Button type="button" variant="link" size="sm" disabled={disabled || snapshot.saving || snapshot.loading || snapshot.draft.state === "sending"}
      onClick={() => void session.discard().catch(() => undefined)}>{t("draft.discard")}</Button> : null}
  </div>
}
