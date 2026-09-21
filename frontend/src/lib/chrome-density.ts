/** Directory density from UI chrome size. html rem is the transcript;
 *  using it here packed titles whenever conversation size was 13px. */

export type ChromeListDensity = {
  rowHeight: string
  rowPx: string
  rowGap: string
  folderGap: string
  sectionGap: string
  sectionLabelHeight: string
  listPx: string
  kind: string
}

const SIDEBAR_VARS: Record<keyof ChromeListDensity, string> = {
  rowHeight: "--sidebar-row-height",
  rowPx: "--sidebar-row-px",
  rowGap: "--sidebar-row-gap",
  folderGap: "--sidebar-folder-gap",
  sectionGap: "--sidebar-section-gap",
  sectionLabelHeight: "--sidebar-section-label-height",
  listPx: "--sidebar-list-px",
  kind: "--sidebar-kind",
}

function px(n: number): string {
  return `${Math.round(n)}px`
}

export function chromeListDensity(chromePx: number): ChromeListDensity {
  const size = Number.isFinite(chromePx) && chromePx > 0 ? chromePx : 13
  return {
    rowHeight: px(size * 2.15),
    rowPx: px(size * 0.62),
    rowGap: px(Math.max(1, size * 0.15)),
    folderGap: px(size * 0.46),
    sectionGap: px(size * 0.77),
    sectionLabelHeight: px(size * 1.7),
    listPx: px(size * 0.77),
    kind: px(size * 1.23),
  }
}

export function writeChromeListDensity(
  style: { setProperty(name: string, value: string): void },
  chromePx: number,
): void {
  const d = chromeListDensity(chromePx)
  ;(Object.keys(SIDEBAR_VARS) as (keyof ChromeListDensity)[]).forEach((key) => {
    style.setProperty(SIDEBAR_VARS[key], d[key])
  })
}
