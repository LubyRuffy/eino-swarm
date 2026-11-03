import { fireEvent, render, screen, within } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { DirectChatScreen } from "./direct-chat-screen"
import type { DirectProvider } from "@/lib/direct-provider"
import { setLocale } from "@/lib/i18n"
import { saveGatewaySession, type GatewaySession } from "@/lib/gateway-account"

const personal: DirectProvider = { id: "personal", label: "My endpoint", baseURL: "https://personal.invalid/v1", apiKey: "private-key", api: "chat", model: "shared-model", catalog: ["shared-model"], timeoutSeconds: 300 }
const gateway: DirectProvider = { ...personal, id: "gateway:owner", sourceKind: "gateway", label: "Server · Team", baseURL: "https://gateway.invalid/v1", apiKey: "account-key" }
const session: GatewaySession = { origin: "https://gateway.invalid", token: "agm_" + "a".repeat(64), user: { id: "owner", username: "User", enabled: true, approval_status: "approved" }, device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" } }

beforeEach(() => setLocale("en"))

it("keeps the signed-in chat clear and distinguishes same-named models in the picker", () => {
  saveGatewaySession(session)
  render(<DirectChatScreen hosts={[]} activeFingerprint="" path="relay" providers={[personal, gateway]} onSelectHost={vi.fn()} onAddHost={vi.fn()} onUnlink={vi.fn()} onModels={vi.fn()} />)
  expect(screen.queryByTestId("direct-web-status")).not.toBeInTheDocument()
  expect(screen.queryByTestId("direct-model-source")).not.toBeInTheDocument()
  expect(screen.queryByRole("button", { name: "Web settings" })).not.toBeInTheDocument()

  const openPicker = () => fireEvent.click(screen.getByRole("button", { name: "Model" }))
  const modelFrom = (source: string) => {
    const heading = screen.getByRole("heading", { name: source })
    return within(heading.parentElement!).getByRole("radio", { name: "shared-model" })
  }
  openPicker()
  expect(modelFrom("My endpoint")).toHaveAttribute("aria-checked", "true")
  expect(modelFrom("Server · Team")).toHaveAttribute("aria-checked", "false")
  fireEvent.click(modelFrom("Server · Team"))
  openPicker()
  expect(modelFrom("My endpoint")).toHaveAttribute("aria-checked", "false")
  expect(modelFrom("Server · Team")).toHaveAttribute("aria-checked", "true")
  expect(document.body.textContent).not.toContain("private-key")
  expect(document.body.textContent).not.toContain("account-key")
})
