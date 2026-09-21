import { Menu, Monitor, Plus } from "lucide-react"
import { useState } from "react"

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
  onUnlink: () => void
  onToggleLocale?: () => void
}) {
  const [menu, setMenu] = useState(false)
  const pathKind = reconnecting ? "reconnecting" : connected ? path : "offline"
  return (
    <header className="relative flex flex-col gap-2 border-b border-border px-3 pb-3 pt-2">
      <div className="flex items-center gap-1">
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
        <h1 className="min-w-0 flex-1 truncate text-lg font-semibold tracking-tight">
          {t("home.app")}
        </h1>
        <Button
          type="button"
          variant="ghost"
          className="size-10 shrink-0 px-0"
          aria-label={t("home.addHost")}
          onClick={onAdd}
        >
          <Plus className="size-5" aria-hidden />
        </Button>
      </div>
      <div
        role="tablist"
        aria-label={t("home.hosts")}
        className="flex min-w-0 items-center gap-1 overflow-x-auto"
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
                "inline-flex h-8 shrink-0 items-center gap-1.5 rounded-full px-3 text-sm",
                selected
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground",
              )}
              onClick={() => onSelect(host.fingerprint)}
            >
              <Monitor className="size-3.5 shrink-0" aria-hidden />
              <span className="max-w-28 truncate">{label}</span>
              {selected ? (
                <span
                  className={cn(
                    "size-1.5 shrink-0 rounded-full",
                    reconnecting
                      ? "animate-pulse bg-destructive"
                      : !connected
                        ? "bg-destructive"
                        : path === "direct"
                          ? "bg-[hsl(var(--running))]"
                          : "bg-primary-foreground/70",
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
