/** What the composer can submit right now. Counts, not names: a quote chip,
 *  a dropped file, or a pasted image is a draft even when the textarea is
 *  empty. A slash prompt that still needs its argument is not. */
export type ComposerDraft = {
  text: string
  attachmentCount: number
  quoteCount: number
  imageCount: number
  slashOpen: boolean
  slashReady: boolean
  awaitingArgument: boolean
}

/** Enter would queue, steer, send, or pick a slash command. */
export function composerDraftSubmittable(draft: ComposerDraft): boolean {
  if (draft.awaitingArgument) return false
  return (
    draft.slashReady ||
    draft.slashOpen ||
    draft.text.trim().length > 0 ||
    draft.attachmentCount > 0 ||
    draft.quoteCount > 0 ||
    draft.imageCount > 0
  )
}

/** Corner of the composer. Idle always offers Send. A running turn with
 *  nothing to submit offers Stop, because that click is the cancel. A
 *  draft Enter would submit takes the slot: leaving Stop there makes the
 *  click look like a cancel, so it does not get pressed. A slash prompt
 *  that is still waiting for its argument keeps both — Send stays
 *  disabled, Stop is still the live action. */
export function composerCornerActions(input: {
  running: boolean
  submittable: boolean
  awaitingArgument: boolean
}): { stop: boolean; send: boolean } {
  if (!input.running) return { stop: false, send: true }
  if (input.awaitingArgument) return { stop: true, send: true }
  if (input.submittable) return { stop: false, send: true }
  return { stop: true, send: false }
}
