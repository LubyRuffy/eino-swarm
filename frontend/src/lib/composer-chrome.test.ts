import { afterEach, describe, expect, it } from "vitest"

import {
  COMPOSER_PAD_VAR,
  COMPOSER_STAGE_ATTR,
  clearComposerPad,
  syncComposerPad,
} from "./composer-chrome"

function stageWithChild(): { stage: HTMLElement; child: HTMLElement } {
  const stage = document.createElement("div")
  stage.setAttribute(COMPOSER_STAGE_ATTR, "")
  const child = document.createElement("div")
  stage.appendChild(child)
  document.body.appendChild(stage)
  return { stage, child }
}

afterEach(() => {
  document.body.replaceChildren()
})

describe("syncComposerPad", () => {
  // The last turn has to be able to scroll out from under the box. A missing
  // stage is a unit-test render, not a hole in the real shell.
  it("writes the box height onto the conversation stage", () => {
    const { stage, child } = stageWithChild()
    syncComposerPad(child, 144)
    expect(stage.style.getPropertyValue(COMPOSER_PAD_VAR)).toBe("144px")
  })

  it("rounds a fractional height so the pad is a CSS pixel", () => {
    const { stage, child } = stageWithChild()
    syncComposerPad(child, 144.6)
    expect(stage.style.getPropertyValue(COMPOSER_PAD_VAR)).toBe("145px")
  })

  it("ignores a zero height so a jsdom layout cannot wipe the CSS fallback", () => {
    const { stage, child } = stageWithChild()
    stage.style.setProperty(COMPOSER_PAD_VAR, "7.5rem")
    syncComposerPad(child, 0)
    expect(stage.style.getPropertyValue(COMPOSER_PAD_VAR)).toBe("7.5rem")
  })

  it("is a no-op when the composer is not on a stage", () => {
    const orphan = document.createElement("div")
    expect(() => syncComposerPad(orphan, 80)).not.toThrow()
    expect(() => syncComposerPad(null, 80)).not.toThrow()
  })
})

describe("clearComposerPad", () => {
  it("removes the inline pad when the composer unmounts", () => {
    const { stage, child } = stageWithChild()
    syncComposerPad(child, 144)
    clearComposerPad(child)
    expect(stage.style.getPropertyValue(COMPOSER_PAD_VAR)).toBe("")
  })
})
