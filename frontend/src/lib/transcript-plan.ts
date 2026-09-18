/** Short transcript lines. The banner holds the markdown. */
export function planNotice(kind: string, text?: string): string | null {
  switch (kind) {
    case "plan":
      return "Planning."
    case "plan_updated":
      return "Plan updated."
    case "plan_implemented":
      return text?.trim() || "The human accepted the plan. Execute it."
    case "plan_cancelled":
      return "Left planning."
    default:
      return null
  }
}

export function isPlanEvent(kind: string): boolean {
  return (
    kind === "plan" ||
    kind === "plan_updated" ||
    kind === "plan_implemented" ||
    kind === "plan_cancelled"
  )
}
