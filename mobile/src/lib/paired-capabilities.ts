import { useSyncExternalStore } from "react"
import { selectionTranslationAvailable, selectedFeatureAllows, type ModelPolicy, type ModelRole } from "../../../frontend/src/lib/model-policy"
import type { RemoteLink } from "./link"

const policies = new WeakMap<RemoteLink, ModelPolicy>()
const translationPreferences = new WeakMap<RemoteLink, boolean>()
const listeners = new Set<() => void>()
let revision = 0
export function publishPairedCapabilities(link: RemoteLink, policy: ModelPolicy | undefined): void {
  if (policy) policies.set(link, policy)
  else policies.delete(link)
  revision++
  for (const listener of listeners) listener()
}
export function publishPairedSelectionTranslation(link: RemoteLink, enabled: boolean | undefined): void {
  if (translationPreferences.get(link) === enabled) return
  if (typeof enabled === "boolean") translationPreferences.set(link, enabled)
  else translationPreferences.delete(link)
  revision++
  for (const listener of listeners) listener()
}

export function usePairedFeature(link: RemoteLink | undefined, providerID: string | undefined, role: ModelRole): boolean {
  useSyncExternalStore(listener => { listeners.add(listener); return () => { listeners.delete(listener) } }, () => revision)
  if (!link) return false
  const policy = policies.get(link)
  if (role === "translation") {
    const enabled = translationPreferences.get(link)
    return enabled === undefined ? selectionTranslationAvailable(policy, providerID) : enabled && selectedFeatureAllows(policy, providerID, role)
  }
  return selectedFeatureAllows(policy, providerID, role)
}
