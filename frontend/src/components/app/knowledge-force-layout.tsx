import ForceGraph, { type LinkObject, type NodeObject } from "force-graph"
import { Maximize, Minus, Plus } from "lucide-react"
import { useEffect, useRef } from "react"

import { Button } from "@/components/ui/button"
import type { KnowledgeGraph } from "@/lib/knowledge"
import { useT } from "@/lib/use-t"

interface LayoutNode extends NodeObject {
  id: string
  title: string
  label: string
}

interface LayoutLink extends LinkObject<LayoutNode> {
  relations?: string[]
  wikilink?: boolean
}

type Layout = InstanceType<typeof ForceGraph<LayoutNode, LayoutLink>>

const MIN_ZOOM = 0.02
const MAX_ZOOM = 6
const NODE_RADIUS = 5
const LARGE_GRAPH_SIZE = 1_000
function tooltip(value: string) {
  const label = document.createElement("span")
  label.textContent = value
  return label
}

function titleLabel(title: string) {
  const characters = Array.from(title)
  return characters.length > 36 ? `${characters.slice(0, 35).join("")}…` : title
}

function layoutData(graph: KnowledgeGraph) {
  const nodes = graph.nodes.map(({ id, title }): LayoutNode => ({ id, title, label: titleLabel(title) }))
  const links = graph.edges.map((edge): LayoutLink => ({ ...edge }))
  return { nodes, links }
}

export default function KnowledgeForceLayout({ graph, selected, onPage }: {
  graph: KnowledgeGraph
  selected?: string
  onPage: (id: string) => void
}) {
  const t = useT()
  const wikiLabel = t("knowledge.graphWikilink")
  const stage = useRef<HTMLDivElement>(null)
  const layout = useRef<Layout | null>(null)
  const focus = useRef<(id: string) => void>(() => undefined)
  const active = useRef(selected)

  useEffect(() => {
    const host = stage.current
    if (!host) return
    const { nodes, links } = layoutData(graph)
    const large = nodes.length > LARGE_GRAPH_SIZE
    const byId = new Map(nodes.map((node) => [node.id, node]))
    const nodeColor = (node: LayoutNode) => node.id === active.current ? colors.highlight : colors.node
    const readColors = () => {
      const style = getComputedStyle(host)
      const color = (token: string) => `hsl(${style.getPropertyValue(token).trim()})`
      return { text: color("--foreground"), node: color("--muted-foreground"), highlight: color("--primary"), edge: color("--muted-foreground"), background: color("--background") }
    }
    let colors = readColors()
    let fitted = false
    let moved = false
    const interacted = () => { moved = true }
    host.addEventListener("pointerdown", interacted)
    host.addEventListener("wheel", interacted, { passive: true })
    const instance = new ForceGraph<LayoutNode, LayoutLink>(host)
      .width(host.clientWidth).height(host.clientHeight)
      .minZoom(MIN_ZOOM).maxZoom(MAX_ZOOM).cooldownTicks(large ? 60 : 160)
      .nodeRelSize(NODE_RADIUS).nodeColor(nodeColor)
      .nodeLabel((node) => tooltip(node.title))
      .nodeCanvasObjectMode(() => "after")
      .nodeCanvasObject((node, ctx, zoom) => {
        if (node.x == null || node.y == null) return
        if ((large || (nodes.length > 50 && zoom < 0.85)) && node.id !== active.current) return
        const fontSize = 12 / zoom
        ctx.save()
        if (node.id === active.current) {
          ctx.beginPath()
          ctx.arc(node.x, node.y, NODE_RADIUS + 3 / zoom, 0, Math.PI * 2)
          ctx.lineWidth = 1.5 / zoom
          ctx.strokeStyle = colors.highlight
          ctx.stroke()
        }
        ctx.font = `${fontSize}px system-ui, sans-serif`
        ctx.textBaseline = "middle"
        ctx.lineWidth = 3 / zoom
        ctx.lineJoin = "round"
        ctx.strokeStyle = colors.background
        ctx.fillStyle = colors.text
        const gap = NODE_RADIUS + 4 / zoom
        const { a: scale, e: offset } = ctx.getTransform()
        const fitsOnRight = (node.x + gap + ctx.measureText(node.label).width) * scale + offset < ctx.canvas.width
        ctx.textAlign = fitsOnRight ? "left" : "right"
        const labelX = node.x + (fitsOnRight ? gap : -gap)
        ctx.strokeText(node.label, labelX, node.y)
        ctx.fillText(node.label, labelX, node.y)
        ctx.restore()
      })
      .linkColor(() => colors.edge)
      .linkLineDash((link) => link.relations?.length ? null : [3, 3])
      .linkLabel((link) => tooltip([...(link.relations ?? []), ...(link.wikilink ? [wikiLabel] : [])].join(" · ")))
      .linkCurvature(({ source, target }) => source === target ? 0.7 : 0)
      .onNodeClick((node) => onPage(node.id))
      .onNodeHover((node) => { host.style.cursor = node ? "pointer" : "grab" })
      .onEngineStop(() => {
        if (fitted || moved) return
        fitted = true
        if (active.current) focus.current(active.current)
        else instance.zoomToFit(250, 54)
      })
      .graphData({ nodes, links })

    instance.d3Force("charge")?.strength(-105)
    instance.d3Force("link")?.distance(86)
    layout.current = instance
    focus.current = (id) => {
      const node = byId.get(id)
      if (node?.x == null || node.y == null) return
      instance.centerAt(node.x, node.y, 220).zoom(Math.max(instance.zoom(), 1.25), 220)
    }

    const resize = new ResizeObserver(() => {
      if (host.clientWidth && host.clientHeight) instance.width(host.clientWidth).height(host.clientHeight)
    })
    resize.observe(host)
    const theme = new MutationObserver(() => {
      colors = readColors()
      instance.nodeColor(nodeColor)
    })
    theme.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "data-palette"] })
    return () => {
      theme.disconnect()
      resize.disconnect()
      host.removeEventListener("pointerdown", interacted)
      host.removeEventListener("wheel", interacted)
      instance._destructor()
      layout.current = null
    }
  }, [graph, onPage, wikiLabel])

  useEffect(() => {
    active.current = selected
    if (selected) focus.current(selected)
  }, [selected])

  const zoomBy = (factor: number) => {
    const instance = layout.current
    if (instance) instance.zoom(Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, instance.zoom() * factor)), 200)
  }

  return (
    <div className="relative min-h-0 flex-1 overflow-hidden rounded-md border bg-background">
      <div ref={stage} data-testid="knowledge-graph-canvas" role="img" aria-label={t("knowledge.graph")} className="absolute inset-0 cursor-grab" />
      <div className="absolute right-2 top-2 flex flex-col gap-1" role="group" aria-label={t("knowledge.graphControls")}>
        <Button size="icon-sm" variant="outline" aria-label={t("knowledge.graphZoomIn")} onClick={() => zoomBy(1.35)}><Plus aria-hidden /></Button>
        <Button size="icon-sm" variant="outline" aria-label={t("knowledge.graphZoomOut")} onClick={() => zoomBy(1 / 1.35)}><Minus aria-hidden /></Button>
        <Button size="icon-sm" variant="outline" aria-label={t("knowledge.graphFit")} onClick={() => layout.current?.zoomToFit(250, 54)}><Maximize aria-hidden /></Button>
      </div>
    </div>
  )
}
