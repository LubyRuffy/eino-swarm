import type { SearchHit } from "./types"

export const SEARCH_DEBOUNCE_MS = 150

/** cmdk filters on `value`. Semantic hits may not contain the typed query, so
 *  the query is included or those rows vanish. */
export function conversationHitValue(hit: SearchHit, query: string): string {
  return `${hit.title} ${hit.snippet} ${hit.thread_id} ${query}`
}

export function shouldQuerySearch(raw: string): boolean {
  return raw.trim().length > 0
}
