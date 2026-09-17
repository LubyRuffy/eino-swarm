import { describe, expect, it } from "vitest"

import { MANAGER_ID } from "@/lib/transcript"
import {
  applyTail,
  historyNewestSeq,
  historyOldestSeq,
  prependOlder,
  rememberRewind,
  rememberStored,
  resetThreadHistory,
} from "./thread-history"
import type { SwarmEvent } from "@/lib/types"

function ev(seq: number, text: string, turnId = "tn_a"): SwarmEvent {
  return {
    thread_id: "th_1",
    turn_id: turnId,
    seq,
    kind: "user_message",
    agent_id: MANAGER_ID,
    text,
    created_at: new Date().toISOString(),
  }
}

describe("thread history buffer", () => {
  it("rebuilds the transcript from a tail page then older rows", () => {
    resetThreadHistory()
    const tail = applyTail("th_1", [ev(4, "later", "tn_b")])
    expect(tail.agents[MANAGER_ID]?.blocks.map((b) => b.text)).toEqual(["later"])
    expect(historyOldestSeq()).toBe(4)
    expect(historyNewestSeq()).toBe(4)

    const full = prependOlder("th_1", [ev(2, "earlier")])
    expect(full?.agents[MANAGER_ID]?.blocks.map((b) => b.text)).toEqual([
      "earlier",
      "later",
    ])
    expect(historyOldestSeq()).toBe(2)
  })

  it("ignores a prepend for a conversation the reader already left", () => {
    resetThreadHistory()
    applyTail("th_1", [ev(4, "later")])
    expect(prependOlder("th_other", [ev(2, "earlier")])).toBeUndefined()
  })

  it("drops stored rows on rewind so a later prepend cannot resurrect them", () => {
    resetThreadHistory()
    applyTail("th_1", [ev(2, "keep"), ev(4, "gone")])
    rememberRewind("th_1", 4)
    expect(historyNewestSeq()).toBe(2)
    rememberStored("th_1", ev(5, "next", "tn_c"))
    expect(historyNewestSeq()).toBe(5)
  })
})
