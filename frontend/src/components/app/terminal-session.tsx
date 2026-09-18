import { FitAddon } from "@xterm/addon-fit"
import { Terminal } from "@xterm/xterm"
import { useEffect, useRef } from "react"

import type { TerminalSession } from "@/store/terminal"
import { terminalSocketURL } from "@/lib/terminal"
import { cn } from "@/lib/utils"

import "@xterm/xterm/css/xterm.css"

function cssHsl(name: string): string {
  const raw = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim()
  return raw ? `hsl(${raw})` : ""
}

function terminalTheme() {
  return {
    background: cssHsl("--card") || cssHsl("--background"),
    foreground: cssHsl("--foreground"),
    cursor: cssHsl("--foreground"),
    cursorAccent: cssHsl("--card"),
    selectionBackground: cssHsl("--accent"),
    selectionForeground: cssHsl("--accent-foreground"),
  }
}

function terminalFont() {
  const root = getComputedStyle(document.documentElement)
  const mono = root.getPropertyValue("--font-mono").trim()
  return mono || "ui-monospace, SFMono-Regular, Menlo, monospace"
}

/** One PTY rendered by xterm. The working directory is whatever the server
 *  resolved for this session's conversation or project at connect time. */
export function TerminalSessionView({
  session,
  visible,
  onReady,
}: {
  session: TerminalSession
  visible: boolean
  onReady: (cwd: string) => void
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | undefined>(undefined)
  const fitRef = useRef<FitAddon | undefined>(undefined)
  const wsRef = useRef<WebSocket | undefined>(undefined)
  const onReadyRef = useRef(onReady)
  onReadyRef.current = onReady

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: terminalFont(),
      theme: terminalTheme(),
      allowProposedApi: false,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(host)
    fit.fit()
    termRef.current = term
    fitRef.current = fit

    const cols = Math.max(term.cols, 10)
    const rows = Math.max(term.rows, 5)
    const ws = new WebSocket(terminalSocketURL(session, cols, rows))
    ws.binaryType = "arraybuffer"
    wsRef.current = ws

    const encoder = new TextEncoder()
    const dataSub = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(encoder.encode(data))
      }
    })

    ws.onmessage = (ev) => {
      if (typeof ev.data === "string") {
        try {
          const msg = JSON.parse(ev.data) as {
            type?: string
            cwd?: string
            error?: string
          }
          if (msg.type === "ready" && msg.cwd) onReadyRef.current(msg.cwd)
          if (msg.type === "error" && msg.error) {
            term.write(`\r\n${msg.error}\r\n`)
          }
        } catch {
          // a non-JSON text frame is still shell output
          term.write(ev.data)
        }
        return
      }
      term.write(new Uint8Array(ev.data as ArrayBuffer))
    }
    ws.onclose = () => {
      term.write("\r\n")
    }

    return () => {
      dataSub.dispose()
      ws.close()
      term.dispose()
      termRef.current = undefined
      fitRef.current = undefined
      wsRef.current = undefined
    }
  }, [session.id, session.threadId, session.projectId])

  useEffect(() => {
    const host = hostRef.current
    const term = termRef.current
    const fit = fitRef.current
    if (!host || !term || !fit || !visible) return
    const apply = () => {
      if (host.clientWidth < 8 || host.clientHeight < 8) return
      fit.fit()
      const ws = wsRef.current
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(
          JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }),
        )
      }
    }
    apply()
    if (typeof ResizeObserver === "undefined") return
    const ro = new ResizeObserver(apply)
    ro.observe(host)
    return () => ro.disconnect()
  }, [visible, session.id])

  return (
    <div
      ref={hostRef}
      className={cn("h-full min-h-0 w-full overflow-hidden p-2", !visible && "hidden")}
      data-testid={`terminal-session-${session.id}`}
    />
  )
}
