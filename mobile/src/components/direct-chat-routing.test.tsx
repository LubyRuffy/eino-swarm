import { act, fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { DirectChatScreen } from "./direct-chat-screen"
import type { DirectProvider } from "@/lib/direct-provider"
import { setLocale } from "@/lib/i18n"
import { saveGatewaySession, type GatewaySession } from "@/lib/gateway-account"
import { saveWebExecution } from "@/lib/web-execution"

const personal: DirectProvider = { id: "personal", label: "My endpoint", baseURL: "https://personal.invalid/v1", apiKey: "private-key", api: "chat", model: "shared-model", catalog: ["shared-model"], timeoutSeconds: 300 }
const gateway: DirectProvider = { ...personal, id: "gateway:owner", sourceKind: "gateway", label: "Server · Team", baseURL: "https://gateway.invalid/v1", apiKey: "account-key" }
const session: GatewaySession = { origin: "https://gateway.invalid", token: "agm_" + "a".repeat(64), user: { id: "owner", username: "User", enabled: true, approval_status: "approved" }, device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" } }

beforeEach(() => setLocale("en"))

function show(providers = [personal]) {
  const onWebSettings = vi.fn()
  render(<DirectChatScreen hosts={[]} activeFingerprint="" path="relay" providers={providers} onSelectHost={vi.fn()} onAddHost={vi.fn()} onUnlink={vi.fn()} onModels={vi.fn()} {...{ onWebSettings }} />)
  return { onWebSettings }
}

it("shows the selected model source independently from Web execution and opens recovery settings", () => {
  const { onWebSettings } = show([personal, gateway])
  expect(screen.getByTestId("direct-model-source")).toHaveTextContent("Personal endpoint · My endpoint")
  expect(screen.getByTestId("direct-web-status")).toHaveTextContent("Off · sign in to use cloud Web tools")
  fireEvent.click(screen.getByRole("button", { name: "Web settings" }))
  expect(onWebSettings).toHaveBeenCalledOnce()

  fireEvent.click(screen.getByRole("button", { name: "Model" }))
  fireEvent.click(screen.getAllByRole("radio", { name: "shared-model" })[1]!)
  expect(screen.getByTestId("direct-model-source")).toHaveTextContent("Server · Team")
  expect(screen.getByTestId("direct-web-status")).toHaveTextContent("Off · sign in to use cloud Web tools")
  expect(document.body.textContent).not.toContain("private-key")
  expect(document.body.textContent).not.toContain("account-key")
})

it("shows account cloud for a personal model and preserves its source when Web tools are disabled", () => {
  saveGatewaySession(session)
  show()
  expect(screen.getByTestId("direct-web-status")).toHaveTextContent("Cloud server · access checked when used")
  expect(screen.getByText("This model must support function tools to use Web tools.")).toBeVisible()
  act(() => saveWebExecution("off"))
  expect(screen.getByTestId("direct-web-status")).toHaveTextContent("Off · ordinary model chat")
  expect(screen.getByTestId("direct-model-source")).toHaveTextContent("Personal endpoint · My endpoint")
  expect(screen.queryByText("This model must support function tools to use Web tools.")).not.toBeInTheDocument()
})

it("shows explicit cloud as unavailable when sign-in is missing or no longer approved", () => {
  saveWebExecution("cloud")
  show()
  expect(screen.getByTestId("direct-web-status")).toHaveTextContent("Cloud unavailable · sign in or switch Web tools off")
  act(() => saveGatewaySession(session))
  expect(screen.getByTestId("direct-web-status")).toHaveTextContent("Cloud server")
  act(() => saveGatewaySession({ ...session, user: { ...session.user, enabled: false } }))
  expect(screen.getByTestId("direct-web-status")).toHaveTextContent("Off · sign in to use cloud Web tools")
})
