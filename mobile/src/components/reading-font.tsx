import { CaseLower, CaseUpper, RotateCcw } from "lucide-react"
import { useCallback, useState } from "react"

import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"

const MIN_SIZE = 12
const MAX_SIZE = 28
const STEP = 2

function boundedSize(size: number) {
  return Math.max(MIN_SIZE, Math.min(MAX_SIZE, Math.round(size)))
}

export function useReadingFont(defaultSize: number) {
  const [size, setValue] = useState(defaultSize)
  const setSize = useCallback((next: number) => {
    const bounded = boundedSize(next)
    setValue(bounded)
    return bounded
  }, [])
  const reset = useCallback(() => setValue(defaultSize), [defaultSize])
  return {
    size,
    setSize,
    decrease: () => setValue((current) => boundedSize(current - STEP)),
    increase: () => setValue((current) => boundedSize(current + STEP)),
    reset,
    canDecrease: size > MIN_SIZE,
    canIncrease: size < MAX_SIZE,
    canReset: size !== defaultSize,
  }
}

export function ReadingFontControls({ font }: { font: ReturnType<typeof useReadingFont> }) {
  return (
    <div role="group" aria-label={t("files.textControls")} className="flex shrink-0 flex-wrap items-center justify-center gap-1">
      <Button type="button" variant="ghost" className="size-11 px-0" aria-label={t("files.textSmaller")}
        disabled={!font.canDecrease} onClick={font.decrease}><CaseLower className="size-5" /></Button>
      <output aria-label={t("files.textSize")} className="min-w-14 text-center text-xs tabular-nums text-muted-foreground">{font.size} px</output>
      <Button type="button" variant="ghost" className="size-11 px-0" aria-label={t("files.textLarger")}
        disabled={!font.canIncrease} onClick={font.increase}><CaseUpper className="size-5" /></Button>
      <Button type="button" variant="ghost" className="h-11 gap-1.5 px-3" aria-label={t("files.textReset")}
        disabled={!font.canReset} onClick={font.reset}><RotateCcw className="size-4" />{t("files.textReset")}</Button>
    </div>
  )
}
