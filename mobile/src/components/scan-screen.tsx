import { useState } from "react"
import { Camera } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/input"
import { parseOffer } from "@/lib/offer"
import { scanPairlinkURI } from "@/lib/scan"

export function ScanScreen({
  onURI,
  busy,
  error,
}: {
  onURI: (uri: string) => void
  busy?: boolean
  error?: string
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
    <main className="mx-auto flex max-w-md flex-col gap-4 p-6">
      <h1 className="text-xl font-semibold">扫码绑定这台 PC</h1>
      <p className="text-sm text-muted-foreground">
        打开 zwai Settings → Phone 的配对 QR。摄像头是产品路径；没有摄像头再粘贴同一条
        URI。
      </p>
      <Button
        onClick={() => void scan()}
        disabled={busy}
        aria-label="Scan QR"
      >
        <Camera className="size-4" />
        Scan QR
      </Button>
      <label className="flex flex-col gap-2 text-sm">
        Pairing URI
        <Textarea
          aria-label="Pairing URI"
          value={paste}
          onChange={(e) => setPaste(e.target.value)}
          placeholder="pairlink:v1:…"
          spellCheck={false}
        />
      </label>
      <Button variant="outline" onClick={submitPaste} disabled={busy}>
        Paste and bind
      </Button>
      {shown ? (
        <p className="text-sm text-destructive" role="alert">
          {shown}
        </p>
      ) : null}
    </main>
  )
}
