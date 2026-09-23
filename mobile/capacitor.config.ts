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
    // HTTP stack places the call. A buffered body is still a valid reply.
    CapacitorHttp: {
      enabled: true,
    },
  },
}

export default config
