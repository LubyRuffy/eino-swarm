import type { CapacitorConfig } from "@capacitor/cli"

const config: CapacitorConfig = {
  appId: "com.lubyruffy.zwai",
  appName: "zwai",
  webDir: "dist",
  // Hub URL is typed in on the PC, never compiled in. Cleartext is allowed so
  // a LAN pairlinkd over http still binds; production should still be https.
  android: {
    allowMixedContent: true,
  },
  server: {
    androidScheme: "https",
    cleartext: true,
  },
  plugins: {
    // The webview origin is not one a model endpoint allows. The platform
    // HTTP stack places discover and the other calls. A completion does not
    // use it: that stack returns the POST body in one piece. StreamBody
    // reads the bytes as they arrive.
    CapacitorHttp: {
      enabled: true,
    },
  },
}

export default config
