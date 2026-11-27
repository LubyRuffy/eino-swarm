import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import type { Settings } from "@/lib/types"
import { useApp } from "@/store/app"
import { FunctionalModelField, FunctionalModels } from "./functional-models"

const settings = (): Settings => ({
  model_policy: { mode: "local", configurable: true, ready: true, scope: "gateway:owner", defaults: { translation: { provider_id: "gateway:owner", model: "server-translation" } } },
  models: { providers: [{ id: "own", label: "Personal", model: "chat", catalog: ["small"] }], bindings: { translation: { provider_id: "own", model: "chosen" } } },
} as Settings)
beforeEach(() => useApp.setState({ meta: undefined, models: [] }))

it("keeps explicit role choices and restores following the gateway default", () => {
  const onChange = vi.fn()
  const { rerender } = render(<FunctionalModelField role="translation" settings={settings()} onChange={onChange} />)
  const model = screen.getByLabelText("Translation Model")
  fireEvent.change(model, { target: { value: " replacement " } })
  fireEvent.blur(model)
  expect(onChange.mock.lastCall?.[0].models.bindings.translation).toEqual({ provider_id: "own", model: "replacement" })
  fireEvent.change(model, { target: { value: " " } })
  fireEvent.blur(model)
  expect(model).toHaveValue("chosen")
  fireEvent.click(screen.getByRole("button", { name: /^Use default$/ }))
  const restored = onChange.mock.lastCall![0] as Settings
  expect(restored.models.bindings).toEqual({})
  rerender(<FunctionalModelField role="translation" settings={restored} onChange={onChange} />)
  expect(screen.queryByLabelText("Translation Model")).toBeNull()
  expect(screen.getByText(/server-translation/)).toBeTruthy()
})

it("does not offer the background decisions role", () => {
  const value = settings()
  value.model_policy!.defaults = {
    ...value.model_policy!.defaults,
    decisions: { provider_id: "gateway:owner", model: "hidden-decider" },
  }
  render(<FunctionalModels settings={value} onChange={vi.fn()} />)
  expect(screen.queryByLabelText(/Decision assistant/)).toBeNull()
  expect(screen.queryByText("hidden-decider")).toBeNull()
  expect(screen.getByLabelText("Embedding model Provider")).toBeTruthy()
})

it("has no model controls for a managed preset", () => {
  const value = settings()
  value.model_policy!.configurable = false
  const { container } = render(<FunctionalModels settings={value} onChange={vi.fn()} />)
  expect(container).toBeEmptyDOMElement()
})

it("explains missing translation and image capabilities instead of hiding their rows", () => {
 const value = settings()
 value.model_policy!.mode = "gateway"
 render(<><FunctionalModels settings={value} onChange={vi.fn()} /><FunctionalModelField role="translation" settings={value} onChange={vi.fn()} /></>)
 expect(screen.getByText("Translation", { exact: true })).toBeInTheDocument()
 expect(screen.getByText("Image generation", { exact: true })).toBeInTheDocument()
 expect(screen.queryByRole("combobox", { name: "Image generation" })).toBeNull()
 expect(screen.getAllByText(/gateway has not provided an editable/)).toHaveLength(3)
 expect(screen.queryByLabelText("Image generation Provider")).toBeNull()
})

it("lists gateway, third-party and local models in separate groups", () => {
  const onChange = vi.fn()
  const value = settings()
  value.model_policy!.account_sources = {
    "gateway:origin": { authorized: true, available: true, configurable: true, capabilities: { translation: { supported: true, available: true, configurable: true, endpoints: ["chat_completions"] } } },
    "gateway:partner": { authorized: true, available: true, configurable: true, capabilities: { translation: { supported: true, available: true, configurable: true, endpoints: ["chat_completions"] } } },
  }
  useApp.setState({ models: [
    { id: "o", provider_id: "gateway:origin", provider_label: "Origin", label: "origin-chat", model: "origin-chat", ready: true, source: "account", catalog_group: "original" },
    { id: "p", provider_id: "gateway:partner", provider_label: "Partner", label: "partner-chat", model: "partner-chat", ready: true, source: "account", catalog_group: "third_party" },
    { id: "l", provider_id: "own", provider_label: "Personal", label: "chat", model: "chat", ready: true, source: "provider" },
  ] })
  render(<FunctionalModelField role="translation" settings={value} onChange={onChange} />)
  fireEvent.click(screen.getByLabelText("Translation Provider"))
  expect(screen.getByText("Gateway")).toBeInTheDocument()
  expect(screen.getByText("Partner")).toBeInTheDocument()
  expect(screen.queryByText("Third-party")).not.toBeInTheDocument()
  expect(screen.getByText("Local")).toBeInTheDocument()
  fireEvent.click(screen.getByRole("option", { name: "partner-chat" }))
  expect(onChange.mock.lastCall?.[0].models.bindings.translation).toEqual({ provider_id: "gateway:partner", model: "partner-chat" })
})

