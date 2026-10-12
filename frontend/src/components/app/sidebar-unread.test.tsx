import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import type { Project, Thread } from "@/lib/types"
import { SidebarThreadRow } from "./sidebar-thread-row"
import { ProjectList } from "./project-list"

const thread = {
  id: "th_unseen", title: "An unseen result", project_id: "pj_1",
  provider_id: "default", reasoning_effort: "", archived: false,
  created_at: "", last_active_at: "", running: false,
  unread: true, completed_seq: 12,
} as Thread
const project = { id: "pj_1", name: "Work", created_at: "", updated_at: "" } as Project

describe("unread conversation completion", () => {
  it("keeps a static completed mark until the conversation is opened", () => {
    const onOpen = vi.fn()
    render(<SidebarThreadRow thread={thread} active={false} running={false}
      onOpen={onOpen} onRename={vi.fn()} onDelete={vi.fn()} />)
    const mark = screen.getByLabelText("Completed, unread")
    expect(mark).toHaveClass("bg-done")
    expect(mark).not.toHaveClass("animate-breathe")
    fireEvent.click(screen.getByRole("button", { name: /An unseen result/ }))
    expect(onOpen).toHaveBeenCalledWith(thread.id)
  })

  it("keeps running and asking ahead of an older unread completion", () => {
    const { rerender } = render(<SidebarThreadRow thread={thread} active={false} running
      onOpen={vi.fn()} onRename={vi.fn()} onDelete={vi.fn()} />)
    expect(screen.getByLabelText("running")).toHaveClass("animate-breathe")
    expect(screen.queryByLabelText("Completed, unread")).not.toBeInTheDocument()
    rerender(<SidebarThreadRow thread={thread} active={false} running asking
      onOpen={vi.fn()} onRename={vi.fn()} onDelete={vi.fn()} />)
    expect(screen.queryByLabelText("Completed, unread")).not.toBeInTheDocument()
  })

  it("shows folded project unread completion in the right icon area without hiding work", () => {
    render(<ProjectList projects={[project]} threadsByProject={{ pj_1: [thread, { ...thread, id: "busy", unread: false, running: true }] }}
      expanded={{ pj_1: false }} onSelect={vi.fn()} onToggle={vi.fn()} onNew={vi.fn()}
      onNewConversation={vi.fn()} onEdit={vi.fn()} onDelete={vi.fn()} onOpenSkill={vi.fn()}
      onReorder={vi.fn()} onOpenThread={vi.fn()} onRenameThread={vi.fn()} onDeleteThread={vi.fn()}
      onReorderThreads={vi.fn()} onPinThread={vi.fn()} onToggleSection={vi.fn()} />)
    const row = screen.getByTestId("project-row")
    const icons = within(row).getByTestId("project-unread-completion")
    expect(icons).toHaveAccessibleName("1 unread completed task")
    expect(within(row).getByLabelText("running")).toBeInTheDocument()
    expect(screen.queryByTestId("project-threads")).not.toBeInTheDocument()
  })

  it("does not paint finished conversations that have already been read", () => {
    render(<SidebarThreadRow thread={{ ...thread, unread: false }} active={false} running={false}
      onOpen={vi.fn()} onRename={vi.fn()} onDelete={vi.fn()} />)
    expect(screen.queryByLabelText("Completed, unread")).not.toBeInTheDocument()
  })
})
