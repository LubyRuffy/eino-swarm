import { ApiError, api } from "@/lib/api"

/** Interrupt-inject and per-bubble retract. Stop stays on `interrupt`. */

export type SteerInjectSlice = {
  preempt: () => Promise<void>
  retractSteer: (seq: number) => Promise<boolean>
  reviseSteer: (seq: number, text: string) => Promise<void>
}

type Host = { activeId?: string }

type SetHost = (partial: { error?: string }) => void

export function steerInjectActions(
  set: SetHost,
  get: () => Host,
  fail: (e: unknown) => string,
): SteerInjectSlice {
  const ignore = (e: unknown, codes: string[]) =>
    e instanceof ApiError &&
    (e.status === 404 || codes.includes(e.code ?? ""))

  return {
    preempt: async () => {
      const id = get().activeId
      if (!id) return
      try {
        await api.preempt(id)
      } catch (e) {
        // Idle / already-consumed is the race with the next model round.
        if (!ignore(e, ["idle", "no_steer"])) set({ error: fail(e) })
      }
    },
    retractSteer: async (seq) => {
      const id = get().activeId
      if (!id || !(seq > 0)) return false
      try {
        await api.retractSteer(id, seq)
        return true
      } catch (e) {
        // False means the bubble is still the model's to read, or it is
        // already gone. The composer must not also take that caption.
        if (!ignore(e, ["idle"])) set({ error: fail(e) })
        return false
      }
    },
    reviseSteer: async (seq, text) => {
      const id = get().activeId
      if (!id || !(seq > 0)) return
      try {
        await api.reviseSteer(id, seq, text)
      } catch (e) {
        // Idle is the race with the turn ending. A 404 is not that race:
        // the manager already read the bubble, and the edit did not land.
        if (e instanceof ApiError && e.code === "idle") return
        set({ error: fail(e) })
      }
    },
  }
}
