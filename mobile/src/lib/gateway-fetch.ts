import { Capacitor } from "@capacitor/core"

/** The pinned native bridge preserves this before replacing fetch. Its HTTP
 * proxy drops redirect/abort options; Gateway CORS allows the original fetch. */
export const gatewayFetch: typeof fetch = (resource, options) => {
  const original = (window as Window & { CapacitorWebFetch?: typeof fetch }).CapacitorWebFetch
  if (typeof original === "function") {
    return original.call(window, resource, { ...options, redirect: "error", credentials: "omit", cache: "no-store" })
  }
  if (Capacitor.isNativePlatform()) throw new TypeError("gateway_transport_unavailable")
  return fetch(resource, { ...options, redirect: "error", credentials: "omit", cache: "no-store" })
}
