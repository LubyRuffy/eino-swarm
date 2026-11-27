import catalog from "../../graph/kinds.json"
import type { KindDef } from "./types"

/** The kind catalog is the file the Go side embeds, so a kind added there is
 *  named, coloured and filterable here without a front-end change. */
export const KINDS = catalog.kinds as KindDef[]
export const OTHER_KIND = catalog.other

const byId = new Map(KINDS.map((kind) => [kind.id, kind]))

export function kindDef(id: string): KindDef | undefined {
  return byId.get(id)
}

export function kindName(id: string, locale: string): string {
  const def = byId.get(id)
  return def?.name[locale] ?? def?.name.en ?? id
}

/** Stable palette slot per kind; unknown kinds share the last slot. */
export function kindSlot(id: string): number {
  const index = KINDS.findIndex((kind) => kind.id === id)
  return index < 0 ? KINDS.length - 1 : index
}

export function attrLabel(kind: string, attr: string, locale: string): string {
  const found = byId.get(kind)?.attrs.find((candidate) => candidate.name === attr)
  return found?.label[locale] ?? found?.label.en ?? attr
}
