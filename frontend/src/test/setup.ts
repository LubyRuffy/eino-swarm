import { afterEach } from "vitest"

import "@testing-library/jest-dom/vitest"
import { applyLocale } from "@/lib/i18n"

/** Node 25 exposes a global `localStorage` that is not Web Storage (it wants
 *  `--localstorage-file`) and it shadows jsdom's. The app talks to
 *  `localStorage` for chrome preferences; give tests a Map. */
const memory = new Map<string, string>()
const storage: Storage = {
  getItem: (key) => memory.get(key) ?? null,
  setItem: (key, value) => {
    memory.set(key, value)
  },
  removeItem: (key) => {
    memory.delete(key)
  },
  clear: () => memory.clear(),
  key: (index) => [...memory.keys()][index] ?? null,
  get length() {
    return memory.size
  },
}
Object.defineProperty(globalThis, "localStorage", {
  value: storage,
  configurable: true,
  writable: true,
})
if (typeof window !== "undefined") {
  Object.defineProperty(window, "localStorage", {
    value: storage,
    configurable: true,
    writable: true,
  })
}

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = ResizeObserverStub as typeof ResizeObserver
}

afterEach(async () => {
  memory.clear()
  applyLocale("en")
  // Imported lazily so a store test's `vi.mock("@/lib/api")` still wins.
  // A static import here would bind the real client before the mock exists.
  const { useApp } = await import("@/store/app")
  const { applyAppearance, defaultAppearance } = await import("@/lib/appearance")
  applyAppearance(defaultAppearance())
  useApp.setState({
    locale: "en",
    theme: "system",
    ...defaultAppearance(),
    schedules: [],
    scheduleUnread: 0,
    scheduleInboxOpen: false,
  })
})
