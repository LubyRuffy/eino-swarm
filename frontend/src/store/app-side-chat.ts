import { create } from "zustand"

import { api, type SideChatExchange } from "@/lib/api"
import { errorMessage } from "@/store/toasts"

export const SIDE_CHAT_QUESTION_BYTES = 4096
const QUOTE_BYTES = 4096
const HISTORY_PAIRS = 4
const encoder = new TextEncoder()

export function sideChatQuestionTooLong(question: string): boolean {
  return encoder.encode(question.trim()).byteLength > SIDE_CHAT_QUESTION_BYTES
}

function clipUTF8(text: string, limit: number): string {
  const bytes = encoder.encode(text)
  if (bytes.length <= limit) return text
  const suffix = "…"
  let end = limit - encoder.encode(suffix).length
  // Leave a whole UTF-8 character before the truncation marker.
  while ((bytes[end] & 0xc0) === 0x80) end--
  return new TextDecoder().decode(bytes.subarray(0, end)) + suffix
}

interface SideChatSession {
  threadId: string
  question: string
  quote: string
  exchanges: SideChatExchange[]
  focus: number
  request?: AbortController
  error?: string
}

interface SideChatStore {
  session?: SideChatSession
  open: (threadId: string, options?: { question?: string; quote?: string }) => boolean
  close: () => void
  cancel: () => void
  edit: (question: string) => void
  clearQuote: () => void
  ask: () => Promise<void>
}

export const useSideChat = create<SideChatStore>((set, get) => ({
  open: (threadId, { question, quote } = {}) => {
    const previous = get().session
    const sameThread = previous?.threadId === threadId
    if (sameThread && question && previous.question.trim()) return false
    const session = sameThread ? previous : { threadId, question: "", quote: "", exchanges: [], focus: 0 }
    set({ session: {
      ...session,
      question: question ?? session.question,
      quote: quote === undefined ? session.quote : clipUTF8(quote, QUOTE_BYTES),
      focus: session.focus + 1,
    } })
    if (!sameThread) previous?.request?.abort()
    return true
  },
  close: () => {
    const session = get().session
    if (!session) return
    set({ session: undefined })
    session.request?.abort()
  },
  cancel: () => {
    const session = get().session
    if (!session?.request) return
    set({ session: { ...session, request: undefined, focus: session.focus + 1 } })
    session.request.abort()
  },
  edit: (question) => {
    const session = get().session
    if (session) set({ session: { ...session, question, error: undefined } })
  },
  clearQuote: () => {
    const session = get().session
    if (session) set({ session: { ...session, quote: "" } })
  },
  ask: async () => {
    const session = get().session
    const question = session?.question.trim()
    if (!session || session.request || !question || sideChatQuestionTooLong(question)) return
    const request = new AbortController()
    set({ session: { ...session, request, error: undefined } })
    try {
      const { answer } = await api.askSideChat(session.threadId, {
        question,
        quote: session.quote || undefined,
        history: session.exchanges.slice(-HISTORY_PAIRS),
      }, request.signal)
      const current = get().session
      // Closing, cancelling or changing tasks relinquishes this response,
      // including when the transport returns after abort was requested.
      if (current?.request !== request) return
      set({ session: {
        ...current,
        request: undefined,
        question: "",
        exchanges: [...current.exchanges, { question, answer }],
      } })
    } catch (error) {
      const current = get().session
      if (current?.request !== request) return
      set({ session: { ...current, request: undefined, error: errorMessage(error) } })
    }
  },
}))
