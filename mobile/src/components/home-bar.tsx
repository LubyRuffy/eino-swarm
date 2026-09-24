import { Search, SquarePen, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { t } from "@/lib/i18n"

/** The inbox rests on find-and-start, not on a message box: typing here
 *  looks through what this PC already has, and the button is the only way a
 *  conversation begins. */
export function HomeBar({
  query,
  onQuery,
  onNewChat,
  disabled,
}: {
  query: string
  onQuery: (q: string) => void
  onNewChat: () => void
  disabled?: boolean
}) {
  return (
    <div className="shrink-0 border-t border-border bg-background px-3 pb-2 pt-1.5">
      <div className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Search
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
          <Input
            type="search"
            aria-label={t("home.search")}
            placeholder={t("home.search")}
            value={query}
            className="h-9 rounded-full bg-muted pl-9 pr-10"
            onChange={(e) => onQuery(e.target.value)}
          />
          {query ? (
            <button
              type="button"
              aria-label={t("home.searchClear")}
              className="absolute right-1 top-1/2 flex size-9 -translate-y-1/2 items-center justify-center rounded-full text-muted-foreground active:bg-accent"
              onClick={() => onQuery("")}
            >
              <X className="size-4" aria-hidden />
            </button>
          ) : null}
        </div>
        <Button
          type="button"
          data-testid="new-chat"
          className="h-9 shrink-0 gap-1.5 rounded-full px-3.5"
          disabled={disabled}
          onClick={onNewChat}
        >
          <SquarePen className="size-4" aria-hidden />
          {t("home.newChat")}
        </Button>
      </div>
    </div>
  )
}
