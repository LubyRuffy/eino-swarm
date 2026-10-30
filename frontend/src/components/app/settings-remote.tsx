import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { SelectItem } from "@/components/ui/select"
import { api } from "@/lib/api"
import type {
  RemoteBinding,
  RemoteOffer,
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
  SettingsChoice,
  settingsHintClass,
  settingsLabelClass,
} from "./settings-field"
import { RemoteLoginSection } from "./settings-remote-login"

const remoteToastId = "settings:remote"

/** While the pairing plate is up, a phone can bind on the hub without this
 *  page remounting. Leaving Phone was the only refresh. */
export const BINDING_WATCH_MS = 1000

type RemoteTabProps = {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
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
}: RemoteTabProps) {
  const t = useT()
  const remote = defaultRemoteSettings(settings.remote)
  const update = (patch: Partial<typeof remote>) =>
    onChange({ ...settings, remote: { ...remote, ...patch } })

  const connection = useRemoteStatus(remote)
  const { status } = connection
  const [offer, setOffer] = useState<RemoteOffer>()
  const [bindings, setBindings] = useState<RemoteBinding[]>([])
  const [busy, setBusy] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const alive = useRef(true)
  const nosec = remote.auth_mode === "nosec"
  const canPair = !nosec || (!!status?.online && status.login?.state === "authorized" && !connection.unavailable)
  const pairingEpoch = useRef(0)
  const pairingAllowed = useRef(canPair)
  pairingAllowed.current = canPair

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

  useEffect(() => {
    if (!canPair) { pairingEpoch.current++; setOffer(undefined) }
  }, [canPair])

  useEffect(() => {
    if (!offer?.expires_at) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [offer?.expires_at])

  useEffect(() => {
    if (!offer) return
    let stopped = false
    const id = window.setInterval(() => {
      if (document.hidden) return
      void api
        .remoteBindings()
        .then((list) => {
          if (!stopped) setBindings(list)
        })
        .catch(() => {})
    }, BINDING_WATCH_MS)
    return () => {
      stopped = true
      window.clearInterval(id)
    }
  }, [offer])

  const remaining = offer?.expires_at
    ? Math.max(0, Math.round((new Date(offer.expires_at).getTime() - now) / 1000))
    : 0

  const showQr = async () => {
    if (!canPair) return
    const owner = pairingEpoch.current
    setBusy(true)
    clearFail()
    try {
      const next = await api.remoteOffer()
      if (!alive.current || !pairingAllowed.current || owner !== pairingEpoch.current) return
      setOffer(next)
      reload()
    } catch (e) {
      if (alive.current) { setOffer(undefined); fail(e) }
    } finally {
      if (alive.current) setBusy(false)
    }
  }

  const login = (newDevice: boolean) => {
    clearFail()
    void connection.perform(() => api.startRemoteLogin(newDevice)).catch(fail)
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
      description={t("settings.remote.desc")}
    >
      <SettingsSection title={t("settings.remote.hub")}>
        <Field
          query={query}
          label={t("settings.remote.enabled")}
          hint={t("settings.remote.enabledHint")}
        >
          <Switch
            checked={remote.enabled}
            onCheckedChange={(v) => update({ enabled: v })}
          />
        </Field>
        <SettingsChoice
          query={query}
          label={t("settings.remote.authMode")}
          hint={t("settings.remote.authModeHint")}
          value={remote.auth_mode ?? "legacy"}
          onValueChange={(value) => update({ auth_mode: value as "legacy" | "nosec" })}
        >
          <SelectItem value="legacy">{t("settings.remote.authLegacy")}</SelectItem>
          <SelectItem value="nosec">{t("settings.remote.authNOSEC")}</SelectItem>
        </SettingsChoice>
        <Field
          query={query}
          label={t("settings.remote.hubUrl")}
          hint={t("settings.remote.hubUrlHint")}
          wide
        >
          <Input
            value={remote.hub_url}
            onChange={(e) => update({ hub_url: e.target.value })}
            autoComplete="off"
            spellCheck={false}
            placeholder="https://"
          />
        </Field>
        <Field
          query={query}
          label={t("settings.remote.displayName")}
          hint={t("settings.remote.displayNameHint")}
          wide
        >
          <Input
            value={remote.display_name}
            onChange={(e) => update({ display_name: e.target.value })}
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.remote.threadLimit")}
          hint={t("settings.remote.threadLimitHint")}
        >
          <Input
            type="number"
            min={1}
            value={remote.thread_limit}
            onChange={(e) => update({ thread_limit: Number(e.target.value) })}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.remote.eventChars")}
          hint={t("settings.remote.eventCharsHint")}
        >
          <Input
            type="number"
            min={1}
            value={remote.event_chars}
            onChange={(e) => update({ event_chars: Number(e.target.value) })}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.remote.watchEvents")}
          hint={t("settings.remote.watchEventsHint")}
        >
          <Input
            type="number"
            min={1}
            value={remote.watch_events}
            onChange={(e) => update({ watch_events: Number(e.target.value) })}
          />
        </Field>
      </SettingsSection>

      {nosec ? <RemoteLoginSection
        enabled={remote.enabled}
        status={status}
        unavailable={connection.unavailable}
        busy={connection.busy}
        onStart={login}
        onCancel={cancelLogin}
      /> : null}

      <SettingsSection
        title={t("settings.remote.qr")}
        description={
          status?.online
            ? t("settings.remote.online", { fp: status.fingerprint ?? "" })
            : t("settings.remote.offline")
        }
        action={
          <Button
            type="button"
            size="sm"
            disabled={busy || connection.busy || !canPair}
            onClick={() => void showQr()}
          >
            {t("settings.remote.showQr")}
          </Button>
        }
      >
        {offer?.png && canPair ? (
          <div className="flex flex-col items-center gap-3 px-4 py-5" data-settings-row="">
            <div className="bg-qr-plate p-3">
              <img
                src={offer.png}
                alt={t("settings.remote.qrAlt")}
                data-testid="remote-qr"
                className="size-64"
              />
            </div>
            <p className="text-xs text-muted-foreground">
              {t("settings.remote.expires", { s: remaining })}
            </p>
            <label className="sr-only" htmlFor="remote-offer-uri">
              {t("settings.remote.uri")}
            </label>
            <Input
              id="remote-offer-uri"
              readOnly
              value={offer.uri}
              className="font-mono text-xs"
            />
          </div>
        ) : (
          <p
            className={cn(chromeTypeClass, "px-4 py-2.5 text-muted-foreground")}
            data-settings-row=""
          >
            {t("settings.remote.qrEmpty")}
          </p>
        )}
      </SettingsSection>

      <SettingsSection title={t("settings.remote.devices")}>
        {bindings.length === 0 || !canPair ? (
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
    </SettingsPage>
  )
}
