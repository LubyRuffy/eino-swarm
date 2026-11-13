/** Place the tools menu above the composer box, not over the text being typed. */
export function composerToolsMenuPlacement(
  button: { top: number },
  box: { top: number },
): { sideOffset: number; maxHeight: number } {
  const sideOffset = Math.max(8, Math.round(button.top - box.top) + 8)
  const available = Math.max(96, Math.round(box.top) - 12)
  return { sideOffset, maxHeight: Math.min(320, available) }
}
