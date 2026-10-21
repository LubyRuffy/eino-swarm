/** Transport wait metadata belongs to Trace; the chat only needs the delay. */
export function modelBackoffNotice(text?: string): string {
  try {
    const body = JSON.parse(text ?? "") as { wait_ms?: unknown } | null
    if (typeof body?.wait_ms === "number" && Number.isFinite(body.wait_ms) && body.wait_ms > 0) {
      return `Waiting ${Math.ceil(body.wait_ms / 1000)}s before retrying the model request.`
    }
  } catch {
    // Old or malformed events still explain the pause without leaking JSON.
  }
  return "Waiting before retrying the model request."
}
