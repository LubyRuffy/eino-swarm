import { Capacitor, registerPlugin } from "@capacitor/core"

import { ModelCallError } from "./openai-wire"

/** CapacitorHttp's POST waits for the whole body, so a completion on the
 *  device would paint once, after the model finished. This plugin forwards
 *  each chunk. The browser keeps fetch, which already streams. */

export type StreamBodyPlugin = {
  open(opts: {
    id: string
    url: string
    method: string
    headers: Record<string, string>
    body: string
    timeoutMs: number
  }): Promise<{ status?: number; aborted?: boolean }>
  cancel(opts: { id: string }): Promise<void>
  addListener(
    eventName: "status" | "chunk",
    listener: (event: { id?: string; status?: number; text?: string }) => void,
  ): Promise<{ remove(): Promise<void> }>
}

const StreamBody = registerPlugin<StreamBodyPlugin>("StreamBody")

export type BodySource = {
  status: number
  chunks: AsyncIterable<string>
}

type FetchLike = typeof fetch

export async function openBody(input: {
  url: string
  headers: Record<string, string>
  body: string
  signal: AbortSignal
  timeoutMs: number
  fetchImpl?: FetchLike
  nativeStream?: StreamBodyPlugin
}): Promise<BodySource> {
  if (input.nativeStream) return openNative(input, input.nativeStream)
  if (input.fetchImpl || !Capacitor.isNativePlatform()) return openFetch(input)
  return openNative(input, StreamBody)
}

async function openFetch(input: {
  url: string
  headers: Record<string, string>
  body: string
  signal: AbortSignal
  fetchImpl?: FetchLike
}): Promise<BodySource> {
  const fetchImpl = input.fetchImpl ?? fetch
  const res = await fetchImpl(input.url, {
    method: "POST",
    headers: input.headers,
    body: input.body,
    signal: input.signal,
  })
  return { status: res.status, chunks: fetchChunks(res, input.signal) }
}

async function* fetchChunks(res: Response, signal: AbortSignal): AsyncGenerator<string> {
  const reader = res.body?.getReader()
  if (!reader) {
    const text = await res.text()
    if (text) yield text
    return
  }
  // A hand-built body is not tied to the fetch signal. Without cancel, the
  // generator stays parked on read() and closing it never finishes.
  const onAbort = () => {
    void reader.cancel().catch(() => undefined)
  }
  signal.addEventListener("abort", onAbort)
  const dec = new TextDecoder()
  try {
    for (;;) {
      const next = await reader.read()
      if (next.done) break
      const text = dec.decode(next.value, { stream: true })
      if (text) yield text
    }
    const rest = dec.decode()
    if (rest) yield rest
  } finally {
    signal.removeEventListener("abort", onAbort)
    reader.releaseLock()
  }
}

class Mailbox {
  private items: string[] = []
  private ended = false
  private failure: unknown
  private waiters: (() => void)[] = []

  get isEnded(): boolean {
    return this.ended
  }

  push(item: string) {
    if (this.ended || !item) return
    this.items.push(item)
    this.kick()
  }

  end(failure?: unknown) {
    if (this.ended) return
    this.ended = true
    if (failure) this.failure = failure
    this.kick()
  }

  private kick() {
    const waiting = this.waiters
    this.waiters = []
    for (const fn of waiting) fn()
  }

  async next(): Promise<IteratorResult<string>> {
    for (;;) {
      const item = this.items.shift()
      if (item !== undefined) return { done: false, value: item }
      if (this.ended) {
        if (this.failure) throw this.failure
        return { done: true, value: undefined }
      }
      await new Promise<void>((resolve) => this.waiters.push(resolve))
    }
  }
}

function streamID(): string {
  const cryptoRef = globalThis.crypto
  if (cryptoRef && "randomUUID" in cryptoRef) return cryptoRef.randomUUID()
  return `s-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

function aborted(signal: AbortSignal): ModelCallError {
  if (signal.reason === "timeout") return new ModelCallError("timeout", "timeout")
  return new ModelCallError("abort", "abort")
}

function nativeFailure(err: unknown, signal: AbortSignal): ModelCallError {
  if (err instanceof ModelCallError) return err
  if (signal.aborted) return aborted(signal)
  const message = err instanceof Error ? err.message : ""
  if (message.includes("timeout")) return new ModelCallError("timeout", "timeout")
  if (message.includes("bad-url")) return new ModelCallError("bad-url", "http")
  return new ModelCallError("network", "http")
}

async function openNative(
  input: {
    url: string
    headers: Record<string, string>
    body: string
    signal: AbortSignal
    timeoutMs: number
  },
  plugin: StreamBodyPlugin,
): Promise<BodySource> {
  const id = streamID()
  const box = new Mailbox()
  let settled = false
  let resolveStatus!: (status: number) => void
  let rejectStatus!: (err: unknown) => void
  const statusP = new Promise<number>((resolve, reject) => {
    resolveStatus = resolve
    rejectStatus = reject
  })
  const settle = (status: number) => {
    if (settled) return
    settled = true
    resolveStatus(status)
  }
  const fail = (err: unknown) => {
    if (!settled) {
      settled = true
      rejectStatus(err)
    }
    box.end(err)
  }
  const statusHandle = await plugin.addListener("status", (ev) => {
    if (ev.id !== id || typeof ev.status !== "number") return
    settle(ev.status)
  })
  const chunkHandle = await plugin.addListener("chunk", (ev) => {
    if (ev.id !== id || !ev.text) return
    box.push(ev.text)
  })
  let cleaned = false
  const cleanup = async () => {
    if (cleaned) return
    cleaned = true
    input.signal.removeEventListener("abort", onAbort)
    await statusHandle.remove().catch(() => undefined)
    await chunkHandle.remove().catch(() => undefined)
  }
  const onAbort = () => {
    fail(aborted(input.signal))
    void plugin.cancel({ id }).catch(() => undefined)
  }
  input.signal.addEventListener("abort", onAbort)
  const timeoutMs = Math.min(Math.max(1, Math.round(input.timeoutMs)), 2_147_483_647)
  const pending = plugin
    .open({
      id,
      url: input.url,
      method: "POST",
      headers: input.headers,
      body: input.body,
      timeoutMs,
    })
    .then(
      (res) => {
        if (typeof res.status === "number") settle(res.status)
        if (res.aborted || input.signal.aborted) fail(aborted(input.signal))
        else {
          settle(typeof res.status === "number" ? res.status : 0)
          box.end()
        }
      },
      (err: unknown) => fail(nativeFailure(err, input.signal)),
    )
  const iterator: AsyncIterator<string> = {
    next: () => box.next(),
    async return() {
      if (!box.isEnded) void plugin.cancel({ id }).catch(() => undefined)
      await cleanup()
      await pending.catch(() => undefined)
      return { done: true, value: undefined }
    },
  }
  try {
    const status = await statusP
    if (input.signal.aborted) throw aborted(input.signal)
    return {
      status,
      chunks: { [Symbol.asyncIterator]: () => iterator },
    }
  } catch (err) {
    await cleanup()
    await pending.catch(() => undefined)
    throw err instanceof ModelCallError ? err : nativeFailure(err, input.signal)
  }
}
