export type PhoneShell = "scan" | "home"

/** Scan is only the empty state. Any saved ticket paints the inbox chrome
 *  (tabs + connecting skeleton), never the bind form as the app. */
export function phoneShell(hostCount: number): PhoneShell {
  return hostCount > 0 ? "home" : "scan"
}
