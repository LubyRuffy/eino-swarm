import type { Page } from "@playwright/test"

/** User view folds spawn / wait_agents. Specs that assert on that chrome
 *  flip the title-bar toggle first; the product default stays compact.
 *  The click writes config.yaml, so a second call is a no-op when already on. */
export async function showDeveloperLog(page: Page) {
  const toUser = page.getByRole("button", { name: "Switch to user view" })
  if (await toUser.isVisible()) return
  await page.getByRole("button", { name: "Switch to developer view" }).click()
}

/** Live geometry of the composer plate: slab behind pins+box, join above. */
export function readComposerPlate(page: Page) {
  return page.evaluate(() => {
    const el = (id: string) => document.querySelector(`[data-testid=${id}]`)
    const fade = el("composer-fade")
    const slab = el("composer-slab")
    const dock = el("composer-dock")
    const root = el("composer")
    if (
      !(fade instanceof HTMLElement) ||
      !(slab instanceof HTMLElement) ||
      !(dock instanceof HTMLElement) ||
      !(root instanceof HTMLElement)
    ) {
      return null
    }
    const f = fade.getBoundingClientRect()
    const s = slab.getBoundingClientRect()
    const d = dock.getBoundingClientRect()
    const bg = getComputedStyle(slab).backgroundColor
    return {
      fadeBottom: f.bottom,
      fadeH: f.height,
      dockTop: d.top,
      slabH: s.height,
      rootH: root.getBoundingClientRect().height,
      slabOpaque: bg !== "rgba(0, 0, 0, 0)" && bg !== "transparent",
      pinsInDock: Boolean(dock.querySelector("[data-testid=composer-pins]")),
    }
  })
}
