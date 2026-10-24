package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Execute the emitted script, rather than matching its spelling: a selected
// conversation can otherwise be lost before the next five-second paint probe.
func TestNativeRecoveryLocationBridgeScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required to exercise the actual native recovery script:", err)
	}
	first := color.NRGBA{R: 40, G: 60, B: 80}
	second := color.NRGBA{R: 215, G: 195, B: 175}
	scripts := map[string]string{
		"current":        recoveryProbeScript("http://127.0.0.1:1234", "session-a", 2, "a53c9e1172d4b608", first, second, 0),
		"nextChallenge":  recoveryProbeScript("http://127.0.0.1:1234", "session-a", 2, "b53c9e1172d4b609", first, second, 0),
		"nextGeneration": recoveryProbeScript("http://127.0.0.1:1234", "session-a", 3, "b53c9e1172d4b609", first, second, 0),
		"nextSession":    recoveryProbeScript("http://127.0.0.1:1234", "session-b", 2, "b53c9e1172d4b609", first, second, 0),
		"nextDocument":   recoveryProbeScript("http://127.0.0.1:1234", "session-a", 2, "b53c9e1172d4b609", first, second, 1),
	}
	// A private historical output lets the exact same receiver assertions prove
	// RED against the frozen release without rewriting shared production files.
	if path := os.Getenv("ZWAI_TEST_RECOVERY_PROBE_SCRIPTS"); path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &scripts); err != nil {
			t.Fatal(err)
		}
	}
	body, err := json.Marshal(scripts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", recoveryLocationScriptReceiver)
	command.Stdin = bytes.NewReader(body)
	output, runErr := command.CombinedOutput()
	t.Log(string(output))
	if runErr != nil {
		t.Fatalf("actual recovery script receiver: %v", runErr)
	}
}

