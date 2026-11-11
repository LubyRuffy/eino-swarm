// @vitest-environment node
import { spawnSync } from "node:child_process"
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import path from "node:path"
import vm from "node:vm"
import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest"

const packageRoot = path.resolve("node_modules/@capacitor/ios")
const nativeRoot = path.join(packageRoot, "Capacitor/Capacitor")
const script = readFileSync(path.join(nativeRoot, "assets/native-bridge.js"), "utf8")

function boot(cookies: boolean, http: boolean, platform: "ios" | "android", registeredScript: string) {
  const prompts: string[] = []
  class NativeDocument {
    get cookie() { return "original=value" }
    set cookie(_value: string) {}
  }
  const document = Object.assign(new NativeDocument(), {
    addEventListener() {}, createEvent: () => ({ initEvent() {} }),
    dispatchEvent: () => true,
  })
  class NativeXHR {
    abort() {} open() {} send() {} setRequestHeader() {}
    getAllResponseHeaders() { return "" } getResponseHeader() { return null }
  }
  const originalFetch = () => Promise.resolve({})
  const context = vm.createContext({
    Document: NativeDocument, HTMLDocument: NativeDocument, document,
    XMLHttpRequest: NativeXHR, fetch: originalFetch,
    console: Object.fromEntries(["debug", "error", "info", "log", "trace", "warn"].map(name => [name, () => {}])),
    navigator: {}, URL, URLSearchParams,
    prompt(body: string) {
      const value = JSON.parse(body) as { type: string }
      // Model a blocked native startup round-trip at the actual SDK boundary.
      if (["CapacitorCookies.isEnabled", "CapacitorHttp"].includes(value.type)) {
        throw new Error("bootstrap-sync-prompt-would-block")
      }
      prompts.push(value.type)
      return value.type === "CapacitorCookies.get" ? "native=value" : ""
    },
    ...(platform === "ios" ? { webkit: { messageHandlers: { bridge: { postMessage() {} } } } } : {
      androidBridge: { postMessage() {} },
      CapacitorCookiesAndroidInterface: { isEnabled: () => cookies },
      CapacitorHttpAndroidInterface: { isEnabled: () => http },
    }),
  })
  vm.runInContext("window = globalThis", context)
  // Execute the JavaScript emitted by the actual native config exporter.
  const exporter = readFileSync(path.join(nativeRoot, "JSExport.swift"), "utf8")
  const literal = exporter.match(/let data = "([^\n]+)"/)
  if (!literal) throw new Error("Native global config exporter changed")
  const values: Record<string, string> = {
    isDebug: "false", loggingEnabled: "false", localUrl: "capacitor://localhost",
    cookiesEnabled: String(cookies), httpEnabled: String(http),
  }
  const globals = literal[1].replace(/\\\((\w+)\)/g, (_match, name: string) => {
    if (!(name in values)) throw new Error("Unknown native config interpolation")
    return values[name]
  })
  vm.runInContext(globals, context)
  vm.runInContext(registeredScript, context, { timeout: 1000 })
  return { context, prompts, document, originalFetch }
}

it("reproduces the original SDK's synchronous startup round-trip", () => {
  expect(() => boot(false, false, "ios", script)).toThrow("bootstrap-sync-prompt-would-block")
})

it("installs the static config before navigation and fails closed", () => {
  const source = readFileSync(path.resolve("ios/App/App/BridgeViewController.swift"), "utf8")
  const loaded = source.slice(source.indexOf("func capacitorDidLoad"), source.indexOf("func viewDidLoad"))
  const appeared = source.slice(source.indexOf("func viewDidLoad"), source.indexOf("func showBootstrapFailure"))
  const failure = source.slice(source.indexOf("func showBootstrapFailure"))
  expect(loaded).toContain("IOSBootstrap.install")
  expect(loaded).toContain("removeAllUserScripts()")
  expect(appeared.indexOf("bootstrapFailed")).toBeLessThan(appeared.indexOf("super.viewDidLoad()"))
  expect(appeared).toContain("showBootstrapFailure()")
  expect(appeared).not.toContain("view =")
  expect(failure).toContain("view.addSubview(message)")
  expect(failure).not.toContain("view =")
  const project = readFileSync(path.resolve("ios/App/App.xcodeproj/project.pbxproj"), "utf8")
  expect(project).toContain("IOSBootstrap.swift in Sources")
})

