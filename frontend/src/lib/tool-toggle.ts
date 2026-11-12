import type { Settings, ToolDescriptor } from "@/lib/types"

type ToolSettings = Settings["tools"]

/** A catalog tool is on unless its own default says otherwise. */
export function catalogToolOn(tool: ToolDescriptor, tools: ToolSettings): boolean {
  return tool.default_off
    ? tools.enabled.includes(tool.name)
    : !tools.disabled.includes(tool.name)
}

/** Record one catalog switch without dropping the other exception names. */
export function withCatalogTool(tool: ToolDescriptor, on: boolean, tools: ToolSettings): ToolSettings {
  const disabled = new Set(tools.disabled)
  const enabled = new Set(tools.enabled)
  if (tool.default_off) {
    if (on) enabled.add(tool.name)
    else enabled.delete(tool.name)
  } else if (on) disabled.delete(tool.name)
  else disabled.add(tool.name)
  return { ...tools, disabled: [...disabled], enabled: [...enabled] }
}
