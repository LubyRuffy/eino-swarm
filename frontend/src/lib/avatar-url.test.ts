import { describe, expect, it } from "vitest"
import type { AccountModels, RemoteLoginStatus } from "./types"
import { acceptAvatarURL, accountAvatarURL } from "./avatar-url"

const picture = "https://images.example.test/a.png?sig=a+b&v=1"

describe("acceptAvatarURL", () => {
  it("keeps a public http(s) address and its query", () => {
    expect(acceptAvatarURL(`  ${picture}  `)).toBe(picture)
    expect(acceptAvatarURL("http://images.example.test/thumb.jpg")).toBe("http://images.example.test/thumb.jpg")
  })

  it("drops addresses that are not safe to request", () => {
    const long = "https://images.example.test/" + "a".repeat(2049)
    for (const raw of [
      "",
      "   ",
      "javascript:alert(1)",
      "HTTPS://images.example.test/a.png",
      "https://user:secret@images.example.test/a.png",
      "https://images.example.test/a.png#fragment",
      "https://images.example.test/a b.png",
      "https://images.example.test/a\nb.png",
      "https://images.example.test/a\u0000b.png",
      "https://images.example.test/a\\b.png",
      "https://images.example.test/a\u00a0b.png",
      "/a.png",
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
    expect(accountAvatarURL(login(picture), models("https://images.example.test/other.png"))).toBe(picture)
    expect(accountAvatarURL(login(), models(picture))).toBe(picture)
    expect(accountAvatarURL(login("javascript:alert(1)"), models(picture))).toBe(picture)
  })

  it("does not show a picture for a signed-out account", () => {
    expect(accountAvatarURL({ state: "signed_out", avatar_url: picture }, { state: "signed_out", models: 0, avatar_url: picture })).toBeUndefined()
  })
})
