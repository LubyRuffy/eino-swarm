import { describe, expect, it } from "vitest"

import {
  deskDest,
  listDestForThread,
  scheduledReturnDest,
  setDeskDest,
  visibleDest,
} from "./desk-nav"

describe("visibleDest", () => {
  it("keeps the wait list up only while the inbox is open", () => {
    expect(visibleDest(true, "projects")).toBe("scheduled")
    expect(visibleDest(true, "chats")).toBe("scheduled")
    expect(visibleDest(true, "clients")).toBe("scheduled")
    expect(visibleDest(false, "scheduled")).toBe("projects")
    expect(visibleDest(false, "clients")).toBe("clients")
    expect(visibleDest(false, "chats")).toBe("chats")
    expect(visibleDest(false, "projects")).toBe("projects")
  })
})

describe("listDestForThread", () => {
  it("keeps a project topic on the project list", () => {
    expect(listDestForThread("pj_1")).toBe("projects")
    expect(listDestForThread("")).toBe("chats")
    expect(listDestForThread(undefined)).toBe("chats")
  })
})

describe("setDeskDest", () => {
  it("remembers the rail choice and ignores a repeat", () => {
    setDeskDest("projects")
    setDeskDest("clients")
    expect(deskDest()).toBe("clients")
    setDeskDest("clients")
    expect(deskDest()).toBe("clients")
    setDeskDest("projects")
  })

  it("returns Escape from Scheduled to the list that was open", () => {
    setDeskDest("chats")
    setDeskDest("scheduled")
    expect(scheduledReturnDest()).toBe("chats")
    setDeskDest("projects")
    setDeskDest("scheduled")
    expect(scheduledReturnDest()).toBe("projects")
    setDeskDest("projects")
  })
})
