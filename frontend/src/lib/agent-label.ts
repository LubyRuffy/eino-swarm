/** A role is a job name. The id is that name plus a sequence.
 *  One worker per role makes a model invent a path so the next spawn is
 *  not a resume. The roster shows the job, not the path. */

const JOB = /^[\p{L}\p{N}]+(?:[-_][\p{L}\p{N}]+)*$/u

export function agentRosterLabel(
  role: string,
  id: string,
): { name: string; tag: string } {
  const rawRole = role.trim()
  const rawID = id.trim()
  const name = jobName(rawRole || rawID)
  return { name, tag: rosterTag(rawRole, rawID, name) }
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

function rosterTag(role: string, id: string, name: string): string {
  if (!id || id === name) return ""
  if (role && id.startsWith(`${role}-`)) {
    const rest = id.slice(role.length + 1)
    if (/^\d+$/.test(rest)) return `#${rest}`
  }
  if (id !== role) return id
  return ""
}

export function agentRosterText(role: string, id: string): string {
  const { name, tag } = agentRosterLabel(role, id)
  return tag ? `${name} ${tag}` : name
}
