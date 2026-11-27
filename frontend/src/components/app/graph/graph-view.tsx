import ForceGraph from "force-graph"
import { Maximize, Minus, Plus } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"
import type { Ring } from "../../../lib/graph/analysis"
import { cn } from "../../../lib/cn"
import {
  chooseLabels, drawLabels, drawLink, drawNode, drawPointerArea, NODE_RADIUS, truncateLabel,
  type CanvasLink, type CanvasNode, type DrawState, type Palette,
} from "./graph-canvas"
import { slotToken, tokenColor } from "./kind-style"

export interface ViewNode { id: string; label: string; slot: number; ring: Ring }
export interface ViewLink { id: string; from: string; to: string; retracted: boolean }

const MIN_ZOOM = 0.05
const MAX_ZOOM = 1.5
const FLOW_FRAME_MS = 80
/** Dash units the flowing ring advances per frame. */
const FLOW_STEP = 0.6

function readPalette(element: Element): Palette {
  return {
    text: tokenColor(element, "--foreground"),
    muted: tokenColor(element, "--muted-foreground"),
    background: tokenColor(element, "--card"),
    edge: tokenColor(element, "--easm-edge"),
    highlight: tokenColor(element, "--easm-highlight"),
    slot: (slot) => tokenColor(element, slotToken(slot)),
  }
}

/** The flowing ring is motion only the user asked to see; with reduced motion
 *  it stays a static dashed ring, which still reads as "in progress". */
export function usePrefersReducedMotion() {
  const [reduced, setReduced] = useState(() => typeof window !== "undefined" && !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)
  useEffect(() => {
    const query = window.matchMedia?.("(prefers-reduced-motion: reduce)")
    if (!query) return
    const update = () => setReduced(query.matches)
    query.addEventListener("change", update)
    return () => query.removeEventListener("change", update)
  }, [])
  return reduced
}

/** Test hook: where each subject sits on screen and which ring it wears, so a
 *  browser test can click a node and read its state off a canvas. */
export interface GraphStageElement extends HTMLDivElement {
  __graphScene?: () => { id: string; ring: Ring; x: number; y: number }[]
}

const toolButton = "grid size-7 place-items-center rounded-md border bg-card/95 text-muted-foreground shadow-sm hover:border-primary hover:text-primary"

