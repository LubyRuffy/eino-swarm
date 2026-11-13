import { useState } from "react"

import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { accountModelsProblem, showLoginGate, showWelcome } from "@/lib/account-models"
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
  const meta = useApp((s) => s.meta)
  if (showLoginGate(meta)) return <LoginRequired />
  return showWelcome(meta) ? <WelcomeDialog /> : null
}

function LoginRequired() {
  const t = useT()
  return (
    <div
      className="absolute inset-0 z-30 flex items-center justify-center bg-background p-6"
      data-testid="login-required-screen"
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="login-required-title"
        data-testid="login-required"
        className="grid w-full max-w-lg gap-4 rounded-lg border bg-background p-6 shadow-lg"
      >
        <h2 id="login-required-title" className="text-lg font-semibold">{t("welcome.title")}</h2>
        <p className="text-(length:--chrome-font-size) text-muted-foreground">{t("welcome.desc")}</p>
        <AccountSignIn />
      </div>
    </div>
  )
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
        {problem ? <p role="status" className="text-(length:--chrome-font-size) text-muted-foreground">{t(problem)}</p> : null}
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