it("titles a third-party speech catalog with the gateway name", () => {
  const value = settings()
  const speech = { authorized: true, available: true, configurable: true, capabilities: { speech: { supported: true, available: true, configurable: true, endpoints: ["audio_speech"] } } }
  value.model_policy!.account_sources = { "gateway:origin": speech, "gateway:partner": speech }
  value.model_policy!.functional_catalogs = [
    { provider_id: "gateway:origin", label: "Origin", catalog_group: "original", speech_models: ["voxcpm2"] },
    { provider_id: "gateway:partner", label: "FOFA", catalog_group: "third_party", speech_models: ["speech"] },
  ]
  render(<FunctionalModelField role="speech" settings={value} onChange={vi.fn()} />)
  fireEvent.click(screen.getByLabelText("Spoken answers Provider"))
  expect(screen.getByText("Gateway")).toBeInTheDocument()
  expect(screen.getByText("FOFA")).toBeInTheDocument()
  expect(screen.queryByText("Third-party")).not.toBeInTheDocument()
  expect(screen.getByRole("option", { name: "voxcpm2" })).toBeInTheDocument()
  expect(screen.getByRole("option", { name: "speech" })).toBeInTheDocument()
})

it("clears the old search pin when disabling semantic search", () => {
 const value = settings()
 value.models.bindings = { embedding: { provider_id: "own", model: "embed" } }
 value.search = { embedding: true, embedding_provider: "own", embedding_model: "old-embed" }
 const onChange = vi.fn()
 render(<FunctionalModels settings={value} onChange={onChange} />)
 fireEvent.click(screen.getByLabelText("Embedding model Provider"))
 fireEvent.click(screen.getByRole("option", { name: "Not configured (off)" }))
 const next = onChange.mock.lastCall![0] as Settings
 expect(next.models.bindings?.embedding).toEqual({ provider_id: "", model: "" })
 expect(next.search).toEqual({ embedding: false, embedding_provider: "", embedding_model: "" })
})

it("clearing an embedding model disables search instead of restoring the name", () => {
 const value = settings()
 value.models.bindings = { embedding: { provider_id: "own", model: "embed" } }
 const onChange = vi.fn()
 render(<FunctionalModels settings={value} onChange={onChange} />)
 const input = screen.getByLabelText("Embedding model Model")
 fireEvent.change(input, { target: { value: " " } })
 fireEvent.blur(input)
 expect(onChange.mock.lastCall![0].models.bindings.embedding).toEqual({ provider_id: "", model: "" })
})

it("displays a legacy embedding pin even when its old switch was false", () => {
 const value = settings()
 value.search = { embedding: false, embedding_provider: "own", embedding_model: "legacy-embed" }
 render(<FunctionalModels settings={value} onChange={vi.fn()} />)
 expect(screen.getByLabelText("Embedding model Model")).toHaveValue("legacy-embed")
})

it("distinguishes a usable gateway-managed translation from an unavailable function", () => {
 const value = settings()
 value.model_policy!.mode = "gateway"
 value.model_policy!.capabilities = { authorized: true, available: true, configurable: true, capabilities: {
  translation: { supported: true, available: true, configurable: false, endpoints: ["chat_completions"] },
 } }
 render(<FunctionalModelField role="translation" settings={value} onChange={vi.fn()} />)
 expect(screen.getByText("Managed by gateway")).toBeInTheDocument()
 expect(screen.queryByLabelText("Translation Provider")).toBeNull()
})
