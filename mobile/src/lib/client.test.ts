import { describe, expect, it, vi } from "vitest"

import {
  bindError,
  deliverResponse,
  DeviceLink,
  hubError,
  linkError,
  redeemOffer,
  TICKET_PROTO,
  type PendingWait,
} from "./client"
import type { RemoteResponse } from "./rpc"
import { generateIdentity, respond } from "./crypto"
import { bytesToB64url, bytesToHex } from "./bytes"
import { marshalFrame, TYPE_DATA, TYPE_HANDSHAKE, TYPE_PUNCH_PING, unmarshalFrame } from "./frame"
import { OpHello, OpList, PROTOCOL_V } from "./rpc"

describe("redeemOffer", () => {
  it("posts the device public key and reads ticket fields", async () => {
    const device = generateIdentity()
    const hostPub = generateIdentity().pub
    const sid = new Uint8Array(16).fill(7)
    const fetcher = vi.fn(async () => {
      return new Response(
        JSON.stringify({
          ticket: "abc123",
          host_pub: bytesToB64url(hostPub),
          session_id: bytesToHex(sid),
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )
    })
    const got = await redeemOffer(
      {
        hubURL: "http://127.0.0.1:7780",
        code: "ScanCode01",
        hostPub,
        lan: [],
      },
      device,
      fetcher as unknown as typeof fetch,
    )
    expect(got.ticket).toBe("abc123")
    expect(got.hostPub).toEqual(hostPub)
    expect(fetcher).toHaveBeenCalledOnce()
    expect(JSON.stringify(fetcher.mock.calls)).toContain("pairings/redeem")
    expect(TICKET_PROTO).toBe("pairlink.ticket.")
  })

  it("unwraps a hub JSON error so the scan screen is not a raw blob", async () => {
    const device = generateIdentity()
    const fetcher = vi.fn(async () => {
      return new Response(JSON.stringify({ error: "host offline" }), { status: 409 })
    })
    await expect(
      redeemOffer(
        {
          hubURL: "http://127.0.0.1:7780",
          code: "ScanCode01",
          hostPub: generateIdentity().pub,
          lan: [],
        },
        device,
        fetcher as unknown as typeof fetch,
      ),
    ).rejects.toThrow("host offline")
  })
})

describe("hubError", () => {
  it("prefers JSON error, then raw text, then the status", () => {
    expect(hubError(409, '{"error":"host offline"}').message).toBe("host offline")
    expect(hubError(500, "not-json").message).toBe("not-json")
    expect(hubError(502, "{").message).toBe("{")
    expect(hubError(503, "   ").message).toBe("503")
  })
})

describe("bindError", () => {
  it("maps a host-offline redeem to the scan copy", async () => {
    const { setLocale, t } = await import("./i18n")
    setLocale("en")
    expect(bindError(new Error("host offline"))).toBe(t("scan.hostOffline"))
    expect(bindError("ws timeout")).toBe(t("err.reconnect"))
    expect(linkError(new Error("offline"))).toBe(t("err.reconnect"))
    expect(linkError(new Error("rpc timeout"))).toBe(t("err.reconnect"))
    expect(bindError("still the original")).toBe("still the original")
  })
})

describe("deliverResponse", () => {
  it("routes a matching id to the waiter and unmatched frames to onPush", () => {
    const pending = new Map<string, PendingWait>()
    const waiter = vi.fn()
    const push = vi.fn()
    pending.set("m1", { resolve: waiter, reject: vi.fn(), timer: 0 })
    const rpc: RemoteResponse = { v: 1, id: "m1", ok: true }
    deliverResponse(pending, rpc, push)
    expect(waiter).toHaveBeenCalledWith(rpc)
    expect(push).not.toHaveBeenCalled()
    const event: RemoteResponse = {
      v: 1,
      id: "",
      ok: true,
      op: "event",
      thread_id: "t",
    }
    deliverResponse(pending, event, push)
    expect(push).toHaveBeenCalledWith(event)
  })
})

class FakeSocket {
  binaryType = "arraybuffer"
  readyState = 0
  sent: Uint8Array[] = []
  onopen: ((ev: Event) => void) | null = null
  onclose: ((ev: Event) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null

  send(data: BufferSource) {
    if (this.readyState !== 1) throw new Error("closed")
    const view =
      data instanceof ArrayBuffer
        ? new Uint8Array(data)
        : new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
    this.sent.push(view.slice())
  }

  close() {
    if (this.readyState === 3) return
    this.readyState = 3
    this.onclose?.({ type: "close" } as Event)
  }

  open() {
    this.readyState = 1
    this.onopen?.({ type: "open" } as Event)
  }

  deliver(bytes: Uint8Array) {
    const copy = bytes.slice()
    this.onmessage?.({ data: copy.buffer } as MessageEvent)
  }
}

async function waitUntil(ok: () => boolean) {
  for (let i = 0; i < 40; i++) {
    if (ok()) return
    await Promise.resolve()
  }
  throw new Error("timed out")
}

async function livePair(opts?: { keepAliveMs?: number; rpcTimeoutMs?: number }) {
  const device = generateIdentity()
  const host = generateIdentity()
  const sid = new Uint8Array(16).fill(3)
  const sock = new FakeSocket()
  const link = new DeviceLink(device, host.pub, sid, {
    keepAliveMs: opts?.keepAliveMs ?? 0,
    rpcTimeoutMs: opts?.rpcTimeoutMs,
  })
  const connected = link.connect("http://127.0.0.1:9", "tick", () => sock as unknown as WebSocket)
  sock.open()
  await waitUntil(() => sock.sent.length >= 1)
  const fr = unmarshalFrame(sock.sent[0])
  expect(fr.type).toBe(TYPE_HANDSHAKE)
  const { msg, sess } = respond(host, device.pub, fr.payload)
  sock.deliver(
    marshalFrame({
      type: TYPE_HANDSHAKE,
      dst: device.pub,
      src: host.pub,
      sessionID: sid,
      payload: msg,
    }),
  )
  await connected
  return { link, sock, device, host, hostSess: sess, sid }
}

describe("DeviceLink drop", () => {
  it("keepalives with a punch-ping data frame", async () => {
    const { sock } = await livePair({ keepAliveMs: 40 })
    const before = sock.sent.length
    await new Promise((r) => setTimeout(r, 55))
    expect(sock.sent.length).toBeGreaterThan(before)
    expect(unmarshalFrame(sock.sent[sock.sent.length - 1]).type).toBe(TYPE_PUNCH_PING)
  })

  it("rejects in-flight rpc and tells the UI when the socket closes", async () => {
    const { link, sock } = await livePair()
    const onDisconnect = vi.fn()
    link.onDisconnect = onDisconnect
    const pending = link.rpc({ op: OpList })
    sock.close()
    await expect(pending).rejects.toThrow("offline")
    expect(onDisconnect).toHaveBeenCalledOnce()
    expect(link.alive()).toBe(false)
    await expect(link.rpc({ op: OpList })).rejects.toThrow("offline")
  })

  it("does not call onDisconnect when the user unlinks", async () => {
    const { link } = await livePair()
    const onDisconnect = vi.fn()
    link.onDisconnect = onDisconnect
    link.close()
    expect(onDisconnect).not.toHaveBeenCalled()
    expect(link.alive()).toBe(false)
  })

  it("announces a model line over hello and ignores a down socket", async () => {
    const { link, sock, host, hostSess, sid } = await livePair()
    const pending = link.announceDevice("Phone 1.0 Device")
    await waitUntil(() => sock.sent.some((b) => unmarshalFrame(b).type === TYPE_DATA))
    const fr = unmarshalFrame(sock.sent[sock.sent.length - 1])
    const req = JSON.parse(new TextDecoder().decode(hostSess.open(fr.payload))) as {
      op: string
      text: string
      id: string
    }
    expect(req.op).toBe(OpHello)
    expect(req.text).toBe("Phone 1.0 Device")
    sock.deliver(
      marshalFrame({
        type: TYPE_DATA,
        dst: fr.src,
        src: host.pub,
        sessionID: sid,
        payload: hostSess.seal(
          new TextEncoder().encode(JSON.stringify({ v: PROTOCOL_V, id: req.id, ok: true })),
        ),
      }),
    )
    const resp = await pending
    expect(resp?.ok).toBe(true)
    link.close()
    await expect(link.announceDevice("Phone 1.0 Device")).resolves.toBeUndefined()
  })

  it("treats a silent rpc timeout as a dropped socket", async () => {
    const { link } = await livePair({ rpcTimeoutMs: 40 })
    const onDisconnect = vi.fn()
    link.onDisconnect = onDisconnect
    await expect(link.rpc({ op: OpList })).rejects.toThrow("rpc timeout")
    expect(onDisconnect).toHaveBeenCalledOnce()
    expect(link.alive()).toBe(false)
  })
})
