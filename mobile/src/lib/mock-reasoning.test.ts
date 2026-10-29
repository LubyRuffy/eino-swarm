import { expect, it } from "vitest"

import { MockHost, MockLink } from "./mock-link"
import { OpWatch } from "./rpc"
import { applyEvent, type CompactBlock } from "./transcript"

it("replays visible reasoning-only fixtures through real watch history in active and completed turns", async () => {
  const prior = location.href
  history.replaceState(null, "", "/?mock=1&tick=0&history=reasoning-only")
  const host = new MockHost(0)
  const link = new MockLink(host)
  try {
    for (const id of ["t-live", "t-recent"]) {
      const ready = await link.rpc({ op: OpWatch, thread_id: id })
      expect(ready.ok).toBe(true)
      const events = ready.events ?? []
      expect(events.length).toBeGreaterThan(0)
      expect(events.every((event) => event.seq > 0)).toBe(true)
      const blocks = events.reduce<CompactBlock[]>(applyEvent, [])
      expect(blocks.filter((block) => block.kind === "reasoning")).toHaveLength(1)
      expect(blocks.some((block) => block.kind === "tool")).toBe(false)
      expect(blocks.some((block) => block.kind === "answer")).toBe(id === "t-recent")
    }
  } finally {
    link.close()
    host.stop()
    history.replaceState(null, "", prior)
  }
})
