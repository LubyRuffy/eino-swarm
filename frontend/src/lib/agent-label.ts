/** The roster label is the agent id resume_agent takes. A #n badge is
 *  not that id. When the id is that job plus a separator (`-`, `_`, `/`),
 *  the whole id is the label. A different token stays beside the job. */

const JOB = /^[\p{L}\p{N}]+(?:[-_][\p{L}\p{N}]+)*$/u

export function agentRosterLabel(
  role: string,
  id: string,
): { name: string; tag: string } {
  const rawRole = role.trim()
  const rawID = id.trim()
  const name = jobName(rawRole || rawID)
  if (!rawID || rawID === name) return { name: name || rawID, tag: "" }
  // worker-4 continues the job. workers-1 does not: the next character
  // is still a letter, so the job stays and the id sits beside it.
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
