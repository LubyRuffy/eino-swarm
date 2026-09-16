import { describe, expect, it } from "vitest"

import {
  THOUGHT_VISIBLE_LINES,
  thoughtExpanded,
  thoughtFollowsStream,
  thoughtHasHiddenPrefix,
} from "./thought-scroll"

describe("thought scroll", () => {
  it("caps a live thought at ten lines", () => {
    expect(THOUGHT_VISIBLE_LINES).toBe(10)
  })

  // Auto-follow lands at the bottom, so the first lines have left the
  // viewport — without a fade the box looks like it starts mid-sentence.
  it("fades the top once earlier lines have scrolled away", () => {
    expect(thoughtHasHiddenPrefix(0)).toBe(false)
    expect(thoughtHasHiddenPrefix(1)).toBe(false)
    expect(thoughtHasHiddenPrefix(8)).toBe(true)
  })

  it("follows the stream only while the reader is at the bottom", () => {
    expect(thoughtFollowsStream(400, 160, 240)).toBe(true)
    expect(thoughtFollowsStream(400, 0, 240)).toBe(false)
  })

  // The chevron has to work while tokens are still arriving. Defaulting
  // open is fine; OR-ing streaming on top of the click is what made the
  // button a no-op.
  it("lets the reader hide a live thought", () => {
    expect(thoughtExpanded(true, null)).toBe(true)
    expect(thoughtExpanded(true, false)).toBe(false)
    expect(thoughtExpanded(true, true)).toBe(true)
  })

  it("stays closed after it finishes unless the reader opens it", () => {
    expect(thoughtExpanded(false, null)).toBe(false)
    expect(thoughtExpanded(false, true)).toBe(true)
    expect(thoughtExpanded(false, false)).toBe(false)
  })
})
