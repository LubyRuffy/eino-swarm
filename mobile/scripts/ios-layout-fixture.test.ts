import { spawnSync } from "node:child_process"
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { afterEach, expect, it } from "vitest"

const roots: string[] = []
afterEach(() => { for (const root of roots.splice(0)) rmSync(root, { recursive: true, force: true }) })

function fixture(bridge = "import UIKit\nclass Bridge {\n        super.capacitorDidLoad()\n}\n") {
  const root = mkdtempSync(path.join(tmpdir(), "zwai-ios-fixture-test-"))
  roots.push(root)
  const mobile = path.join(root, "mobile")
  const app = path.join(mobile, "ios/App/App")
  for (const directory of ["scripts", "node_modules", "e2e", "ios/App/App/public", "ios/App/AppUITests"]) {
    mkdirSync(path.join(mobile, directory), { recursive: true })
  }
  const script = path.join(mobile, "scripts/ios-layout-fixture.py")
  cpSync(path.join(path.dirname(fileURLToPath(import.meta.url)), "ios-layout-fixture.py"), script)
  writeFileSync(path.join(app, "public/index.html"), "<html><head></head><body>checkout assets</body></html>")
  writeFileSync(path.join(app, "BridgeViewController.swift"), bridge)
  writeFileSync(path.join(mobile, "e2e/ios-wait-layout.swift"), "fixture driver")
  writeFileSync(path.join(mobile, "e2e/ios-gateway-account.swift"), 'let fixtureOrigin = "__ZWAI_GATEWAY_FIXTURE_ORIGIN__"')
  const ipa = path.join(root, "ipa-public")
  mkdirSync(path.join(ipa, "assets"), { recursive: true })
  const files = {
    "index.html": Buffer.from("<html><head></head><body>signed IPA assets</body></html>"),
    "assets/app.js": Buffer.from("window.formalCandidate = true;\n"),
    "assets/pixels.bin": Buffer.from([0, 255, 128, 10]),
  }
  for (const [name, bytes] of Object.entries(files)) writeFileSync(path.join(ipa, name), bytes)
  return { root, script, app, ipa, files }
}

it("keeps every signed IPA public byte while the private native wrapper selects offline input", () => {
  const f = fixture()
  const sourceBridge = readFileSync(path.join(f.app, "BridgeViewController.swift"))
  const run = spawnSync("python3", [f.script], { encoding: "utf8", env: {
    ...process.env, TMPDIR: f.root, IOS_LAYOUT_PUBLIC: f.ipa,
    IOS_LAYOUT_PRESERVE_PUBLIC: "1", IOS_LAYOUT_LOCALE: "zh", IOS_LAYOUT_QUERY: "/?mock=1&history=answer-backoff",
  } })
  expect(run.status, run.stderr).toBe(0)
  const app = path.join(run.stdout.trim(), "mobile/ios/App/App")
  for (const [name, bytes] of Object.entries(f.files)) {
    expect(readFileSync(path.join(app, "public", name))).toEqual(bytes)
    expect(readFileSync(path.join(f.ipa, name))).toEqual(bytes)
  }
  const bridge = readFileSync(path.join(app, "BridgeViewController.swift"), "utf8")
  expect(bridge).toContain(".atDocumentStart")
  expect(bridge).toContain("answer-backoff")
  expect(bridge).toContain('\\"zh\\"')
  expect(readFileSync(path.join(f.app, "BridgeViewController.swift"))).toEqual(sourceBridge)
})

it("refuses an unknown native bootstrap boundary without changing supplied IPA assets", () => {
  const f = fixture("import UIKit\n// no known navigation boundary\n")
  const run = spawnSync("python3", [f.script], { encoding: "utf8", env: {
    ...process.env, TMPDIR: f.root, IOS_LAYOUT_PUBLIC: f.ipa, IOS_LAYOUT_PRESERVE_PUBLIC: "1",
  } })
  expect(run.status).not.toBe(0)
  expect(run.stderr).toContain("exactly one bootstrap boundary")
  for (const [name, bytes] of Object.entries(f.files)) expect(readFileSync(path.join(f.ipa, name))).toEqual(bytes)
})

