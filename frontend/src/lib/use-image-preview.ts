import { useCallback, useLayoutEffect, useRef, useState } from "react"

type Size = { width: number; height: number }
type FitMode = "window" | "width"
type ZoomAnchor = { imageX: number; imageY: number; viewportX: number; viewportY: number }
type ManualZoom = { scale: number; anchor?: ZoomAnchor }
const MIN_ZOOM = 0.1
const MAX_ZOOM = 4
const ZOOM_STEP = 0.25

/** Fit follows the viewport; a manually chosen scale keeps its pixel size. */
export function useImagePreview(defaultFit: FitMode = "window") {
  const viewport = useRef<HTMLDivElement>(null)
  const [bounds, setBounds] = useState<Size>({ width: 0, height: 0 })
  const [image, setImage] = useState<Size>()
  const [zoom, setZoom] = useState<FitMode | ManualZoom>(defaultFit)
  const [failed, setFailed] = useState(false)
  const fittingWidth = zoom === "width"
  const widthScale = image && bounds.width > 0 ? Math.min(1, bounds.width / image.width) : 1
  const windowScale = image && bounds.width > 0 && bounds.height > 0
    ? Math.min(widthScale, bounds.height / image.height)
    : 1
  const manualZoom = typeof zoom === "object" ? zoom : undefined
  const scale = manualZoom?.scale ?? (fittingWidth ? widthScale : windowScale)
  const anchor = manualZoom?.anchor
  const width = image ? image.width * scale : undefined
  const height = image ? image.height * scale : undefined
  const zoomAt = useCallback((nextScale: number, startScale: number, nextAnchor: ZoomAnchor) => {
    // An exceptionally small auto-fit can magnify smoothly from its starting scale.
    const minimum = Math.min(MIN_ZOOM, startScale)
    setZoom({ scale: Math.min(MAX_ZOOM, Math.max(minimum, nextScale)), anchor: nextAnchor })
  }, [])

  useLayoutEffect(() => {
    const element = viewport.current
    if (!element) return
    const measure = () => setBounds({ width: element.clientWidth, height: element.clientHeight })
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useLayoutEffect(() => {
    const element = viewport.current
    if (!element || fittingWidth) return
    const imageWidth = width ?? 0
    const imageHeight = height ?? 0
    const insetX = (element.clientWidth - imageWidth) / 2
    const insetY = (element.clientHeight - imageHeight) / 2
    if (anchor) {
      element.scrollLeft = Math.max(0, anchor.imageX * imageWidth + Math.max(0, insetX) - anchor.viewportX)
      element.scrollTop = Math.max(0, anchor.imageY * imageHeight + Math.max(0, insetY) - anchor.viewportY)
      return
    }
    element.scrollLeft = Math.max(0, -insetX)
    element.scrollTop = Math.max(0, -insetY)
  }, [width, height, fittingWidth, anchor])

  useLayoutEffect(() => {
    const element = viewport.current
    if (!element || !fittingWidth) return
    // Start at the top of the image and leave scrolling alone on viewport resize.
    element.scrollLeft = 0
    element.scrollTop = 0
  }, [fittingWidth, image])

  return {
    viewport, width, height, scale, failed,
    ready: !!image && !failed,
    fitting: zoom === "window",
    fittingWidth,
    canZoomIn: scale < MAX_ZOOM,
    canZoomOut: scale > MIN_ZOOM,
    onLoad: (element: HTMLImageElement) => {
      if (element.naturalWidth > 0 && element.naturalHeight > 0) {
        setImage({ width: element.naturalWidth, height: element.naturalHeight })
      } else setFailed(true)
    },
    onError: () => setFailed(true),
    zoomAt,
    zoomIn: () => setZoom({ scale: Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, Math.round((scale + ZOOM_STEP) * 100) / 100)) }),
    zoomOut: () => setZoom({ scale: Math.max(MIN_ZOOM, Math.round((scale - ZOOM_STEP) * 100) / 100) }),
    actualSize: () => setZoom({ scale: 1 }),
    fit: () => setZoom("window"),
    fitWidth: () => setZoom("width"),
  }
}
