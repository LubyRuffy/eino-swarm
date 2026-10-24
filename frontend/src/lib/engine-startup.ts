/** A refused mutation is never retried. It only requests a fresh read-only snapshot. */
export const ENGINE_RESTORING_EVENT = "zwai:engine-restoring"

export function notifyEngineRestoring(code?: string) {
  if (code === "restoring" && typeof window !== "undefined") {
    window.dispatchEvent(new Event(ENGINE_RESTORING_EVENT))
  }
}
