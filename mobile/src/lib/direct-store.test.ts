import { describe, expect, it } from "vitest"

import {
  blankProvider,
  loadProviders,
  providerChoices,
  saveProviders,
  type DirectProvider,
} from "./direct-provider"
import { loadThreads, newThread, saveThreads, threadTitle } from "./direct-threads"

function row(patch: Partial<DirectProvider> = {}): DirectProvider {
  return {
    ...blankProvider(),
    id: "p-1",
    label: "Desk",
    baseURL: "https://endpoint.invalid/v1/",
    model: "one",
    catalog: ["one", "two", "two"],
    ...patch,
  }
}

describe("direct providers", () => {
  it("drops a row that is not an http endpoint and dedupes the catalog", () => {
    saveProviders([
      row(),
      row({ id: "p-2", baseURL: "javascript:alert(1)" }),
    ])
    const loaded = loadProviders()
    expect(loaded).toHaveLength(1)
    expect(loaded[0].baseURL).toBe("https://endpoint.invalid/v1")
    expect(loaded[0].catalog).toEqual(["one", "two"])
    expect(loaded[0].api).toBe("chat")
  })

  it("offers one choice per model, grouped by the name the user typed", () => {
    const choices = providerChoices([row({ api: "responses", catalog: ["two"] })])
    expect(choices.map((choice) => choice.model)).toEqual(["one", "two"])
    expect(choices[0]).toMatchObject({ provider_label: "Desk", default: true })
    expect(choices[1].default).toBe(false)
  })

  it("forgets the list when the last row is removed", () => {
    saveProviders([row()])
    saveProviders([])
    expect(loadProviders()).toEqual([])
  })

  it("rejects a timeout under ten seconds back to the default", () => {
    saveProviders([row({ timeoutSeconds: 1 })])
    expect(loadProviders()[0].timeoutSeconds).toBe(300)
  })
})

describe("direct threads", () => {
  it("stores the words and the file name, not the bytes", () => {
    const thread = newThread({ providerId: "p-1", model: "one", reasoning: "low" })
    thread.title = threadTitle("first line\nrest", "")
    thread.messages = [
      {
        id: "m",
        role: "user",
        text: "see",
        attachments: [{ name: "shot.png", kind: "image", dataUrl: "data:image/png;base64,aaaa" }],
      },
    ]
    saveThreads([thread])
    const loaded = loadThreads()
    expect(loaded[0].title).toBe("first line")
    expect(loaded[0].messages[0].attachments?.[0]).toEqual({ name: "shot.png", kind: "image" })
    expect(JSON.stringify(loaded)).not.toContain("base64")
  })

  it("titles a file-only send by the file name", () => {
    expect(threadTitle("", "notes.txt")).toBe("notes.txt")
    expect(threadTitle("x".repeat(80), "")).toHaveLength(48)
  })
})
