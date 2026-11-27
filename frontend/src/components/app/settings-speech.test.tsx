import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { SpeechSettings } from "./settings-speech"
import { speechPlayback } from "@/lib/speech-media"
import { defaultSpeechSettings } from "@/lib/speech-types"
import type { Settings, Meta } from "@/lib/types"
import { useApp } from "@/store/app"
import { gatewayVoicePolicy } from "@/test/gateway-policy"

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

function setup(mode: "local" | "fofa", onChange = vi.fn(), onFlush = vi.fn(async () => {}), model_policy?: Settings["model_policy"], deviceId = "") {
  useApp.setState({ meta: { product_mode: mode, model_policy } as Meta })
  const speech = defaultSpeechSettings()
  speech.input.has_api_key = true
  speech.input.device_id = deviceId
  function Host() {
    const [settings, setSettings] = useState({ speech, models: { default: "", providers: [] }, model_policy } as unknown as Settings)
    return <SpeechSettings settings={settings} onChange={(next) => { setSettings(next); onChange(next) }} onFlush={onFlush} />
  }
  render(<Host />)
  return { onChange, onFlush }
}

describe("Speech settings", () => {
  it("explains why speech tests are disabled and enables them after the corresponding switch", () => {
    vi.stubGlobal("navigator", { mediaDevices: { getUserMedia: vi.fn() } })
    vi.stubGlobal("MediaRecorder", vi.fn())
    setup("local")
    const recording = screen.getByRole("button", { name: "Test voice input" })
    const reading = screen.getByRole("button", { name: "Test reading" })
    expect(recording).toBeDisabled()
    expect(reading).toBeDisabled()
    expect(screen.getByText("Enable Voice input above before testing the microphone.")).toBeVisible()
    expect(screen.getByText("Enable Read aloud above before testing playback.")).toBeVisible()
    expect(recording).toHaveAccessibleDescription("Enable Voice input above before testing the microphone.")
    expect(reading).toHaveAccessibleDescription("Enable Read aloud above before testing playback.")
    fireEvent.click(screen.getAllByLabelText("Enabled")[0])
    expect(recording).toBeEnabled()
    fireEvent.click(screen.getAllByLabelText("Enabled")[1])
    expect(reading).toBeDisabled()
    expect(screen.getByText("Enter text to test playback.")).toBeVisible()
    fireEvent.change(screen.getByLabelText("Text for the reading test"), { target: { value: "Playback fixture" } })
    expect(reading).toBeEnabled()
    act(() => useApp.setState(state => ({ meta: { ...state.meta, speech: {
      input: { ready: false }, output: { ready: false },
    } } as Meta })))
    expect(recording).toBeDisabled()
    expect(reading).toBeDisabled()
    expect(screen.getAllByText("The speech service is not ready. Check the speech configuration or gateway account status.")).toHaveLength(2)
  })

  it("explains when the environment cannot record audio", () => {
    vi.stubGlobal("MediaRecorder", undefined)
    setup("local")
    fireEvent.click(screen.getAllByLabelText("Enabled")[0])
    expect(screen.getByRole("button", { name: "Test voice input" })).toBeDisabled()
    expect(screen.getByText("Microphone recording is not supported in this environment.")).toBeVisible()
  })

  it("keeps Stop reading available when a setting is disabled during playback", () => {
    vi.spyOn(speechPlayback, "getSnapshot").mockReturnValue({ key: "settings:speech", state: "playing" })
    const stop = vi.spyOn(speechPlayback, "stop").mockImplementation(() => {})
    setup("local")
    const button = screen.getByRole("button", { name: "Stop reading" })
    expect(button).toBeEnabled()
    fireEvent.click(button)
    expect(stop).toHaveBeenCalledOnce()
  })

  it("hides fixed gateway parameters and keeps recording settings collapsed until opened or searched", () => {
    const model_policy = gatewayVoicePolicy()
    const speech = defaultSpeechSettings()
    useApp.setState({ meta: { product_mode: "fofa", model_policy, speech: {
      ...speech,
      input: { ...speech.input, execution: "gateway", ready: true, model: "", label: "", audio: {
        version: 1, status: "configured", scope: "input", overrides: {}, parameters: {
          response_format: { values: ["json"], default: "json", configurable: false },
          max_file_bytes: 25 * 1024 * 1024,
        },
      } },
      output: { ...speech.output, execution: "gateway", ready: true, model: "", label: "", audio: {
        version: 1, status: "configured", scope: "output", overrides: {}, parameters: {
          voice: { values: ["calm", "bright"], default: "calm", configurable: true },
          response_format: { values: ["pcm"], default: "pcm", configurable: false },
          speed: { min: 1, max: 1, default: 1, configurable: false },
        },
      } },
    } } as unknown as Meta })
    const onChange = vi.fn()
    const settings = { speech, models: { default: "", providers: [] }, model_policy } as unknown as Settings
    const { rerender } = render(<SpeechSettings settings={settings} onChange={onChange} onFlush={async () => {}} />)
    const voice = screen.getByRole("combobox", { name: "Voice ID" })
    expect(voice.closest("[data-settings-row]")).not.toBeNull()
    fireEvent.click(voice)
    fireEvent.click(screen.getByRole("option", { name: "bright" }))
    expect(onChange.mock.lastCall![0].speech.gateway_parameters).toEqual({ output: { voice: "bright" } })
    expect(screen.queryByText("Audio format")).toBeNull()
    expect(screen.queryByText("Speech speed")).toBeNull()
    expect(screen.getByText("Advanced settings").closest("details")?.open).toBe(false)
    rerender(<SpeechSettings settings={settings} onChange={onChange} onFlush={async () => {}} query="Voice ID" />)
    expect(screen.getByRole("combobox", { name: "Voice ID" })).toBeVisible()
    expect(screen.queryByRole("combobox", { name: "Microphone" })).toBeNull()
    rerender(<SpeechSettings settings={settings} onChange={onChange} onFlush={async () => {}} query="Recording format" />)
    expect(screen.getByText("Advanced settings").closest("details")?.open).toBe(true)
    expect(screen.getByRole("combobox", { name: "Recording format" })).toBeVisible()
  })

  it("distinguishes generating from playing in the reading test", () => {
    let snapshot: ReturnType<typeof speechPlayback.getSnapshot> = { key: "settings:speech", state: "loading" }
    let changed: (() => void) | undefined
    vi.spyOn(speechPlayback, "getSnapshot").mockImplementation(() => snapshot)
    vi.spyOn(speechPlayback, "subscribe").mockImplementation((listener) => { changed = listener; return () => {} })
    setup("local")
    expect(screen.getByRole("status")).toHaveTextContent("Generating speech…")
    act(() => { snapshot = { key: "settings:speech", state: "playing" }; changed?.() })
    expect(screen.getByRole("status")).toHaveTextContent("Playing audio")
    act(() => { snapshot = { state: "idle" }; changed?.() })
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("saves a physical microphone and can return to the system default", async () => {
    const enumerateDevices = vi.fn(async () => [
      { kind: "audioinput", deviceId: "default", label: "Default - MacBook Pro Microphone" },
      { kind: "audioinput", deviceId: "camera", label: "Camera 100-Audio" },
      { kind: "audiooutput", deviceId: "speakers", label: "Speakers" },
    ])
    vi.stubGlobal("navigator", { mediaDevices: { enumerateDevices, addEventListener: vi.fn(), removeEventListener: vi.fn() } })
    const { onChange } = setup("local")
    const microphone = screen.getByRole("combobox", { name: "Microphone" })
    expect(microphone).toHaveTextContent("System default")
    fireEvent.click(microphone)
    fireEvent.click(await screen.findByRole("option", { name: "Camera 100-Audio" }))
    expect(onChange.mock.lastCall![0].speech.input.device_id).toBe("camera")
    fireEvent.click(screen.getByRole("combobox", { name: "Microphone" }))
    fireEvent.click(await screen.findByRole("option", { name: "System default" }))
    expect(onChange.mock.lastCall![0].speech.input.device_id).toBe("")
  })

  it("shows the system default on disconnect and reuses the preference on reconnect", async () => {
    const builtIn = { kind: "audioinput", deviceId: "built-in", label: "Built-in microphone" }
    const external = { kind: "audioinput", deviceId: "external", label: "External microphone" }
    const enumerateDevices = vi.fn(async () => [builtIn, external])
    const mediaDevices = Object.assign(new EventTarget(), { enumerateDevices })
    vi.stubGlobal("navigator", { mediaDevices })
    const { onChange } = setup("local", undefined, undefined, undefined, external.deviceId)
    const microphone = screen.getByRole("combobox", { name: "Microphone" })
    await waitFor(() => expect(microphone).toHaveTextContent(external.label))

    enumerateDevices.mockResolvedValue([builtIn])
    mediaDevices.dispatchEvent(new Event("devicechange"))
    await waitFor(() => expect(microphone).toHaveTextContent("System default"))
    expect(onChange).not.toHaveBeenCalled()

    enumerateDevices.mockResolvedValue([builtIn, external])
    mediaDevices.dispatchEvent(new Event("devicechange"))
    await waitFor(() => expect(microphone).toHaveTextContent(external.label))
    expect(onChange).not.toHaveBeenCalled()

    enumerateDevices.mockResolvedValue([builtIn])
    mediaDevices.dispatchEvent(new Event("devicechange"))
    await waitFor(() => expect(microphone).toHaveTextContent("System default"))
    fireEvent.click(microphone)
    expect(screen.queryByRole("option", { name: "Unavailable" })).toBeNull()
    fireEvent.click(await screen.findByRole("option", { name: "System default" }))
    expect(onChange.mock.lastCall![0].speech.input.device_id).toBe("")
  })

  it("configures local speech independently and preserves a saved secret until explicitly cleared", () => {
    const { onChange } = setup("local")
    fireEvent.change(screen.getAllByLabelText("API base URL")[0], { target: { value: "http://speech.invalid/v1" } })
    fireEvent.change(screen.getAllByLabelText("Model")[0], { target: { value: "recognizer" } })
    fireEvent.click(screen.getAllByLabelText("Enabled")[0])
    const current = onChange.mock.lastCall![0] as Settings
    expect(current.speech?.input).toMatchObject({ enabled: true, source: "custom", base_url: "http://speech.invalid/v1", model: "recognizer" })
    expect(current.speech?.input.api_key).toBeUndefined()
    expect(current.speech?.output.enabled).toBe(false)
    fireEvent.click(screen.getByRole("button", { name: "Clear saved key" }))
    expect(onChange.mock.lastCall![0].speech.input.api_key).toBe("")
  })

  it("keeps the signed-in account voice role when local settings already have a model policy", () => {
    const { onChange } = setup("local", vi.fn(), vi.fn(async () => {}), { ...gatewayVoicePolicy(true), mode: "local", scope: "local" })
    expect(screen.queryByLabelText("API base URL")).toBeNull()
    expect(screen.getAllByText("Not configured or unavailable").length).toBeGreaterThan(0)
    fireEvent.change(screen.getByLabelText("Speech speed"), { target: { value: "1.2" } })
    expect(onChange.mock.lastCall![0].speech.output).toMatchObject({ source: "account", speed: 1.2 })
  })

  it("defaults on only for configured gateway roles and remembers an independent opt-out", () => {
    const { onChange } = setup("fofa", vi.fn(), vi.fn(async () => {}), gatewayVoicePolicy())
    const defaults = defaultSpeechSettings()
    act(() => useApp.setState(state => ({ meta: { ...state.meta, speech: {
      ...defaults, execution: "gateway",
      input: { ...defaults.input, ready: false, label: "" },
      output: { ...defaults.output, enabled: true, ready: true, model: "voice", label: "Voice", execution: "gateway" },
    } } as Meta })))
    expect(screen.queryByLabelText("API base URL")).toBeNull()
    expect(screen.queryByLabelText("API key")).toBeNull()
    expect(screen.getByRole("heading", { name: "Voice input" })).toBeVisible()
    const input = screen.getByRole("switch", { name: "Voice input" })
    const output = screen.getByRole("switch", { name: "Read aloud" })
    expect(input).toBeDisabled()
    expect(input).not.toBeChecked()
    expect(output).toBeChecked()
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(output)
    expect(output).not.toBeChecked()
    expect(onChange.mock.lastCall![0].speech.output).toMatchObject({ enabled: false, gateway_disabled: true, source: "account" })
    expect(onChange.mock.lastCall![0].speech.input.gateway_disabled).toBeUndefined()
    fireEvent.click(output)
    expect(output).toBeChecked()
    expect(onChange.mock.lastCall![0].speech.output.gateway_disabled).toBe(false)
  })

  it("does not infer Gateway voice permission from product mode alone", () => {
    const { onChange } = setup("fofa")
    expect(screen.queryByRole("heading", { name: "Voice input" })).toBeNull()
    expect(screen.queryByLabelText("Enabled")).toBeNull()
    expect(onChange).not.toHaveBeenCalled()
  })

  it("discovers models only after pending configuration writes finish", async () => {
    const order: string[] = []
    const onFlush = vi.fn(async () => { order.push("saved") })
    const fetcher = vi.fn(async (_path: RequestInfo | URL, _init?: RequestInit) => { order.push("discovered"); return new Response(JSON.stringify({ models: ["recognizer", 42] }), { status: 200 }) })
    vi.stubGlobal("fetch", fetcher)
    setup("local", vi.fn(), onFlush)
    fireEvent.click(screen.getAllByRole("button", { name: "Discover models" })[0])
    await waitFor(() => expect(fetcher).toHaveBeenCalledOnce())
    expect(order).toEqual(["saved", "discovered"])
    expect(fetcher.mock.calls[0]?.[0]).toBe("/api/audio/models")
    expect(document.querySelectorAll("datalist option")).toHaveLength(1)
  })

  it("cancels discovery when its endpoint changes and ignores the old list", async () => {
    let resolve!: (response: Response) => void
    let signal!: AbortSignal
    vi.stubGlobal("fetch", vi.fn((_path: RequestInfo | URL, init?: RequestInit) => {
      signal = init!.signal as AbortSignal
      return new Promise<Response>((done) => { resolve = done })
    }))
    setup("local")
    fireEvent.click(screen.getAllByRole("button", { name: "Discover models" })[0])
    await waitFor(() => expect(signal).toBeDefined())
    fireEvent.change(screen.getAllByLabelText("API base URL")[0], { target: { value: "http://replacement.invalid/v1" } })
    expect(signal.aborted).toBe(true)
    resolve(new Response(JSON.stringify({ models: ["old model"] }), { status: 200 }))
    await waitFor(() => expect(screen.getAllByRole("button", { name: "Discover models" })[0]).toBeEnabled())
    expect(document.querySelectorAll("datalist option")).toHaveLength(0)
  })
})
