import { create } from "zustand"

/** A diagnostic may arrive for an earlier turn after the next one has begun. */
export const useTraceFocus = create<{
  turnId?: string
  request: number
  open: (turnId: string) => void
  clear: () => void
}>((set) => ({
  request: 0,
  open: (turnId) => set((s) => ({ turnId, request: s.request + 1 })),
  clear: () => set({ turnId: undefined }),
}))
