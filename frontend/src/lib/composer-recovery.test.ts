import { afterEach, describe, expect, it } from "vitest"
import { explicitComposerRecovery, installComposerRecoveryAttribution } from "./composer-recovery"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

const original = useApp.getState()
afterEach(() => useApp.setState(original))

describe("Native recovery attribution", () => {
  it("accepts explicit matching recovery scopes only in the desktop shell", () => {
    expect(explicitComposerRecovery("?recovery_thread=th_a")).toEqual({})
    expect(explicitComposerRecovery("?shell=desktop&recovery_thread=th_a&recovery_draft=thread:th_a")).toEqual({ threadId: "th_a", draftKey: "thread:th_a" })
    expect(explicitComposerRecovery("?shell=desktop&recovery_thread=th_a&recovery_draft=thread:th_b")).toEqual({ threadId: "th_a" })
    expect(explicitComposerRecovery("?shell=desktop&recovery_project=project_a&recovery_draft=new:project_a")).toEqual({ projectId: "project_a", draftKey: "new:project_a" })
    expect(explicitComposerRecovery("?shell=desktop&recovery_thread=../other").threadId).toBeUndefined()
  })

  it("publishes current thread and real turn attribution without message contents", () => {
    useProjects.setState({ selectedId: "project_a" })
    useApp.setState({ activeId: "th_a", status: { running: true, turn_id: "tn_live" } })
    const stop = installComposerRecoveryAttribution()
    const read = () => (window as Window & { __zwaiRecoveryAttribution?: Record<string, unknown> }).__zwaiRecoveryAttribution
    expect(read()).toMatchObject({ thread_id: "th_a", turn_id: "tn_live", draft_key: "thread:th_a", draft_saved: false })
    useApp.setState({ activeId: "th_b", status: { running: false, turn_id: "tn_b" } })
    expect(read()).toMatchObject({ thread_id: "th_b", turn_id: "tn_b", draft_key: "thread:th_b" })
    expect(Object.keys(read()!)).not.toContain("text")
    stop()
  })
})
