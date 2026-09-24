import type { Locator, Page } from "@playwright/test"

/** Distance from the transcript pane top to the named user bubble. */
export function userMessageGap(transcript: Locator, text: string) {
  return transcript.evaluate((el, needle) => {
    const nodes = el.querySelectorAll('[data-testid="user-message"]')
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes.item(i)
      if (!n?.textContent?.includes(String(needle))) continue
      if (!(n instanceof HTMLElement)) return 999
      return n.getBoundingClientRect().top - el.getBoundingClientRect().top
    }
    return 999
  }, text)
}

/** User view folds spawn / wait_agents. Specs that assert on that chrome
 *  open the app menu (bottom-left …) and flip developer view; the product
 *  default stays compact. The click writes config.yaml, so a second call
 *  is a no-op when already on. */
export async function showDeveloperLog(page: Page) {
  await page.getByRole("button", { name: "App menu" }).click()
  const toDev = page.getByRole("menuitem", { name: "Switch to developer view" })
  if (await toDev.isVisible()) {
    await toDev.click()
    return
  }
  await page.getByRole("button", { name: "App menu" }).click()
}

/** User view keeps a fold per thought/tool group. The live ticker is the
 *  last group of the running turn — matching every work-fold is a strict-mode
 *  miss once an earlier answer has split the groups. */
export function liveWorkFold(page: Page) {
  return page.getByTestId("work-fold").filter({ has: page.locator(".animate-spin") })
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
