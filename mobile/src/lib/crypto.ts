import { chacha20poly1305 } from "@noble/ciphers/chacha.js"
import { x25519 } from "@noble/curves/ed25519.js"
import { hkdf } from "@noble/hashes/hkdf.js"
import { sha256 } from "@noble/hashes/sha2.js"
import { concatBytes, getU64BE, putU64BE, randomBytes, utf8 } from "./bytes"

export const KEY_SIZE = 32
const NONCE_SIZE = 12

export type Identity = {
  priv: Uint8Array
  pub: Uint8Array
}

export function generateIdentity(): Identity {
  return fromPrivate(randomBytes(KEY_SIZE))
}

export function fromPrivate(priv: Uint8Array): Identity {
  if (priv.length !== KEY_SIZE) throw new Error("bad private key length")
  const pub = x25519.getPublicKey(priv)
  return { priv: new Uint8Array(priv), pub: new Uint8Array(pub) }
}

export function fingerprint(pub: Uint8Array): string {
  const sum = sha256(pub)
  return Array.from(sum.slice(0, 8), (x) => x.toString(16).padStart(2, "0")).join("")
}

function dh(priv: Uint8Array, pub: Uint8Array): Uint8Array {
  return new Uint8Array(x25519.getSharedSecret(priv, pub))
}

export type Handshake = {
  self: Identity
  peerPub: Uint8Array
  ephPriv: Uint8Array
  ephPub: Uint8Array
}

export function initiate(self: Identity, peerPub: Uint8Array): {
  hs: Handshake
  msg: Uint8Array
} {
  if (peerPub.length !== KEY_SIZE) throw new Error("bad peer key")
  const ephPriv = randomBytes(KEY_SIZE)
  const ephPub = new Uint8Array(x25519.getPublicKey(ephPriv))
  return {
    hs: {
      self,
      peerPub: new Uint8Array(peerPub),
      ephPriv,
      ephPub,
    },
    msg: ephPub,
  }
}

export function respond(
  self: Identity,
  peerPub: Uint8Array,
  theirEph: Uint8Array,
): { sess: Session; msg: Uint8Array } {
  if (peerPub.length !== KEY_SIZE || theirEph.length !== KEY_SIZE) {
    throw new Error("bad handshake")
  }
  const ephPriv = randomBytes(KEY_SIZE)
  const ephPub = new Uint8Array(x25519.getPublicKey(ephPriv))
  const sess = derive(self, peerPub, ephPriv, theirEph, false)
  return { sess, msg: ephPub }
}

export function finish(hs: Handshake, theirEph: Uint8Array): Session {
  if (theirEph.length !== KEY_SIZE) throw new Error("bad handshake")
  return derive(hs.self, hs.peerPub, hs.ephPriv, theirEph, true)
}

function derive(
  self: Identity,
  peerPub: Uint8Array,
  ephPriv: Uint8Array,
  theirEph: Uint8Array,
  initiator: boolean,
): Session {
  const ee = dh(ephPriv, theirEph)
  let es = dh(ephPriv, peerPub)
  let se = dh(self.priv, theirEph)
  const ss = dh(self.priv, peerPub)
  if (!initiator) {
    const tmp = es
    es = se
    se = tmp
  }
  const ikm = concatBytes(ee, es, se, ss)
  const key = hkdf(sha256, ikm, utf8("pairlink"), utf8("pairlink/v1"), 64)
  let send = key.slice(0, 32)
  let recv = key.slice(32)
  if (!initiator) {
    const tmp = send
    send = recv
    recv = tmp
  }
  return new Session(send, recv)
}

export class Session {
  private sendN = 0n
  private recvN = 0n

  constructor(
    private sendKey: Uint8Array,
    private recvKey: Uint8Array,
  ) {}

  seal(plain: Uint8Array): Uint8Array {
    this.sendN += 1n
    const n = this.sendN
    const nonce = new Uint8Array(NONCE_SIZE)
    putU64BE(nonce, 4, n)
    const cipher = chacha20poly1305(this.sendKey, nonce)
    const sealed = cipher.encrypt(plain)
    const out = new Uint8Array(8 + sealed.length)
    putU64BE(out, 0, n)
    out.set(sealed, 8)
    return out
  }

  open(msg: Uint8Array): Uint8Array {
    if (msg.length < 8 + 16) throw new Error("short message")
    const n = getU64BE(msg, 0)
    const nonce = new Uint8Array(NONCE_SIZE)
    putU64BE(nonce, 4, n)
    const cipher = chacha20poly1305(this.recvKey, nonce)
    const plain = cipher.decrypt(msg.slice(8))
    if (n > this.recvN) this.recvN = n
    return new Uint8Array(plain)
  }
}
