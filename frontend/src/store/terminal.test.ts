import { afterEach, describe, expect, it } from "vitest"

import { resetTerminalStore, useTerminal } from "./terminal"

afterEach(() => {
  resetTerminalStore()
})

describe("terminal store", () => {
  it("opens a new session on every spawn with the target of that click", () => {
    const first = useTerminal.getState().spawn({ threadId: "th_1" })
    const second = useTerminal.getState().spawn({ projectId: "pj_1" })
    const { sessions, open, activeId } = useTerminal.getState()
    expect(open).toBe(true)
    expect(sessions).toHaveLength(2)
    expect(sessions[0]).toMatchObject({ id: first, threadId: "th_1" })
    expect(sessions[1]).toMatchObject({ id: second, projectId: "pj_1" })
    expect(activeId).toBe(second)
  })

  it("does not spawn without a conversation or a project", () => {
    expect(useTerminal.getState().spawn({})).toBeUndefined()
    expect(useTerminal.getState().sessions).toHaveLength(0)
    expect(useTerminal.getState().open).toBe(false)
  })

  it("caps how many shells one click-happy session can keep", () => {
    for (let i = 0; i < 10; i++) {
      useTerminal.getState().spawn({ threadId: "th_1" })
    }
    expect(useTerminal.getState().sessions).toHaveLength(8)
  })

  it("toggles the panel without killing shells, and spawn still uses a new cwd target", () => {
    useTerminal.getState().spawn({ threadId: "th_old" })
    useTerminal.getState().toggle()
    expect(useTerminal.getState().open).toBe(false)
    expect(useTerminal.getState().sessions).toHaveLength(1)
    useTerminal.getState().toggle({ threadId: "th_old" })
    expect(useTerminal.getState().open).toBe(true)
    expect(useTerminal.getState().sessions).toHaveLength(1)
    useTerminal.getState().spawn({ threadId: "th_new" })
    expect(useTerminal.getState().sessions).toHaveLength(2)
    expect(useTerminal.getState().sessions[1]?.threadId).toBe("th_new")
  })

  it("closes the last tab and the panel together", () => {
    const id = useTerminal.getState().spawn({ threadId: "th_1" })
    useTerminal.getState().closeSession(id!)
    expect(useTerminal.getState().sessions).toHaveLength(0)
    expect(useTerminal.getState().open).toBe(false)
  })

  it("records the cwd the server resolved, not one the client guessed", () => {
    const id = useTerminal.getState().spawn({ threadId: "th_1" })!
    useTerminal.getState().setCwd(id, "/tmp/project")
    expect(useTerminal.getState().sessions[0]?.cwd).toBe("/tmp/project")
  })

  it("does not open an empty panel when there is nowhere to start a shell", () => {
    useTerminal.getState().toggle()
    expect(useTerminal.getState().open).toBe(false)
    expect(useTerminal.getState().sessions).toHaveLength(0)
  })

  it("clamps the panel height so a drag cannot hide the transcript", () => {
    useTerminal.getState().setHeight(80)
    expect(useTerminal.getState().height).toBe(120)
    useTerminal.getState().setHeight(900)
    expect(useTerminal.getState().height).toBe(640)
  })
})
