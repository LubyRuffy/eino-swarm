import { createServer } from "node:http"
import { expect, test } from "@playwright/test"
import { composer, freshConversation, send, waitForIdle, waitForDraftSaved } from "./session"

test.use({ permissions: ["microphone"], launchOptions: { args: ["--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream"] } })

function wave(): Buffer {
  const samples = 1600
  const bytes = Buffer.alloc(44 + samples * 2)
  bytes.write("RIFF", 0); bytes.writeUInt32LE(bytes.length - 8, 4); bytes.write("WAVEfmt ", 8)
  bytes.writeUInt32LE(16, 16); bytes.writeUInt16LE(1, 20); bytes.writeUInt16LE(1, 22)
  bytes.writeUInt32LE(16000, 24); bytes.writeUInt32LE(32000, 28); bytes.writeUInt16LE(2, 32); bytes.writeUInt16LE(16, 34)
  bytes.write("data", 36); bytes.writeUInt32LE(samples * 2, 40)
  for (let i = 0; i < samples; i++) bytes.writeInt16LE(Math.round(Math.sin(i * Math.PI / 20) * 1000), 44 + i * 2)
  return bytes
}

test("reopening the Voice input drawer does not request microphone access", async ({ page, request }) => {
  const previous = (await (await request.get("/api/settings")).json()).settings.speech
  await page.addInitScript(() => {
    const observed = { enumerations: 0, requests: 0, stops: 0 }
    Object.defineProperty(window, "micProbe", { value: observed })
    Object.defineProperty(navigator, "mediaDevices", { value: {
      enumerateDevices: async () => {
        observed.enumerations += 1
        return [{ kind: "audioinput", deviceId: observed.requests ? "fixture" : "", label: observed.requests ? "Fixture microphone" : "" }]
      },
      getUserMedia: async () => {
        observed.requests += 1
        return { getTracks: () => [{ stop: () => { observed.stops += 1 } }] }
      },
    } })
  })
  const probe = () => page.evaluate(() => (window as unknown as {
    micProbe: { enumerations: number; requests: number; stops: number }
  }).micProbe)
  await page.goto("/")
  await page.getByRole("button", { name: "Settings", exact: true }).click()
  const settings = page.getByRole("dialog", { name: "Settings", exact: true })
  const models = settings.getByRole("tab", { name: "Models", exact: true })
  const configure = settings.getByRole("button", { name: "Configure Voice input", exact: true })
  const input = page.getByRole("dialog", { name: "Voice input", exact: true })
  const microphone = input.getByRole("combobox", { name: "Microphone", exact: true })
  try {
    await models.click()
    expect(await probe()).toEqual({ enumerations: 0, requests: 0, stops: 0 })
    await configure.click()
    await input.getByRole("switch", { name: "Voice input", exact: true }).setChecked(true)
    await expect(microphone).toHaveText("System default")
    await expect.poll(async () => (await probe()).enumerations).toBeGreaterThan(0)
    const first = await probe()
    expect(first.requests).toBe(0)
    await input.getByRole("button", { name: "Close", exact: true }).click()
    await settings.getByRole("tab", { name: "General", exact: true }).click()
    await models.click()
    expect((await probe()).enumerations).toBe(first.enumerations)
    await configure.click()
    await expect.poll(async () => (await probe()).enumerations).toBeGreaterThan(first.enumerations)
    expect((await probe()).requests).toBe(0)
    await microphone.click()
    await expect(page.getByRole("option", { name: "Fixture microphone" })).toBeVisible()
    await expect.poll(async () => (await probe()).stops).toBe(1)
    await page.keyboard.press("Escape")
    await microphone.click()
    await expect(page.getByRole("option", { name: "Fixture microphone" })).toBeVisible()
    expect((await probe()).requests).toBe(1)
  } finally {
    await request.put("/api/settings", { data: { speech: previous } })
  }
})

