import { api } from "@/lib/api"
import type { Thread, ThreadStatus } from "@/lib/types"

/** /plan and ask_user actions. Kept out of app.ts so that file stays under
 *  1000 lines — the store object would otherwise grow with every slash. */

export type PlanAskSlice = {
  setPlan: (text: string) => Promise<void>
  savePlan: (text: string) => Promise<void>
  leavePlan: () => Promise<void>
  implementPlan: () => Promise<void>
  answerAsk: (
    callId: string,
    answers: Record<string, { answers: string[] }>,
  ) => Promise<void>
}

type PlanHost = {
  activeId?: string
  status: ThreadStatus
  threads: Thread[]
  newThread: (projectId?: string) => Promise<string | undefined>
  send: (text: string) => Promise<void>
}

type SetPlan = (
  partial:
    | Partial<{ threads: Thread[]; error?: string; status: ThreadStatus }>
    | ((s: PlanHost) => Partial<{ threads: Thread[]; error?: string; status: ThreadStatus }>),
) => void

export function planAskActions(
  set: SetPlan,
  get: () => PlanHost,
  deps: {
    currentThread: () => Promise<string | undefined>
    fail: (e: unknown) => string
    withRunningClock: (
      status: ThreadStatus,
      extra?: Partial<ThreadStatus>,
    ) => ThreadStatus
  },
): PlanAskSlice {
  const patchThreads = (id: string, updated: Thread) =>
    set((s) => ({
      threads: s.threads.map((t) => (t.id === id ? { ...t, ...updated } : t)),
      error: undefined,
    }))

  return {
    setPlan: async (text) => {
      let id = await deps.currentThread()
      if (!id) {
        id = await get().newThread()
        if (!id) return
      }
      const running = get().status.running
      try {
        const updated = await api.patchThread(id, { plan_mode: true })
        patchThreads(id, updated)
        const arg = text.trim()
        if (arg && !running) await get().send(arg)
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    savePlan: async (text) => {
      const id = get().activeId
      if (!id) return
      try {
        const updated = await api.patchThread(id, { plan_markdown: text })
        patchThreads(id, updated)
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    leavePlan: async () => {
      const id = get().activeId
      if (!id) return
      try {
        const updated = await api.patchThread(id, { plan_mode: false })
        patchThreads(id, updated)
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    implementPlan: async () => {
      const id = get().activeId
      if (!id) return
      try {
        const got = await api.implementPlan(id)
        set((s) => ({
          threads: s.threads.map((t) =>
            t.id === id ? { ...t, plan_mode: false } : t,
          ),
          status: deps.withRunningClock(s.status, {
            turn_id: got.turn?.id,
            started_at: got.turn?.started_at,
          }),
          error: undefined,
        }))
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },

    answerAsk: async (callId, answers) => {
      const id = get().activeId
      if (!id) return
      try {
        await api.answerTurn(id, { call_id: callId, answers })
        set((s) => ({
          error: undefined,
          status: { ...s.status, awaiting_answer: false },
        }))
      } catch (e) {
        set({ error: deps.fail(e) })
      }
    },
  }
}
