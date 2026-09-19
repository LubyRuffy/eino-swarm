import { describe, expect, it } from "vitest"

import { finish, generateIdentity, initiate, respond } from "./crypto"

describe("crypto session", () => {
  it("lets initiator and responder open each other's seals", () => {
    const a = generateIdentity()
    const b = generateIdentity()
    const { hs, msg } = initiate(a, b.pub)
    const { sess: bSess, msg: reply } = respond(b, a.pub, msg)
    const aSess = finish(hs, reply)
    const sealed = aSess.seal(new TextEncoder().encode("ping"))
    expect(new TextDecoder().decode(bSess.open(sealed))).toBe("ping")
    const back = bSess.seal(new TextEncoder().encode("pong"))
    expect(new TextDecoder().decode(aSess.open(back))).toBe("pong")
  })

  it("does not let a third identity open the ciphertext", () => {
    const a = generateIdentity()
    const b = generateIdentity()
    const c = generateIdentity()
    const { hs, msg } = initiate(a, b.pub)
    const { sess: bSess, msg: reply } = respond(b, a.pub, msg)
    const aSess = finish(hs, reply)
    const sealed = aSess.seal(new TextEncoder().encode("secret"))
    const { hs: hs2, msg: msg2 } = initiate(c, b.pub)
    const { msg: reply2 } = respond(b, c.pub, msg2)
    const cSess = finish(hs2, reply2)
    expect(() => cSess.open(sealed)).toThrow()
    expect(() => bSess.open(sealed)).not.toThrow()
  })
})