const recoveryLocationScriptReceiver = `
const assert = require("node:assert/strict");
const vm = require("node:vm");
const scripts = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const results = [];
const locationType = "zwai:recovery-location";
const healthType = "zwai:recovery";
function receiver(options = {}) {
  const messages = [], frames = [], listeners = new Set(), nodes = new Map();
  const calls = { add: 0, remove: 0, replace: 0, bridge: [], created: 0 };
  const window = { __zwaiRecoveryAttribution: options.attribution };
  const makeElement = () => ({
    style: {}, children: [], setAttribute() {},
    appendChild(child) { this.children.push(child); },
    replaceChildren() { calls.replace++; this.children = []; }
  });
  let root = options.root === "missing" ? null : { firstElementChild: options.root === "empty" ? null : {} };
  const document = {
    getElementById(id) { return id === "root" ? root : nodes.get(id); },
    createElement() { calls.created++; return makeElement(); },
    documentElement: { appendChild(child) { nodes.set(child.id, child); } }
  };
  window.addEventListener = (name, callback) => { assert.equal(name, locationType); calls.add++; listeners.add(callback); };
  window.removeEventListener = (name, callback) => { assert.equal(name, locationType); calls.remove++; listeners.delete(callback); };
  const install = (kind) => {
    const bridge = {};
    const send = function(message) {
      assert.equal(this, bridge, "the actual bridge receiver must retain its this binding");
      calls.bridge.push(kind);
      messages.push(message === "wails:runtime:ready" ? message : JSON.parse(message));
    };
    if (kind === "wails") { bridge.invoke = send; window._wails = bridge; }
    if (kind === "webkit") { bridge.postMessage = send; window.webkit = { messageHandlers: { external: bridge } }; }
    if (kind === "chrome") { bridge.postMessage = send; window.chrome = { webview: bridge }; }
  };
  for (const kind of options.bridges || ["wails"]) install(kind);
  const context = vm.createContext({ window, document, location: { origin: options.origin || "http://127.0.0.1:1234" }, requestAnimationFrame: (callback) => frames.push(callback) });
  return {
    window, messages, frames, listeners, calls, nodes,
    mountRoot() { root = { firstElementChild: {} }; },
    run(name = "current") { vm.runInContext(scripts[name], context, { filename: "zwai-native-recovery-probe.js", timeout: 1000 }); },
    notify() {
      const event = {};
      Object.defineProperty(event, "detail", { get() { throw new Error("event.detail is untrusted and must not be read"); } });
      for (const callback of [...listeners]) callback(event);
    },
    frame() { for (const callback of frames.splice(0)) callback(); },
    locations() { return messages.filter((message) => message.type === locationType); },
    health() { return messages.filter((message) => message.type === healthType); }
  };
}
function check(name, test) {
  try { test(); results.push({ name, passed: true }); }
  catch (error) { results.push({ name, passed: false, error: error.stack }); }
}
check("bootstrap publishes current global scope before any animation frame", () => {
  const r = receiver({ attribution: { thread_id: "th_initial", turn_id: "tn_initial", project_id: "pr_initial", draft_key: "thread:th_initial" } });
  r.run();
  assert.deepEqual(r.locations(), [{ type: locationType, session: "session-a", generation: 2, document: 0, thread_id: "th_initial", turn_id: "tn_initial", project_id: "pr_initial", draft_key: "thread:th_initial" }]);
  assert.equal(r.health().length, 0);
  assert.equal(r.messages[0].type, locationType);
  assert.equal("challenge" in r.locations()[0], false);
  assert.equal(r.listeners.size, 1);
});
check("DOM notification reads latest global attribution and never event.detail", () => {
  const r = receiver(); r.run();
  r.window.__zwaiRecoveryAttribution = { thread_id: "th_latest", turn_id: "tn_latest", project_id: "pr_latest", draft_key: "thread:th_latest" };
  r.notify();
  assert.equal(r.locations().length, 2);
  assert.deepEqual(r.locations()[1], { type: locationType, session: "session-a", generation: 2, document: 0, thread_id: "th_latest", turn_id: "tn_latest", project_id: "pr_latest", draft_key: "thread:th_latest" });
  r.window.__zwaiRecoveryAttribution = { thread_id: 1, turn_id: null, project_id: {}, draft_key: false };
  r.notify();
  assert.deepEqual(r.locations()[2], { type: locationType, session: "session-a", generation: 2, document: 0, thread_id: "", turn_id: "", project_id: "", draft_key: "" });
  assert.equal(r.health().length, 0, "location notifications cannot supply paint health");
});
check("repeated probes keep one listener and emit each independent nonce after two frames", () => {
  const r = receiver(); r.run(); r.run("nextChallenge");
  assert.equal(r.calls.add, 1); assert.equal(r.calls.remove, 0); assert.equal(r.listeners.size, 1);
  assert.equal(r.locations().length, 1, "a paint probe must not duplicate location bootstrap");
  r.notify(); assert.equal(r.locations().length, 2);
  assert.equal(r.messages.filter((message) => message === "wails:runtime:ready").length, 1);
  assert.equal(r.nodes.get("zwai-native-render-probe").children.length, 64);
  r.frame(); assert.equal(r.health().length, 0);
  r.window.__zwaiRecoveryAttribution = { thread_id: "th_after_frame", draft_key: "thread:th_after_frame" };
  r.frame();
  assert.deepEqual(r.health().map((message) => message.challenge), ["a53c9e1172d4b608", "b53c9e1172d4b609"]);
  for (const message of r.health()) {
    assert.equal(message.session, "session-a"); assert.equal(message.generation, 2);
    assert.equal(message.thread_id, "th_after_frame"); assert.equal(message.draft_key, "thread:th_after_frame");
  }
});
for (const next of ["nextGeneration", "nextSession"]) check(next + " removes the old location listener", () => {
  const r = receiver(); r.run(); const old = [...r.listeners][0]; r.run(next);
  assert.equal(r.calls.add, 2); assert.equal(r.calls.remove, 1); assert.equal(r.listeners.size, 1);
  assert.equal(r.listeners.has(old), false); assert.equal(r.locations().length, 2);
  r.notify(); assert.equal(r.locations().length, 3);
  assert.equal(r.locations()[2].session, next === "nextSession" ? "session-b" : "session-a");
  assert.equal(r.locations()[2].generation, next === "nextGeneration" ? 3 : 2);
  assert.equal(r.locations()[2].document, 0);
});
check("the same document cannot adopt a reload epoch but a new document can", () => {
  const r = receiver(); r.run(); const old = [...r.listeners][0]; r.run("nextDocument");
  assert.equal(r.calls.add, 1); assert.equal(r.calls.remove, 0); assert.equal(r.listeners.has(old), true);
  assert.equal(r.locations().length, 1); assert.equal(r.frames.length, 1);
  assert.equal(r.window.__zwaiRecoveryDocument.document, 0);
  r.notify(); assert.equal(r.locations()[1].document, 0);
  // A callback already queued before reload cannot acquire the new epoch.
  old(); assert.equal(r.locations()[2].document, 0);
  const fresh = receiver(); fresh.run("nextDocument");
  assert.equal(fresh.locations()[0].document, 1); assert.equal(fresh.listeners.size, 1);
  fresh.frame(); assert.equal(fresh.health().length, 0); fresh.frame();
  assert.equal(fresh.health()[0].document, 1);
  assert.equal(fresh.health()[0].challenge, "b53c9e1172d4b609");
});
check("an unmounted root still pins its document without messages or paint", () => {
  for (const root of ["missing", "empty"]) {
    const r = receiver({ root }); r.run();
    assert.deepEqual(r.messages, []); assert.equal(r.calls.created, 0); assert.equal(r.calls.add, 0); assert.equal(r.frames.length, 0);
    assert.equal(r.window.__zwaiRecoveryDocument.document, 0);
    r.mountRoot(); r.run("nextDocument");
    assert.deepEqual(r.messages, []); assert.equal(r.calls.created, 0); assert.equal(r.calls.add, 0); assert.equal(r.frames.length, 0);
    r.run(); assert.equal(r.locations()[0].document, 0);
  }
});
for (const bridges of [["wails"], ["webkit"], ["chrome"], ["wails", "webkit", "chrome"]]) check("bound bridge " + bridges.join(","), () => {
  const r = receiver({ bridges }); r.run(); r.notify(); r.frame(); r.frame();
  assert.equal(r.locations().length, 2); assert.equal(r.health().length, 1);
  assert.equal(r.calls.bridge.every((kind) => kind === bridges[0]), true);
});
for (const options of [{ origin: "https://unrelated.invalid" }, { root: "missing" }, { root: "empty" }, { bridges: [] }]) check("reject unavailable receiver " + JSON.stringify(options), () => {
  const r = receiver(options); r.run();
  assert.deepEqual(r.messages, []); assert.equal(r.listeners.size, 0); assert.equal(r.calls.created, 0); assert.equal(r.frames.length, 0);
  if (options.origin) assert.equal(r.window.__zwaiRecoveryDocument, undefined);
});
check("paint repair remains independent of listener replacement", () => {
  const r = receiver(); r.run();
  r.nodes.get("zwai-native-render-probe").children.pop(); r.run();
  assert.equal(r.nodes.get("zwai-native-render-probe").children.length, 64);
  assert.equal(r.calls.replace, 2); assert.equal(r.calls.add, 1);
});
console.log(JSON.stringify({ tests: results.length, failed: results.filter((result) => !result.passed).length, results }));
if (results.some((result) => !result.passed)) process.exitCode = 1;
`
