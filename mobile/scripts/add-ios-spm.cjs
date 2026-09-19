#!/usr/bin/env node
// Capacitor CLI 7.6 lowercases --packagemanager then compares to "SPM",
// so `npx cap add ios --packagemanager SPM` still demands CocoaPods.
// This calls the same add path with the SPM template and package manager.
const path = require("node:path")
const { loadConfig } = require("@capacitor/cli/dist/config")
const { addCommand } = require("@capacitor/cli/dist/tasks/add")

async function main() {
  const config = await loadConfig()
  config.ios.packageManager = Promise.resolve("SPM")
  config.cli.assets.ios.platformTemplateArchive = "ios-spm-template.tar.gz"
  config.cli.assets.ios.platformTemplateArchiveAbs = path.resolve(
    config.cli.assetsDirAbs,
    "ios-spm-template.tar.gz",
  )
  await addCommand(config, "ios")
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
