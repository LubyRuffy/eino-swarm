import { useState } from "react"

import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { accountModelsProblem, showWelcome } from "@/lib/account-models"
import { toastError, errorMessage } from "@/store/toasts"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { dismissWelcome } from "@/store/app-models"
import { openSettings } from "@/store/settings-sheet"

import { AccountSignIn } from "./account-sign-in"

/** First run with nothing to run: lead with the server account, keep the
 *  person's own model service one click away, and never trap them. Signing in
 *  or configuring a model hides it by itself; "Not now" is remembered. */
export function WelcomeGate() {
  const open = useApp((s) => showWelcome(s.meta))
  return open ? <WelcomeDialog /> : null
}

function WelcomeDialog() {
  const t = useT()
  const account = useApp((s) => s.meta?.account_models)
  const [saving, setSaving] = useState(false)
  const problem = accountModelsProblem(account)

  // Hiding is a persisted answer: either choice below settles the question.
  const answer = (after?: () => void) => {
    setSaving(true)
    dismissWelcome()
      .then(after)
      .catch((error) => toastError(errorMessage(error)))
      .finally(() => setSaving(false))
  }

  return (
    <Dialog open onOpenChange={(next) => { if (!next && !saving) answer() }}>
      <DialogContent data-testid="welcome-gate" className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("welcome.title")}</DialogTitle>
          <DialogDescription>{t("welcome.desc")}</DialogDescription>
        </DialogHeader>
        <AccountSignIn />
        {problem ? <p role="status" className="text-sm text-muted-foreground">{t(problem)}</p> : null}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Button type="button" variant="outline" size="sm" disabled={saving} onClick={() => answer(() => openSettings("models"))}>
            {t("welcome.own")}
          </Button>
          <Button type="button" variant="ghost" size="sm" disabled={saving} onClick={() => answer()}>
            {t("welcome.later")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
