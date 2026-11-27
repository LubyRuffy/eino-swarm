import { publishGatewayCapabilities } from "./gateway-capabilities"
import { gatewayAudioBootstrap } from "@/test/gateway-audio"
import { speechPlayback } from "../../../frontend/src/lib/speech-media"
import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { SpeechSnapshot } from "../../../frontend/src/lib/speech-types"
import { saveGatewaySession, clearGatewaySession, loadGatewaySession } from "./gateway-account"
import { acceptPhoneSpeechConfig, setPhoneSpeechSource, transcribePhoneSpeech, synthesizePhoneSpeech, usePhoneSpeech, usePhoneSpeechSync } from "./phone-speech"
import type { SavedLink } from "./store"
import type { RemoteLink } from "./link"
import type { RemoteResponse } from "./rpc"
import { loadSpeechCache } from "./speech-cache"
import { notifyGatewayRefusal } from "./gateway-authority"

const pc: SavedLink = { fingerprint: "pc-a", hostPub: "host", hubURL: "https://hub.test", ticket: "ticket", sessionID: "session", gatewayOwnerID: "owner", gatewayMobileID: "mobile", gatewayOrigin: "https://gateway.test" }
const speech: SpeechSnapshot = {
  execution: "gateway", gateway_origin: "https://gateway.test",
  input: { enabled: true, ready: true, model: "transcription", source_id: "gateway:preset", label: "ASR", language: "", prompt: "", recording_format: "auto", max_duration_seconds: 120, timeout_seconds: 120 },
  output: { enabled: false, ready: true, model: "speech", source_id: "gateway:preset", label: "TTS", voice: "", speed: 1, format: "mp3", instructions: "", timeout_seconds: 120 },
  limits: { input_bytes: 1024, output_bytes: 1024, chunk_chars: 1000, staging_ttl_seconds: 300 },
}
const signIn = (owner = "owner") => {
  const session = { origin: speech.gateway_origin!, token: "agm_" + "a".repeat(64), user: { id: owner, username: owner, approval_status: "approved" }, device: { id: "mobile", expires_at: "2099-01-01T00:00:00Z" } }
  saveGatewaySession(session)
  const bootstrap = gatewayAudioBootstrap(session)
  bootstrap.models.defaults = {}
  publishGatewayCapabilities(session, bootstrap)
}
afterEach(() => { act(() => setPhoneSpeechSource(undefined, null)); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe("phone speech capability follows the selected PC", () => {
  it("sends both default Gateway voice directions directly with the phone session", async () => {
    signIn()
    const rpc = vi.fn<RemoteLink["rpc"]>(async () => { throw new Error("Default gateway voice must bypass PC RPC") })
    setPhoneSpeechSource(pc, { alive: () => true, rpc } as unknown as RemoteLink)
    expect(acceptPhoneSpeechConfig(pc, { ...speech, execution: "pc_local", gateway_origin: undefined,
      input: { ...speech.input, execution: "gateway", gateway_origin: pc.gatewayOrigin },
      output: { ...speech.output, enabled: true, execution: "gateway", gateway_origin: pc.gatewayOrigin } })).toBe(true)
    const fetcher = vi.fn<typeof fetch>(async url => String(url).endsWith("/transcriptions")
      ? new Response(JSON.stringify({ text: "Recognized directly" }))
      : new Response(new Uint8Array([0, 1]), { headers: { "Content-Type": "audio/mpeg" } }))
    vi.stubGlobal("fetch", fetcher)
    const view = renderHook(() => usePhoneSpeech())
    try {
      expect(view.result.current).toMatchObject({ inputReady: true, outputReady: true })
      const recording = Object.assign(new Blob([new Uint8Array([0, 1])], { type: "audio/wav" }), { arrayBuffer: async () => new Uint8Array([0, 1]).buffer })
      await expect(transcribePhoneSpeech(recording, new AbortController().signal)).resolves.toBe("Recognized directly")
      await expect(synthesizePhoneSpeech("Read directly", new AbortController().signal)).resolves.toMatchObject({ type: "audio/mpeg" })
      expect(fetcher.mock.calls.map(([url]) => String(url))).toEqual(["https://gateway.test/v1/audio/transcriptions", "https://gateway.test/v1/audio/speech"])
      expect(rpc).not.toHaveBeenCalled()
    } finally { view.unmount() }
  })
  it.each(["configuration", "disconnect"] as const)("stops already queued sound when paired speech loses its %s authority", async (change) => {
    signIn()
    const config = { ...speech, output: { ...speech.output, enabled: true, format: "pcm" as const } }
    setPhoneSpeechSource(pc, { alive: () => true } as RemoteLink)
    acceptPhoneSpeechConfig(pc, config)
    const audioSource = { connect: vi.fn(), disconnect: vi.fn(), start: vi.fn(), stop: vi.fn(), onended: undefined as (() => void) | undefined }
    class Context {
      state = "running"
      currentTime = 0
      destination = {}
      resume = async () => undefined
      close = async () => { this.state = "closed" }
      createBuffer(_channels: number, length: number, rate: number) {
        return { duration: length / rate, getChannelData: () => new Float32Array(length) }
      }
      createBufferSource = () => audioSource
    }
    vi.stubGlobal("AudioContext", Context)
    vi.stubGlobal("fetch", vi.fn<typeof fetch>(async () => new Response(new Uint8Array([0, 1]), { headers: { "Content-Type": "audio/pcm" } })))
    const playing = speechPlayback.play("owned-answer", "Complete answer", { chunkChars: 100, synthesize: synthesizePhoneSpeech })
    try {
      await waitFor(() => expect(audioSource.start).toHaveBeenCalledOnce())
      if (change === "disconnect") setPhoneSpeechSource(pc, null)
      else acceptPhoneSpeechConfig(pc, { ...config, output: { ...config.output, enabled: false } })
      expect(speechPlayback.getSnapshot()).toEqual({ state: "idle" })
      expect(audioSource.stop).toHaveBeenCalledOnce()
      await playing
    } finally {
      speechPlayback.stop()
      await playing
    }
  })

  it("plays every segment through the final sample across a paired phone model refresh", async () => {
    signIn()
    const config = { ...speech, output: { ...speech.output, enabled: true, format: "pcm" as const }, limits: { ...speech.limits, chunk_chars: 7 } }
    const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: true, speech: config } as RemoteResponse)
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const view = renderHook(({ providers }) => {
      usePhoneSpeechSync(link, pc, [pc], false, providers)
      return usePhoneSpeech()
    }, { initialProps: { providers: [{ id: "chat-service", label: "Before refresh" }] } })
    await waitFor(() => expect(view.result.current.outputReady).toBe(true))
    const sources: { onended?: () => void; stop: ReturnType<typeof vi.fn> }[] = []
    class Context {
      state = "running"
      currentTime = 0
      destination = {}
      resume = async () => undefined
      close = async () => { this.state = "closed" }
      createBuffer(_channels: number, length: number, rate: number) {
        return { duration: length / rate, getChannelData: () => new Float32Array(length) }
      }
      createBufferSource() {
        const source = { onended: undefined as (() => void) | undefined, connect: vi.fn(), disconnect: vi.fn(), start: vi.fn(), stop: vi.fn() }
        sources.push(source)
        return source
      }
    }
    vi.stubGlobal("AudioContext", Context)
    const texts: string[] = []
    vi.stubGlobal("fetch", vi.fn<typeof fetch>(async (_url, init) => {
      texts.push(JSON.parse(String(init?.body)).input)
      return new Response(new Uint8Array([0, 1]), { headers: { "Content-Type": "audio/pcm" } })
    }))
    let finished = false
    const playing = speechPlayback.play("complete-answer", "Alpha. Beta. Gamma.", { chunkChars: config.limits.chunk_chars, synthesize: synthesizePhoneSpeech }).then(() => { finished = true })
    await waitFor(() => expect(sources).toHaveLength(1))
    view.rerender({ providers: [{ id: "chat-service", label: "After refresh" }] })
    expect(speechPlayback.getSnapshot()).toMatchObject({ key: "complete-answer", state: "playing" })
    expect(sources[0].stop).not.toHaveBeenCalled()
    for (let i = 0; i < 3; i++) {
      await waitFor(() => expect(sources).toHaveLength(i + 1))
      expect(finished).toBe(false)
      sources[i].onended?.()
    }
    await playing
    expect(texts).toEqual(["Alpha.", "Beta.", "Gamma."])
    expect(speechPlayback.getSnapshot()).toEqual({ state: "idle" })
    view.unmount()
  })

  it.each(["transcribe", "speak"] as const)("keeps paired %s alive when the phone's unrelated model catalog refreshes", async (operation) => {
    signIn()
    const config = { ...speech, output: { ...speech.output, enabled: true } }
    const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: true, speech: config } as RemoteResponse)
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const view = renderHook(({ providers }) => {
      usePhoneSpeechSync(link, pc, [pc], false, providers)
      return usePhoneSpeech()
    }, { initialProps: { providers: [{ id: "chat-service", label: "Before refresh" }] } })
    await waitFor(() => expect(view.result.current.inputReady).toBe(true))
    const epoch = view.result.current.epoch
    let release!: (response: Response) => void
    let requestSignal!: AbortSignal
    vi.stubGlobal("fetch", vi.fn<typeof fetch>((_url, init) => {
      requestSignal = init!.signal as AbortSignal
      return new Promise(resolve => { release = resolve })
    }))
    const work = (operation === "transcribe"
      ? transcribePhoneSpeech(new Blob(["recording"], { type: "audio/wav" }), new AbortController().signal)
      : synthesizePhoneSpeech("Complete reply", new AbortController().signal))
      .then(value => ({ value, error: undefined }), (error: unknown) => ({ value: undefined, error }))
    await waitFor(() => expect(requestSignal).toBeDefined())
    // Account catalog refreshes replace provider metadata while a paired PC
    // still owns the exact same speech settings and authority.
    view.rerender({ providers: [{ id: "chat-service", label: "After refresh" }] })
    const interrupted = requestSignal.aborted
    const refreshedEpoch = view.result.current.epoch
    release(operation === "transcribe"
      ? new Response(JSON.stringify({ text: "Complete dictation" }))
      : new Response(new Uint8Array([1, 2]), { headers: { "Content-Type": "audio/mpeg" } }))
    const outcome = await work
    expect(interrupted).toBe(false)
    expect(refreshedEpoch).toBe(epoch)
    expect(outcome.error).toBeUndefined()
    if (operation === "transcribe") expect(outcome.value).toBe("Complete dictation")
    else expect(outcome.value).toMatchObject({ type: "audio/mpeg", size: 2 })
    view.unmount()
  })


  it.each(["account", "anonymous QR"])("streams an ordinary Gateway output for %s while ASR stays on the PC", async (binding) => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2026-11-16T00:00:00Z"))
    signIn()
    saveGatewaySession({ ...loadGatewaySession()!, device: { ...loadGatewaySession()!.device, expires_at: new Date(Date.now() + 1000).toISOString() } })
    const rpc = vi.fn<RemoteLink["rpc"]>(async request => {
      if (request.op === "audio_speech") throw new Error("Gateway output must not use PC audio RPC")
      return { ok: true, audio: { audio_id: request.audio_id!, ...(request.op === "audio_transcribe" ? { text: "recognized on PC" } : {}) } } as RemoteResponse
    })
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const configured = { ...speech, execution: "pc_local" as const, gateway_origin: undefined,
      output: { ...speech.output, enabled: true, format: "pcm" as const, model: "configured-voice", execution: "gateway" as const, gateway_origin: speech.gateway_origin } }
    let requestSignal!: AbortSignal
    const fetcher = vi.fn<typeof fetch>(async (_url, init) => {
      requestSignal = init!.signal as AbortSignal
      return new Response('data: {"type":"speech.audio.delta","audio":"AAE="}\n\ndata: {"type":"speech.audio.done"}\n\n', { headers: { "Content-Type": "text/event-stream" } })
    })
    vi.stubGlobal("fetch", fetcher)
    const selected = binding === "account" ? pc : { ...pc, gatewayOrigin: undefined, gatewayOwnerID: undefined, gatewayMobileID: undefined }
    setPhoneSpeechSource(selected, link)
    expect(acceptPhoneSpeechConfig(selected, configured)).toBe(true)
    const view = renderHook(() => usePhoneSpeech())
    let cancelAudio: () => void = () => undefined
    try {
      expect(view.result.current).toMatchObject({ inputReady: true, outputReady: true })
      const recording = Object.assign(new Blob([new Uint8Array([0, 1])], { type: "audio/wav" }), { arrayBuffer: async () => new Uint8Array([0, 1]).buffer })
      await expect(transcribePhoneSpeech(recording, new AbortController().signal)).resolves.toBe("recognized on PC")
      const audio = await synthesizePhoneSpeech("text", new AbortController().signal)
      if (!("chunks" in audio)) throw new Error("Expected Gateway PCM stream")
      cancelAudio = () => audio.cancel()
      const samples = []
      for await (const chunk of audio.chunks) samples.push(...chunk)
      expect(samples).toEqual([0, 1])
      expect(fetcher).toHaveBeenCalledWith("https://gateway.test/v1/audio/speech", expect.objectContaining({ headers: { Authorization: "Bearer " + loadGatewaySession()!.token, "Content-Type": "application/json" } }))
      expect(JSON.parse(String(fetcher.mock.calls[0][1]?.body))).toMatchObject({ model: "configured-voice", stream_format: "sse" })
      expect(rpc.mock.calls.some(([request]) => request.op === "audio_speech")).toBe(false)
      expect(requestSignal.aborted).toBe(false)
      await vi.advanceTimersByTimeAsync(1001)
      expect(audio.signal?.aborted).toBe(true)
    } finally { cancelAudio(); view.unmount() }
  })

  it.each(["signed-out", "wrong-owner", "wrong-mobile", "wrong-bound-origin"])("refuses ordinary Gateway output authority %s without changing local ASR", async (reason) => {
    signIn()
    if (reason === "signed-out") clearGatewaySession()
    if (reason === "wrong-owner") signIn("different-owner")
    if (reason === "wrong-mobile") saveGatewaySession({ ...loadGatewaySession()!, device: { ...loadGatewaySession()!.device, id: "different-mobile" } })
    const selected = reason === "wrong-bound-origin" ? { ...pc, gatewayOrigin: "https://different.test" } : pc
    const link = { alive: () => true } as RemoteLink
    const fetcher = vi.fn<typeof fetch>(); vi.stubGlobal("fetch", fetcher)
    setPhoneSpeechSource(selected, link)
    expect(acceptPhoneSpeechConfig(selected, { ...speech, execution: "pc_local", gateway_origin: undefined,
      output: { ...speech.output, enabled: true, execution: "gateway", gateway_origin: speech.gateway_origin } })).toBe(true)
    const view = renderHook(() => usePhoneSpeech())
    try {
      expect(view.result.current).toMatchObject({ inputReady: true, outputReady: false })
      await expect(synthesizePhoneSpeech("text", new AbortController().signal)).rejects.toThrow("unavailable")
      expect(fetcher).not.toHaveBeenCalled()
    } finally { view.unmount() }
  })

  it("keeps explicit personal PC output usable when legacy Gateway ASR has no Mobile authority", async () => {
    clearGatewaySession()
    const rpc = vi.fn<RemoteLink["rpc"]>(async request => ({ ok: true, audio: request.op === "audio_speech"
      ? { audio_id: request.audio_id!, mime: "audio/wav", size: 2 }
      : request.op === "audio_chunk" ? { audio_id: request.audio_id!, mime: "audio/wav", size: 2, offset: 0, next: 2, more: false, data: "AAE=" }
        : { audio_id: request.audio_id! } } as RemoteResponse))
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const fetcher = vi.fn<typeof fetch>(); vi.stubGlobal("fetch", fetcher)
    setPhoneSpeechSource(pc, link)
    expect(acceptPhoneSpeechConfig(pc, { ...speech, output: { ...speech.output, enabled: true, execution: "pc_local" } })).toBe(true)
    const view = renderHook(() => usePhoneSpeech())
    try {
      expect(view.result.current).toMatchObject({ inputReady: false, outputReady: true })
      await expect(synthesizePhoneSpeech("text", new AbortController().signal)).resolves.toMatchObject({ type: "audio/wav", size: 2 })
      await expect(transcribePhoneSpeech(new Blob(["audio"]), new AbortController().signal)).rejects.toThrow("unavailable")
      expect(fetcher).not.toHaveBeenCalled()
    } finally { view.unmount() }
  })

  it("uses the legacy global PC route when an old host has no output authority fields", async () => {
    clearGatewaySession()
    const rpc = vi.fn<RemoteLink["rpc"]>(async request => ({ ok: true, audio: request.op === "audio_speech"
      ? { audio_id: request.audio_id!, mime: "audio/wav", size: 2 }
      : request.op === "audio_chunk" ? { audio_id: request.audio_id!, mime: "audio/wav", size: 2, offset: 0, next: 2, more: false, data: "AAE=" }
        : { audio_id: request.audio_id! } } as RemoteResponse))
    setPhoneSpeechSource(pc, { alive: () => true, rpc } as unknown as RemoteLink)
    expect(acceptPhoneSpeechConfig(pc, { ...speech, execution: "pc_local", gateway_origin: undefined, output: { ...speech.output, enabled: true } })).toBe(true)
    expect(loadSpeechCache(pc)?.snapshot.output).not.toHaveProperty("execution")
    await expect(synthesizePhoneSpeech("text", new AbortController().signal)).resolves.toMatchObject({ type: "audio/wav", size: 2 })
    expect(rpc.mock.calls[0][0].op).toBe("audio_speech")
  })

  it("preserves Gateway expiry through the selected-PC stream wrapper after EOF", async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2026-11-16T00:00:00Z"))
    signIn()
    const session = loadGatewaySession()!
    saveGatewaySession({ ...session, device: { ...session.device, expires_at: new Date(Date.now() + 1000).toISOString() } })
    setPhoneSpeechSource(pc, { alive: () => true } as RemoteLink)
    expect(acceptPhoneSpeechConfig(pc, { ...speech, output: { ...speech.output, enabled: true, format: "pcm" } })).toBe(true)
    let requestSignal!: AbortSignal
    vi.stubGlobal("fetch", vi.fn<typeof fetch>(async (_url, init) => {
      requestSignal = init!.signal as AbortSignal
      return new Response(new Uint8Array([0, 1]), { headers: { "Content-Type": "audio/pcm" } })
    }))
    const audio = await synthesizePhoneSpeech("read this", new AbortController().signal)
    if (!("chunks" in audio)) throw new Error("Expected PCM stream")
    try {
      const reader = audio.chunks[Symbol.asyncIterator]()
      await expect(reader.next()).resolves.toMatchObject({ done: false })
      await expect(reader.next()).resolves.toMatchObject({ done: true })
      expect(audio.signal).toBe(requestSignal)
      expect(audio.signal?.aborted).toBe(false)
      await vi.advanceTimersByTimeAsync(1001)
      expect(audio.signal?.aborted).toBe(true)
      expect(vi.getTimerCount()).toBe(0)
    } finally { audio.cancel() }
  })

  it.each(["disconnect", "configuration"] as const)("cancels PCM playback after headers on selected-PC %s", async (change) => {
    signIn()
    const config = { ...speech, output: { ...speech.output, enabled: true, format: "pcm" as SpeechSnapshot["output"]["format"] } }
    const link = { alive: () => true } as RemoteLink
    setPhoneSpeechSource(pc, link)
    expect(acceptPhoneSpeechConfig(pc, config)).toBe(true)
    let requestSignal!: AbortSignal
    const cancelled = vi.fn()
    vi.stubGlobal("fetch", vi.fn<typeof fetch>(async (_url, init) => {
      requestSignal = init!.signal as AbortSignal
      return new Response(new ReadableStream<Uint8Array>({ start(controller) { controller.enqueue(new Uint8Array([1, 2])) }, cancel: cancelled }), { headers: { "Content-Type": "audio/pcm" } })
    }))
    const audio = await synthesizePhoneSpeech("read this", new AbortController().signal)
    if (!("chunks" in audio)) throw new Error("Expected PCM stream")
    const reader = audio.chunks[Symbol.asyncIterator]()
    await expect(reader.next()).resolves.toMatchObject({ done: false, value: new Uint8Array([1, 2]) })
    const next = reader.next()
    if (change === "disconnect") setPhoneSpeechSource(pc, null)
    else acceptPhoneSpeechConfig(pc, { ...config, output: { ...config.output, enabled: false } })
    await expect(next).rejects.toMatchObject({ name: "AbortError" })
    expect(requestSignal.aborted).toBe(true)
    expect(cancelled).toHaveBeenCalledOnce()
  })

  it.each(["transcribe", "speak"] as const)("cancels an ongoing Gateway %s request on PC disconnect before reconnecting", async (operation) => {
    signIn()
    const config = { ...speech, output: { ...speech.output, enabled: true } }
    const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: true, speech: config } as RemoteResponse)
    let alive = true
    const original = { alive: () => alive, rpc } as unknown as RemoteLink
    const view = renderHook(({ link }: { link: RemoteLink | null }) => { usePhoneSpeechSync(link, pc, [pc]); return usePhoneSpeech() }, { initialProps: { link: original as RemoteLink | null } })
    await waitFor(() => expect(view.result.current.inputReady).toBe(true))
    const epoch = view.result.current.epoch
    let release!: (response: Response) => void
    let requestSignal!: AbortSignal
    const fetcher = vi.fn<typeof fetch>((_url, init) => { requestSignal = init!.signal as AbortSignal; return new Promise((resolve) => { release = resolve }) })
    vi.stubGlobal("fetch", fetcher)
    const cancelled = vi.fn()
    window.addEventListener("zwai.phone.speech.cancel", cancelled)
    try {
      const work = (operation === "transcribe" ? transcribePhoneSpeech(new Blob(["recording"], { type: "audio/wav" }), new AbortController().signal)
        : synthesizePhoneSpeech("public reply", new AbortController().signal)).then((value) => ({ value, error: undefined }), (error: unknown) => ({ value: undefined, error }))
      await waitFor(() => expect(fetcher).toHaveBeenCalledOnce())
      alive = false
      view.rerender({ link: null })
      const offline = { ...view.result.current }
      const replacement = { alive: () => true, rpc } as unknown as RemoteLink
      view.rerender({ link: replacement })
      await waitFor(() => expect(rpc).toHaveBeenCalledTimes(2))
      const abortedOnReconnect = requestSignal.aborted
      const cancelCount = cancelled.mock.calls.length
      const reconnectedEpoch = view.result.current.epoch
      release(operation === "transcribe" ? new Response(JSON.stringify({ text: "recognized" }), { headers: { "Content-Type": "application/json" } })
        : new Response(new Uint8Array([82, 73, 70, 70]), { headers: { "Content-Type": "audio/wav" } }))
      const outcome = await work
      expect(offline).toMatchObject({ connected: false, inputReady: false, outputReady: false })
      expect(offline.epoch).toBeGreaterThan(epoch)
      expect(reconnectedEpoch).toBeGreaterThan(epoch)
      expect(abortedOnReconnect).toBe(true)
      expect(cancelCount).toBeGreaterThan(0)
      expect(outcome.error).toMatchObject({name:"AbortError"})
      expect(outcome.value).toBeUndefined()
      expect(fetcher).toHaveBeenCalledOnce()
      expect(fetcher.mock.calls[0][1]?.headers).toMatchObject({ Authorization: "Bearer agm_" + "a".repeat(64) })
      expect(rpc.mock.calls.every(([request]) => request.op === "speech_config")).toBe(true)
    } finally { window.removeEventListener("zwai.phone.speech.cancel", cancelled); view.unmount() }
  })

  it("keeps recording and the Gateway request alive during an ordinary account health refresh", async () => {
    signIn()
    const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: true, speech } as RemoteResponse)
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const view = renderHook(() => { usePhoneSpeechSync(link, pc, [pc]); return usePhoneSpeech() })
    await waitFor(() => expect(view.result.current.inputReady).toBe(true))
    const epoch = view.result.current.epoch
    let release!: (response: Response) => void
    let requestSignal!: AbortSignal
    const fetcher = vi.fn<typeof fetch>((_url, init) => { requestSignal = init!.signal as AbortSignal; return new Promise((resolve) => { release = resolve }) })
    vi.stubGlobal("fetch", fetcher)
    const work = transcribePhoneSpeech(new Blob(["recording"], { type: "audio/wav" }), new AbortController().signal)
      .then((value) => ({ value, error: undefined }), (error: unknown) => ({ value: undefined, error }))
    await waitFor(() => expect(requestSignal).toBeDefined())
    act(() => {
      const current = loadGatewaySession()!
      // The real 30-second account refresh republishes this same authority.
      saveGatewaySession({ ...current, user: { ...current.user, avatar_url: "https://avatar.invalid/updated" }, device: { ...current.device, expires_at: "2099-02-01T00:00:00Z" } })
    })
    const cancelled = requestSignal.aborted
    const refreshedEpoch = view.result.current.epoch
    release(new Response(JSON.stringify({ text: "recognized" }), { headers: { "Content-Type": "application/json" } }))
    const outcome = await work
    expect(cancelled).toBe(false)
    expect(refreshedEpoch).toBe(epoch)
    expect(outcome.error).toBeUndefined()
    expect(outcome.value).toBe("recognized")
    expect(fetcher).toHaveBeenCalledOnce()
    view.unmount()
  })

  it.each(["configuration", "pairing", "account", "credential", "approval", "refusal", "PC"] as const)("still cancels an ongoing Gateway request when its %s changes", async (change) => {
    signIn()
    const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: true, speech } as RemoteResponse)
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const view = renderHook(({ selected }: { selected: SavedLink }) => { usePhoneSpeechSync(link, selected, [selected]); return usePhoneSpeech() }, { initialProps: { selected: pc } })
    await waitFor(() => expect(view.result.current.inputReady).toBe(true))
    let release!: (response: Response) => void
    let requestSignal!: AbortSignal
    vi.stubGlobal("fetch", vi.fn<typeof fetch>((_url, init) => { requestSignal = init!.signal as AbortSignal; return new Promise((resolve) => { release = resolve }) }))
    const work = transcribePhoneSpeech(new Blob(["recording"], { type: "audio/wav" }), new AbortController().signal)
      .then((value) => ({ value, error: undefined }), (error: unknown) => ({ value: undefined, error }))
    await waitFor(() => expect(requestSignal).toBeDefined())
    await act(async () => {
      if (change === "configuration") acceptPhoneSpeechConfig(pc, { ...speech, input: { ...speech.input, enabled: false } })
      else if (change === "account") clearGatewaySession()
      else if (change === "credential") saveGatewaySession({ ...loadGatewaySession()!, token: "agm_" + "b".repeat(64) })
      else if (change === "approval") { const current = loadGatewaySession()!; saveGatewaySession({ ...current, user: { ...current.user, approval_status: undefined } }) }
      else if (change === "refusal") notifyGatewayRefusal(loadGatewaySession()!, 403)
      else view.rerender({ selected: change === "pairing" ? { ...pc, ticket: "new-grant" } : { ...pc, fingerprint: "different-PC" } })
    })
    const cancelled = requestSignal.aborted
    release(new Response(JSON.stringify({ text: "late response" }), { headers: { "Content-Type": "application/json" } }))
    const outcome = await work
    expect(cancelled).toBe(true)
    expect(outcome.error).toMatchObject({ name: "AbortError" })
    expect(outcome.value).toBeUndefined()
    view.unmount()
  })

  it("cancels local model execution when the selected PC link is replaced", async () => {
    const config = { ...speech, execution: "pc_local" as const }
    let finish!: (response: RemoteResponse) => void
    let requestSignal!: AbortSignal
    let alive = true
    const rpc = vi.fn<RemoteLink["rpc"]>(async (request, options) => {
      if (request.op === "speech_config") return { ok: true, speech: config } as RemoteResponse
      if (request.op === "audio_transcribe") {
        requestSignal = options!.signal!
        return await new Promise<RemoteResponse>((resolve) => { finish = resolve })
      }
      return { ok: true } as RemoteResponse
    })
    const link = { alive: () => alive, rpc } as unknown as RemoteLink
    const view = renderHook(({ current }: { current: RemoteLink | null }) => { usePhoneSpeechSync(current, pc, [pc]); return usePhoneSpeech() }, { initialProps: { current: link as RemoteLink | null } })
    await waitFor(() => expect(view.result.current.inputReady).toBe(true))
    const bytes = new Uint8Array([82, 73, 70, 70])
    const recording = Object.assign(new Blob([bytes], { type: "audio/wav" }), { arrayBuffer: async () => bytes.buffer })
    const work = transcribePhoneSpeech(recording, new AbortController().signal).then((value) => ({ value, error: undefined }), (error: unknown) => ({ value: undefined, error }))
    await waitFor(() => expect(finish).toBeTypeOf("function"))
    alive = false
    view.rerender({ current: null })
    const cancelled = requestSignal.aborted
    const request = rpc.mock.calls.find(([request]) => request.op === "audio_transcribe")![0]
    finish({ ok: true, audio: { audio_id: request.audio_id!, text: "late local text" } } as RemoteResponse)
    const outcome = await work
    expect(cancelled).toBe(true)
    expect(outcome.error).toMatchObject({ name: "AbortError" })
    expect(view.result.current.inputReady).toBe(false)
    view.unmount()
  })

  it("retains cached preferences offline without executing them or borrowing another account's credential", () => {
    signIn()
    setPhoneSpeechSource(pc, null)
    acceptPhoneSpeechConfig(pc, speech)
    const view = renderHook(() => usePhoneSpeech())
    expect(view.result.current).toMatchObject({ inputReady: false, outputReady: false, connected: false })
    act(() => setPhoneSpeechSource(undefined, null))
    act(() => setPhoneSpeechSource(pc, null))
    expect(view.result.current.inputReady).toBe(false)
    act(() => { clearGatewaySession(); signIn("different-owner"); setPhoneSpeechSource(pc, null) })
    expect(view.result.current.inputReady).toBe(false)
  })

  it("requires the selected PC's live link for local execution and never falls back to a cached Gateway", () => {
    signIn()
    setPhoneSpeechSource(pc, null)
    acceptPhoneSpeechConfig(pc, { ...speech, execution: "pc_local" })
    const view = renderHook(() => usePhoneSpeech())
    expect(view.result.current.inputReady).toBe(false)
    act(() => setPhoneSpeechSource(pc, { alive: () => true } as RemoteLink))
    expect(view.result.current.inputReady).toBe(true)
    act(() => setPhoneSpeechSource({ ...pc, fingerprint: "pc-b" }, null))
    expect(view.result.current).toMatchObject({ inputReady: false, outputReady: false })
    expect(view.result.current.inputReady).toBe(false)
  })

  it("reads current configuration over the real link interface and removes executable cache for an old host", async () => {
    signIn()
    const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: true, speech } as RemoteResponse)
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const view = renderHook(() => { usePhoneSpeechSync(link, pc, [pc]); return usePhoneSpeech() })
    await waitFor(() => expect(view.result.current.inputReady).toBe(true))
    expect(rpc).toHaveBeenCalledWith({ op: "speech_config" }, expect.objectContaining({ dropOnTimeout: false }))
    view.unmount()
    const old = { alive: () => true, rpc: vi.fn().mockResolvedValue({ ok: false, code: "unknown_op" }) } as unknown as RemoteLink
    const legacy = renderHook(() => { usePhoneSpeechSync(old, pc, [pc]); return usePhoneSpeech() })
    await waitFor(() => expect(legacy.result.current).toMatchObject({ inputReady: false, outputReady: false }))
  })

  it("revokes a cached executable snapshot when the live PC publishes invalid configuration", () => {
    signIn()
    setPhoneSpeechSource(pc, {alive:()=>true} as RemoteLink)
    acceptPhoneSpeechConfig(pc, speech)
    const view = renderHook(() => usePhoneSpeech())
    expect(view.result.current.inputReady).toBe(true)
    act(() => { expect(acceptPhoneSpeechConfig(pc, { ...speech, input: { ...speech.input, timeout_seconds: -1 } })).toBe(false) })
    expect(view.result.current.inputReady).toBe(false)
    expect(view.result.current).toMatchObject({ inputReady: false, outputReady: false })
    expect(loadSpeechCache(pc)).toBeUndefined()
  })

  it("does not keep an old enabled cache after the live PC refuses an oversized configuration", async () => {
    signIn()
    setPhoneSpeechSource(pc, null)
    acceptPhoneSpeechConfig(pc, speech)
    const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: false, code: "audio_config_limit" } as RemoteResponse)
    const link = { alive: () => true, rpc } as unknown as RemoteLink
    const view = renderHook(() => { usePhoneSpeechSync(link, pc, [pc]); return usePhoneSpeech() })
    await waitFor(() => expect(rpc).toHaveBeenCalled())
    await waitFor(() => expect(view.result.current).toMatchObject({ inputReady: false, outputReady: false }))
    expect(loadSpeechCache(pc)).toBeUndefined()
  })
})

