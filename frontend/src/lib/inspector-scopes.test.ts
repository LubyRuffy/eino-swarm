import { describe, expect, it } from "vitest"
import { builtinInspectorScopes } from "./inspector-scopes"

describe("inspector scope", () => {
  it("keeps task and project ownership on the shared tab strip", () => {
    expect(builtinInspectorScopes).toEqual({
      agents: "thread", files: "thread", trace: "thread", memory: "project",
    })
  })
})
