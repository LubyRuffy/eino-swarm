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
import { defaultRemoteSettings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import { Field, SettingsPage, SettingsSection } from "./settings-field"

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
  const [token, setToken] = useState("")
  const [offer, setOffer] = useState<RemoteOffer>()
  const [bindings, setBindings] = useState<RemoteBinding[]>([])
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  const reload = () => {
    void api
      .remoteStatus()
      .then(setStatus)
      .catch((e) => setError(e instanceof Error ? e.message : String(e)))
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
    setError(undefined)
    try {
      const next = await api.remoteOffer()
      setOffer(next)
      reload()
    } catch (e) {
      setOffer(undefined)
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const storeToken = async () => {
    setBusy(true)
    setError(undefined)
    try {
      const st = await api.saveRemoteToken(token.trim())
      setStatus(st)
      setToken("")
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
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
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <SettingsPage
      title={t("settings.remote.title")}
      description={t("settings.remote.desc")}
    >
      {error ? (
        <p className="text-sm text-destructive" role="alert">
          {error}
        </p>
      ) : null}

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
        >
          <Input
            value={remote.hub_url}
            onChange={(e) => update({ hub_url: e.target.value })}
            autoComplete="off"
            spellCheck={false}
            placeholder="https://"
          />
        </Field>
        <div
          className="flex items-start justify-between gap-6 px-4 py-3.5"
          data-settings-row=""
        >
          <div className="min-w-0 flex-1">
            <label htmlFor="remote-host-token" className="text-sm font-medium leading-none">
              {t("settings.remote.token")}
            </label>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {status?.has_token
                ? t("settings.remote.tokenSet")
                : t("settings.remote.tokenHint")}
            </p>
          </div>
          <div className="flex w-56 shrink-0 gap-2">
            <Input
              id="remote-host-token"
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              autoComplete="off"
              spellCheck={false}
            />
            <Button type="button" size="sm" disabled={busy} onClick={() => void storeToken()}>
              {t("settings.remote.storeToken")}
            </Button>
          </div>
        </div>
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
          <p className="px-4 py-3 text-sm text-muted-foreground" data-settings-row="">
            {t("settings.remote.qrEmpty")}
          </p>
        )}
      </SettingsSection>

      <SettingsSection title={t("settings.remote.devices")}>
        {bindings.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted-foreground" data-settings-row="">
            {t("settings.remote.noDevices")}
          </p>
        ) : (
          bindings.map((b) => (
            <div
              key={b.id}
              className="flex items-center justify-between gap-3 px-4 py-3"
              data-settings-row=""
            >
              <div className="min-w-0">
                <p className="font-mono text-sm">{b.device_fp}</p>
                <p className="text-xs text-muted-foreground">{b.created_at}</p>
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
    </SettingsPage>
  )
}
