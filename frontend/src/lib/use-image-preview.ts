import { useLayoutEffect, useRef, useState } from "react"

type Size = { width: number; height: number }
const MIN_ZOOM = 0.1
const MAX_ZOOM = 4
const ZOOM_STEP = 0.25

/** Fit follows the viewport; a manually chosen scale keeps its pixel size. */
export function useImagePreview() {
  const viewport = useRef<HTMLDivElement>(null)
  const [bounds, setBounds] = useState<Size>({ width: 0, height: 0 })
  const [image, setImage] = useState<Size>()
  const [manualScale, setManualScale] = useState<number>()
  const [failed, setFailed] = useState(false)
  const fitScale = image && bounds.width > 0 && bounds.height > 0
    ? Math.min(1, bounds.width / image.width, bounds.height / image.height)
    : 1
  const scale = manualScale ?? fitScale
  const width = image ? image.width * scale : undefined
  const height = image ? image.height * scale : undefined

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
    if (!element) return
    // Keep the image's centre visible when switching between fit and detail.
    element.scrollLeft = Math.max(0, ((width ?? 0) - element.clientWidth) / 2)
    element.scrollTop = Math.max(0, ((height ?? 0) - element.clientHeight) / 2)
  }, [width, height])

  return {
    viewport, width, height, scale, failed,
    ready: !!image && !failed,
    fitting: manualScale === undefined,
    canZoomIn: scale < MAX_ZOOM,
    canZoomOut: scale > MIN_ZOOM,
    onLoad: (element: HTMLImageElement) => {
      if (element.naturalWidth > 0 && element.naturalHeight > 0) {
        setImage({ width: element.naturalWidth, height: element.naturalHeight })
      } else setFailed(true)
    },
    onError: () => setFailed(true),
    zoomIn: () => setManualScale(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, Math.round((scale + ZOOM_STEP) * 100) / 100))),
    zoomOut: () => setManualScale(Math.max(MIN_ZOOM, Math.round((scale - ZOOM_STEP) * 100) / 100)),
    actualSize: () => setManualScale(1),
    fit: () => setManualScale(undefined),
  }
}
