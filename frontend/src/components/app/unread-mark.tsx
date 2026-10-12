import { useT } from "@/lib/use-t"

/** A finished result needs attention without suggesting that work is running. */
export function UnreadMark({ count, testId }: { count?: number; testId?: string }) {
  const t = useT()
  const label = count == null ? t("status.unread") : t(
    count === 1 ? "sidebar.unreadOne" : "sidebar.unreadMany", { count },
  )
  return <span role="img" aria-label={label} title={label} data-testid={testId}
    className="block size-1.5 shrink-0 rounded-full bg-done leading-none" />
}
