import { capabilityAllows, capabilityConfigurable, type SourceCapabilities } from "./capabilities"
/** The gateway supplies permission and presets; clients never infer a login channel. */
export type ModelRef = { provider_id: string; model: string }
export type ModelRole = "conversation" | "title" | "compact" | "translation" | "image" | "embedding" | "transcription" | "speech" | "ocr" | "decisions"
export type ModelPolicy = {
  account_sources?: Record<string, SourceCapabilities | undefined>
  capabilities?: SourceCapabilities
  selection_translation?: boolean
  capability_status?: "ready" | "unavailable"
  mode: "local" | "gateway"
  configurable: boolean
  ready: boolean
  scope: string
  defaults?: Partial<Record<ModelRole, ModelRef>>
  bindings?: Partial<Record<ModelRole | "composer_prediction", ModelRef>>
  roles?: { id: string; enabled: boolean; label: string }[]
  /** Typed names omitted from the conversation list, one entry per signed-in catalog. */
  functional_catalogs?: FunctionalCatalog[]
}

export interface FunctionalCatalog {
  provider_id: string
  label?: string
  catalog_group?: string
  embedding_models?: string[]
  image_models?: string[]
  transcription_models?: string[]
  speech_models?: string[]
}

export const modelRoles: ModelRole[] = ["conversation", "title", "compact", "translation", "image", "embedding", "transcription", "speech", "ocr", "decisions"]

/** Roles a person can assign. Decisions stays a background call. */
export const configurableModelRoles = modelRoles.filter((role): role is Exclude<ModelRole, "decisions"> => role !== "decisions")

/** An absent gateway manifest is unresolved, including with an older host. */
export function policyAllows(policy: ModelPolicy | undefined, role: ModelRole): boolean {
  return policy?.mode === "local" || capabilityAllows(policy?.capabilities, role)
}
export function policyConfigurable(policy: ModelPolicy | undefined, role: ModelRole): boolean {
  return policy?.mode === "local" || Boolean(policy?.configurable && capabilityConfigurable(policy.capabilities, role))
}

export function selectedFeatureAllows(policy: ModelPolicy | undefined, providerId: string | undefined, role: ModelRole): boolean {
  if (policy?.mode === "gateway") return policyAllows(policy, role)
  if (providerId?.startsWith("gateway:")) return capabilityAllows(policy?.account_sources?.[providerId], role)
  return policy?.mode === "local"
}

export function selectionTranslationAvailable(policy: ModelPolicy | undefined, providerId: string | undefined): boolean {
  return policy?.selection_translation !== false && selectedFeatureAllows(policy, providerId, "translation")
}
