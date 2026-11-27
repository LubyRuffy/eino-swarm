import { expect, test } from "@playwright/test"
import { defaultSpeechSettings } from "../src/lib/speech-types"

// Exercise the managed-account page with declared gateway capabilities. This
// fixture supplies the capability declaration only. Managed-mode settings saves
// cross the real HTTP handler; account-profile authorization also has Go tests.
test("managed Gateway speech keeps independent controls and editable parameters", async ({ page, request }, testInfo) => {
  const model_policy = {
    mode: "gateway", configurable: false, ready: true, capability_status: "ready", scope: "gateway:ui-fixture",
    capabilities: { authorized: true, available: true, configurable: false, capabilities: {
      transcription: { supported: true, available: true, configurable: false, endpoints: ["audio_transcriptions"] },
      speech: { supported: true, available: true, configurable: false, endpoints: ["audio_speech"] },
    } },
  }
  const speech = defaultSpeechSettings()
  speech.input.gateway_disabled = true
  speech.output.gateway_disabled = true
  const snapshot = {
    ...speech,
    input: { ...speech.input, enabled: true, ready: true, model: "transcription", label: "", execution: "gateway", audio: {
      version: 1, status: "configured", scope: "input", overrides: {}, parameters: {
        response_format: { values: ["json"], default: "json", configurable: false },
        language: { values: ["zh", "en"], configurable: true }, max_file_bytes: 25 * 1024 * 1024,
      },
    } },
    output: { ...speech.output, enabled: true, ready: true, model: "speech", label: "", execution: "gateway", audio: {
      version: 1, status: "configured", scope: "output", overrides: {}, parameters: {
        voice: { values: ["calm", "bright"], default: "calm", configurable: true },
        response_format: { values: ["pcm"], default: "pcm", configurable: false },
        stream_format: { values: ["audio", "sse"], default: "sse", configurable: true },
        speed: { min: 0.5, max: 2, default: 1, configurable: true }, max_input_characters: 1000,
      },
    } },
  }
  const previous = (await (await request.get("/api/settings")).json()).settings
  expect((await request.put("/api/settings", { data: { product: { mode: "fofa" }, speech } })).ok()).toBe(true)
  const savedSpeech = async () => (await (await request.get("/api/settings")).json()).settings.speech
  try {
    await page.route("**/api/meta", async route => {
      const meta = await (await route.fetch()).json()
      await route.fulfill({ json: { ...meta, product_mode: "fofa", model_policy, speech: snapshot } })
    })
    await page.route("**/api/settings", async route => {
      const response = await route.fetch()
      expect(response.ok()).toBe(true)
      const body = await response.json()
      await route.fulfill({ json: { ...body, settings: { ...body.settings, model_policy } } })
    })
    await page.goto("/")
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    const settings = page.getByRole("dialog", { name: "Settings" })
    await settings.getByRole("tab", { name: "Models", exact: true }).click()
    const openInput = () => settings.getByRole("button", { name: "Configure Voice input", exact: true }).click()
    const openOutput = () => settings.getByRole("button", { name: "Configure Read aloud", exact: true }).click()
    const input = page.getByRole("dialog", { name: "Voice input", exact: true })
    const output = page.getByRole("dialog", { name: "Read aloud", exact: true })
    const inputToggle = input.getByRole("switch", { name: "Voice input", exact: true })
    const outputToggle = output.getByRole("switch", { name: "Read aloud", exact: true })
    await openInput()
    await expect(inputToggle).not.toBeChecked()
    await inputToggle.click()
    await expect.poll(async () => (await savedSpeech()).input.gateway_disabled).toBe(false)
    await expect(input.getByRole("button", { name: "Test voice input", exact: true })).toBeEnabled()
    await expect(input.getByLabel("API key", { exact: true })).toHaveCount(0)
    const advanced = input.getByText("Advanced settings", { exact: true })
    const recordingFormat = input.getByRole("combobox", { name: "Recording format" })
    await expect(recordingFormat).toBeHidden()
    await advanced.click()
    await expect(recordingFormat).toBeVisible()
    await recordingFormat.click()
    await page.getByRole("option", { name: "WAV", exact: true }).click()
    await expect.poll(async () => (await savedSpeech()).input.recording_format).toBe("wav")
    await page.keyboard.press("Escape")
    await openOutput()
    await expect(outputToggle).not.toBeChecked()
    await outputToggle.click()
    await expect.poll(async () => (await savedSpeech()).output.gateway_disabled).toBe(false)
    await expect(output.getByRole("button", { name: "Test reading", exact: true })).toBeDisabled()
    await expect(output.getByRole("button", { name: "Test reading", exact: true })).toHaveAccessibleDescription("Enter text to test playback.")
    await expect(output.getByLabel("API key", { exact: true })).toHaveCount(0)
    const voice = output.getByRole("combobox", { name: "Voice ID", exact: true })
    await voice.scrollIntoViewIfNeeded()
    const row = voice.locator("xpath=ancestor::*[@data-settings-row][1]")
    const controlBounds = (await voice.boundingBox())!
    const rowBounds = (await row.boundingBox())!
    expect(controlBounds.width).toBeLessThanOrEqual(288)
    expect(controlBounds.width).toBeLessThan(rowBounds.width * 0.6)
    expect(controlBounds.x).toBeGreaterThan(rowBounds.x + rowBounds.width * 0.35)
    await expect(output.getByText("Audio format", { exact: true })).toHaveCount(0)
    const stream = output.getByRole("combobox", { name: "Stream format" })
    await stream.click()
    await page.getByRole("option", { name: "audio", exact: true }).click()
    await output.getByRole("spinbutton", { name: "Speech speed" }).fill("1.5")
    await expect.poll(async () => (await savedSpeech()).gateway_parameters?.output).toEqual({ stream_format: "audio", speed: 1.5 })
    await voice.click()
    await page.getByRole("option", { name: "bright", exact: true }).click()
    await expect.poll(async () => (await savedSpeech()).gateway_parameters?.output?.voice).toBe("bright")
    await expect(output.getByRole("button", { name: "Follow gateway default", exact: true })).toHaveCount(0)
    await voice.click()
    await page.getByRole("option", { name: "Follow gateway default (calm)", exact: true }).click()
    await expect.poll(async () => (await savedSpeech()).gateway_parameters?.output).toEqual({ stream_format: "audio", speed: 1.5 })
    await stream.click()
    await page.getByRole("option", { name: "Follow gateway default (sse)", exact: true }).click()
    await expect.poll(async () => (await savedSpeech()).gateway_parameters?.output).toEqual({ speed: 1.5 })
    await output.getByRole("spinbutton", { name: "Speech speed" }).fill("")
    await expect.poll(async () => (await savedSpeech()).gateway_parameters?.output).toEqual({})
    // Use keyboard navigation on the same real dropdown to guard accessibility.
    await voice.focus()
    await page.keyboard.press("Enter")
    await expect(page.getByRole("listbox")).toBeVisible()
    await page.keyboard.press("End")
    await expect(page.getByRole("option", { name: "bright", exact: true })).toBeFocused()
    await page.keyboard.press("Enter")
    await expect(voice).toHaveText("bright")
    await page.keyboard.press("Escape")
    await settings.getByRole("button", { name: "Back to app" }).click()
    await page.getByRole("button", { name: "Settings", exact: true }).click()
    await settings.getByRole("tab", { name: "Models", exact: true }).click()
    await openOutput()
    await expect(voice).toHaveText("bright")
    for (const width of [1024, 1920]) {
      await page.setViewportSize({ width, height: 1100 })
      await expect.poll(async () => {
        const bounds = await output.boundingBox()
        return bounds ? Math.abs(bounds.x + bounds.width - width) : Infinity
      }).toBeLessThan(2)
      expect((await output.boundingBox())!.width).toBeLessThanOrEqual(512)
      await page.screenshot({ path: testInfo.outputPath(`gateway-speech-${width}.png`) })
    }
    await page.keyboard.press("Escape")
    await openInput()
    await inputToggle.click()
    await expect.poll(async () => (await savedSpeech()).input.gateway_disabled).toBe(true)
    await expect(recordingFormat).toHaveCount(0)
    await page.keyboard.press("Escape")
    await openOutput()
    await expect(outputToggle).toBeChecked()
    await page.keyboard.press("Escape")
    await settings.getByRole("tab", { name: "General", exact: true }).click()
    await settings.getByRole("combobox", { name: "Language", exact: true }).click()
    await page.getByRole("option", { name: "中文", exact: true }).click()
    await page.getByRole("tab", { name: "模型", exact: true }).click()
    await page.getByRole("button", { name: "配置语音朗读", exact: true }).click()
    await expect(page.getByRole("dialog", { name: "语音朗读", exact: true })).toBeVisible()
    await expect(page.getByRole("combobox", { name: "音色 ID", exact: true })).toHaveText("bright")
    await page.screenshot({ path: testInfo.outputPath("gateway-speech-zh-light.png") })
    await page.emulateMedia({ colorScheme: "dark" })
    await expect(page.locator("html")).toHaveClass(/dark/)
    await page.screenshot({ path: testInfo.outputPath("gateway-speech-zh-dark.png") })
  } finally {
    expect((await request.put("/api/settings", { data: { product: { mode: previous.product.mode }, speech: previous.speech, ui: previous.ui } })).ok()).toBe(true)
  }
})
