import { describe, expect, it } from "vitest"
import { acceptAvatarURL } from "./avatar-url"

describe("acceptAvatarURL", () => {
  it("keeps a public address and drops one that must not be requested", () => {
    const picture = "https://images.fofa.info/a.png?sig=a+b&v=1"
    expect(acceptAvatarURL(`  ${picture}  `)).toBe(picture)
    expect(acceptAvatarURL("http://images.fofa.info/thumb.jpg")).toBe("http://images.fofa.info/thumb.jpg")
    expect(acceptAvatarURL("https://fofa.info/a.png")).toBe("https://fofa.info/a.png")
    expect(acceptAvatarURL("https://CDN.Images.Fofa.Info/a.png")).toBe("https://CDN.Images.Fofa.Info/a.png")
    expect(acceptAvatarURL("https://i.nosec.org/a.png")).toBe("https://i.nosec.org/a.png")
    expect(acceptAvatarURL("https://nosec.org/a.png")).toBe("https://nosec.org/a.png")
    expect(acceptAvatarURL("https://www.baimaohui.net/a.png")).toBe("https://www.baimaohui.net/a.png")
    for (const raw of ["javascript:alert(1)", "https://user:secret@images.fofa.info/a.png", "https://images.fofa.info/a.png#x", "https://images.fofa.info/a b.png", "/a.png", "https://images.example.test/a.png", "https://notfofa.info/a.png", "https://fofa.info.evil.com/a.png", "https://notnosec.org/a.png", "https://nosec.org.evil.com/a.png", "https://notbaimaohui.net/a.png", "https://baimaohui.net.evil.com/a.png"]) {
      expect(acceptAvatarURL(raw)).toBeUndefined()
    }
  })
})
