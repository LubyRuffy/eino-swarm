import { afterEach, describe, expect, it, vi } from "vitest"

import { playScanChime, primeScanChime, resetScanChime, scanChimeSrc } from "./scan-chime"

describe("scan chime", () => {
  afterEach(() => {
    resetScanChime()
    vi.restoreAllMocks()
  })

  it("builds a wav ding", () => {
    const src = scanChimeSrc()
    expect(src.startsWith("data:audio/wav;base64,")).toBe(true)
    const bytes = Uint8Array.from(atob(src.slice("data:audio/wav;base64,".length)), (c) =>
      c.charCodeAt(0),
    )
    expect(String.fromCharCode(...bytes.subarray(0, 4))).toBe("RIFF")
    expect(String.fromCharCode(...bytes.subarray(8, 12))).toBe("WAVE")
    expect(bytes.length).toBeGreaterThan(1000)
    expect(scanChimeSrc()).toBe(src)
  })

  it("unlocks silently on the tap and plays the ding later", async () => {
    let audio: HTMLAudioElement | undefined
    const play = vi.spyOn(HTMLAudioElement.prototype, "play").mockImplementation(function (
      this: HTMLAudioElement,
    ) {
      audio = this
      return Promise.resolve()
    })
    const pause = vi.spyOn(HTMLAudioElement.prototype, "pause").mockImplementation(() => undefined)
    primeScanChime()
    expect(audio?.volume).toBe(0)
    expect(play).toHaveBeenCalledTimes(1)
    await Promise.resolve()
    expect(pause).toHaveBeenCalledTimes(1)
    expect(audio?.volume).toBe(1)
    playScanChime()
    expect(play).toHaveBeenCalledTimes(2)
    expect(audio?.volume).toBe(1)
    await Promise.resolve()
    expect(pause).toHaveBeenCalledTimes(1)
  })

  it("does not pause a ding that arrives before the unlock play settles", async () => {
    const gates: Array<() => void> = []
    vi.spyOn(HTMLAudioElement.prototype, "play").mockImplementation(
      () =>
        new Promise((resolve) => {
          gates.push(() => resolve())
        }),
    )
    const pause = vi.spyOn(HTMLAudioElement.prototype, "pause").mockImplementation(() => undefined)
    primeScanChime()
    playScanChime()
    gates[0]?.()
    await Promise.resolve()
    expect(pause).not.toHaveBeenCalled()
  })

  it("still dings when the unlock play returns no promise", () => {
    const play = vi.spyOn(HTMLAudioElement.prototype, "play").mockReturnValue(undefined as never)
    const pause = vi.spyOn(HTMLAudioElement.prototype, "pause").mockImplementation(() => undefined)
    primeScanChime()
    expect(pause).toHaveBeenCalled()
    playScanChime()
    expect(play).toHaveBeenCalledTimes(2)
  })
})
