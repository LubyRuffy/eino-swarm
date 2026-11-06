import { expect, it, vi } from "vitest"
import { continuePhoneTurn } from "./phone-turn"
import type { RemoteLink } from "./link"

it.each([true, false])("sends an explicit %s decision to the original phone thread", async (proceed) => {
  const rpc = vi.fn().mockResolvedValue({ ok: true })
  await continuePhoneTurn({ alive: () => true, rpc } as unknown as RemoteLink, "thread", proceed)
  expect(rpc).toHaveBeenCalledTimes(1)
  expect(rpc).toHaveBeenCalledWith({ op: "continue", thread_id: "thread", continue: proceed })
})

it("surfaces an offline, unsupported or refused decision to the card", async () => {
  await expect(continuePhoneTurn(null, "thread", true)).rejects.toThrow()
  const rpc = vi.fn().mockResolvedValueOnce({ ok: false, code: "unknown_op" }).mockResolvedValueOnce({ ok: false, error: "No longer paused" })
  const link = { alive: () => true, rpc } as unknown as RemoteLink
  await expect(continuePhoneTurn(link, "thread", true)).rejects.toThrow()
  await expect(continuePhoneTurn(link, "thread", false)).rejects.toThrow("No longer paused")
  rpc.mockRejectedValueOnce(new Error("Lost transport"))
  await expect(continuePhoneTurn(link, "thread", true)).rejects.toThrow()
})
