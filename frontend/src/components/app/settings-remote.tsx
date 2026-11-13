import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import type {
  RemoteBinding,
  RemoteStatus,
  Settings,
} from "@/lib/types"
import {
  bindingFingerprint,
  bindingTitle,
  bindingUsesLastSeen,
  bindingWhen,
} from "@/lib/remote-binding"
import { chromeTypeClass } from "@/lib/chrome-type"
import { defaultRemoteSettings } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import { useRemoteStatus } from "@/lib/use-remote-status"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

import {
  Field,
  SettingsPage,
  SettingsSection,
  settingsHintClass,
  settingsLabelClass,
} from "./settings-field"
import { RemoteLoginSection } from "./settings-remote-login"

const remoteToastId = "settings:remote"

type RemoteTabProps = {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
  /** The owning sheet flushes old drafts and adopts the saved account receipt. */
  onAccountLogin?: (newDevice: boolean) => Promise<RemoteStatus>
}

export function RemoteTab(props: RemoteTabProps) {
  const remote = defaultRemoteSettings(props.settings.remote)
  const authority = JSON.stringify([remote.enabled, remote.hub_url, remote.auth_mode])
  return <RemoteTabBody key={authority} {...props} />
}

function RemoteTabBody({
  settings,
  onChange,
  query = "",
  onAccountLogin,
}: RemoteTabProps) {
  const t = useT()
  const remote = defaultRemoteSettings(settings.remote)
  const update = (patch: Partial<typeof remote>) =>
    onChange({ ...settings, remote: { ...remote, ...patch } })

  const connection = useRemoteStatus(remote)
  const { status } = connection
  const [bindings, setBindings] = useState<RemoteBinding[]>([])
  const [busy, setBusy] = useState(false)
  const alive = useRef(true)

  const fail = (e: unknown) =>
    toastError(errorMessage(e), {
      id: remoteToastId,
      title: t("settings.remote.failed"),
    })
  const clearFail = () => useToasts.getState().dismiss(remoteToastId)

  const reload = () => {
    void connection.refresh()
    void api
      .remoteBindings()
      .then((list) => { if (alive.current) setBindings(list) })
      .catch(() => {})
  }

  useEffect(() => {
    alive.current = true
    reload()
    return () => { alive.current = false }
  }, [])

  const login = (newDevice: boolean) => {
    clearFail()
    void connection.perform(() => onAccountLogin ? onAccountLogin(newDevice) : api.startRemoteLogin(newDevice)).catch(fail)
  }
  const cancelLogin = () => {
    clearFail()
    void connection.perform(api.cancelRemoteLogin).catch(fail)
  }

  const revoke = async (id: string) => {
    setBusy(true)
    try {
      await api.revokeRemoteBinding(id)
      reload()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <SettingsPage
      title={t("settings.remote.title")}
      description={t("settings.remote.accountDesc")}
    >
      <p className={cn(settingsHintClass, "-mt-5")}>{t("settings.remote.accountSetup")}</p>
      <RemoteLoginSection
        accountEntry
        enabled={remote.enabled}
        status={status}
        unavailable={connection.unavailable}
        busy={connection.busy}
        onStart={login}
        onCancel={cancelLogin}
        onConnectionChange={(enabled) => update({ enabled })}
        query={query}
      />

      {remote.enabled || bindings.length > 0 ? (
      <SettingsSection title={t("settings.remote.devices")}>
        {bindings.length === 0 ? (
          <p
            className={cn(chromeTypeClass, "px-4 py-2.5 text-muted-foreground")}
            data-settings-row=""
          >
            {t("settings.remote.noDevices")}
          </p>
        ) : (
          bindings.map((b) => (
            <div
              key={b.id}
              className="flex items-center justify-between gap-3 px-4 py-2.5"
              data-settings-row=""
            >
              <div className="min-w-0">
                <p
                  className={cn(
                    settingsLabelClass,
                    "truncate",
                    !b.device?.trim() && "font-mono",
                  )}
                >
                  {bindingTitle(b)}
                </p>
                <p className={cn(settingsHintClass, "truncate")}>
                  {bindingFingerprint(b)
                    ? `${bindingFingerprint(b)} · `
                    : ""}
                  {bindingUsesLastSeen(b)
                    ? t("settings.remote.lastSeen", { time: bindingWhen(b) })
                    : bindingWhen(b)}
                </p>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={busy}
                onClick={() => void revoke(b.id)}
              >
                {t("settings.remote.revoke")}
              </Button>
            </div>
          ))
        )}
      </SettingsSection>
      ) : null}

      {remote.enabled ? (
        <SettingsSection title={t("settings.remote.other")}>
          <Field
            query={query}
            label={t("settings.remote.keepAwake")}
            hint={t("settings.remote.keepAwakeHint")}
          >
            <Switch
              checked={remote.keep_awake}
              onCheckedChange={(v) => update({ keep_awake: v })}
            />
          </Field>
        </SettingsSection>
      ) : null}
    </SettingsPage>
  )
}
