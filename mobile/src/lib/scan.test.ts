import { afterEach, describe, expect, it, vi } from "vitest"

import { encodeOffer } from "./offer"
import {
  attachStream,
  frameSize,
  openRearCamera,
  pairlinkFromQR,
  payloadFromRGBA,
  releaseStream,
  sampleFrame,
  ScanCameraError,
  scanCameraMessage,
  startDecodeLoop,
  startLiveScan,
} from "./scan"
import { setLocale, t } from "./i18n"

const native = vi.hoisted(() => ({ on: false }))
const requestPermissions = vi.hoisted(() => vi.fn())

vi.mock("@capacitor/core", () => ({
  Capacitor: { isNativePlatform: () => native.on },
}))

vi.mock("@capacitor/camera", () => ({
  Camera: { requestPermissions },
}))

vi.mock("jsqr", () => ({ default: vi.fn() }))

import jsQR from "jsqr"

function offerURI(): string {
  return encodeOffer({
    hubURL: "http://127.0.0.1:7780",
    code: "ScanCode01",
    hostPub: new Uint8Array(32).fill(1),
    lan: [],
  })
}

function installMedia(getUserMedia: ReturnType<typeof vi.fn>) {
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: { getUserMedia },
  })
}

describe("live scan", () => {
  afterEach(() => {
    native.on = false
    requestPermissions.mockReset()
    vi.mocked(jsQR).mockReset()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it("shrinks a preview frame so decoding does not stall the phone", () => {
    expect(frameSize(0, 10)).toEqual({ width: 0, height: 0 })
    expect(frameSize(100, 50, 0)).toEqual({ width: 0, height: 0 })
    expect(frameSize(100, 50)).toEqual({ width: 100, height: 50 })
    expect(frameSize(1280, 720, 640)).toEqual({ width: 640, height: 360 })
  })

  it("reads a QR payload and ignores a short buffer", () => {
    expect(payloadFromRGBA(new Uint8ClampedArray(4), 2, 2)).toBeNull()
    expect(jsQR).not.toHaveBeenCalled()
    vi.mocked(jsQR).mockReturnValue(null)
    const blank = new Uint8ClampedArray(16)
    expect(payloadFromRGBA(blank, 2, 2)).toBeNull()
    vi.mocked(jsQR).mockReturnValue({ data: "   " } as never)
    expect(payloadFromRGBA(blank, 2, 2)).toBeNull()
    vi.mocked(jsQR).mockReturnValue({ data: " pairlink " } as never)
    expect(payloadFromRGBA(blank, 2, 2)).toBe("pairlink")
  })

  it("accepts only a pairlink offer", () => {
    expect(pairlinkFromQR("   ")).toBeNull()
    expect(pairlinkFromQR("not-an-offer")).toBeNull()
    const uri = offerURI()
    expect(pairlinkFromQR("  " + uri + "  ")).toBe(uri)
  })

  it("maps camera failures onto the scan copy", () => {
    setLocale("en")
    expect(scanCameraMessage(new ScanCameraError("denied"))).toBe(t("scan.cameraDenied"))
    expect(scanCameraMessage(new ScanCameraError("missing"))).toBe(t("scan.noCamera"))
    expect(scanCameraMessage(new Error("boom"))).toBe(t("scan.cameraFailed"))
    setLocale("zh")
    expect(scanCameraMessage(new ScanCameraError("denied"))).toBe(t("scan.cameraDenied"))
  })

  it("samples a scaled frame and skips a video that has not started", () => {
    const video = document.createElement("video")
    const canvas = document.createElement("canvas")
    expect(sampleFrame(video, canvas)).toBeNull()
    Object.defineProperty(video, "videoWidth", { value: 1280 })
    Object.defineProperty(video, "videoHeight", { value: 720 })
    const image = { width: 640, height: 360, data: new Uint8ClampedArray(16) } as ImageData
    const drawImage = vi.fn()
    const getContext = vi.spyOn(canvas, "getContext")
    getContext.mockReturnValueOnce(null)
    expect(sampleFrame(video, canvas, 640)).toBeNull()
    getContext.mockReturnValueOnce({
      drawImage,
      getImageData: () => image,
    } as unknown as CanvasRenderingContext2D)
    expect(sampleFrame(video, canvas, 640)).toBe(image)
    expect(canvas.width).toBe(640)
    expect(canvas.height).toBe(360)
    expect(drawImage).toHaveBeenCalledWith(video, 0, 0, 640, 360)
  })

  it("reports one code and then stops sampling", () => {
    vi.useFakeTimers()
    const sample = vi.fn().mockReturnValueOnce(null).mockImplementationOnce(() => {
      throw new Error("canvas")
    }).mockReturnValueOnce("code")
    const onText = vi.fn()
    const handle = startDecodeLoop({ sample, onText, intervalMs: 20 })
    vi.advanceTimersByTime(20)
    expect(onText).not.toHaveBeenCalled()
    vi.advanceTimersByTime(20)
    expect(onText).not.toHaveBeenCalled()
    vi.advanceTimersByTime(20)
    expect(onText).toHaveBeenCalledTimes(1)
    expect(onText).toHaveBeenCalledWith("code")
    vi.advanceTimersByTime(200)
    expect(sample).toHaveBeenCalledTimes(3)
    handle.stop()
  })

  it("stops a loop that has not seen a code", () => {
    vi.useFakeTimers()
    const sample = vi.fn().mockReturnValue(null)
    const handle = startDecodeLoop({ sample, onText: vi.fn(), intervalMs: 20 })
    handle.stop()
    vi.advanceTimersByTime(100)
    expect(sample).not.toHaveBeenCalled()
  })

  it("opens the rear camera and falls back when that lens is refused", async () => {
    const stream = { getTracks: () => [] } as unknown as MediaStream
    const getUserMedia = vi
      .fn()
      .mockRejectedValueOnce(new DOMException("lens", "OverconstrainedError"))
      .mockResolvedValueOnce(stream)
    installMedia(getUserMedia)
    await expect(openRearCamera()).resolves.toBe(stream)
    expect(getUserMedia).toHaveBeenNthCalledWith(1, {
      audio: false,
      video: { facingMode: { ideal: "environment" } },
    })
    expect(getUserMedia).toHaveBeenNthCalledWith(2, { audio: false, video: true })
  })

  it("does not open a preview when the system camera permission is denied", async () => {
    native.on = true
    requestPermissions.mockResolvedValue({ camera: "denied", photos: "prompt" })
    const getUserMedia = vi.fn()
    installMedia(getUserMedia)
    await expect(openRearCamera()).rejects.toMatchObject({ reason: "denied" })
    expect(getUserMedia).not.toHaveBeenCalled()
  })

  it("asks for the rear camera after the system permission is granted", async () => {
    native.on = true
    requestPermissions.mockResolvedValue({ camera: "granted", photos: "prompt" })
    const stream = { getTracks: () => [] } as unknown as MediaStream
    const getUserMedia = vi.fn().mockResolvedValue(stream)
    installMedia(getUserMedia)
    await expect(openRearCamera()).resolves.toBe(stream)
    expect(requestPermissions).toHaveBeenCalledWith({ permissions: ["camera"] })
  })

  it("reports a missing camera when the device has none", async () => {
    installMedia(
      vi
        .fn()
        .mockRejectedValueOnce(new DOMException("none", "NotFoundError"))
        .mockRejectedValueOnce(new DOMException("none", "NotFoundError")),
    )
    await expect(openRearCamera()).rejects.toMatchObject({ reason: "missing" })
  })

  it("reports a denied preview without a second prompt", async () => {
    const getUserMedia = vi.fn().mockRejectedValue(new DOMException("no", "NotAllowedError"))
    installMedia(getUserMedia)
    await expect(openRearCamera()).rejects.toMatchObject({ reason: "denied" })
    expect(getUserMedia).toHaveBeenCalledTimes(1)
  })

  it("reports a missing camera when the browser has no media devices", async () => {
    Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: undefined })
    await expect(openRearCamera()).rejects.toMatchObject({ reason: "missing" })
  })

  it("reports a failed preview for an unexpected camera error", async () => {
    installMedia(vi.fn().mockRejectedValue(new DOMException("busy", "AbortError")))
    await expect(openRearCamera()).rejects.toMatchObject({ reason: "failed" })
  })

  it("attaches a muted inline preview", async () => {
    const video = document.createElement("video")
    const play = vi.fn().mockResolvedValue(undefined)
    video.play = play
    const stream = { getTracks: () => [] } as unknown as MediaStream
    await attachStream(video, stream)
    expect(video.srcObject).toBe(stream)
    expect(video.muted).toBe(true)
    expect(video.playsInline).toBe(true)
    expect(video.getAttribute("playsinline")).toBe("true")
    expect(play).toHaveBeenCalled()
  })

  it("stops every track on release", () => {
    const stop = vi.fn()
    releaseStream({ getTracks: () => [{ stop }] } as unknown as MediaStream)
    expect(stop).toHaveBeenCalled()
  })

  it("decodes a live preview and releases it when stopped", async () => {
    vi.useFakeTimers()
    const stopTrack = vi.fn()
    const stream = { getTracks: () => [{ stop: stopTrack }] } as unknown as MediaStream
    installMedia(vi.fn().mockResolvedValue(stream))
    const video = document.createElement("video")
    video.play = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(video, "videoWidth", { value: 100 })
    Object.defineProperty(video, "videoHeight", { value: 80 })
    const image = {
      width: 8,
      height: 8,
      data: new Uint8ClampedArray(8 * 8 * 4),
    } as ImageData
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({
      drawImage: () => undefined,
      getImageData: () => image,
    } as unknown as CanvasRenderingContext2D)
    vi.mocked(jsQR).mockReturnValue({ data: offerURI() } as never)
    const onText = vi.fn()
    const handle = await startLiveScan({ video, onText })
    await vi.advanceTimersByTimeAsync(200)
    expect(onText).toHaveBeenCalledWith(offerURI())
    handle.stop()
    expect(stopTrack).toHaveBeenCalled()
    expect(video.srcObject).toBeNull()
    handle.stop()
    expect(stopTrack).toHaveBeenCalledTimes(1)
  })

  it("releases the preview when playback never starts", async () => {
    const stopTrack = vi.fn()
    const stream = { getTracks: () => [{ stop: stopTrack }] } as unknown as MediaStream
    installMedia(vi.fn().mockResolvedValue(stream))
    const video = document.createElement("video")
    video.play = vi.fn().mockRejectedValue(new DOMException("blocked", "NotAllowedError"))
    await expect(startLiveScan({ video, onText: vi.fn() })).rejects.toMatchObject({ reason: "denied" })
    expect(stopTrack).toHaveBeenCalled()
  })
})
