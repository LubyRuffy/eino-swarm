/** Same rule as frontend/src/lib/agent-label.ts. The phone bundle does not
 *  import the desktop sources. The label is the agent id resume_agent
 *  takes. A #n badge is a different token. */

const JOB = /^[\p{L}\p{N}]+(?:[-_][\p{L}\p{N}]+)*$/u

export function agentRosterLabel(
  role: string,
  id: string,
): { name: string; tag: string } {
  const rawRole = role.trim()
  const rawID = id.trim()
  const name = jobName(rawRole || rawID)
  if (!rawID || rawID === name) return { name: name || rawID, tag: "" }
  if (idContinuesJob(rawID, name)) return { name: rawID, tag: "" }
  return { name, tag: rawID }
}

function idContinuesJob(id: string, job: string): boolean {
  if (!job || !id.startsWith(job) || id.length === job.length) return false
  const next = id[job.length]
  return next === "-" || next === "_" || next === "/" || next === "\\"
}

function jobName(role: string): string {
  const slashed = role.includes("/") || role.includes("\\")
  let segment = (role.split(/[/\\]/)[0] ?? role).trim()
  if (slashed) segment = segment.replace(/[-_]\d+$/u, "")
  const word = segment.match(JOB)
  let name = word?.[0] ?? ""
  if (!name) return role.trim()
  if ([...name].length > 32) name = [...name].slice(0, 32).join("")
  return name
}

export function agentRosterText(role: string, id: string): string {
  const { name, tag } = agentRosterLabel(role, id)
  return tag ? `${name} ${tag}` : name
}
