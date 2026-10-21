import { t, type Locale } from "./i18n"

export function parseCloudTool(text?: string): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(text ?? "")
    return typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)
      ? parsed as Record<string, unknown> : {}
  } catch {
    return {}
  }
}

export function cloudToolSummary(tool: Record<string, unknown>, locale: Locale = "en"): string {
  return t(locale, "notice.cloudTool", {
    type: typeof tool.type === "string" ? tool.type : "?",
    status: typeof tool.status === "string" ? tool.status : "?",
  })
}
