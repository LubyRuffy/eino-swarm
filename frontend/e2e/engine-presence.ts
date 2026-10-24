import type { FullConfig } from "@playwright/test"

/** The private suite owns a CLI presence across browser/worker replacement.
 * Production idle shutdown remains covered by its dedicated lifecycle tests. */
export default async function enginePresence(config: FullConfig) {
  const base = String(config.projects[0]?.use.baseURL ?? "")
  const reserved = await fetch(new URL("/api/presence", base), {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ surface: "tui" }),
    signal: AbortSignal.timeout(5000),
  })
  if (!reserved.ok) throw new Error(`suite presence reservation: HTTP ${reserved.status}`)
  const { id } = await reserved.json() as { id: string }
  if (!id) throw new Error("suite presence reservation has no identity")
  const controller = new AbortController()
  const deadline = setTimeout(() => controller.abort(), 5000)
  let reader: ReadableStreamDefaultReader<Uint8Array>
  try {
    const response = await fetch(new URL(`/api/presence/${encodeURIComponent(id)}`, base), {
      headers: { Accept: "text/event-stream" }, signal: controller.signal,
    })
    if (!response.ok || !response.body) throw new Error(`suite presence hold: HTTP ${response.status}`)
    reader = response.body.getReader()
    if ((await reader.read()).done) throw new Error("suite presence ended before acknowledgement")
  } catch (error) { controller.abort(); throw error }
  finally { clearTimeout(deadline) }
  let closing = false, lost: unknown
  const held = (async () => {
    while (!(await reader.read()).done) { /* keep the acknowledged CLI connection */ }
    if (!closing) lost = new Error("suite presence ended before teardown")
  })().catch((error: unknown) => { if (!closing) lost = error })
  return async () => {
    closing = true
    controller.abort()
    await held
    if (lost) throw lost
  }
}
