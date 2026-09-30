import { AlertTriangle } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  isOutputBudgetError,
  MAX_COMPLETION_SETTINGS_KEY,
} from "@/lib/output-budget"
import { useT } from "@/lib/use-t"
import { openSettings } from "@/store/settings-sheet"

/** A failed turn. The output-budget sentence is the one a desktop client
 *  can fix in Settings, so that row grows a hint and a link. Other errors
 *  stay the server text: a phone cannot edit this cap, and a random
 *  failure has nowhere to send the user. */
export function TranscriptError({ text }: { text: string }) {
  const t = useT()
  const budget = isOutputBudgetError(text)
  return (
    <div
      className="my-2 flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
      data-testid={budget ? "output-budget-error" : undefined}
    >
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">
        <p className="stream-text">{text}</p>
        {budget ? (
          <p className="mt-1">
            {t("transcript.outputBudgetHint")}{" "}
            <Button
              type="button"
              variant="link"
              className="h-auto px-0 py-0 text-sm text-destructive underline underline-offset-4"
              onClick={() => openSettings("swarm", MAX_COMPLETION_SETTINGS_KEY)}
            >
              {t("transcript.outputBudgetSettings")}
            </Button>
          </p>
        ) : null}
      </div>
    </div>
  )
}
