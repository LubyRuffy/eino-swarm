import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { App } from "@/App"
import "@/index.css"
import { applyAppearance, readAppearance } from "@/lib/appearance"
import { applyLocale, readLocalePref } from "@/lib/i18n"
import { installTitlebarZoom } from "@/lib/titlebar"

// The native title bar is hidden, so a double-click on the app chrome has to
// ask the window to zoom. Dragging is already native; this is only zoom.
applyLocale(readLocalePref())
applyAppearance(readAppearance())
installTitlebarZoom()

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
