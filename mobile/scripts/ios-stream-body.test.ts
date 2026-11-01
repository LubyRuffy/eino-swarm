// @vitest-environment node
import { spawnSync } from "node:child_process"
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { afterAll, beforeAll, describe, expect, it } from "vitest"

const mobile = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const plugin = path.join(mobile, "ios/App/App/StreamBodyPlugin.swift")
const scenario = path.join(mobile, "scripts/ios-stream-body-regression.swift")
let root = ""
let keepEvidence = false

// The transport and UTF-8 reader below are the complete production Swift file.
// Only the Capacitor bridge is replaced: UIKit cannot be imported by a host test.
const bridge = `import Foundation
public protocol CAPBridgedPlugin {}
public let CAPPluginReturnPromise = "promise"
public struct CAPPluginMethod {
    public init(name: String, returnType: String) {}
}
@objc open class CAPPlugin: NSObject {
    public var onEvent: ((String, [String: Any]) -> Void)?
    public func notifyListeners(_ event: String, data: [String: Any]) { onEvent?(event, data) }
}
@objc public class CAPPluginCall: NSObject {
    private let options: [String: Any]
    public var onResolve: (([String: Any]) -> Void)?
    public var onReject: ((String) -> Void)?
    public init(_ options: [String: Any]) { self.options = options }
    public func getString(_ key: String) -> String? { options[key] as? String }
    public func getObject(_ key: String) -> [String: Any]? { options[key] as? [String: Any] }
    public func getDouble(_ key: String) -> Double? { options[key] as? Double }
    public func resolve(_ data: [String: Any] = [:]) { onResolve?(data) }
    public func reject(_ message: String) { onReject?(message) }
}
`

function run(command: string, args: string[], label: string, timeout = 30_000) {
  const result = spawnSync(command, args, { cwd: root, encoding: "utf8", timeout })
  writeFileSync(path.join(root, `${label}.json`), JSON.stringify({
    command, args, status: result.status, signal: result.signal, error: result.error?.message,
    stdout: result.stdout, stderr: result.stderr, umask: process.umask().toString(8),
  }, null, 2), { mode: 0o600 })
  if (result.status !== 0) keepEvidence = true
  expect(result.status, `${label}; signal: ${result.signal}; error: ${result.error?.message ?? ""}; evidence: ${root}\n${result.stdout}\n${result.stderr}`).toBe(0)
  return result
}

describe.skipIf(process.platform !== "darwin")("iOS native model response streaming", () => {
  beforeAll(() => {
    root = mkdtempSync(path.join(tmpdir(), "zwai-ios-stream-test-"))
    writeFileSync(path.join(root, "Capacitor.swift"), bridge, { mode: 0o600 })
    writeFileSync(path.join(root, "StreamBodyPlugin.source.swift"), readFileSync(plugin), { mode: 0o600 })
    run("swiftc", ["-swift-version", "5", "-emit-module", "-emit-object", "-parse-as-library",
      "-module-name", "Capacitor", "-emit-module-path", path.join(root, "Capacitor.swiftmodule"),
      path.join(root, "Capacitor.swift"), "-o", path.join(root, "Capacitor.o")], "bridge-compile")
    run("swiftc", ["-swift-version", "5", "-I", root, plugin, scenario,
      path.join(root, "Capacitor.o"), "-o", path.join(root, "stream-regression")], "production-compile")
  }, 60_000)

  afterAll(() => {
    if (root && !keepEvidence) rmSync(root, { recursive: true, force: true })
  })

  it.each(["sequential", "nonzero", "unicode", "tails", "urlsession"])(
    "keeps the native reader alive and preserves response bytes: %s", (name) => {
      run(path.join(root, "stream-regression"), [name], name, 15_000)
    }, 20_000,
  )
})
