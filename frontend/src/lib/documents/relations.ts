import { attrLocal, elementsByLocal, parseXml } from "./xml"
import { resolvePath } from "./paths"

export type ZipRelation = { target: string; external: boolean }

export function relationships(xml: string): Map<string, ZipRelation> {
  const rels = new Map<string, ZipRelation>()
  if (!xml) return rels
  const doc = parseXml(xml)
  for (const rel of elementsByLocal(doc, "Relationship")) {
    const id = rel.getAttribute("Id") ?? ""
    const target = rel.getAttribute("Target") ?? ""
    if (!id || !target) continue
    rels.set(id, { target, external: (rel.getAttribute("TargetMode") ?? "").toLowerCase() === "external" })
  }
  return rels
}

export function relationPath(fromFile: string, rel: ZipRelation | undefined): string | null {
  if (!rel || rel.external) return null
  return resolvePath(fromFile, rel.target)
}

/** Relationship ids share the local name `id` with ordinary ids. Prefer the
 * namespaced `r:id` when both are present. */
export function relationshipId(element: Element): string {
  for (const attr of element.attributes) {
    if (attr.localName === "id" && (attr.prefix === "r" || attr.name.includes(":"))) return attr.value
  }
  return attrLocal(element, "id")
}
