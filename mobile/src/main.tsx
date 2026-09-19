import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { App } from "./app"
import { applySystemTheme } from "./lib/theme"
import "./index.css"

applySystemTheme()

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
