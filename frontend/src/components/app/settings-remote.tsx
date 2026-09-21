import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import type {
  RemoteBinding,
  RemoteOffer,
  RemoteStatus,
  Settings,
} from "@/lib/types"
import {
  bindingFingerprint,
  bindingTitle,
  bindingUsesLastSeen,
  bindingWhen,
} from "@/lib/remote-binding"
import { defaultRemoteSettings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

import { Field, SettingsPage, SettingsSection } from "./settings-field"

const remoteToastId = "settings:remote"

export function RemoteTab({
  settings,
  onChange,
  query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const remote = defaultRemoteSettings(settings.remote)
  const update = (patch: Partial<typeof remote>) =>
    onChange({ ...settings, remote: { ...remote, ...patch } })

  const [status, setStatus] = useState<RemoteStatus>()
  const [offer, setOffer] = useState<RemoteOffer>()
  const [bindings, setBindings] = useState<RemoteBinding[]>([])
  const [busy, setBusy] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  const fail = (e: unknown) =>
    toastError(errorMessage(e), {
      id: remoteToastId,
      title: t("settings.remote.failed"),
    })
  const clearFail = () => useToasts.getState().dismiss(remoteToastId)

  const reload = () => {
    void api.remoteStatus().then(setStatus).catch(fail)
    void api
      .remoteBindings()
      .then(setBindings)
      .catch(() => setBindings([]))
  }

  useEffect(() => {
    reload()
  }, [])

  useEffect(() => {
    if (!offer?.expires_at) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [offer?.expires_at])

  const remaining = offer?.expires_at
    ? Math.max(0, Math.round((new Date(offer.expires_at).getTime() - now) / 1000))
    : 0

  const showQr = async () => {
    setBusy(true)
    clearFail()
    try {
      const next = await api.remoteOffer()
      setOffer(next)
      reload()
    } catch (e) {
      setOffer(undefined)
      fail(e)
    } finally {
      setBusy(false)
    }
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
            disabled={busy}
            onClick={() => void showQr()}
          >
            {t("settings.remote.showQr")}
          </Button>
        }
      >
        {offer?.png ? (
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
          <p className="px-4 py-2.5 text-sm text-muted-foreground" data-settings-row="">
            {t("settings.remote.qrEmpty")}
          </p>
        )}
      </SettingsSection>

      <SettingsSection title={t("settings.remote.devices")}>
        {bindings.length === 0 ? (
          <p className="px-4 py-2.5 text-sm text-muted-foreground" data-settings-row="">
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
                <p className={b.device?.trim() ? "truncate text-sm" : "truncate font-mono text-sm"}>
                  {bindingTitle(b)}
                </p>
                <p className="truncate text-xs text-muted-foreground">
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
