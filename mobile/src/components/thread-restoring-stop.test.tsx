import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { setLocale } from "@/lib/i18n"
import { ThreadScreen } from "./thread-screen"

it("can stop a selected not-yet-restored stored running turn without fabricating live running state", () => {
  setLocale("en")
  const onStop = vi.fn()
  const detail = { id: "t1", title: "Pending recovery", turns: [{ id: "persisted-turn", status: "running" }] }
  const props = { detail, blocks: [], onBack: vi.fn(), onSend: vi.fn(), onSteer: vi.fn(), onStop, onAnswer: vi.fn(), onAnswerStructured: vi.fn() }
  const frame = render(<ThreadScreen {...props} restoring />)
  const stop = screen.getByRole("button", { name: /^Stop$/ })
  expect(stop).toBeEnabled()
  fireEvent.click(stop)
  expect(onStop).toHaveBeenCalledOnce()
  expect(detail).not.toHaveProperty("running")
  frame.rerender(<ThreadScreen {...props} restoring={false} />)
  expect(screen.queryByRole("button", { name: /^Stop$/ })).toBeNull()
})