it("rechecks a paired PC voice opt-out before using the direct Gateway", async () => {
  signIn()
  const enabled = { ...speech, input: { ...speech.input, user_disabled: false } }
  const disabled = { ...enabled, input: { ...enabled.input, user_disabled: true, enabled: false, ready: false } }
  const rpc = vi.fn<RemoteLink["rpc"]>().mockResolvedValue({ ok: true, speech: disabled } as RemoteResponse)
  setPhoneSpeechSource(pc, { alive: () => true, rpc } as unknown as RemoteLink)
  expect(acceptPhoneSpeechConfig(pc, enabled)).toBe(true)
  const view = renderHook(() => usePhoneSpeech())
  expect(view.result.current.inputReady).toBe(true)
  const fetcher = vi.fn<typeof fetch>()
  vi.stubGlobal("fetch", fetcher)
  await act(async () => {
    await expect(transcribePhoneSpeech(new Blob(["recording"]), new AbortController().signal)).rejects.toMatchObject({ name: "AbortError" })
  })
  expect(rpc).toHaveBeenCalledWith({ op: "speech_config" }, expect.anything())
  expect(fetcher).not.toHaveBeenCalled()
  expect(view.result.current.inputReady).toBe(false)
})

it("cancels the paired preference read when voice is canceled before Gateway admission", async () => {
  signIn()
  const enabled = { ...speech, input: { ...speech.input, user_disabled: false } }
  const waitForAbort = vi.fn()
  const rpc = vi.fn<RemoteLink["rpc"]>((_request, options) => {
    const signal = options?.signal
    if (!signal) return Promise.resolve({ v: 1, id: "config", ok: true, speech: enabled })
    return new Promise((_resolve, reject) => {
      signal.addEventListener("abort", () => {
        waitForAbort()
        reject(new DOMException("Aborted", "AbortError"))
      }, { once: true })
    })
  })
  setPhoneSpeechSource(pc, { alive: () => true, rpc } as unknown as RemoteLink)
  expect(acceptPhoneSpeechConfig(pc, enabled)).toBe(true)
  const fetcher = vi.fn<typeof fetch>()
  vi.stubGlobal("fetch", fetcher)
  const controller = new AbortController()
  const outcome = transcribePhoneSpeech(new Blob(["recording"]), controller.signal).catch(error => error)
  expect(rpc).toHaveBeenCalledOnce()
  controller.abort()
  expect(await outcome).toMatchObject({ name: "AbortError" })
  expect(waitForAbort).toHaveBeenCalledOnce()
  expect(fetcher).not.toHaveBeenCalled()
})
