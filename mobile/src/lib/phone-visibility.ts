import { useEffect, useRef } from "react"

/** A visible phone can recover its link; waking it is not reading a conversation. */
export function usePhoneVisibility(onVisible: () => void) {
  const latest = useRef(onVisible)
  latest.current = onVisible
  useEffect(() => {
    const wake = () => {
      if (document.visibilityState && document.visibilityState !== "visible") return
      latest.current()
    }
    document.addEventListener("visibilitychange", wake)
    window.addEventListener("online", wake)
    window.addEventListener("pageshow", wake)
    return () => {
      document.removeEventListener("visibilitychange", wake)
      window.removeEventListener("online", wake)
      window.removeEventListener("pageshow", wake)
    }
  }, [])
}
