import { describe, expect, it } from "vitest"

import {
  FOLLOW_BOTTOM_PX,
  INITIAL_FOLLOW,
  eventIsFromNestedScroller,
  isFollowBottom,
  reduceFollow,
  shouldShowJump,
  type FollowState,
} from "./follow-scroll"

describe("isFollowBottom", () => {
  it("treats the live edge as the bottom", () => {
    expect(isFollowBottom(400, 160, 240)).toBe(true)
    expect(isFollowBottom(400, 160 - FOLLOW_BOTTOM_PX, 240)).toBe(true)
    expect(isFollowBottom(400, 159 - FOLLOW_BOTTOM_PX, 240)).toBe(false)
  })
})

describe("reduceFollow", () => {
  it("starts pinned with no jump button", () => {
    expect(INITIAL_FOLLOW).toEqual({ pinned: true, unread: false })
    expect(shouldShowJump(INITIAL_FOLLOW)).toBe(false)
  })

  // A wheel-up is the reader leaving, even if they are still inside the
  // slack that used to re-pin them on every token.
  it("unpins on user-up without showing a jump yet", () => {
    const next = reduceFollow(INITIAL_FOLLOW, { type: "user-up" })
    expect(next.pinned).toBe(false)
    expect(shouldShowJump(next)).toBe(false)
  })

  it("shows the jump only after content arrives while unpinned", () => {
    const away = reduceFollow(INITIAL_FOLLOW, { type: "user-up" })
    const next = reduceFollow(away, { type: "content" })
    expect(next).toEqual({ pinned: false, unread: true })
    expect(shouldShowJump(next)).toBe(true)
  })

  it("ignores content while still pinned", () => {
    expect(reduceFollow(INITIAL_FOLLOW, { type: "content" })).toBe(INITIAL_FOLLOW)
  })

  it("jump pins and hides the button", () => {
    const unread: FollowState = { pinned: false, unread: true }
    expect(reduceFollow(unread, { type: "jump" })).toEqual({
      pinned: true,
      unread: false,
    })
    expect(shouldShowJump(reduceFollow(unread, { type: "jump" }))).toBe(false)
  })

  it("scrolling back to the bottom re-pins", () => {
    const unread: FollowState = { pinned: false, unread: true }
    expect(reduceFollow(unread, { type: "user-scroll", atBottom: true })).toEqual({
      pinned: true,
      unread: false,
    })
  })

  it("a scroll that leaves the bottom unpins without a jump yet", () => {
    const next = reduceFollow(INITIAL_FOLLOW, {
      type: "user-scroll",
      atBottom: false,
    })
    expect(next.pinned).toBe(false)
    expect(shouldShowJump(next)).toBe(false)
  })

  it("reset restores the initial pin", () => {
    const unread: FollowState = { pinned: false, unread: true }
    expect(reduceFollow(unread, { type: "reset" })).toEqual(INITIAL_FOLLOW)
  })
})

describe("eventIsFromNestedScroller", () => {
  it("ignores a wheel that started inside an inner overflow box", () => {
    const root = document.createElement("div")
    const nested = document.createElement("div")
    Object.defineProperty(nested, "scrollHeight", { value: 200 })
    Object.defineProperty(nested, "clientHeight", { value: 40 })
    const inner = document.createElement("span")
    nested.appendChild(inner)
    root.appendChild(nested)
    expect(eventIsFromNestedScroller(inner, root)).toBe(true)
    expect(eventIsFromNestedScroller(root, root)).toBe(false)
  })
})
