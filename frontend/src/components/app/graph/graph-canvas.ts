import type { NodeObject } from "force-graph"
import type { Ring } from "../../../lib/graph/analysis"

export interface CanvasNode extends NodeObject {
  id: string
  label: string
  slot: number
  ring: Ring
  radius: number
}

export interface CanvasLink {
  source: CanvasNode
  target: CanvasNode
  retracted: boolean
}

export interface Palette {
  text: string
  muted: string
  background: string
  edge: string
  highlight: string
  slot: (slot: number) => string
}

/** Mutable view state the draw callbacks read every frame. */
export interface DrawState {
  highlighted: Set<string>
  selected: string | null
  labels: Map<string, CanvasNode>
  /** Dash offset of the flowing ring; advances only while something is being analysed. */
  phase: number
}

export const FONT_SIZE = 12
export const NODE_RADIUS = 9
const DASH = [3, 3]
const LABEL_CHARS = 22
/** A small graph names every node; a large one only the ones the pointer or a to-do points at. */
const NAME_ALL_UP_TO = 14

export const truncateLabel = (label: string) => {
  const chars = Array.from(label)
  return chars.length > LABEL_CHARS ? `${chars.slice(0, LABEL_CHARS - 1).join("")}…` : label
}

/** The ring is the analysis state: dashed while there is something left to do
 *  for the subject, flowing while it is being done, solid once settled. */
export function ringDash(ring: Ring): number[] {
  return ring === "settled" || ring === "retracted" ? [] : DASH
}

export function drawNode(node: CanvasNode, ctx: CanvasRenderingContext2D, zoom: number, state: DrawState, palette: Palette) {
  const x = node.x ?? 0
  const y = node.y ?? 0
  const retracted = node.ring === "retracted"
  const marked = state.highlighted.has(node.id)
  const color = retracted ? palette.muted : palette.slot(node.slot)
  ctx.save()
  if (retracted) ctx.globalAlpha = 0.45
  else if (state.highlighted.size && !marked) ctx.globalAlpha = 0.3
  ctx.strokeStyle = marked || node.id === state.selected ? palette.highlight : color
  ctx.lineWidth = Math.max(2, 0.5 / zoom)
  ctx.setLineDash(ringDash(node.ring))
  ctx.lineDashOffset = node.ring === "doing" ? -state.phase : 0
  ctx.beginPath(); ctx.arc(x, y, node.radius * 1.35, 0, Math.PI * 2); ctx.stroke()
  ctx.setLineDash([])
  ctx.fillStyle = color
  ctx.beginPath(); ctx.arc(x, y, node.radius, 0, Math.PI * 2); ctx.fill()
  ctx.restore()
}

/** force-graph finds the node under the pointer by painting every node in a
 *  unique colour on a hidden canvas. It reuses nodeCanvasObject there unless
 *  told otherwise, which would paint the real palette and make nodes
 *  unclickable, so the hit area is painted separately. */
export function drawPointerArea(node: CanvasNode, color: string, ctx: CanvasRenderingContext2D) {
  ctx.fillStyle = color
  ctx.beginPath()
  ctx.arc(node.x ?? 0, node.y ?? 0, node.radius * 1.35 + 2, 0, Math.PI * 2)
  ctx.fill()
}

export function drawLink(link: CanvasLink, ctx: CanvasRenderingContext2D, zoom: number, state: DrawState, palette: Palette) {
  const { source: start, target: end } = link
  const dx = (end.x ?? 0) - (start.x ?? 0)
  const dy = (end.y ?? 0) - (start.y ?? 0)
  const length = Math.hypot(dx, dy)
  if (!length) return
  const marked = state.highlighted.has(start.id) && state.highlighted.has(end.id)
  const angle = Math.atan2(dy, dx)
  const gap = NODE_RADIUS * 1.6
  const x1 = (start.x ?? 0) + (gap * dx) / length
  const y1 = (start.y ?? 0) + (gap * dy) / length
  const x2 = (end.x ?? 0) - (gap * dx) / length
  const y2 = (end.y ?? 0) - (gap * dy) / length
  ctx.save()
  if (link.retracted) ctx.globalAlpha = 0.4
  else if (state.highlighted.size && !marked) ctx.globalAlpha = 0.3
  ctx.strokeStyle = ctx.fillStyle = marked ? palette.highlight : palette.edge
  ctx.lineWidth = marked ? Math.max(2, 0.8 / zoom) : Math.max(1, 0.25 / zoom)
  // Links are never dashed: a dashed line already means "not analysed yet" on a node.
  ctx.setLineDash([])
  ctx.beginPath(); ctx.moveTo(x1, y1); ctx.lineTo(x2, y2); ctx.stroke()
  const ax = x2 + 2 * Math.cos(angle)
  const ay = y2 + 2 * Math.sin(angle)
  ctx.beginPath()
  ctx.moveTo(ax, ay)
  ctx.lineTo(ax - 10 * Math.cos(angle) - 4 * Math.cos(angle - Math.PI / 2), ay - 10 * Math.sin(angle) - 4 * Math.sin(angle - Math.PI / 2))
  ctx.lineTo(ax - 10 * Math.cos(angle) - 4 * Math.cos(angle + Math.PI / 2), ay - 10 * Math.sin(angle) - 4 * Math.sin(angle + Math.PI / 2))
  ctx.closePath()
  ctx.fill()
  ctx.restore()
}

export function drawLabels(ctx: CanvasRenderingContext2D, zoom: number, state: DrawState, palette: Palette) {
  ctx.save()
  ctx.textAlign = "center"
  ctx.textBaseline = "top"
  ctx.lineJoin = "round"
  const font = Math.max(FONT_SIZE, FONT_SIZE * zoom) / zoom
  ctx.font = `400 ${font}px sans-serif`
  for (const node of state.labels.values()) {
    const y = (node.y ?? 0) + node.radius * 1.35 + 4 / zoom
    ctx.strokeStyle = palette.background
    ctx.lineWidth = 3 / zoom
    ctx.fillStyle = !state.highlighted.size || state.highlighted.has(node.id) ? palette.text : palette.muted
    ctx.strokeText(node.label, node.x ?? 0, y)
    ctx.fillText(node.label, node.x ?? 0, y)
  }
  ctx.restore()
}

export function chooseLabels(nodes: CanvasNode[], focus: { hovered: string | null; selected: string | null; highlighted: ReadonlySet<string> }): Map<string, CanvasNode> {
  const placed = nodes.filter((node) => Number.isFinite(node.x))
  const chosen = placed.length <= NAME_ALL_UP_TO
    ? placed
    : placed.filter((node) => node.id === focus.hovered || node.id === focus.selected || focus.highlighted.has(node.id))
  return new Map(chosen.map((node) => [node.id, node]))
}
