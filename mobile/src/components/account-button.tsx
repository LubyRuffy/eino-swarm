import { Settings } from "lucide-react"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"

export function AccountButton({ onClick }: { onClick: () => void }) {
  return (
    <Button type="button" variant="ghost" className="size-9 shrink-0 px-0" aria-label={t("account.title")} onClick={() => onClick()}>
      <Settings className="size-5" aria-hidden />
    </Button>
  )
}
