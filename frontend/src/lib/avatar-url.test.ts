import { describe, expect, it } from "vitest"
import type { AccountModels, RemoteLoginStatus } from "./types"
import { acceptAvatarURL, accountAvatarURL } from "./avatar-url"

const picture = "https://images.fofa.info/a.png?sig=a+b&v=1"

describe("acceptAvatarURL", () => {
  it("keeps a public http(s) address and its query", () => {
    expect(acceptAvatarURL(`  ${picture}  `)).toBe(picture)
    expect(acceptAvatarURL("http://images.fofa.info/thumb.jpg")).toBe("http://images.fofa.info/thumb.jpg")
    expect(acceptAvatarURL("https://fofa.info/a.png")).toBe("https://fofa.info/a.png")
    expect(acceptAvatarURL("https://CDN.Images.Fofa.Info/a.png")).toBe("https://CDN.Images.Fofa.Info/a.png")
  })

  it("drops addresses that are not safe to request", () => {
    const long = "https://images.fofa.info/" + "a".repeat(2049)
    for (const raw of [
      "",
      "   ",
      "javascript:alert(1)",
      "HTTPS://images.fofa.info/a.png",
      "https://user:secret@images.fofa.info/a.png",
      "https://images.fofa.info/a.png#fragment",
      "https://images.fofa.info/a b.png",
      "https://images.fofa.info/a\nb.png",
      "https://images.fofa.info/a\u0000b.png",
      "https://images.fofa.info/a\\b.png",
      "https://images.fofa.info/a\u00a0b.png",
      "/a.png",
      "https://images.example.test/a.png",
      "https://notfofa.info/a.png",
      "https://fofa.info.evil.com/a.png",
      "https://i.nosec.org/a.png",
      "https://127.0.0.1/a.png",
      long,
      12,
      null,
    ]) {
      expect(acceptAvatarURL(raw)).toBeUndefined()
    }
  })
})

describe("accountAvatarURL", () => {
  const login = (avatar_url?: string): RemoteLoginStatus => ({ state: "authorized", display_name: "Ada", avatar_url })
  const models = (avatar_url?: string): AccountModels => ({ state: "ready", models: 1, avatar_url })

  it("prefers the login picture and otherwise uses the account catalog picture", () => {
    expect(accountAvatarURL(login(picture), models("https://images.fofa.info/other.png"))).toBe(picture)
    expect(accountAvatarURL(login(), models(picture))).toBe(picture)
    expect(accountAvatarURL(login("javascript:alert(1)"), models(picture))).toBe(picture)
  })

  it("does not show a picture for a signed-out account", () => {
    expect(accountAvatarURL({ state: "signed_out", avatar_url: picture }, { state: "signed_out", models: 0, avatar_url: picture })).toBeUndefined()
  })
})
