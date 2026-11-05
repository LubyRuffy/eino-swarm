import { useAccountModelsSync } from "@/lib/use-account-models-sync"

/** Renders nothing; keeps the model list in step with sign-in. */
export function AccountModelsSync() {
  useAccountModelsSync()
  return null
}
