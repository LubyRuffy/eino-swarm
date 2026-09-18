import { ApiError, api } from "@/lib/api"

/** Interrupt-inject and per-bubble retract. Stop stays on `interrupt`. */

export type SteerInjectSlice = {
  preempt: () => Promise<void>
  retractSteer: (seq: number) => Promise<void>
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
      if (!id || !(seq > 0)) return
      try {
        await api.retractSteer(id, seq)
      } catch (e) {
        if (!ignore(e, ["idle"])) set({ error: fail(e) })
      }
    },
  }
}
