import { create } from "zustand"

export type ToastKind = "error"

export type AppToast = {
  id: string
  kind: ToastKind
  title?: string
  message: string
}

/** Settings action failures must not vanish under the fold. Same id
 *  replaces in place so retrying the same action does not stack a new
 *  card per click. */
const MAX_TOASTS = 4

let seq = 0

interface ToastState {
  toasts: AppToast[]
  push: (toast: Omit<AppToast, "id"> & { id?: string }) => string
  dismiss: (id: string) => void
  clear: () => void
}

export const useToasts = create<ToastState>((set) => ({
  toasts: [],
  push: (toast) => {
    const id = toast.id?.trim() || `toast-${++seq}`
    const next: AppToast = {
      id,
      kind: toast.kind,
      title: toast.title,
      message: toast.message,
    }
    set((s) => {
      const idx = s.toasts.findIndex((item) => item.id === id)
      if (idx >= 0) {
        const toasts = s.toasts.slice()
        toasts[idx] = next
        return { toasts }
      }
      return { toasts: [...s.toasts, next].slice(-MAX_TOASTS) }
    })
    return id
  },
  dismiss: (id) =>
    set((s) => ({ toasts: s.toasts.filter((item) => item.id !== id) })),
  clear: () => set({ toasts: [] }),
}))

export function resetToasts() {
  seq = 0
  useToasts.setState({ toasts: [] })
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export function toastError(
  message: string,
  opts?: { id?: string; title?: string },
): string {
  return useToasts.getState().push({
    id: opts?.id,
    kind: "error",
    title: opts?.title,
    message,
  })
}
