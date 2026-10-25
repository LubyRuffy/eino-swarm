import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ComposerDraftSession, type ComposerDraftSnapshot } from "@/lib/composer-draft"
import { useApp } from "@/store/app"
import { ComposerDraftStatus } from "./composer-draft-status"

function snapshot(patch: Partial<ComposerDraftSnapshot> = {}): ComposerDraftSnapshot {
  return {
    draft: { key: "thread:status", revision: 1, text: "Unsent input", quotes: [], files: [], state: "editing" },
    loaded: true, loading: false, saving: false, saved: true,
    ...patch,
  }
}

function session() {
  const draft = new ComposerDraftSession("thread:status")
  for (const method of ["flush", "load", "useStoredDraft", "saveCurrentDraft", "resumeEditing", "discard"] as const) {
    vi.spyOn(draft, method).mockResolvedValue(undefined)
  }
  return draft
}

describe("Composer draft notices", () => {
  // Routine persistence must not take space or present a destructive action
  // while someone is composing. Only a problem needs their attention.
  it.each([
    ["initial load", { loaded: false, saved: false }],
    ["restoring", { loaded: false, loading: true, saved: false }],
    ["waiting to save", { saved: false }],
    ["saving", { saved: false, saving: true }],
    ["saved", {}],
    ["empty saved draft", { draft: { ...snapshot().draft, text: "" } }],
    ["sending", { draft: { ...snapshot().draft, state: "sending" as const } }],
  ] as const)("leaves no notice or reserved space when %s", (_state, patch) => {
    const { container } = render(<ComposerDraftStatus session={session()} snapshot={snapshot(patch)} />)
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByTestId("composer-draft-status")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Delete draft" })).not.toBeInTheDocument()
  })

  it("shows a failed save with retry and discard, then removes the whole notice after recovery", () => {
    const draft = session()
    const state = snapshot({ saved: false, error: "Storage unavailable" })
    const view = render(<ComposerDraftStatus session={draft} snapshot={state} />)
    expect(screen.getByRole("alert")).toHaveTextContent("Draft not saved")
    expect(screen.getByText("Storage unavailable")).toBeVisible()
    fireEvent.click(screen.getByRole("button", { name: "Retry saving" }))
    expect(draft.flush).toHaveBeenCalledOnce()
    fireEvent.click(screen.getByRole("button", { name: "Delete draft" }))
    expect(draft.discard).toHaveBeenCalledOnce()
    view.rerender(<ComposerDraftStatus session={draft} snapshot={snapshot()} />)
    expect(view.container).toBeEmptyDOMElement()
  })

  it("lets a failed restore retry loading and has no discard action for an empty draft", () => {
    const draft = session()
    render(<ComposerDraftStatus session={draft} snapshot={snapshot({
      loaded: false, saved: false, error: "Could not restore", draft: { ...snapshot().draft, text: "" },
    })} />)
    expect(screen.getByRole("alert")).toHaveTextContent("Draft not saved")
    fireEvent.click(screen.getByRole("button", { name: "Retry restoring" }))
    expect(draft.load).toHaveBeenCalledOnce()
    expect(screen.queryByRole("button", { name: "Delete draft" })).not.toBeInTheDocument()
  })

  it("shows a conflict even without an error string and retains both resolution actions", () => {
    const draft = session()
    render(<ComposerDraftStatus session={draft} snapshot={snapshot({ conflict: true })} />)
    expect(screen.getByRole("alert")).toHaveTextContent("Another window changed this draft")
    expect(screen.queryByText("Draft saved on this computer")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Use the saved version (replace this input)" }))
    fireEvent.click(screen.getByRole("button", { name: "Save this input (replace the saved version)" }))
    expect(draft.useStoredDraft).toHaveBeenCalledOnce()
    expect(draft.saveCurrentDraft).toHaveBeenCalledOnce()
    expect(screen.queryByRole("button", { name: "Retry saving" })).not.toBeInTheDocument()
  })

  it("keeps an unknown send visible until the user checks the conversation", () => {
    const draft = session()
    render(<ComposerDraftStatus session={draft} snapshot={snapshot({ draft: { ...snapshot().draft, state: "uncertain" } })} />)
    expect(screen.getByRole("alert")).toHaveTextContent("send status is unknown")
    expect(screen.getByRole("alert")).toHaveTextContent("will not be resent automatically")
    fireEvent.click(screen.getByRole("button", { name: "I checked the conversation — continue editing" }))
    expect(draft.resumeEditing).toHaveBeenCalledOnce()
  })

  it("keeps failure actions disabled while their save is pending", () => {
    render(<ComposerDraftStatus session={session()} snapshot={snapshot({ saving: true, saved: false, error: "Storage unavailable" })} />)
    expect(screen.getByRole("button", { name: "Retry saving" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Delete draft" })).toBeDisabled()
  })

  it.each<[string, Partial<ComposerDraftSnapshot>]>([
    ["failed restoration", { loaded: false, error: "Could not restore" }],
    ["conflict", { conflict: true }],
    ["unknown send", { error: "Send not acknowledged", draft: { ...snapshot().draft, state: "uncertain" } }],
  ])("prevents read-only %s actions from changing a draft", (_problem, patch) => {
    const draft = session()
    render(<ComposerDraftStatus session={draft} snapshot={snapshot(patch)} disabled />)
    for (const button of screen.getAllByRole("button")) {
      expect(button).toBeDisabled()
      fireEvent.click(button)
    }
    expect(draft.flush).not.toHaveBeenCalled()
    expect(draft.load).not.toHaveBeenCalled()
    expect(draft.useStoredDraft).not.toHaveBeenCalled()
    expect(draft.saveCurrentDraft).not.toHaveBeenCalled()
    expect(draft.resumeEditing).not.toHaveBeenCalled()
    expect(draft.discard).not.toHaveBeenCalled()
  })

  it("renders the exceptional notice and actions in Chinese", () => {
    useApp.setState({ locale: "zh" })
    render(<ComposerDraftStatus session={session()} snapshot={snapshot({ saved: false, error: "保存失败" })} />)
    expect(screen.getByRole("alert")).toHaveTextContent("草稿尚未保存")
    expect(screen.getByRole("button", { name: "重试保存" })).toBeVisible()
    expect(screen.getByRole("button", { name: "删除草稿" })).toBeVisible()
  })
})