describe.skipIf(process.platform !== "darwin")("native iOS bootstrap registration", () => {
  let evidence = ""
  let keepEvidence = false
  let serial = 0
  const adapter = path.resolve("ios/App/App/IOSBootstrap.swift")
  const scenario = path.resolve("scripts/ios-bootstrap-regression.swift")

  function run(command: string, args: string[], label: string) {
    const result = spawnSync(command, args, { encoding: "utf8", timeout: 30_000 })
    writeFileSync(path.join(evidence, label + ".json"), JSON.stringify({
      command, args, status: result.status, signal: result.signal, error: result.error?.message,
      stdout: result.stdout, stderr: result.stderr, umask: process.umask().toString(8),
    }, null, 2), { mode: 0o600 })
    if (result.status !== 0) keepEvidence = true
    expect(result.status, `${label}; evidence: ${evidence}\n${result.stdout}\n${result.stderr}`).toBe(0)
  }

  beforeAll(() => {
    evidence = mkdtempSync(path.join(tmpdir(), "zwai-ios-bootstrap-test-"))
    // These are real WebKit objects and the complete production Swift helper.
    // No WebView, application window, fake Capacitor API or native GUI is used.
    run("xcrun", ["swiftc", "-swift-version", "5", adapter, scenario,
      "-o", path.join(evidence, "bootstrap")], "compile")
  }, 40_000)
  afterEach(({ task }) => { if (task.result?.state === "fail") keepEvidence = true })
  afterAll(() => { if (evidence && !keepEvidence) rmSync(evidence, { recursive: true, force: true }) })

  function register(cookies: boolean, http: boolean, check = "fixed") {
    const name = `${serial++}-${check}`
    const output = path.join(evidence, name + ".js")
    run(path.join(evidence, "bootstrap"), [check, path.join(nativeRoot, "assets/native-bridge.js"),
      output, String(cookies), String(http)], name)
    return existsSync(output) ? readFileSync(output, "utf8") : ""
  }

  it.each([[false, false], [true, false], [false, true], [true, true]])(
    "boots the Swift-registered SDK without static config prompts: cookies=%s http=%s", (cookies, http) => {
    const registered = register(cookies, http)
    expect(registered).not.toContain("CapacitorCookies.isEnabled")
    expect(registered).not.toContain("type: 'CapacitorHttp'")
    expect(registered).toContain("CapacitorNativeConfig")
    const state = boot(cookies, http, "ios", registered)
    expect(state.prompts).toEqual([])
    expect(state.context.fetch === state.originalFetch).toBe(!http)
    if (cookies) {
      expect(state.document.cookie).toBe("native=value")
      state.document.cookie = "changed=value"
      expect(state.prompts).toEqual(["CapacitorCookies.get", "CapacitorCookies.set"])
    } else {
      expect(state.document.cookie).toBe("original=value")
      expect(state.prompts).toEqual([])
    }
  },
  )

  it.each([[false, false], [true, false], [false, true], [true, true]])(
  "keeps the SDK Android config interfaces: cookies=%s http=%s", (cookies, http) => {
    const state = boot(cookies, http, "android", register(cookies, http))
    expect(state.prompts).toEqual([])
    expect(state.context.fetch === state.originalFetch).toBe(!http)
  },
  )

  it.each(["missing", "duplicate", "bad-time", "all-frames", "drift"])(
    "rejects unsupported registration before changing any script: %s", (check) => {
      expect(register(false, false, check)).toBe("")
    },
  )
})
