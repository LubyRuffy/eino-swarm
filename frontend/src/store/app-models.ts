import { api } from "@/lib/api"
import type { Meta } from "@/lib/types"

import { useApp } from "./app"

let generation = 0

/** Reload meta and the model list together. A slow earlier answer must not
 *  overwrite a newer one: after sign-out it would bring the server's models
 *  back into the picker. */
export async function refreshMetaAndModels(): Promise<void> {
  const mine = ++generation
  const [meta, listed] = await Promise.all([api.meta(), api.models()])
  if (mine !== generation) return
  useApp.setState({ meta, models: listed.models })
}

/** Remember that the first-run welcome was answered. Only this field is sent,
 *  so nothing else in the settings is touched. */
export async function dismissWelcome(): Promise<void> {
  await api.saveSettings({ ui: { welcome_dismissed: true } })
  useApp.setState((state) => state.meta
    ? { meta: { ...state.meta, ui: { ...(state.meta.ui as Meta["ui"]), welcome_dismissed: true } as Meta["ui"] } }
    : {})
}
