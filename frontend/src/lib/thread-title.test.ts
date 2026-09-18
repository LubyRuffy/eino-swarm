import { describe, expect, it } from "vitest"

import { preferNamedTitles } from "./thread-title"
import type { Thread } from "./types"

function thread(id: string, title: string, title_auto: boolean): Thread {
  return {
    id,
    title,
    title_auto,
    project_id: "",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: "",
    last_active_at: "",
    running: false,
  }
}

describe("preferNamedTitles", () => {
  it("keeps a generated name when the list is still the placeholder", () => {
    const local = [thread("th_1", "Weekly status", false)]
    const incoming = [thread("th_1", "Investigate the overdue items…", true)]
    expect(preferNamedTitles(local, incoming)[0]?.title).toBe("Weekly status")
    expect(preferNamedTitles(local, incoming)[0]?.title_auto).toBe(false)
  })

  it("takes the list once the namer has landed there", () => {
    const local = [thread("th_1", "Weekly status", false)]
    const incoming = [thread("th_1", "Ops report", false)]
    expect(preferNamedTitles(local, incoming)[0]?.title).toBe("Ops report")
  })
})
