import { afterEach } from "vitest"

import "@testing-library/jest-dom/vitest"

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

const { setLocale } = await import("@/lib/i18n")

afterEach(() => {
  memory.clear()
  setLocale("en")
})
