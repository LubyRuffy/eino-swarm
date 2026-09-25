import { Code2, Ellipsis, FoldHorizontal, Moon, Sun, UnfoldHorizontal } from "lucide-react"
import { useState } from "react"

import { isDark } from "@/components/app/app-chrome"
import { Button } from "@/components/ui/button"
import { toggleContentWidth, toggleTranscriptMode } from "@/lib/appearance"
import { requestDesktopUpdateCheck } from "@/components/app/desktop-update"
import { desktopShell, uniqueSurfaces } from "@/lib/shell"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

/** 应用菜单 (App menu): the … beside Settings, bottom-left of the
 *  conversation list. Width, developer view and who else is connected
 *  live here; the title bar keeps status, terminal and the side panel. */
export function ChromeMenu({
  onToggleTheme,
  onToggleLocale,
}: {
  onToggleTheme: () => void
  onToggleLocale: () => void
}) {
  const t = useT()
  const theme = useApp((s) => s.theme)
  const version = useApp((s) => s.meta?.version ?? "")
  const clients = useApp((s) => s.meta?.clients)
  const contentWidth = useApp((s) => s.contentWidth)
  const transcriptMode = useApp((s) => s.transcriptMode)
  const setAppearance = useApp((s) => s.setAppearance)
  const dark = isDark(theme)
  const wide = contentWidth === "full"
  const developer = transcriptMode === "developer"
  const shared =
    (clients?.length ?? 0) >= 2
      ? uniqueSurfaces(clients)
          .map((surface) =>
            surface === "desktop" || surface === "web" || surface === "tui"
              ? t(`surface.${surface}`)
              : surface,
          )
          .join(" · ")
      : ""
  const [open, setOpen] = useState(false)
  const run = (fn: () => void) => {
    setOpen(false)
    fn()
  }
  const itemClass =
    "flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-accent"
  return (
    <div className="relative shrink-0">
      <Button
        variant="ghost"
        size="icon-sm"
        className="text-sidebar-foreground"
        aria-label={t("sidebar.appMenu")}
        aria-expanded={open}
        aria-haspopup="menu"
        style={{
          width: "var(--sidebar-row-height)",
          height: "var(--sidebar-row-height)",
        }}
        onClick={() => setOpen((next) => !next)}
      >
        <Ellipsis />
      </Button>
      {open ? (
        <>
          <button
            type="button"
            className="fixed inset-0 z-20 cursor-default border-0 bg-transparent p-0"
            aria-label={t("sidebar.closeMenu")}
            onClick={() => setOpen(false)}
          />
          <div
            role="menu"
            className="absolute bottom-full left-0 z-30 mb-1 w-max min-w-44 max-w-72 rounded-md border bg-popover p-1 text-popover-foreground shadow-md"
          >
            <button
              type="button"
              role="menuitem"
              aria-pressed={wide}
              className={itemClass}
              onClick={() =>
                run(() =>
                  setAppearance({ contentWidth: toggleContentWidth(contentWidth) }),
                )
              }
            >
              {wide ? (
                <FoldHorizontal className="size-3.5 shrink-0" />
              ) : (
                <UnfoldHorizontal className="size-3.5 shrink-0" />
              )}
              {wide ? t("header.switchToStandard") : t("header.switchToWide")}
            </button>
            <button
              type="button"
              role="menuitem"
              aria-pressed={developer}
              className={itemClass}
              onClick={() =>
                run(() =>
                  setAppearance({
                    transcriptMode: toggleTranscriptMode(transcriptMode),
                  }),
                )
              }
            >
              <Code2 className="size-3.5 shrink-0" />
              {developer ? t("header.switchToUser") : t("header.switchToDeveloper")}
            </button>
            <button
              type="button"
              role="menuitem"
              className={itemClass}
              onClick={() => run(onToggleTheme)}
            >
              {dark ? <Sun className="size-3.5 shrink-0" /> : <Moon className="size-3.5 shrink-0" />}
              {t("header.switchTheme")}
            </button>
            <button
              type="button"
              role="menuitem"
              className={itemClass}
              onClick={() => run(onToggleLocale)}
            >
              {t("header.switchLanguage")}
            </button>
            {desktopShell() ? (
              <button
                type="button"
                role="menuitem"
                className={itemClass}
                onClick={() => run(requestDesktopUpdateCheck)}
              >
                {t("sidebar.checkUpdate")}
              </button>
            ) : null}
            {shared || version ? (
              <div className="mt-1 border-t">
                {shared ? (
                  <p
                    data-testid="shared-clients"
                    className="px-2 py-1.5 text-xs text-muted-foreground"
                  >
                    {t("header.shared", { where: shared })}
                  </p>
                ) : null}
                {version ? (
                  <p
                    data-testid="app-version"
                    className="px-2 py-1.5 text-xs text-muted-foreground"
                  >
                    {t("sidebar.version", { version })}
                  </p>
                ) : null}
              </div>
            ) : null}
          </div>
        </>
      ) : null}
    </div>
  )
}
