/** How many lines of a thought stay visible before the box scrolls.
 *  A streaming model can dump a page of reasoning; filling the transcript
 *  with it hides the answer that is about to arrive. Keep the CSS
 *  `.thought-scroll` max-height in sync (ten × leading-6). */
export const THOUGHT_VISIBLE_LINES = 10

const TOP_FADE_PX = 2
const FOLLOW_SLACK_PX = 8

/** True when earlier lines have left the top of the box. The fade is the
 *  hint that they are still there if you scroll up. */
export function thoughtHasHiddenPrefix(scrollTop: number): boolean {
  return scrollTop > TOP_FADE_PX
}

/** Stay pinned to incoming tokens unless the reader has scrolled up.
 *  Yanking them back to the bottom while they are reading the plan is
 *  the same bug the transcript itself refuses to have. */
export function thoughtFollowsStream(
  scrollHeight: number,
  scrollTop: number,
  clientHeight: number,
): boolean {
  return scrollHeight - scrollTop - clientHeight < FOLLOW_SLACK_PX
}

/** A live thought starts open; a finished one starts closed. `choice` is
 *  null until the reader clicks — after that the click wins, even while
 *  tokens are still arriving. Streaming used to force the row open and
 *  the chevron did nothing. */
export function thoughtExpanded(
  streaming: boolean,
  choice: boolean | null,
): boolean {
  return choice ?? streaming
}
