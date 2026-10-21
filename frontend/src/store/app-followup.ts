import { ApiError, api } from "@/lib/api"
import { resolveLocale, t, type LocalePref } from "@/lib/i18n"
import type { Followup } from "@/lib/types"
import { bumpFollowups, isFollowupGeneration } from "./followup-sync"

type FollowupHost = {
  activeId?: string
  locale: LocalePref
  followups: Followup[]
  error?: string
}

type FollowupPatch = Partial<Pick<FollowupHost, "followups" | "error">>
type SetFollowups = (patch: FollowupPatch | ((s: FollowupHost) => FollowupPatch)) => void

export function followupActions(
  set: SetFollowups,
  get: () => FollowupHost & { deleteFollowup: (id: string) => Promise<boolean> },
  fail: (error: unknown) => string,
) {
  const injecting = new Set<string>()
  const unavailable = (error: unknown) =>
    error instanceof ApiError && error.status === 404 &&
    (!error.code || error.code === "followup_unavailable")

  const refresh = async (id: string) => {
    const token = bumpFollowups()
    try {
      const list = await api.followups(id)
      if (!isFollowupGeneration(token) || get().activeId !== id) return false
      set({ followups: list })
      return true
    } catch (error) {
      if (!isFollowupGeneration(token) || get().activeId !== id) return false
      throw error
    }
  }

  return {
    refreshFollowups: async () => {
      const id = get().activeId
      if (!id) {
        set({ followups: [] })
        return
      }
      try {
        await refresh(id)
      } catch (error) {
        if (get().activeId === id) set({ error: fail(error) })
      }
    },

    steerFollowup: async (fid: string) => {
      const id = get().activeId
      if (!id) return
      const key = JSON.stringify([id, fid])
      if (injecting.has(key)) return
      injecting.add(key)
      const previousError = get().error
      try {
        await api.steerFollowup(id, fid)
        if (get().activeId !== id) return
        bumpFollowups()
        set((s) => ({
          followups: s.followups.filter((f) => f.id !== fid),
          error: s.error === previousError ? undefined : s.error,
        }))
      } catch (error) {
        if (get().activeId !== id) return
        if (!unavailable(error)) {
          if (!(error instanceof ApiError && error.code === "idle")) {
            set({ error: fail(error) })
          }
          return
        }
        // A missing queue identity must never be retried using its old text:
        // another client or the finishing turn may already have delivered it.
        bumpFollowups()
        set((s) => ({ followups: s.followups.filter((f) => f.id !== fid) }))
        let refreshed: boolean
        try {
          refreshed = await refresh(id)
        } catch {
          if (get().activeId === id) {
            set({ error: t(resolveLocale(get().locale), "queue.unavailableRefreshFailed") })
          }
          return
        }
        if (refreshed && get().activeId === id) {
          set({ error: t(resolveLocale(get().locale), "queue.unavailableRefreshed") })
        }
      } finally {
        injecting.delete(key)
      }
    },

    deleteFollowup: async (fid: string) => {
      const id = get().activeId
      if (!id) return false
      try {
        await api.deleteFollowup(id, fid)
        if (get().activeId !== id) return false
        bumpFollowups()
        set((s) => ({ followups: s.followups.filter((f) => f.id !== fid) }))
        return true
      } catch (error) {
        if (get().activeId !== id) return false
        void refresh(id).catch(() => undefined)
        // Only an absent row permits handing its text back to the composer.
        if (error instanceof ApiError && error.status === 404 && error.code !== "thread_not_found") return true
        set({ error: fail(error) })
        return false
      }
    },

    requeueFollowup: async (fid: string, text: string) => {
      const id = get().activeId
      if (!id) return
      try {
        const item = await api.requeueFollowup(id, fid, text)
        if (get().activeId !== id) return
        bumpFollowups()
        set((s) => ({ followups: [...s.followups.filter((f) => f.id !== fid), item] }))
      } catch (error) {
        if (get().activeId !== id) return
        void refresh(id).catch(() => undefined)
        set({ error: fail(error) })
      }
    },

    clearFollowups: async () => {
      const items = get().followups
      await Promise.all(items.map((f) => get().deleteFollowup(f.id)))
    },
  }
}
