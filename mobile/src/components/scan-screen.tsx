import { useState } from "react"
import { Camera } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/input"
import { localeSwitchLabel, t } from "@/lib/i18n"
import { parseOffer } from "@/lib/offer"
import { scanPairlinkURI } from "@/lib/scan"

export function ScanScreen({
  onURI,
  busy,
  error,
  onToggleLocale,
  embedded,
}: {
  onURI: (uri: string) => void
  busy?: boolean
  error?: string
  onToggleLocale?: () => void
  embedded?: boolean
}) {
  const [paste, setPaste] = useState("")
  const [localError, setLocalError] = useState<string>()

  const submitPaste = () => {
    setLocalError(undefined)
    try {
      parseOffer(paste)
      onURI(paste.trim())
    } catch (e) {
      setLocalError(e instanceof Error ? e.message : String(e))
    }
  }

  const scan = async () => {
    setLocalError(undefined)
    try {
      const uri = await scanPairlinkURI()
      setPaste(uri)
      onURI(uri)
    } catch (e) {
      setLocalError(e instanceof Error ? e.message : String(e))
    }
  }

  const shown = error || localError

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
          <h1 className="text-2xl font-semibold tracking-tight">{t("scan.title")}</h1>
          {onToggleLocale ? (
            <Button variant="ghost" onClick={onToggleLocale} aria-label={localeSwitchLabel()}>
              {localeSwitchLabel()}
            </Button>
          ) : null}
        </header>
      )}
      <p className="text-sm leading-relaxed text-muted-foreground">{t("scan.hint")}</p>
      <Button onClick={() => void scan()} disabled={busy} aria-label={t("scan.camera")}>
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
      {shown ? (
        <p className="text-sm text-destructive" role="alert">
          {shown}
        </p>
      ) : null}
    </main>
  )
}