test("speech drawers hide diagnostics until enabled, then record and play", async ({ page, request }, testInfo) => {
  const previous = (await (await request.get("/api/settings")).json()).settings.speech
  let recordedBytes = 0
  const spoken: string[] = []
  const upstream = createServer((req, res) => {
    void (async () => {
      const chunks: Buffer[] = []
      for await (const chunk of req) chunks.push(Buffer.from(chunk))
      const bytes = Buffer.concat(chunks)
      if (req.url === "/v1/audio/transcriptions") {
        const form = await new Request("http://fixture.invalid/", { method: "POST", headers: { "Content-Type": req.headers["content-type"]! }, body: bytes }).formData()
        const file = form.get("file") as File
        const audio = Buffer.from(await file.arrayBuffer())
        expect(audio.subarray(0, 4).toString()).toBe("RIFF")
        recordedBytes = audio.length
        res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify({ text: "fixture transcription" }))
      } else if (req.url === "/v1/audio/speech") {
        spoken.push(JSON.parse(bytes.toString()).input)
        res.writeHead(200, { "Content-Type": "audio/wav" }); res.end(wave())
      } else { res.writeHead(404); res.end() }
    })().catch(() => { res.writeHead(500); res.end() })
  })
  await new Promise<void>(resolve => upstream.listen(0, "127.0.0.1", resolve))
  const address = upstream.address()
  if (!address || typeof address === "string") throw new Error("Speech fixture did not listen")
  try {
    expect((await request.put("/api/settings", { data: { speech: {
      input: { enabled: false, source: "custom", base_url: `http://127.0.0.1:${address.port}/v1`, model: "fixture-asr", recording_format: "wav", device_id: "removed-input" },
      output: { enabled: false, source: "custom", base_url: `http://127.0.0.1:${address.port}/v1`, model: "fixture-tts", format: "wav" },
    } } })).ok()).toBe(true)
    await page.addInitScript(() => {
      const originalStart = MediaRecorder.prototype.start
      MediaRecorder.prototype.start = function (...args) {
        this.addEventListener("dataavailable", event => {
          if (event.data.size) (window as typeof window & { recordingChunk?: boolean }).recordingChunk = true
        })
        return Reflect.apply(originalStart, this, args)
      }
      const originalPlay = AudioBufferSourceNode.prototype.start
      AudioBufferSourceNode.prototype.start = function (...args) {
        if (this.buffer?.length) (window as typeof window & { playedSamples?: number }).playedSamples = this.buffer.length
        return Reflect.apply(originalPlay, this, args)
      }
    })
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const settings = page.getByRole("dialog", { name: "Settings", exact: true })
    await settings.getByRole("tab", { name: "Models", exact: true }).click()
    await settings.getByRole("button", { name: "Configure Voice input", exact: true }).click()
    const input = page.getByRole("dialog", { name: "Voice input", exact: true })
    const microphone = input.getByRole("combobox", { name: "Microphone", exact: true })
    const record = input.getByRole("button", { name: "Test voice input", exact: true })
    await expect(microphone).toHaveCount(0)
    await expect(record).toHaveCount(0)
    await input.screenshot({ path: testInfo.outputPath("disabled-voice-input.png") })
    await input.getByRole("switch", { name: "Voice input", exact: true }).setChecked(true)
    await expect(microphone).toHaveText("System default")
    await expect(record).toBeEnabled()
    await record.click()
    await expect.poll(() => page.evaluate(() => (window as typeof window & { recordingChunk?: boolean }).recordingChunk)).toBe(true)
    await input.getByRole("button", { name: "Finish and transcribe", exact: true }).click()
    await expect(input.getByLabel("Recognized text")).toHaveValue("fixture transcription")
    expect(recordedBytes).toBeGreaterThan(44)
    expect((await (await request.get("/api/settings")).json()).settings.speech.input.device_id).toBe("removed-input")
    await microphone.click()
    await expect(page.getByRole("option", { name: "Unavailable", exact: true })).toHaveCount(0)
    await page.getByRole("option", { name: "System default", exact: true }).click()
    await expect.poll(async () => (await (await request.get("/api/settings")).json()).settings.speech.input.device_id ?? "").toBe("")
    await input.getByRole("button", { name: "Close", exact: true }).click()
    await settings.getByRole("button", { name: "Configure Read aloud", exact: true }).click()
    const output = page.getByRole("dialog", { name: "Read aloud", exact: true })
    const play = output.getByRole("button", { name: "Test reading", exact: true })
    await expect(play).toHaveCount(0)
    await output.getByRole("switch", { name: "Read aloud", exact: true }).setChecked(true)
    await expect(play).toBeDisabled()
    await output.getByLabel("Text for the reading test").fill("Playback diagnostic")
    await expect(play).toBeEnabled()
    await play.click()
    await expect.poll(() => spoken).toEqual(["Playback diagnostic"])
    await expect.poll(() => page.evaluate(() => (window as typeof window & { playedSamples?: number }).playedSamples ?? 0)).toBeGreaterThan(0)
    await expect(play).toBeVisible()
  } finally {
    await request.put("/api/settings", { data: { speech: previous } })
    upstream.closeAllConnections()
    await new Promise<void>(resolve => upstream.close(() => resolve()))
  }
})

