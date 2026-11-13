/** Place the tools menu above the composer box, not over the text being typed. */
export function composerToolsMenuPlacement(
  button: { top: number },
  box: { top: number },
): { sideOffset: number; maxHeight: number } {
  const sideOffset = Math.max(8, Math.round(button.top - box.top) + 8)
  const available = Math.max(96, Math.round(box.top) - 12)
  return { sideOffset, maxHeight: Math.min(320, available) }
}

/** Blank means the whole menu. Matching ignores case and surrounding space. */
export function toolMenuQuery(value: string): string {
  return value.trim().toLowerCase()
}

export function toolMenuMatches(query: string, fields: string[]): boolean {
  const needle = toolMenuQuery(query)
  if (needle === "") return true
  return fields.some((field) => field.toLowerCase().includes(needle))
}

/** A connection match keeps that connection's tools. Otherwise only names that contain the query stay. */
export function filterRemoteTools<T extends { name: string }>(
  tools: readonly T[],
  query: string,
  connection: string,
  connectionID: string,
): { shown: boolean; tools: T[] } {
  if (toolMenuQuery(query) === "") return { shown: true, tools: [...tools] }
  if (toolMenuMatches(query, [connection, connectionID])) return { shown: true, tools: [...tools] }
  const matched = tools.filter((tool) => tool.name.toLowerCase().includes(toolMenuQuery(query)))
  return { shown: matched.length > 0, tools: matched }
}

/** The extra line stays off the row. Hover shows the name and, when it adds anything, the summary. */
export function toolHoverText(name: string, summary: string): string {
  const detail = summary.trim()
  if (!detail || detail === name) return name
  return `${name}\n${detail}`
}
