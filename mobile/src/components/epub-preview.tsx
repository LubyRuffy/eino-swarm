import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { Loader2 } from "lucide-react"

import { parseEpub } from "../../../frontend/src/lib/documents/epub"
import type { BookPreview } from "../../../frontend/src/lib/documents/types"
import { ReadingFontControls, useReadingFont } from "@/components/reading-font"
import { t } from "@/lib/i18n"
import { usePinch } from "@/lib/use-pinch"

type ReadingAction = "previous" | "next" | "controls"
const defaultFontSize = 18
const tapTarget = "min-w-0 bg-transparent focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-primary"

function setLayout(element: HTMLElement, styles: Record<string, string>) {
  for (const [property, value] of Object.entries(styles)) element.style.setProperty(property, value, "important")
}

function installTextScaling(body: HTMLElement, view: Window) {
  // Sample before writing so inherited and relative book styles keep their original proportions.
  const sizes = [body, ...body.querySelectorAll<HTMLElement>("*")].map((element) => {
    const { fontSize, lineHeight } = view.getComputedStyle(element)
    return { element, fontSize, lineHeight }
  })
  for (const { element, fontSize, lineHeight } of sizes) {
    const scaled = (value: string) => `calc(${value} * var(--zwai-font-scale))`
    const styles: Record<string, string> = { "font-size": scaled(fontSize) }
    if (lineHeight.endsWith("px")) styles["line-height"] = scaled(lineHeight)
    setLayout(element, styles)
  }
}

