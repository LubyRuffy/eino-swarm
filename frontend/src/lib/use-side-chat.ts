import { useCallback, useLayoutEffect, useRef } from "react"

import { useVisibleDest } from "@/components/app/dest-rail"
import { useApp } from "@/store/app"
import { useSideChat } from "@/store/app-side-chat"

export function useSideChatScope(activeId: string | undefined, clientOpen: boolean) {
  const pane = useVisibleDest()
  const owner = !clientOpen && pane !== "clients" && pane !== "scheduled" ? activeId : undefined
  const shown = useSideChat((s) => Boolean(owner && s.session?.threadId === owner))
  const button = useRef<HTMLButtonElement>(null)

  useLayoutEffect(() => () => {
    const side = useSideChat.getState()
    if (owner && side.session?.threadId === owner) side.close()
  }, [owner])

  const open = useCallback((options?: { question?: string; quote?: string }) => {
    if (!owner || useApp.getState().activeId !== owner) return false
    return useSideChat.getState().open(owner, options)
  }, [owner])

  const close = useCallback(() => {
    useSideChat.getState().close()
    button.current?.focus()
  }, [])

  const toggle = owner ? { open: shown, buttonRef: button, toggle: () => shown ? close() : open() } : undefined
  const fromQuote = owner ? (quote: string) => open({ quote }) : undefined
  const fromComposer = (question: string) => open(question ? { question } : undefined)
  return { owner, shown, toggle, fromQuote, fromComposer, close }
}
