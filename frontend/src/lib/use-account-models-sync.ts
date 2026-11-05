import { useEffect } from "react"

import { refreshMetaAndModels } from "@/store/app-models"
import { useApp } from "@/store/app"

import { accountModelsStale } from "./account-models"
import { useDesktopAccount } from "./use-remote-status"

export const ACCOUNT_MODELS_POLL_MS = 1500
// Go retries a failed refresh on its own schedule; looking forever from here
// would only keep a quiet window busy.
export const ACCOUNT_MODELS_POLL_MAX = 20

/** Keeps meta and the model list in step with sign-in. Go refreshes the
 *  server's models after sign-in and sign-out finish, a moment after the
 *  account status changes, so look again until the two agree. */
export function useAccountModelsSync(): void {
  const { status, unavailable } = useDesktopAccount()
  const account = useApp((s) => s.meta?.account_models)
  const login = unavailable ? undefined : status?.login?.state
  const stale = accountModelsStale(login, account)
  useEffect(() => {
    if (!stale) return
    let attempts = 0
    const look = () => {
      attempts += 1
      void refreshMetaAndModels().catch(() => undefined)
    }
    look()
    const timer = setInterval(() => {
      if (attempts >= ACCOUNT_MODELS_POLL_MAX) {
        clearInterval(timer)
        return
      }
      look()
    }, ACCOUNT_MODELS_POLL_MS)
    return () => clearInterval(timer)
  }, [stale])
}
