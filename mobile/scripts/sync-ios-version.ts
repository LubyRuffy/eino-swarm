import { readFileSync, writeFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

import { parseAndroidVersion } from "./android-release.ts"

export function syncedProjectVersion(project: string, version: string): string {
  const { versionName, versionCode } = parseAndroidVersion({ version })
  const buildMatches = project.match(/CURRENT_PROJECT_VERSION = \d+;/g) ?? []
  const versionMatches = project.match(/MARKETING_VERSION = [\d.]+;/g) ?? []
  if (buildMatches.length === 0 || versionMatches.length === 0) {
    throw new Error("iOS project version settings are missing")
  }
  return project
    .replaceAll(/CURRENT_PROJECT_VERSION = \d+;/g, `CURRENT_PROJECT_VERSION = ${versionCode};`)
    .replaceAll(/MARKETING_VERSION = [\d.]+;/g, `MARKETING_VERSION = ${versionName};`)
}

const scriptPath = fileURLToPath(import.meta.url)
if (process.argv[1] && path.resolve(process.argv[1]) === scriptPath) {
  const root = path.resolve(path.dirname(scriptPath), "..")
  const pkg = JSON.parse(readFileSync(path.join(root, "package.json"), "utf8")) as { version: string }
  const projectPath = path.join(root, "ios/App/App.xcodeproj/project.pbxproj")
  const plist = readFileSync(path.join(root, "ios/App/App/Info.plist"), "utf8")
  if (!plist.includes("$(MARKETING_VERSION)") || !plist.includes("$(CURRENT_PROJECT_VERSION)")) {
    throw new Error("iOS Info.plist must derive its version from project settings")
  }
  const before = readFileSync(projectPath, "utf8")
  const after = syncedProjectVersion(before, pkg.version)
  if (after !== before) writeFileSync(projectPath, after)
  process.stdout.write(`iOS version ${pkg.version}\n`)
}
