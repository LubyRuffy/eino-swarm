import { describe, expect, it } from "vitest"
import { acceptAvatarURL } from "./avatar-url"

describe("acceptAvatarURL", () => {
  it("keeps a public address and drops one that must not be requested", () => {
    const picture = "https://images.example.test/a.png?sig=a+b&v=1"
    expect(acceptAvatarURL(`  ${picture}  `)).toBe(picture)
    expect(acceptAvatarURL("http://images.example.test/thumb.jpg")).toBe("http://images.example.test/thumb.jpg")
    for (const raw of ["javascript:alert(1)", "https://user:secret@images.example.test/a.png", "https://images.example.test/a.png#x", "https://images.example.test/a b.png", "/a.png"]) {
      expect(acceptAvatarURL(raw)).toBeUndefined()
    }
  })
})
