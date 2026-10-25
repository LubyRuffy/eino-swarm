import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"
import { composerDraftKey, composerDraftSession } from "./composer-draft"

export function explicitComposerRecovery(search: string): { threadId?: string; projectId?: string; draftKey?: string } {
  const params = new URLSearchParams(search)
  if (params.get("shell") !== "desktop") return {}
  const safe = (value: string | null) => value && /^[\w-]{1,128}$/.test(value) ? value : undefined
  const threadId = safe(params.get("recovery_thread"))
  const projectId = safe(params.get("recovery_project"))
  const expectedDraft = composerDraftKey(threadId, projectId)
  const draftKey = params.get("recovery_draft") === expectedDraft ? expectedDraft : undefined
  return threadId ? { threadId, ...(draftKey ? { draftKey } : {}) } : { projectId, ...(draftKey ? { draftKey } : {}) }
}

/** Native recovery reads attribution only; it never receives task instructions.
 *  A stale saved ACK cannot prove that later unacknowledged input survived. */
export function installComposerRecoveryAttribution() {
  let draftUnsubscribe: (() => void) | undefined
  let key = ""
  let location = ""
  const publish = () => {
    const state = useApp.getState()
    const projectId = state.activeId ? state.threads.find((thread) => thread.id === state.activeId)?.project_id : useProjects.getState().selectedId
    const currentKey = composerDraftKey(state.activeId, projectId)
    if (key !== currentKey) {
      draftUnsubscribe?.()
      key = currentKey
      draftUnsubscribe = composerDraftSession(key).subscribe(publish)
    }
    const draft = composerDraftSession(key).getSnapshot()
    const turnId = state.status.turn_id || state.transcript.turns.at(-1)?.id || state.turns.at(-1)?.id
    ;(window as Window & { __zwaiRecoveryAttribution?: object }).__zwaiRecoveryAttribution = {
      thread_id: state.activeId,
      turn_id: turnId,
      project_id: projectId,
      draft_key: key,
      draft_saved: draft.saved,
      draft_state: draft.draft.state,
      draft_revision: draft.draft.revision,
    }
    const current = JSON.stringify([state.activeId, turnId, projectId, key])
    if (current !== location) {
      location = current
      // Native reads the published scope now; this event never proves paint health.
      window.dispatchEvent(new Event("zwai:recovery-location"))
    }
  }
  const app = useApp.subscribe(publish)
  const projects = useProjects.subscribe(publish)
  publish()
  return () => { app(); projects(); draftUnsubscribe?.() }
}
