import type { CapacitorConfig } from "@capacitor/cli"

const config: CapacitorConfig = {
  appId: "com.lubyruffy.zwai",
  appName: "zwai",
  webDir: "dist",
  // Account mode uses the configurable default Gateway; anonymous QR offers
  // carry their Hub URL. Cleartext keeps independent LAN hubs usable.
  android: {
    allowMixedContent: true,
  },
  server: {
    androidScheme: "https",
    cleartext: true,
  },
  plugins: {
    // Private model endpoints may not allow the WebView origin, so their
    // discovery uses the platform HTTP stack. Gateway control/catalog use
    // the preserved WebView fetch for redirect and abort guarantees. Model
    // completions use StreamBody to read bytes as they arrive.
    CapacitorHttp: {
      enabled: true,
    },
  },
}

export default config
