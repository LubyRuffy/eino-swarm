import { b64urlToBytes, bytesToB64url, bytesToHex, equalBytes, hexToBytes } from "./bytes"
import { fingerprint, finish, initiate, type Identity, type Session } from "./crypto"
import {
  httpToWS,
  marshalFrame,
  originURL,
  TYPE_DATA,
  TYPE_HANDSHAKE,
  unmarshalFrame,
} from "./frame"
import { parseOffer, type Offer } from "./offer"
import {
  decodeResponse,
  encodeRequest,
  nextRPCId,
  PROTOCOL_V,
  type RemoteRequest,
  type RemoteResponse,
} from "./rpc"
import {
  loadOrCreateIdentity,
  saveLink,
  type SavedLink,
} from "./store"

export const TICKET_PROTO = "pairlink.ticket."

export type RedeemResult = {
  ticket: string
  hostPub: Uint8Array
  sessionID: Uint8Array
}

export async function redeemOffer(
  offer: Offer,
  device: Identity,
  fetcher: typeof fetch = fetch,
): Promise<RedeemResult> {
  const res = await fetcher(originURL(offer.hubURL) + "/pairlink/v1/pairings/redeem", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      code: offer.code,
      device_pub: bytesToB64url(device.pub),
    }),
  })
  const text = await res.text()
  if (!res.ok) throw new Error(text || res.statusText)
  const body = JSON.parse(text) as {
    ticket: string
    host_pub: string
    session_id: string
  }
  const hostPub = b64urlToBytes(body.host_pub)
  if (hostPub.length !== 32) throw new Error("bad host_pub")
  return {
    ticket: body.ticket,
    hostPub,
    sessionID: hexToBytes(body.session_id),
  }
}

export class DeviceLink {
  private ws: WebSocket | null = null
  private sess: Session | null = null
  private pending = new Map<string, (r: RemoteResponse) => void>()
  path = "relay"
  sessionIDHex = ""

  constructor(
    readonly identity: Identity,
    readonly hostPub: Uint8Array,
    readonly sessionID: Uint8Array,
  ) {
    this.sessionIDHex = bytesToHex(sessionID)
  }

  async connect(
    hubURL: string,
    ticket: string,
    open: (url: string, protocol: string) => WebSocket = (u, p) => new WebSocket(u, [p]),
  ): Promise<void> {
    const url = httpToWS(hubURL)
    const proto = TICKET_PROTO + ticket
    const ws = open(url, proto)
    ws.binaryType = "arraybuffer"
    this.ws = ws
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => reject(new Error("ws timeout")), 10_000)
      ws.onopen = () => {
        window.clearTimeout(timer)
        resolve()
      }
      ws.onerror = () => {
        window.clearTimeout(timer)
        reject(new Error("ws error"))
      }
    })
    await this.handshake()
    ws.onmessage = (ev) => this.onMessage(ev)
  }

  private handshake(): Promise<void> {
    if (!this.ws) return Promise.reject(new Error("offline"))
    const { hs, msg } = initiate(this.identity, this.hostPub)
    this.ws.send(
      marshalFrame({
        type: TYPE_HANDSHAKE,
        dst: this.hostPub,
        src: this.identity.pub,
        sessionID: this.sessionID,
        payload: msg,
      }),
    )
    return new Promise((resolve, reject) => {
      const t = window.setTimeout(() => reject(new Error("handshake timeout")), 5000)
      this.ws!.onmessage = (ev) => {
        try {
          const fr = unmarshalFrame(asBytes(ev.data))
          if (fr.type === TYPE_HANDSHAKE && equalBytes(fr.src, this.hostPub)) {
            this.sess = finish(hs, fr.payload)
            window.clearTimeout(t)
            resolve()
            return
          }
          this.onFrame(fr)
        } catch (err) {
          window.clearTimeout(t)
          reject(err)
        }
      }
    })
  }

  private onMessage(ev: MessageEvent) {
    try {
      this.onFrame(unmarshalFrame(asBytes(ev.data)))
    } catch {
      // junk frame: keep the session
    }
  }

  private onFrame(fr: ReturnType<typeof unmarshalFrame>) {
    if (fr.type !== TYPE_DATA || !this.sess) return
    let plain: Uint8Array
    try {
      plain = this.sess.open(fr.payload)
    } catch {
      return
    }
    let resp: RemoteResponse
    try {
      resp = decodeResponse(plain)
    } catch {
      return
    }
    const wait = this.pending.get(resp.id)
    if (wait) {
      this.pending.delete(resp.id)
      wait(resp)
    }
  }

  async rpc(
    partial: Omit<RemoteRequest, "v" | "id"> & { id?: string },
  ): Promise<RemoteResponse> {
    if (!this.sess || !this.ws) throw new Error("offline")
    const req: RemoteRequest = {
      v: PROTOCOL_V,
      id: partial.id ?? nextRPCId(),
      ...partial,
    }
    const p = new Promise<RemoteResponse>((resolve, reject) => {
      const t = window.setTimeout(() => {
        this.pending.delete(req.id)
        reject(new Error("rpc timeout"))
      }, 15_000)
      this.pending.set(req.id, (r) => {
        window.clearTimeout(t)
        resolve(r)
      })
    })
    this.ws.send(
      marshalFrame({
        type: TYPE_DATA,
        dst: this.hostPub,
        src: this.identity.pub,
        sessionID: this.sessionID,
        payload: this.sess.seal(encodeRequest(req)),
      }),
    )
    return p
  }

  close() {
    this.ws?.close()
    this.ws = null
    this.sess = null
  }
}

function asBytes(data: unknown): Uint8Array {
  if (data instanceof ArrayBuffer) return new Uint8Array(data)
  if (data instanceof Uint8Array) return data
  throw new Error("not binary")
}

export async function bindFromURI(
  uri: string,
  fetcher: typeof fetch = fetch,
): Promise<{ offer: Offer; saved: SavedLink; identity: Identity; redeemed: RedeemResult }> {
  const offer = parseOffer(uri)
  const identity = loadOrCreateIdentity()
  const redeemed = await redeemOffer(offer, identity, fetcher)
  const saved: SavedLink = {
    hubURL: offer.hubURL,
    ticket: redeemed.ticket,
    hostPub: bytesToB64url(redeemed.hostPub),
    sessionID: bytesToHex(redeemed.sessionID),
    fingerprint: fingerprint(redeemed.hostPub),
  }
  saveLink(saved)
  return { offer, saved, identity, redeemed }
}

export async function openSaved(
  saved: SavedLink,
  identity = loadOrCreateIdentity(),
  open?: (url: string, protocol: string) => WebSocket,
): Promise<DeviceLink> {
  const link = new DeviceLink(
    identity,
    b64urlToBytes(saved.hostPub),
    hexToBytes(saved.sessionID),
  )
  await link.connect(saved.hubURL, saved.ticket, open)
  return link
}
