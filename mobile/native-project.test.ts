import { createHash } from "node:crypto"
import { existsSync, readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { describe, expect, it } from "vitest"

const root = path.dirname(fileURLToPath(import.meta.url))
const leakedHost = /aigateway|example\.test|127\.0\.0\.1|localhost:\d+/

function read(rel: string): string {
  return readFileSync(path.join(root, rel), "utf8")
}

describe("native phone apps", () => {
  it("ships an iOS app that asks for the camera to scan a pairing QR", () => {
    const plistPath = path.join(root, "ios/App/App/Info.plist")
    expect(existsSync(plistPath), "ios project missing — cap add ios").toBe(true)
    const plist = read("ios/App/App/Info.plist")
    expect(plist).toMatch(/NSCameraUsageDescription/)
    expect(plist).toMatch(/NSAllowsArbitraryLoads/)
    expect(plist).not.toMatch(leakedHost)
    const pbx = read("ios/App/App.xcodeproj/project.pbxproj")
    expect(pbx).toMatch(/PRODUCT_BUNDLE_IDENTIFIER = com\.lubyruffy\.zwai/)
    expect(pbx).toMatch(/IPHONEOS_DEPLOYMENT_TARGET = 15\.0/)
    expect(pbx).not.toMatch(/IPHONEOS_DEPLOYMENT_TARGET = 14\.0/)
    const spm = read("ios/App/CapApp-SPM/Package.swift")
    expect(spm).toMatch(/CapacitorCamera/)
    expect(spm).toMatch(/CapacitorApp/)
    expect(spm).toMatch(/CapacitorBrowser/)
    expect(spm).toMatch(/\.iOS\(\.v15\)/)
    expect(spm).not.toMatch(leakedHost)
  })

  it("ships an Android app that asks for the camera to scan a pairing QR", () => {
    const manifestPath = path.join(root, "android/app/src/main/AndroidManifest.xml")
    expect(existsSync(manifestPath), "android project missing — cap add android").toBe(true)
    const manifest = read("android/app/src/main/AndroidManifest.xml")
    expect(manifest).toMatch(/android\.permission\.CAMERA/)
    expect(manifest).toMatch(/android\.permission\.REQUEST_INSTALL_PACKAGES/)
    expect(manifest).toMatch(/usesCleartextTraffic="true"/)
    expect(manifest).toMatch(/enableOnBackInvokedCallback="true"/)
    expect(manifest).not.toMatch(leakedHost)
    const gradle = read("android/app/build.gradle")
    expect(gradle).toMatch(/applicationId ["']com\.lubyruffy\.zwai["']/)
    expect(gradle).not.toMatch(leakedHost)
  })

  it("ships an Android release build that reads version and signing from outside the tree", () => {
    // A password or version baked into build.gradle would ship in git and
    // freeze Play versionCode at 1. CLI injects both; the file only reads.
    const gradle = read("android/app/build.gradle")
    expect(gradle).toMatch(/zwaiVersionName/)
    expect(gradle).toMatch(/zwaiVersionCode/)
    expect(gradle).toMatch(/ANDROID_VERSION/)
    expect(gradle).toMatch(/ANDROID_VERSION_CODE/)
    expect(gradle).toMatch(/signingConfigs/)
    expect(gradle).toMatch(/ANDROID_KEYSTORE/)
    expect(gradle).toMatch(/keystore\.properties/)
    expect(gradle).toMatch(/signingConfigs\.debug/)
    expect(gradle).not.toMatch(/storePassword\s+["'][^"']+["']/)
    expect(gradle).not.toMatch(/keyPassword\s+["'][^"']+["']/)
    expect(gradle).not.toMatch(/storeFile\s+file\(["'][^"']+["']\)/)
    expect(gradle).not.toMatch(leakedHost)

    const ignore = read("android/.gitignore") + "\n" + readFileSync(path.join(root, "../.gitignore"), "utf8")
    expect(ignore).toMatch(/keystore\.properties/)
    expect(ignore).toMatch(/\*\.jks/)
    expect(ignore).toMatch(/\*\.keystore/)

    const makefile = readFileSync(path.join(root, "../Makefile"), "utf8")
    expect(makefile).toMatch(/mobile-android-release/)
    const pkg = JSON.parse(read("package.json"))
    expect(pkg.scripts["cap:android-release"]).toMatch(/android-release/)
  })

  it("asks the page before Android back finishes the activity", () => {
    // A conversation is React state. WebView history stays empty, so the
    // activity back dispatcher must not finish until the page says it is
    // on the inbox or the unbound scan screen.
    const java = read("android/app/src/main/java/com/lubyruffy/zwai/MainActivity.java")
    const hook = read("src/lib/android-back.ts").match(/ANDROID_BACK_HOOK = "([^"]+)"/)?.[1]
    expect(hook).toBeTruthy()
    expect(java).toContain(hook)
    expect(java).toMatch(/OnBackPressedCallback/)
    expect(java).toMatch(/evaluateJavascript/)
    expect(java).toMatch(/setEnabled\(false\)/)
    expect(java).toMatch(/postDelayed/)
    expect(java).toMatch(/removeCallbacks/)
    const finishes = java.split("finish()").length - 1
    expect(finishes).toBe(1)
    expect(java).toMatch(/private void leaveApp[\s\S]*finish\(\)/)
    const registered = java.indexOf("registerPlugin(AppUpdatePlugin.class)")
    const bridge = java.indexOf("super.onCreate")
    expect(registered).toBeGreaterThan(-1)
    expect(bridge).toBeGreaterThan(registered)
    const plugin = read("android/app/src/main/java/com/lubyruffy/zwai/AppUpdatePlugin.java")
    expect(plugin).toMatch(/setInstanceFollowRedirects\(false\)/)
    expect(plugin).toMatch(/LubyRuffy\/eino-swarm/)
    expect(plugin).toMatch(/release-assets\.githubusercontent\.com/)
    expect(plugin).toMatch(/\.fileprovider/)
    expect(plugin).toMatch(/ClipData/)
    expect(plugin).toMatch(/allowed\(current, hop == 0\)/)
    expect(plugin).toMatch(/REQUEST_INSTALL_PACKAGES|install_permission|ACTION_MANAGE_UNKNOWN_APP_SOURCES/)
    const gradle = read("android/app/capacitor.build.gradle")
    expect(gradle).toMatch(/capacitor-app/)
    expect(gradle).toMatch(/capacitor-browser/)
  })

  it("does not compile a hub URL into the Capacitor config", () => {
    const cfg = read("capacitor.config.ts")
    expect(cfg).toMatch(/cleartext:\s*true/)
    expect(cfg).not.toMatch(/https?:\/\//)
    expect(cfg).not.toMatch(leakedHost)
  })

  it("ships the zwai mark as the launcher, not Capacitor's cyan lattice", () => {
    const ios = readBin("ios/App/App/Assets.xcassets/AppIcon.appiconset/AppIcon-512@2x.png")
    expect(pngSize(ios)).toEqual([1024, 1024])
    const android = readBin("android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png")
    expect(pngSize(android)).toEqual([192, 192])
    const splash = readBin("ios/App/App/Assets.xcassets/Splash.imageset/splash-2732x2732.png")
    expect(pngSize(splash)).toEqual([2732, 2732])
    // Hashes of the cap add defaults. A re-add that restores those files
    // is a product bug: the home screen would show someone else's logo.
    expect(sha256(ios)).not.toBe(
      "29e4777e319de3ee5a52c3a8004ec19d0568414004257e36d7c94a077d71c93b",
    )
    expect(sha256(android)).not.toBe(
      "87cb2f2ffe992652bb4fa768c73719a37b5852ab17fbf8e170e888f7a42b0761",
    )
    expect(sha256(splash)).not.toBe(
      "1b5002b74a5500e697298ced06ca2811ac33f2771f236f3c720ff23243890530",
    )
  })
})

function readBin(rel: string): Buffer {
  return readFileSync(path.join(root, rel))
}

function pngSize(buf: Buffer): [number, number] {
  if (buf[0] !== 0x89 || buf[1] !== 0x50) {
    throw new Error("not a png")
  }
  return [buf.readUInt32BE(16), buf.readUInt32BE(20)]
}

function sha256(buf: Buffer): string {
  return createHash("sha256").update(buf).digest("hex")
}
