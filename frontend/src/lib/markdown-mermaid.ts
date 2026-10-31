import type { MermaidConfig } from "mermaid"

export type MermaidAppearance = {
  dark: boolean
  fontFamily: string
  colors: Record<string, string>
}

const COLOR_TOKENS: Record<string, string> = {
  background: "background",
  primaryColor: "muted",
  primaryTextColor: "foreground",
  primaryBorderColor: "border",
  secondaryColor: "accent",
  secondaryTextColor: "foreground",
  secondaryBorderColor: "border",
  tertiaryColor: "card",
  tertiaryTextColor: "foreground",
  tertiaryBorderColor: "border",
  lineColor: "muted-foreground",
  textColor: "foreground",
  edgeLabelBackground: "background",
}

/** Mermaid derives shades from hex colours. Resolve the existing CSS tokens
 * through the browser rather than keeping a second light/dark palette. */
export function readMermaidAppearance(): MermaidAppearance {
  const root = document.documentElement
  const computed = getComputedStyle(root)
  const colors: Record<string, string> = {}
  const probe = document.createElement("span")
  probe.hidden = true
  document.body.appendChild(probe)
  try {
    for (const [key, token] of Object.entries(COLOR_TOKENS)) {
      const value = computed.getPropertyValue(`--${token}`).trim()
      if (!value) continue
      probe.style.color = `hsl(${value})`
      const rgb = getComputedStyle(probe).color.match(/^rgb\(\s*(\d+)[, ]+\s*(\d+)[, ]+\s*(\d+)\s*\)$/)
      if (rgb) colors[key] = "#" + rgb.slice(1).map((v) => Number(v).toString(16).padStart(2, "0")).join("")
    }
  } finally {
    probe.remove()
  }
  return {
    dark: root.classList.contains("dark"),
    fontFamily: computed.getPropertyValue("--font-content").trim() || computed.fontFamily,
    colors,
  }
}

/** Optional resource-bearing Mermaid constructs remain source. Plain labels,
 * including URLs and <br/> line breaks, do not need to fetch anything. */
export function mermaidSourceUsesResources(source: string): boolean {
  const metadata = /@\s*\{/.test(source)
  // Metadata is YAML: quoted/escaped keys and braces inside labels must not
  // hide an image request. Escaped metadata is intentionally kept as source.
  return (metadata && /(?:["']?\bimg["']?\s*:|\\)/i.test(source)) ||
    /!\[[^\]]*\]/.test(source) ||
    /url\s*\(|@import/i.test(source) ||
    (/\b(?:classDef|style|linkStyle|UpdateElementStyle|UpdateRelStyle)\b/i.test(source) && source.includes("\\"))
}

let renderQueue: Promise<unknown> = Promise.resolve()
let nextID = 0

/** Mermaid has process-wide configuration and a DOM measurement phase. Keep
 * both under one queue so simultaneous diagrams cannot borrow another theme. */
export function renderMermaid(
  source: string,
  appearance: MermaidAppearance,
  cancelled: () => boolean,
): Promise<string | null> {
  const task = renderQueue.then(async () => {
    if (cancelled() || mermaidSourceUsesResources(source)) return null
    const { default: mermaid } = await import("mermaid")
    if (cancelled()) return null
    if (source.length > (mermaid.mermaidAPI.defaultConfig.maxTextSize ?? Infinity)) return null
    const config: MermaidConfig = {
      startOnLoad: false,
      securityLevel: "strict",
      suppressErrorRendering: true,
      htmlLabels: false,
      theme: "base",
      themeVariables: { ...appearance.colors, darkMode: appearance.dark },
      fontFamily: appearance.fontFamily || undefined,
      flowchart: { htmlLabels: false },
      dompurifyConfig: {
        FORBID_TAGS: ["style", "img", "image", "iframe", "object", "embed", "link"],
      },
    }
    // Diagram frontmatter/directives cannot override host rendering or safety.
    config.secure = [
      ...Object.keys(mermaid.mermaidAPI.defaultConfig),
      ...Object.keys(config),
      "secure", "securityLevel", "startOnLoad", "maxTextSize", "maxEdges",
      "suppressErrorRendering", "theme", "themeVariables", "themeCSS",
      "fontFamily", "altFontFamily", "htmlLabels", "flowchart", "dompurifyConfig",
    ]
    mermaid.initialize(config)
    const container = document.createElement("div")
    container.className = "md-mermaid-measure"
    container.setAttribute("aria-hidden", "true")
    document.body.appendChild(container)
    try {
      const { svg } = await mermaid.render(`mermaid-${++nextID}`, source, container)
      return cancelled() ? null : svg
    } finally {
      container.remove()
    }
  })
  renderQueue = task.catch(() => undefined)
  return task
}

/** A percentage-sized standalone SVG otherwise gets the image default width
 * (300px). Preserve its measured layout so a large diagram can scroll at its
 * intended text size instead of shrinking every label to fit the column. */
export function mermaidSVGDataURL(svg: string): string | null {
  const parsed = new DOMParser().parseFromString(svg, "image/svg+xml")
  const root = parsed.documentElement
  if (root.localName !== "svg" || root.namespaceURI !== "http://www.w3.org/2000/svg" ||
    parsed.querySelector("parsererror")) return null
  const box = root.getAttribute("viewBox")?.trim().split(/[\s,]+/).map(Number)
  if (!box || box.length !== 4 || !box.every(Number.isFinite) || box[2] <= 0 || box[3] <= 0) return null
  root.setAttribute("width", String(box[2]))
  root.setAttribute("height", String(box[3]))
  root.style.removeProperty("max-width")
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(new XMLSerializer().serializeToString(root))}`
}

type PositionNode = { position?: { start?: { offset?: number }; end?: { offset?: number } } }

/** Use the parser's source span; a Mermaid body can itself contain backticks.
 * Container prefixes on closing fences belong to Markdown, not the diagram. */
export function mermaidFenceIsClosed(source: string, node: unknown): boolean {
  const position = (node as PositionNode | undefined)?.position
  const start = position?.start?.offset
  const end = position?.end?.offset
  if (start == null || end == null) return false
  const block = source.slice(start, end)
  const opener = /^(`{3,}|~{3,})/.exec(block)?.[1]
  const lastLine = block.slice(block.lastIndexOf("\n") + 1)
  const closer = /^(?:[ \t]*>[ \t]?)*[ \t]*(`{3,}|~{3,})[ \t]*$/.exec(lastLine)?.[1]
  return !!opener && !!closer && block.includes("\n") &&
    opener[0] === closer[0] && closer.length >= opener.length
}