// The phone may measure the chapter and handle its events. The package itself
// still cannot execute scripts, access the app, submit forms or open windows.
function paginate(frame: HTMLIFrameElement, fromEnd: boolean, fontSize: number, onBoundary: (step: number) => void, toggleControls: () => void) {
  const doc = frame.contentDocument
  if (doc?.URL !== "about:srcdoc") return
  const { body, documentElement: root, defaultView: view } = doc
  if (!body || !view) return

  const style = doc.createElement("style")
  style.textContent = `
    :where(body) { font-size: ${defaultFontSize}px; line-height: 1.65 }
    img, svg, video { max-width: 100% !important; max-height: calc(var(--zwai-page-height) - 32px) !important;
      object-fit: contain; break-inside: avoid }
    pre { white-space: pre-wrap }
  `
  doc.head.append(style)
  setLayout(root, { overflow: "hidden", margin: "0", padding: "0" })
  setLayout(body, {
    display: "block", "box-sizing": "border-box", position: "static", overflow: "visible",
    margin: "0 var(--zwai-page-inset)", padding: "12px 0", border: "0",
    "min-width": "0", "min-height": "0", "max-width": "none", "max-height": "none",
    "column-count": "auto", "column-gap": "calc(2 * var(--zwai-page-inset))",
    "column-fill": "auto", "overflow-wrap": "anywhere", "writing-mode": "horizontal-tb",
  })

  let width = 0
  let pages = 1
  let page = 0
  let direction = 1
  let stayAtEnd = fromEnd
  let appliedFontSize = defaultFontSize
  let scalableText = false
  const content = doc.createRange()
  content.selectNodeContents(body)
  const show = (next: number) => {
    page = Math.max(0, Math.min(next, pages - 1))
    setLayout(body, { transform: `translateX(${-direction * page * width}px)` })
    root.scrollLeft = 0
    root.scrollTop = 0
  }
  const measure = (anchor?: Range) => {
    width = frame.clientWidth
    const height = frame.clientHeight
    if (!width || !height) return
    const inset = Math.min(20, Math.floor(width * 0.05))
    // A native WebView may expand its inner viewport to the document height.
    // Use the phone's visible frame for the size of one page.
    setLayout(root, { width: `${width}px`, height: `${height}px` })
    setLayout(body, {
      "--zwai-page-inset": `${inset}px`, "--zwai-page-height": `${height}px`,
      // iOS leaves count:1 / width:auto as one long column. A measured
      // column width makes it fragment into overflow pages.
      width: `${width - 2 * inset}px`, height: `${height}px`, "column-width": `${width - 2 * inset}px`,
    })
    direction = view.getComputedStyle(body).direction === "rtl" ? -1 : 1
    // WebKit can include a trailing empty column in scrollWidth. Count the
    // laid-out content, relative to the body so a current translation cancels out.
    const edges = content.getBoundingClientRect()
    const origin = body.getBoundingClientRect()
    const extent = direction === 1 ? edges.right - origin.left : origin.right - edges.left
    pages = Math.max(1, Math.ceil(extent / width))
    const position = anchor?.getClientRects()[0]
    const anchorPage = position?.height
      ? Math.floor((direction === 1 ? position.left - origin.left : origin.right - position.right) / width)
      : page
    show(stayAtEnd ? pages - 1 : anchorPage)
  }
  const visibleOnPage = (rect: DOMRect) => rect.right > 0 && rect.left < width && rect.bottom > 0 && rect.top < frame.clientHeight
  const textAt = (x: number, y: number) => {
    const anchor = doc.caretRangeFromPoint?.(x, y)
    const text = anchor?.startContainer
    if (!anchor || text?.nodeType !== Node.TEXT_NODE || !body.contains(text)) return
    const length = (text as Text).length
    if (!length) return
    const offset = Math.min(anchor.startOffset, length - 1)
    anchor.setStart(text, offset)
    anchor.setEnd(text, offset + 1)
    const rect = anchor.getClientRects()[0]
    if (rect && visibleOnPage(rect)) return anchor
  }
  const anchorAt = (x: number, y: number) => {
    const anchor = textAt(x, y)
    if (anchor) return anchor
    // A midpoint in blank space can resolve to an off-page caret in a column layout.
    for (const rect of content.getClientRects()) {
      if (!visibleOnPage(rect) || rect.width < 2 || rect.height < 2) continue
      const nearby = textAt(
        Math.min(Math.min(width, rect.right) - 1, Math.max(x, rect.left + 1)),
        Math.min(Math.min(frame.clientHeight, rect.bottom) - 1, Math.max(y, rect.top + 1)),
      )
      if (nearby) return nearby
    }
  }
  const applyFontSize = (size: number) => {
    if (size === appliedFontSize) return false
    setLayout(root, { "--zwai-font-scale": String(size / defaultFontSize) })
    if (!scalableText) {
      installTextScaling(body, view)
      scalableText = true
    }
    appliedFontSize = size
    return true
  }
  const resizeText = (size: number, anchor?: Range) => {
    if (size === appliedFontSize) return
    const position = anchor ?? anchorAt(width / 2, frame.clientHeight / 2)
    applyFontSize(size)
    stayAtEnd = false
    measure(position)
  }
  const action = (input: ReadingAction) => {
    if (input === "controls") {
      toggleControls()
      return
    }
    if (!width) return
    stayAtEnd = false
    const step = input === "previous" ? -1 : 1
    if (page + step < 0 || page + step >= pages) onBoundary(step)
    else show(page + step)
  }

  applyFontSize(fontSize)
  measure()
  const resize = new ResizeObserver(() => measure())
  resize.observe(frame)
  let active = true
  void doc.fonts?.ready.then(() => {
    if (active) measure()
  })
  return { action, anchorAt, resizeText, stop: () => { active = false; resize.disconnect() } }
}

