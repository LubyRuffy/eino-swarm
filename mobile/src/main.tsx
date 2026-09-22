import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { App } from "./app"
import { mockSavedLink, wantsMock } from "./lib/mock-link"
import { saveLink } from "./lib/store"
import { applySystemTheme } from "./lib/theme"
import "./index.css"

applySystemTheme()
// ?mock=1 boots straight onto the inbox against the scripted host, so the
// screens behind the scan form can be walked without a PC.
if (wantsMock()) saveLink(mockSavedLink())

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
