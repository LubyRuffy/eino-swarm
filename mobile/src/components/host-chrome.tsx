import { Menu, Monitor, SquarePen } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/cn"
import { localeSwitchLabel, t } from "@/lib/i18n"
import { linkLabel, type SavedLink } from "@/lib/store"

export function HostChrome({
  hosts,
  activeFingerprint,
  path,
  connected = true,
  reconnecting = false,
  onSelect,
  onAdd,
  onNewChat,
  onUnlink,
  onToggleLocale,
}: {
  hosts: SavedLink[]
  activeFingerprint: string
  path: string
  connected?: boolean
  reconnecting?: boolean
  onSelect: (fingerprint: string) => void
  onAdd: () => void
  onNewChat: () => void
  onUnlink: () => void
  onToggleLocale?: () => void
}) {
  const [menu, setMenu] = useState(false)
  const rail = useRef<HTMLDivElement>(null)
  const pathKind = reconnecting ? "reconnecting" : connected ? path : "offline"

  // Enough PCs and the row scrolls. A header that leaves the one you are
  // actually on past its edge names no PC at all.
  useEffect(() => {
    const chip = rail.current?.querySelector('[aria-selected="true"]')
    chip?.scrollIntoView?.({ behavior: "smooth", block: "nearest", inline: "nearest" })
  }, [activeFingerprint, hosts.length])

  return (
    // One row: which PC this is beats repeating the app's own name.
    <header className="relative flex h-14 shrink-0 items-center gap-1 border-b border-border px-2">
      <Button
        type="button"
        variant="ghost"
        className="size-10 shrink-0 px-0"
        aria-label={t("home.menu")}
        aria-expanded={menu}
        onClick={() => setMenu((open) => !open)}
      >
        <Menu className="size-5" aria-hidden />
      </Button>
      {menu ? (
        <>
          <button
            type="button"
            className="fixed inset-0 z-20 bg-foreground/20"
            aria-label={t("home.close")}
            onClick={() => setMenu(false)}
          />
          <div
            role="menu"
            className="absolute left-2 top-12 z-30 w-56 rounded-xl border border-border bg-card p-1 shadow-lg"
          >
            <MenuRow
              onClick={() => {
                setMenu(false)
                onAdd()
              }}
            >
              {t("home.addHost")}
            </MenuRow>
            <MenuRow
              onClick={() => {
                setMenu(false)
                onUnlink()
              }}
            >
              {t("home.unlink")}
            </MenuRow>
            {onToggleLocale ? (
              <MenuRow
                onClick={() => {
                  setMenu(false)
                  onToggleLocale()
                }}
              >
                {localeSwitchLabel()}
              </MenuRow>
            ) : null}
          </div>
        </>
      ) : null}
      <h1 className="sr-only">{t("home.app")}</h1>
      <div
        ref={rail}
        role="tablist"
        aria-label={t("home.hosts")}
        className="rail flex min-w-0 flex-1 items-center gap-1 overflow-x-auto"
      >
        {hosts.map((host) => {
          const selected = host.fingerprint === activeFingerprint
          const label = linkLabel(host, hosts)
          return (
            <button
              key={host.fingerprint}
              type="button"
              role="tab"
              aria-label={label}
              aria-selected={selected}
              className={cn(
                "inline-flex h-9 shrink-0 items-center gap-1.5 rounded-full px-3 text-sm font-medium",
                selected
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground",
              )}
              onClick={() => onSelect(host.fingerprint)}
            >
              <Monitor className="size-3.5 shrink-0" aria-hidden />
              <span className="max-w-28 truncate">{label}</span>
              {selected ? (
                // Color is liveness. Path stays on the title; gray on this chip reads as down.
                <span
                  className={cn(
                    "size-1.5 shrink-0 rounded-full",
                    reconnecting
                      ? "animate-pulse bg-destructive"
                      : connected
                        ? "bg-[hsl(var(--online))]"
                        : "bg-muted-foreground",
                  )}
                  title={
                    reconnecting
                      ? t("home.reconnecting")
                      : !connected
                        ? t("home.offline")
                        : path === "direct"
                          ? t("home.direct")
                          : t("home.relay")
                  }
                  aria-label={`path=${pathKind}`}
                />
              ) : null}
            </button>
          )
        })}
      </div>
      {/* Add a PC is on the menu at the other end of this row; the corner
          belongs to the thing you reach for every time you pick up the phone. */}
      <Button
        type="button"
        variant="ghost"
        data-testid="new-chat-top"
        className="size-10 shrink-0 px-0"
        aria-label={t("home.newChat")}
        onClick={onNewChat}
      >
        <SquarePen className="size-5" aria-hidden />
      </Button>
    </header>
  )
}

function MenuRow({ children, onClick }: { children: string; onClick: () => void }) {
  return (
    <button
      type="button"
      role="menuitem"
      className="flex h-10 w-full items-center rounded-lg px-3 text-left text-sm hover:bg-accent"
      onClick={onClick}
    >
      {children}
    </button>
  )
}
