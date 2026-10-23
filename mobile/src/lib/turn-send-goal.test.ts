import { expect, it, vi } from "vitest"
import { sendComposed } from "./turn-send"
import { OpSend, OpStart, OpSteer, PROTOCOL_V } from "./rpc"

// The PC owns command dispatch. The phone must not trim the suffix off before
// its Start/Send/Steer reaches the same engine boundary as the desktop.
for (const op of [OpStart, OpSend, OpSteer]) {
  it(`preserves the complete goal command for the PC ${op} boundary`, async () => {
    const rpc = vi.fn(async () => ({ v: PROTOCOL_V, id: "receipt", ok: true }))
    const text = "检查 assigned operation /goal"
    await sendComposed(rpc, { op, text, thread_id: op === OpStart ? undefined : "existing-completed-thread" })
    expect(rpc).toHaveBeenCalledTimes(1)
    expect(rpc).toHaveBeenCalledWith(expect.objectContaining({ op, text }))
  })
}
