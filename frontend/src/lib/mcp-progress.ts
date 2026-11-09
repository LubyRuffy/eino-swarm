import type { MessageKey } from "./i18n"

export function isMCPTool(name: string): boolean {
  return name.startsWith("mcp_")
}

export function formatMCPProgress(progress: string, t: (key: MessageKey) => string): string {
  const message = progress.replace(/\s+/g, " ").trim()
  return message ? `${t("mcp.toolProgress")} · ${message}` : t("mcp.toolWaiting")
}
