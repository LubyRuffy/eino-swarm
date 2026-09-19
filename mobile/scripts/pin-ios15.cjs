#!/usr/bin/env node
// Capacitor's iOS template still pins 14.0. Xcode 27's simulator SDK
// refuses anything below 15. cap sync must not snap us back.
const fs = require("node:fs")
const path = require("node:path")

const root = path.resolve(__dirname, "..")
const pbx = path.join(root, "ios/App/App.xcodeproj/project.pbxproj")
const pkg = path.join(root, "ios/App/CapApp-SPM/Package.swift")

if (fs.existsSync(pbx)) {
  const next = fs
    .readFileSync(pbx, "utf8")
    .replaceAll("IPHONEOS_DEPLOYMENT_TARGET = 14.0;", "IPHONEOS_DEPLOYMENT_TARGET = 15.0;")
  fs.writeFileSync(pbx, next)
}
if (fs.existsSync(pkg)) {
  const next = fs.readFileSync(pkg, "utf8").replaceAll(".iOS(.v14)", ".iOS(.v15)")
  fs.writeFileSync(pkg, next)
}
