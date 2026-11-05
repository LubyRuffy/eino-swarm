import { usePhoneAccount } from "@/components/gateway-account-shell"
import { useEffect, useState } from "react"
import { Camera } from "lucide-react"

import { ScanViewfinder } from "@/components/scan-viewfinder"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/input"
import { localeSwitchLabel, t } from "@/lib/i18n"
import { parseOffer } from "@/lib/offer"
import { primeScanChime } from "@/lib/scan-chime"

export function ScanScreen({
  onURI,
  busy,
  error,
  onToggleLocale,
  embedded,
  onAddModel,
}: {
  onURI: (uri: string) => void
  busy?: boolean
  error?: string
  onToggleLocale?: () => void
  embedded?: boolean
  onAddModel?: () => void
}) {
  const account = usePhoneAccount()
  const [paste, setPaste] = useState("")
  const [localError, setLocalError] = useState<string>()
  const [live, setLive] = useState(false)
  // A signed-out phone opens on the account: pairing by QR, a pasted URI and a
  // model service of one's own are still here, one tap away.
  const accountFirst = !embedded && Boolean(account.openAccount)
  const [other, setOther] = useState(false)

  const submitPaste = () => {
    setLocalError(undefined)
    try {
      parseOffer(paste)
      onURI(paste.trim())
    } catch (e) {
      setLocalError(e instanceof Error ? e.message : String(e))
    }
  }

  const openScanner = () => {
    if (busy) return
    setLocalError(undefined)
    primeScanChime()
    setLive(true)
  }

  const shown = error || localError
  useEffect(() => { if (shown) setOther(true) }, [shown])
  const showOther = !accountFirst || other

  return (
    <main
      className={
        embedded
          ? "mx-auto flex max-w-md flex-col gap-4 px-5 pb-6"
          : "mx-auto flex h-full max-w-md flex-col gap-5 overflow-y-auto px-5 py-8"
      }
    >
      {embedded ? null : (
        <header className="flex items-start justify-between gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{t(accountFirst ? "welcome.title" : "scan.title")}</h1>
          {onToggleLocale ? (
            <Button variant="ghost" onClick={onToggleLocale} aria-label={localeSwitchLabel()}>
              {localeSwitchLabel()}
            </Button>
          ) : null}
        </header>
      )}
      {accountFirst ? (
        <>
          <p className="text-sm leading-relaxed text-muted-foreground">{t("welcome.hint")}</p>
          <Button onClick={() => account.openAccount?.()}>{t("account.login")}</Button>
          <Button
            type="button"
            variant="ghost"
            aria-expanded={showOther}
            aria-controls="welcome-other-ways"
            onClick={() => setOther((value) => !value)}
          >
            {t("welcome.other")}
          </Button>
        </>
      ) : null}
      {showOther ? (
        <section id="welcome-other-ways" className={embedded ? "flex flex-col gap-4" : "flex flex-col gap-5"}>
          {accountFirst ? <p className="text-sm leading-relaxed text-muted-foreground">{t("welcome.otherHint")}</p> : null}
          <p className="text-sm leading-relaxed text-muted-foreground">{t("scan.hint")}</p>
          <Button type="button" onClick={openScanner} disabled={busy} aria-label={t("scan.camera")}>
            <Camera className="size-4" />
            {t("scan.camera")}
          </Button>
          <label className="flex flex-col gap-2 text-sm text-muted-foreground">
            {t("scan.uri")}
            <Textarea
              aria-label={t("scan.uri")}
              value={paste}
              onChange={(e) => setPaste(e.target.value)}
              placeholder="pairlink:v1:…"
              spellCheck={false}
            />
          </label>
          <Button variant="outline" onClick={submitPaste} disabled={busy}>
            {t("scan.paste")}
          </Button>
          {onAddModel && !embedded ? (
            <Button type="button" variant="outline" onClick={onAddModel}>
              {t("chat.connect")}
            </Button>
          ) : null}
        </section>
      ) : null}
      {shown ? (
        <p className="text-sm text-destructive" role="alert">
          {shown}
        </p>
      ) : null}
      {live ? (
        <ScanViewfinder
          onClose={() => setLive(false)}
          onURI={(uri) => {
            setPaste(uri)
            setLive(false)
            onURI(uri)
          }}
          onError={(message) => {
            setLive(false)
            setLocalError(message)
          }}
        />
      ) : null}
    </main>
  )
}
