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
    expect(spm).toMatch(/\.iOS\(\.v15\)/)
    expect(spm).not.toMatch(leakedHost)
  })

  it("ships an Android app that asks for the camera to scan a pairing QR", () => {
    const manifestPath = path.join(root, "android/app/src/main/AndroidManifest.xml")
    expect(existsSync(manifestPath), "android project missing — cap add android").toBe(true)
    const manifest = read("android/app/src/main/AndroidManifest.xml")
    expect(manifest).toMatch(/android\.permission\.CAMERA/)
    expect(manifest).toMatch(/usesCleartextTraffic="true"/)
    expect(manifest).not.toMatch(leakedHost)
    const gradle = read("android/app/build.gradle")
    expect(gradle).toMatch(/applicationId ["']com\.lubyruffy\.zwai["']/)
    expect(gradle).not.toMatch(leakedHost)
  })

  it("does not compile a hub URL into the Capacitor config", () => {
    const cfg = read("capacitor.config.ts")
    expect(cfg).toMatch(/cleartext:\s*true/)
    expect(cfg).not.toMatch(/https?:\/\//)
    expect(cfg).not.toMatch(leakedHost)
  })
})
