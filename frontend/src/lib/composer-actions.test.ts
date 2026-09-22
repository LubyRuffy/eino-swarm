import { describe, expect, it } from "vitest"

import {
  composerCornerActions,
  composerDraftSubmittable,
  type ComposerDraft,
} from "./composer-actions"

function draft(over: Partial<ComposerDraft> = {}): ComposerDraft {
  return {
    text: "",
    attachmentCount: 0,
    quoteCount: 0,
    imageCount: 0,
    slashOpen: false,
    slashReady: false,
    awaitingArgument: false,
    ...over,
  }
}

describe("composerDraftSubmittable", () => {
  it("treats trimmed text as a draft", () => {
    expect(composerDraftSubmittable(draft({ text: "draft" }))).toBe(true)
  })

  it("ignores whitespace", () => {
    expect(composerDraftSubmittable(draft({ text: "  \n" }))).toBe(false)
  })

  it("treats a quote, a file, or an image as a draft with an empty box", () => {
    expect(composerDraftSubmittable(draft({ quoteCount: 1 }))).toBe(true)
    expect(composerDraftSubmittable(draft({ attachmentCount: 1 }))).toBe(true)
    expect(composerDraftSubmittable(draft({ imageCount: 1 }))).toBe(true)
  })

  it("does not treat a slash prompt that still needs an argument as a draft", () => {
    expect(
      composerDraftSubmittable(
        draft({ text: "/goal ", awaitingArgument: true }),
      ),
    ).toBe(false)
  })

  it("treats an open slash menu as a draft", () => {
    expect(
      composerDraftSubmittable(draft({ text: "/", slashOpen: true })),
    ).toBe(true)
  })
})

describe("composerCornerActions", () => {
  it("offers Send while idle", () => {
    expect(
      composerCornerActions({
        running: false,
        submittable: false,
        awaitingArgument: false,
      }),
    ).toEqual({ stop: false, send: true })
  })

  it("offers Stop while a turn is running and the box is empty", () => {
    expect(
      composerCornerActions({
        running: true,
        submittable: false,
        awaitingArgument: false,
      }),
    ).toEqual({ stop: true, send: false })
  })

  it("replaces Stop with Send once the draft can be submitted", () => {
    expect(
      composerCornerActions({
        running: true,
        submittable: true,
        awaitingArgument: false,
      }),
    ).toEqual({ stop: false, send: true })
  })

  it("keeps Stop beside a disabled Send while a slash argument is missing", () => {
    expect(
      composerCornerActions({
        running: true,
        submittable: false,
        awaitingArgument: true,
      }),
    ).toEqual({ stop: true, send: true })
  })
})
