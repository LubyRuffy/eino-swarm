import { b64urlToBytes, bytesToB64url, bytesToHex, equalBytes, hexToBytes } from "./bytes"
import { fingerprint, finish, initiate, type Identity, type Session } from "./crypto"
import { t } from "./i18n"
import {
  httpToWS,
  marshalFrame,
  originURL,
  TYPE_DATA,
  TYPE_HANDSHAKE,
  TYPE_PUNCH_PING,
  unmarshalFrame,
} from "./frame"
import { parseOffer, type Offer } from "./offer"
import { deviceLabel } from "./device"
import {
  decodeResponse,
  encodeRequest,
  nextRPCId,
  OpHello,
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
/** Same cadence as the Go host: hub quiet-WS is 60s, so this must be shorter. */
export const KEEP_ALIVE_MS = 15_000
export const RPC_TIMEOUT_MS = 15_000
export const WS_OPEN_TIMEOUT_MS = 10_000
export const HANDSHAKE_TIMEOUT_MS = 5_000

const DROP_MESSAGES = new Set([
  "offline",
  "ws error",
  "ws timeout",
  "rpc timeout",
  "handshake timeout",
])

export type RedeemResult = {
  ticket: string
  hostPub: Uint8Array
  sessionID: Uint8Array
}

export function hubError(status: number, text: string): Error {
  const trimmed = text.trim()
  if (trimmed.startsWith("{")) {
    try {
      const body = JSON.parse(trimmed) as { error?: string }
      if (body.error?.trim()) return new Error(body.error.trim())
    } catch {
      // raw text is still the error
    }
  }
  return new Error(trimmed || String(status))
}

export function bindError(err: unknown): string {
  return linkError(err)
}

export function linkError(err: unknown): string {
  const raw = err instanceof Error ? err.message : String(err)
  if (raw === "host offline") return t("scan.hostOffline")
  if (DROP_MESSAGES.has(raw)) return t("err.reconnect")
  return raw
}

export type PendingWait = {
  resolve: (r: RemoteResponse) => void
  reject: (e: Error) => void
  timer: number
}

export type DeviceLinkOpts = {
  keepAliveMs?: number
  rpcTimeoutMs?: number
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
  if (!res.ok) throw hubError(res.status, text)
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
  private pending = new Map<string, PendingWait>()
  private keepTimer = 0
  private closing = false
  private dropped = false
  private readonly keepAliveMs: number
  private readonly rpcTimeoutMs: number
  path = "relay"
  sessionIDHex = ""
  onPush?: (resp: RemoteResponse) => void
  onDisconnect?: (err: Error) => void

  constructor(
    readonly identity: Identity,
    readonly hostPub: Uint8Array,
    readonly sessionID: Uint8Array,
    opts: DeviceLinkOpts = {},
  ) {
    this.sessionIDHex = bytesToHex(sessionID)
    this.keepAliveMs = opts.keepAliveMs ?? KEEP_ALIVE_MS
    this.rpcTimeoutMs = opts.rpcTimeoutMs ?? RPC_TIMEOUT_MS
  }

  alive(): boolean {
    return Boolean(this.sess && this.ws && this.ws.readyState === 1)
  }

  async connect(
    hubURL: string,
    ticket: string,
    open: (url: string, protocol: string) => WebSocket = (u, p) => new WebSocket(u, [p]),
  ): Promise<void> {
    this.closing = false
    this.dropped = false
    const url = httpToWS(hubURL)
    const proto = TICKET_PROTO + ticket
    const ws = open(url, proto)
    ws.binaryType = "arraybuffer"
    this.ws = ws
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => reject(new Error("ws timeout")), WS_OPEN_TIMEOUT_MS)
      const fail = (err: Error) => {
        window.clearTimeout(timer)
        reject(err)
      }
      ws.onopen = () => {
        window.clearTimeout(timer)
        resolve()
      }
      ws.onerror = () => fail(new Error("ws error"))
      ws.onclose = () => fail(new Error("offline"))
    })
    await this.handshake()
    ws.onmessage = (ev) => this.onMessage(ev)
    ws.onerror = () => this.drop(new Error("ws error"))
    ws.onclose = () => this.drop(new Error("offline"))
    this.startKeepAlive()
  }

  /** Tell the PC the model line. An old host answers unknown_op; ignore it. */
  async announceDevice(label = deviceLabel()): Promise<RemoteResponse | undefined> {
    if (!this.alive()) return
    try {
      return await this.rpc({ op: OpHello, text: label })
    } catch {
      return
    }
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
      const t = window.setTimeout(() => reject(new Error("handshake timeout")), HANDSHAKE_TIMEOUT_MS)
      const fail = (err: Error) => {
        window.clearTimeout(t)
        reject(err)
      }
      this.ws!.onclose = () => fail(new Error("offline"))
      this.ws!.onerror = () => fail(new Error("ws error"))
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
    deliverResponse(this.pending, resp, this.onPush)
  }

  async rpc(
    partial: Omit<RemoteRequest, "v" | "id"> & { id?: string },
  ): Promise<RemoteResponse> {
    if (!this.alive() || !this.sess || !this.ws) throw new Error("offline")
    const req: RemoteRequest = {
      v: PROTOCOL_V,
      id: partial.id ?? nextRPCId(),
      ...partial,
    }
    const p = new Promise<RemoteResponse>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pending.delete(req.id)
        reject(new Error("rpc timeout"))
        this.drop(new Error("rpc timeout"))
      }, this.rpcTimeoutMs)
      this.pending.set(req.id, { resolve, reject, timer })
    })
    try {
      this.ws.send(
        marshalFrame({
          type: TYPE_DATA,
          dst: this.hostPub,
          src: this.identity.pub,
          sessionID: this.sessionID,
          payload: this.sess.seal(encodeRequest(req)),
        }),
      )
    } catch {
      this.clearWait(req.id)
      this.drop(new Error("offline"))
      throw new Error("offline")
    }
    return p
  }

  close() {
    this.closing = true
    this.stopKeepAlive()
    const ws = this.ws
    this.ws = null
    this.sess = null
    this.failPending(new Error("offline"))
    try {
      ws?.close()
    } catch {
      // already gone
    }
  }

  private startKeepAlive() {
    this.stopKeepAlive()
    if (this.keepAliveMs <= 0) return
    this.keepTimer = window.setInterval(() => this.ping(), this.keepAliveMs)
  }

  private stopKeepAlive() {
    if (this.keepTimer) {
      window.clearInterval(this.keepTimer)
      this.keepTimer = 0
    }
  }

  private ping() {
    if (!this.alive() || !this.ws) return
    try {
      // Data frame, not a WebSocket ping: the hub only resets its idle
      // deadline on a binary message. Control pings never reach ReadMessage.
      this.ws.send(
        marshalFrame({
          type: TYPE_PUNCH_PING,
          dst: this.identity.pub,
          src: this.identity.pub,
          sessionID: this.sessionID,
          payload: new Uint8Array(0),
        }),
      )
    } catch {
      this.drop(new Error("offline"))
    }
  }

  private drop(err: Error) {
    if (this.closing || this.dropped) return
    this.dropped = true
    this.stopKeepAlive()
    const ws = this.ws
    this.ws = null
    this.sess = null
    this.failPending(err)
    try {
      ws?.close()
    } catch {
      // already gone
    }
    this.onDisconnect?.(err)
  }

  private clearWait(id: string) {
    const wait = this.pending.get(id)
    if (!wait) return
    window.clearTimeout(wait.timer)
    this.pending.delete(id)
  }

  private failPending(err: Error) {
    const waiting = [...this.pending.values()]
    this.pending.clear()
    for (const wait of waiting) {
      window.clearTimeout(wait.timer)
      wait.reject(err)
    }
  }
}

/** Watch pushes have an empty id. A matching id is an RPC reply. */
export function deliverResponse(
  pending: Map<string, PendingWait>,
  resp: RemoteResponse,
  onPush?: (r: RemoteResponse) => void,
) {
  const wait = resp.id ? pending.get(resp.id) : undefined
  if (wait) {
    pending.delete(resp.id)
    window.clearTimeout(wait.timer)
    wait.resolve(resp)
    return
  }
  onPush?.(resp)
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