it("selects the account scenario and loopback gateway only in the private wrapper", () => {
  const f = fixture()
  const run = spawnSync("python3", [f.script], { encoding: "utf8", env: {
    ...process.env, TMPDIR: f.root, IOS_LAYOUT_PUBLIC: f.ipa, IOS_LAYOUT_PRESERVE_PUBLIC: "1",
    IOS_LAYOUT_GATEWAY_ORIGIN: "http://127.0.0.1:19440", IOS_LAYOUT_SCENARIO: "ios-gateway-account.swift",
    IOS_LAYOUT_QUERY: "/", IOS_LAYOUT_LOCALE: "en",
  } })
  expect(run.status, run.stderr).toBe(0)
  const root = run.stdout.trim()
  const app = path.join(root, "mobile/ios/App/App")
  for (const [name, bytes] of Object.entries(f.files)) expect(readFileSync(path.join(app, "public", name))).toEqual(bytes)
  const bridge = readFileSync(path.join(app, "BridgeViewController.swift"), "utf8")
  expect(bridge).toContain("zwai.phone.gateway.origin")
  expect(bridge).toContain("http://127.0.0.1:19440")
  expect(readFileSync(path.join(root, "mobile/ios/App/AppUITests/BindFlowTests.swift"), "utf8"))
    .toBe('let fixtureOrigin = "http://127.0.0.1:19440"')
  expect(readFileSync(path.join(f.app, "BridgeViewController.swift"), "utf8")).not.toContain("gateway.origin")
})

it.each(["https://gateway.example", "http://127.0.0.1:19440/path", "http://127.0.0.1:19440?token=private", "http://127.0.0.1:0", "http://127.0.0.1:invalid"])(
  "refuses a non-loopback or non-origin account fixture URL %s", (origin) => {
    const f = fixture()
    const run = spawnSync("python3", [f.script], { encoding: "utf8", env: {
      ...process.env, TMPDIR: f.root, IOS_LAYOUT_PUBLIC: f.ipa, IOS_LAYOUT_PRESERVE_PUBLIC: "1",
      IOS_LAYOUT_GATEWAY_ORIGIN: origin, IOS_LAYOUT_SCENARIO: "ios-gateway-account.swift",
    } })
    expect(run.status).not.toBe(0)
    expect(run.stderr).toContain("loopback origin")
  },
)

it("refuses account setup that would rewrite packaged web bytes", () => {
  const f = fixture()
  const run = spawnSync("python3", [f.script], { encoding: "utf8", env: {
    ...process.env, TMPDIR: f.root, IOS_LAYOUT_PUBLIC: f.ipa,
    IOS_LAYOUT_GATEWAY_ORIGIN: "http://127.0.0.1:19440", IOS_LAYOUT_SCENARIO: "ios-gateway-account.swift",
  } })
  expect(run.status).not.toBe(0)
  expect(run.stderr).toContain("preserve packaged public bytes")
})

it("refuses the account scenario before copying when no fixture origin was supplied", () => {
  const f = fixture()
  const run = spawnSync("python3", [f.script], { encoding: "utf8", env: {
    ...process.env, TMPDIR: f.root, IOS_LAYOUT_PUBLIC: f.ipa, IOS_LAYOUT_PRESERVE_PUBLIC: "1",
    IOS_LAYOUT_SCENARIO: "ios-gateway-account.swift",
  } })
  expect(run.status).not.toBe(0)
  expect(run.stderr).toContain("needs a loopback origin")
})

it("refuses a scenario path outside the reviewed mobile e2e directory", () => {
  const f = fixture()
  const run = spawnSync("python3", [f.script], { encoding: "utf8", env: {
    ...process.env, TMPDIR: f.root, IOS_LAYOUT_PUBLIC: f.ipa, IOS_LAYOUT_PRESERVE_PUBLIC: "1",
    IOS_LAYOUT_SCENARIO: "../../private.swift",
  } })
  expect(run.status).not.toBe(0)
  expect(run.stderr).toContain("reviewed e2e Swift file")
})