export function PhoneEpubPreview({ bytes, status, download }: {
  bytes?: Uint8Array
  status?: ReactNode
  download?: ReactNode
}) {
  const [parsed, setParsed] = useState<{ bytes: Uint8Array; book: BookPreview | null }>()
  const [chapter, setChapter] = useState({ index: 0, fromEnd: false })
  const [showControls, setShowControls] = useState(false)
  const [ready, setReady] = useState(false)
  const [frameFailed, setFrameFailed] = useState(false)
  const font = useReadingFont(defaultFontSize)
  const { size: fontSize, reset: resetFont, setSize: setFontSize } = font
  const controlsId = useId()
  const reader = useRef<ReturnType<typeof paginate>>(undefined)
  const navigation = useRef<HTMLDivElement>(null)
  const result = parsed?.bytes === bytes ? parsed : undefined
  const book = result?.book
  const current = book?.chapters[chapter.index]
  const reading = !!current && !frameFailed

  useEffect(() => {
    let active = true
    setChapter({ index: 0, fromEnd: false })
    setShowControls(false)
    setReady(false)
    setFrameFailed(false)
    resetFont()
    if (bytes) {
      void parseEpub(bytes).then(
        (book) => { if (active) setParsed({ bytes, book }) },
        () => { if (active) setParsed({ bytes, book: null }) },
      )
    }
    return () => {
      active = false
      reader.current?.stop()
      reader.current = undefined
    }
  }, [bytes, resetFont])

  useLayoutEffect(() => {
    reader.current?.resizeText(fontSize)
  }, [fontSize])

  usePinch(navigation, (start) => {
    const surface = navigation.current
    const chapterReader = reader.current
    if (!surface || !chapterReader) return
    const bounds = surface.getBoundingClientRect()
    const anchor = chapterReader.anchorAt(start.x - bounds.left, start.y - bounds.top)
    return (point) => {
      const size = setFontSize(fontSize * point.distance / start.distance)
      chapterReader.resizeText(size, anchor)
    }
  }, reading && ready)

  function openChapter(index: number, fromEnd = false) {
    if (!book || !book.chapters[index] || index === chapter.index) return
    reader.current?.stop()
    reader.current = undefined
    setReady(false)
    setChapter({ index, fromEnd })
  }

  return (
    <div data-testid="document-preview" className="relative flex min-h-0 min-w-0 flex-1 flex-col">
      {reading ? (
        <iframe
          key={chapter.index}
          title={current.title}
          tabIndex={-1}
          sandbox="allow-same-origin"
          referrerPolicy="no-referrer"
          srcDoc={current.srcdoc}
          className={`min-h-0 w-full flex-1 border-0 bg-white ${ready ? "" : "invisible"}`}
          onLoad={(event) => {
            reader.current?.stop()
            reader.current = paginate(event.currentTarget, chapter.fromEnd, fontSize,
              (step) => openChapter(chapter.index + step, step < 0), () => setShowControls((open) => !open))
            setFrameFailed(!reader.current)
            setReady(!!reader.current)
          }}
        />
      ) : (
        <div className="px-4 pt-4">
          {status}
          {result || frameFailed ? <p className="text-sm text-muted-foreground">{t("files.unavailable")}</p> : null}
          {bytes && !result ? <p role="status" className="flex items-center gap-2 text-sm"><Loader2 className="size-4 animate-spin" />{t("files.loading")}</p> : null}
        </div>
      )}
      {/* Touch and keyboard stay in the phone document; the EPUB frame runs no scripts. */}
      {reading ? (
        <div
          ref={navigation}
          role="group"
          aria-label={t("files.readerNavigation")}
          className="absolute inset-0 grid touch-none grid-cols-3"
          onKeyDown={(event) => {
            if (event.altKey || event.ctrlKey || event.metaKey) return
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
            event.preventDefault()
            reader.current?.action(event.key === "ArrowLeft" ? "previous" : "next")
          }}
        >
          <button type="button" aria-label={t("files.previousPage")} onClick={() => reader.current?.action("previous")}
            className={tapTarget} />
          <button type="button" aria-label={t("files.readerControls")} aria-expanded={showControls} aria-controls={controlsId}
            onClick={() => reader.current?.action("controls")}
            className={tapTarget} />
          <button type="button" aria-label={t("files.nextPage")} onClick={() => reader.current?.action("next")}
            className={tapTarget} />
        </div>
      ) : null}
      <div
        id={controlsId}
        hidden={reading && (!ready || !showControls)}
        className={reading ? "absolute inset-x-0 top-0 border-b border-border bg-background px-4 py-3 shadow-sm" : "px-4"}
      >
        {reading && book ? (
          <select aria-label={t("files.contents")} value={chapter.index}
            className="mb-2 w-full rounded-md border border-input bg-background px-2 py-1 text-base"
            onChange={(event) => { openChapter(Number(event.target.value)); setShowControls(false) }}>
            {book.chapters.map(({ title }, index) => <option key={index} value={index}>{title}</option>)}
          </select>
        ) : null}
        {reading ? <ReadingFontControls font={font} /> : null}
        {download}
      </div>
    </div>
  )
}
