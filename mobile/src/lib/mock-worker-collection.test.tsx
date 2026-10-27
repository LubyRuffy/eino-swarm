import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { ThreadLog } from "@/components/thread-blocks"
import { setLocale } from "./i18n"
import { MockHost, MockLink } from "./mock-link"
import { OpWatch } from "./rpc"
import { applyEvent, type CompactBlock } from "./transcript"

afterEach(cleanup)

for (const locale of ["en", "zh"] as const) {
  for (const state of ["waiting", "paused", "done", "stopped"] as const) {
    it(`replays the ${state} collection fixture through the phone renderer in ${locale}`, async () => {
      const previous = location.href
      history.replaceState(null, "", `/?mock=1&tick=0&history=worker-collection&collection=${state}`)
      setLocale(locale)
      const host = new MockHost(0)
      const link = new MockLink(host)
      try {
        const ready = await link.rpc({ op: OpWatch, thread_id: "t-live" })
        expect(ready.ok).toBe(true)
        expect(ready.status?.running).toBe(state === "waiting" || state === "paused")
        const events = ready.events ?? []
        const pause = events.find((event) => event.kind === "max_iterations")
        expect(JSON.parse(pause?.text ?? "{}")).toMatchObject({
          reason: "workers_uncollected", automatic_resume: state !== "paused",
        })
        if (state === "stopped") {
          expect(events.find((event) => event.kind === "error")).toMatchObject({ err: "stopped before collecting worker results" })
          expect(events.at(-1)).toMatchObject({ kind: "finished", agent_id: "worker-1" })
          expect(events.some((event) => event.kind === "workers_collection_resumed" || event.kind === "done")).toBe(false)
        }
        const blocks = events.reduce<CompactBlock[]>((all, event) => applyEvent(all, event), [])
        const { container } = render(<ThreadLog blocks={blocks} running={ready.status?.running ?? false} />)
        const messages = locale === "en" ? {
          waiting: "Waiting for delegated work to finish. The manager will resume once to collect the results.",
          paused: "Delegated results have not been collected. The manager is paused.",
          done: "Continuing to collect delegated results.",
          stopped: "Stopped before collecting delegated results.",
        } : {
          waiting: "正在等待委派工作结束。主管会自动继续一次，收取结果。",
          paused: "委派结果尚未收齐，主管已暂停。",
          done: "正在继续收取委派结果。",
          stopped: "已停止，委派结果未收齐。",
        }
        expect(screen.getByText(messages[state])).toHaveClass("text-muted-foreground")
        expect(container.querySelector(".text-destructive")).toBeNull()
        expect(container).not.toHaveTextContent(/automatic_resume|wait_id|worker_ids|extend_by|tool-round limit|stopped before collecting worker results/)
      } finally {
        link.close()
        host.stop()
        history.replaceState(null, "", previous)
      }
    })
  }
}
