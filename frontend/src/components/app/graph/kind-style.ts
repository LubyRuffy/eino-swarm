import { kindSlot } from "../../../lib/graph/kinds"

/** Kinds share the palette slots the EASM portrait already themes for light,
 *  dark and every palette. A kind added to the catalog gets the next slot by
 *  catalog order; colours repeat once the slots run out. */
const SLOT_TOKENS = [
  "--easm-root", "--easm-domain", "--easm-org", "--easm-cert", "--easm-icp", "--easm-asn",
  "--easm-ipc", "--easm-fid", "--easm-icon-hash", "--easm-mx",
] as const

export const slotToken = (slot: number) => SLOT_TOKENS[slot % SLOT_TOKENS.length]
export const kindToken = (kind: string) => slotToken(kindSlot(kind))
export const kindCssColor = (kind: string) => `hsl(var(${kindToken(kind)}))`

/** Resolves a token for canvas drawing, which cannot read CSS variables itself. */
export function tokenColor(element: Element, token: string) {
  const value = getComputedStyle(element).getPropertyValue(token).trim()
  return value ? `hsl(${value})` : "gray"
}
