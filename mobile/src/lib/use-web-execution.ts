import { useEffect, useState } from "react"
import { loadGatewaySession } from "./gateway-account"
import { approvedWebSession, loadPcWebExecution, loadWebExecution, watchWebAuthority } from "./web-execution"

export function useWebExecution() {
  const read = () => {
    const session = loadGatewaySession()
    return { mode: loadWebExecution(session), pcMode: loadPcWebExecution(session), cloudSignedIn: Boolean(approvedWebSession()), authority: session ? session.origin + "\x00" + session.user.id : "" }
  }
  const [value, setValue] = useState(read)
  useEffect(() => watchWebAuthority(() => {
    const next = read()
    setValue((current) => current.mode === next.mode && current.pcMode === next.pcMode && current.cloudSignedIn === next.cloudSignedIn && current.authority === next.authority ? current : next)
  }), [])
  return value
}
