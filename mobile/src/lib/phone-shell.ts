export type PhoneShell = "scan" | "home"

/** Scan is only the empty state. A saved ticket or a saved model paints the
 *  inbox chrome, never the bind form as the app. */
export function phoneShell(hostCount: number, providerCount = 0): PhoneShell {
  return hostCount > 0 || providerCount > 0 ? "home" : "scan"
}