export function GraphView({ nodes, links, highlighted, selected, active = true, height = 280, text, onSelect }: {
  nodes: readonly ViewNode[]
  links: readonly ViewLink[]
  highlighted: ReadonlySet<string>
  selected: string | null
  active?: boolean
  height?: number
  text: { graph: string; empty: string; fit: string; zoomIn: string; zoomOut: string }
  onSelect: (id: string | null) => void
}) {
  const reducedMotion = usePrefersReducedMotion()
  const stageRef = useRef<HTMLDivElement>(null)
  const mountRef = useRef<HTMLDivElement>(null)
  const graphRef = useRef<InstanceType<typeof ForceGraph<CanvasNode, CanvasLink>> | null>(null)
  const nodesRef = useRef<CanvasNode[]>([])
  const paletteRef = useRef<Palette | null>(null)
  const drawState = useRef<DrawState>({ highlighted: new Set(), selected: null, labels: new Map(), phase: 0 })
  const hovered = useRef<string | null>(null)
  const userMoved = useRef(false)
  const fitOnStop = useRef(false)
  const fitOnFirstTick = useRef(false)
  const activeRef = useRef(active)
  activeRef.current = active
  const onSelectRef = useRef(onSelect)
  onSelectRef.current = onSelect
  const flowing = !reducedMotion && nodes.some((node) => node.ring === "doing")

  const repaint = useCallback(() => {
    const graph = graphRef.current
    if (graph) graph.nodeCanvasObject(graph.nodeCanvasObject())
  }, [])

  const updateLabels = useCallback(() => {
    drawState.current.labels = chooseLabels(nodesRef.current, {
      hovered: hovered.current, selected: drawState.current.selected, highlighted: drawState.current.highlighted,
    })
    repaint()
  }, [repaint])

  const fit = useCallback(() => {
    const graph = graphRef.current
    if (!graph || !nodesRef.current.length || !Number.isFinite(nodesRef.current[0].x)) return
    graph.zoomToFit(0, 36)
    if (graph.zoom() > 1) graph.zoom(1)
    updateLabels()
  }, [updateLabels])

  // One graph instance per mount; data changes swap its contents.
  useEffect(() => {
    const stage = stageRef.current
    const mount = mountRef.current
    if (!stage || !mount) return
    paletteRef.current = readPalette(stage)
    const palette = () => paletteRef.current ?? readPalette(stage)
    const moved = () => { userMoved.current = true }
    stage.addEventListener("wheel", moved, { passive: true })
    stage.addEventListener("pointerdown", moved)
    const graph = new ForceGraph<CanvasNode, CanvasLink>(mount)
      .width(stage.clientWidth).height(stage.clientHeight)
      .minZoom(MIN_ZOOM).maxZoom(MAX_ZOOM).cooldownTime(8000)
      .nodeId("id").nodeVal((n) => n.radius * 2.5)
      .nodeLabel(() => "").linkLabel(() => "")
      .nodeCanvasObject((n, ctx, zoom) => drawNode(n, ctx, zoom, drawState.current, palette()))
      .linkCanvasObject((l, ctx, zoom) => drawLink(l, ctx, zoom, drawState.current, palette()))
      .nodePointerAreaPaint(drawPointerArea)
      .linkPointerAreaPaint(() => {})
      .onRenderFramePost((ctx, zoom) => drawLabels(ctx, zoom, drawState.current, palette()))
      .onNodeClick((n) => onSelectRef.current(n.id))
      .onBackgroundClick(() => onSelectRef.current(null))
      .onNodeHover((n) => { hovered.current = n?.id ?? null; stage.style.cursor = n ? "pointer" : "grab"; updateLabels() })
      // The layout takes seconds to settle; framing it only then leaves the
      // first seconds showing an empty corner.
      .onEngineTick(() => {
        if (!fitOnFirstTick.current) return
        fitOnFirstTick.current = false
        if (!userMoved.current) fit()
      })
      .onEngineStop(() => {
        if (fitOnStop.current && !userMoved.current) fit()
        fitOnStop.current = false
        updateLabels()
      })
    graph.d3Force("charge")?.strength(-220)
    graph.d3Force("link")?.distance(70)
    graph.d3Force("center")?.strength(0.1)
    graphRef.current = graph
    ;(stage as GraphStageElement).__graphScene = () => nodesRef.current.map((node) => {
      const point = graph.graph2ScreenCoords(node.x ?? 0, node.y ?? 0)
      return { id: node.id, ring: node.ring, x: point.x, y: point.y }
    })
    const resize = new ResizeObserver(() => {
      if (!stage.clientWidth || !stage.clientHeight) return
      graph.width(stage.clientWidth).height(stage.clientHeight)
      if (!userMoved.current) fit()
    })
    resize.observe(stage)
    const theme = new MutationObserver(() => { paletteRef.current = readPalette(stage); repaint() })
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "data-palette"] })
    const pause = () => (document.hidden || !activeRef.current ? graph.pauseAnimation() : graph.resumeAnimation())
    document.addEventListener("visibilitychange", pause)
    pause()
    return () => {
      document.removeEventListener("visibilitychange", pause)
      stage.removeEventListener("wheel", moved)
      stage.removeEventListener("pointerdown", moved)
      resize.disconnect()
      theme.disconnect()
      delete (stage as GraphStageElement).__graphScene
      graph._destructor()
      graphRef.current = null
    }
  }, [fit, repaint, updateLabels])

  useEffect(() => {
    const graph = graphRef.current
    if (graph) document.hidden || !active ? graph.pauseAnimation() : graph.resumeAnimation()
  }, [active])

  // Positions carry over by id, so a subject that stays does not jump when a
  // neighbour arrives; a newcomer starts beside the subject it is linked to.
  useEffect(() => {
    const graph = graphRef.current
    if (!graph) return
    const previous = new Map(nodesRef.current.map((node) => [node.id, node] as const))
    const next: CanvasNode[] = nodes.map((node) => Object.assign(previous.get(node.id) ?? {}, {
      id: node.id, label: truncateLabel(node.label), slot: node.slot, ring: node.ring, radius: NODE_RADIUS,
    }))
    const byId = new Map(next.map((node) => [node.id, node] as const))
    const edges = links.flatMap((link) => {
      const source = byId.get(link.from)
      const target = byId.get(link.to)
      return source && target ? [{ source, target, retracted: link.retracted }] : []
    })
    if (previous.size) {
      for (const node of next) {
        if (Number.isFinite(node.x)) continue
        const anchor = edges.map((edge) => edge.source === node ? edge.target : edge.target === node ? edge.source : undefined)
          .find((other) => other && Number.isFinite(other.x))
        const angle = Math.random() * Math.PI * 2
        node.x = (anchor?.x ?? 0) + Math.cos(angle) * 40
        node.y = (anchor?.y ?? 0) + Math.sin(angle) * 40
      }
    }
    const before = graph.graphData()
    const changed = next.length !== before.nodes.length || next.some((node, i) => node !== before.nodes[i]) ||
      edges.length !== before.links.length ||
      edges.some((edge, i) => before.links[i].source.id !== edge.source.id || before.links[i].target.id !== edge.target.id)
    nodesRef.current = next
    if (changed) {
      fitOnStop.current = !before.nodes.length || !userMoved.current
      fitOnFirstTick.current = fitOnStop.current
      if (!before.nodes.length) userMoved.current = false
      graph.warmupTicks(before.nodes.length ? 0 : 120).graphData({ nodes: next, links: edges })
    } else {
      before.links.forEach((link, i) => { link.retracted = edges[i].retracted })
    }
    updateLabels()
  }, [nodes, links, updateLabels])

  useEffect(() => {
    drawState.current.highlighted = new Set(highlighted)
    drawState.current.selected = selected
    updateLabels()
  }, [highlighted, selected, updateLabels])

  // force-graph stops painting when its layout settles, so the flow is driven
  // here and only while a subject is actually being analysed.
  useEffect(() => {
    if (!flowing || !active) return
    const timer = window.setInterval(() => {
      if (document.hidden) return
      drawState.current.phase = (drawState.current.phase + FLOW_STEP) % 6
      repaint()
    }, FLOW_FRAME_MS)
    return () => window.clearInterval(timer)
  }, [flowing, active, repaint])

  const zoomBy = (step: number) => {
    const graph = graphRef.current
    if (!graph) return
    userMoved.current = true
    graph.zoom(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, graph.zoom() + step)), 300)
  }

  return (
    <div className="relative overflow-hidden rounded-xl border bg-card" style={{ height }}>
      <div ref={stageRef} role="group" aria-label={text.graph} data-testid="graph-stage"
        data-flowing={flowing ? "true" : "false"} className="absolute inset-0 overflow-hidden">
        <div ref={mountRef} className="absolute inset-0" />
      </div>
      {!nodes.length ? <p className="pointer-events-none absolute inset-0 m-0 grid place-items-center px-6 text-center text-xs text-muted-foreground">{text.empty}</p> : null}
      <div className={cn("absolute top-2 left-2 z-10 flex items-start gap-1.5")}>
        <button type="button" className={toolButton} aria-label={text.zoomIn} title={text.zoomIn} onClick={() => zoomBy(0.3)}><Plus className="size-4" aria-hidden /></button>
        <button type="button" className={toolButton} aria-label={text.zoomOut} title={text.zoomOut} onClick={() => zoomBy(-0.3)}><Minus className="size-4" aria-hidden /></button>
        <button type="button" className={toolButton} aria-label={text.fit} title={text.fit} onClick={() => { userMoved.current = false; fit() }}><Maximize className="size-4" aria-hidden /></button>
      </div>
    </div>
  )
}