for (const recordingFormat of ["auto", "wav"] as const) {
test(`speech ${recordingFormat} settings gate real recording, preserve edits and read a completed answer`, async ({ page, request }) => {
  const previous = (await (await request.get("/api/settings")).json()).settings.speech
  let receive!: (audio: { data: string; mime: string; size: number }) => void
  const received = new Promise<{ data: string; mime: string; size: number }>((resolve) => { receive = resolve })
  let release!: () => void
  const recognitionGate = new Promise<void>((resolve) => { release = resolve })
  const spoken: string[] = []
  const upstream = createServer((req, res) => {
    void (async () => {
      const chunks: Buffer[] = []
      for await (const chunk of req) chunks.push(Buffer.from(chunk))
      const bytes = Buffer.concat(chunks)
      if (req.url === "/v1/audio/transcriptions") {
        const form = await new Request("http://fixture.invalid/", { method: "POST", headers: { "Content-Type": req.headers["content-type"]! }, body: bytes }).formData()
        const file = form.get("file") as File
        const audio = Buffer.from(await file.arrayBuffer())
        receive({ data: audio.toString("base64"), mime: file.type, size: audio.length })
        await recognitionGate
        res.writeHead(200, { "Content-Type": "application/json" }); res.end(JSON.stringify({ text: "recognized speech" }))
      } else if (req.url === "/v1/audio/speech") {
        spoken.push(JSON.parse(bytes.toString()).input)
        res.writeHead(200, { "Content-Type": "audio/wav" }); res.end(wave())
      } else { res.writeHead(404); res.end() }
    })().catch(() => { res.writeHead(500); res.end() })
  })
  await new Promise<void>((resolve) => upstream.listen(0, "127.0.0.1", resolve))
  const address = upstream.address()
  if (!address || typeof address === "string") throw new Error("Fixture did not listen")
  const baseURL = `http://127.0.0.1:${address.port}/v1`
  try {
    const configured = await request.put("/api/settings", { data: { speech: {
      input: { enabled: false, source: "custom", base_url: baseURL, model: "fixture-asr", recording_format: recordingFormat, device_id: recordingFormat === "auto" ? "removed-input" : "" },
      output: { enabled: true, source: "custom", base_url: baseURL, model: "fixture-tts", format: "wav" },
      limits: { chunk_chars: 64 },
    } } })
    expect(configured.ok()).toBe(true)
    await freshConversation(page)
    await expect(page.getByLabel("Start voice input")).toHaveCount(0)
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const settings = page.getByRole("dialog", { name: "Settings", exact: true })
    await expect(settings.getByRole("tab", { name: "Speech", exact: true })).toHaveCount(0)
    await settings.getByRole("tab", { name: "Models", exact: true }).click()
    await settings.getByRole("button", { name: "Configure Voice input", exact: true }).click()
    const input = page.getByRole("dialog", { name: "Voice input", exact: true })
    await input.getByRole("switch", { name: "Voice input", exact: true }).setChecked(true)
    await input.getByRole("button", { name: "Close", exact: true }).click()
    await settings.getByRole("button", { name: "Back to app" }).click()
    await expect(page.getByLabel("Start voice input")).toBeVisible()
    expect((await (await request.get("/api/settings")).json()).settings.speech.input.enabled).toBe(true)
    await composer(page).fill("original draft")
    await page.getByLabel("Start voice input").click()
    await expect(page.getByLabel("Finish and transcribe")).toBeVisible()
    await expect(page.getByTestId("voice-input-control").getByRole("status")).toHaveText("0:01")
    await page.getByLabel("Finish and transcribe").click()
    const captured = await received
    expect(captured.size).toBeGreaterThan(100)
    expect(captured.mime).toMatch(/^audio\//)
    const decoded = await page.evaluate(async ({ data }) => {
      const context = new AudioContext()
      try {
        const bytes = Uint8Array.from(atob(data), (char) => char.charCodeAt(0))
        const audio = await context.decodeAudioData(bytes.buffer)
        return { duration: audio.duration, channels: audio.numberOfChannels, sampleRate: audio.sampleRate }
      } finally { await context.close() }
    }, captured)
    expect(decoded.duration).toBeGreaterThan(0.5)
    expect(decoded.channels).toBeGreaterThan(0)
    if (recordingFormat === "wav") {
      expect(captured.mime).toBe("audio/wav")
      expect(Buffer.from(captured.data, "base64").subarray(0, 4).toString()).toBe("RIFF")
      expect(decoded.channels).toBe(1)
    }
    await composer(page).fill("edited during recognition")
    await expect(page.getByLabel("Send", { exact: true })).toBeDisabled()
    release()
    await expect(composer(page)).toHaveValue("edited during recognition recognized speech")
    await waitForDraftSaved(page)
    await send(page, "Summarize the current state")
    await waitForIdle(page)
    const answer = page.getByTestId("assistant-message").filter({ has: page.getByLabel("Read aloud", { exact: true }) }).last()
    await answer.hover()
    await answer.getByLabel("Read aloud", { exact: true }).click()
    await expect.poll(() => spoken.length).toBeGreaterThan(0)
    await expect(answer.getByLabel("Read aloud", { exact: true })).toBeVisible()
    expect(spoken.every((text) => [...text].length <= 64)).toBe(true)
    expect(spoken.join(" ")).not.toContain("```")
    expect(spoken.join(" ")).not.toContain("https://")
  } finally {
    release()
    await request.put("/api/settings", { data: { speech: previous } })
    upstream.closeAllConnections()
    await new Promise<void>((resolve) => upstream.close(() => resolve()))
  }
})
}
