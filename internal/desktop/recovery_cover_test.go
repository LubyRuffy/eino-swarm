package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type recoveryCoverTestWindow struct{ application.Window }

func TestRecoveryCoverRoutingPreservesTheRendererMessageReceiver(t *testing.T) {
	origin := &application.OriginInfo{Origin: "http://127.0.0.1:1234", IsMainFrame: true}
	for _, recovery := range []*RecoveryOptions{nil, {SessionID: "session"}} {
		for _, message := range []string{`{"type":"zwai:render-cover","colour":[0,128,255]}`, `{"type":"zwai:recovery"}`} {
			calls := 0
			opts := Options{URL: origin.Origin, Recovery: recovery, rawMessageHandler: func(window application.Window, got string, gotOrigin *application.OriginInfo) {
				calls++
				if window != nil || got != message || gotOrigin != origin {
					t.Fatal("native decoration routing changed the recovery receiver input")
				}
			}}
			appOptions(opts).RawMessageHandler(nil, message, origin)
			if calls != 1 {
				t.Fatalf("existing raw receiver calls=%d", calls)
			}
		}
	}
}

// These messages change only native decoration; no renderer-health receipt is
// allowed to gain this capability from a subframe or another page.
func TestRecoveryCoverMessagesRequireTrustedOpaqueColour(t *testing.T) {
	window := &recoveryCoverTestWindow{}
	trusted := &application.OriginInfo{Origin: "http://127.0.0.1:1234", TopOrigin: "http://127.0.0.1:1234", IsMainFrame: true}
	valid := `{"type":"zwai:render-cover","colour":[0,128,255]}`
	for _, tc := range []struct {
		name    string
		window  application.Window
		message string
		origin  *application.OriginInfo
		accept  bool
	}{
		{"valid", window, valid, trusted, true},
		{"main frame without top origin", window, valid, &application.OriginInfo{Origin: trusted.Origin, IsMainFrame: true}, true},
		{"nil window", nil, valid, trusted, false},
		{"nil origin", window, valid, nil, false},
		{"subframe", window, valid, &application.OriginInfo{Origin: trusted.Origin}, false},
		{"wrong origin", window, valid, &application.OriginInfo{Origin: "http://127.0.0.1:4321", IsMainFrame: true}, false},
		{"wrong top origin", window, valid, &application.OriginInfo{Origin: trusted.Origin, TopOrigin: "https://unrelated.invalid", IsMainFrame: true}, false},
		{"oversize", window, valid + strings.Repeat(" ", 256), trusted, false},
		{"invalid JSON", window, `{`, trusted, false},
		{"health receipt", window, `{"type":"zwai:recovery","colour":[0,128,255]}`, trusted, false},
		{"location receipt", window, `{"type":"zwai:recovery-location","colour":[0,128,255]}`, trusted, false},
		{"missing colour", window, `{"type":"zwai:render-cover"}`, trusted, false},
		{"null colour", window, `{"type":"zwai:render-cover","colour":null}`, trusted, false},
		{"short colour", window, `{"type":"zwai:render-cover","colour":[0,128]}`, trusted, false},
		{"long colour", window, `{"type":"zwai:render-cover","colour":[0,128,255,0]}`, trusted, false},
		{"negative colour", window, `{"type":"zwai:render-cover","colour":[-1,128,255]}`, trusted, false},
		{"overflow colour", window, `{"type":"zwai:render-cover","colour":[0,256,255]}`, trusted, false},
		{"fraction colour", window, `{"type":"zwai:render-cover","colour":[0,128.5,255]}`, trusted, false},
		{"null channel", window, `{"type":"zwai:render-cover","colour":[0,null,255]}`, trusted, false},
		{"string channel", window, `{"type":"zwai:render-cover","colour":[0,"128",255]}`, trusted, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			recoveryCoverMessageHandler("http://127.0.0.1:1234/?shell=desktop", tc.window, tc.message, tc.origin, func(got application.Window, colour [3]byte) {
				calls++
				if got != window || colour != [3]byte{0, 128, 255} {
					t.Fatalf("wrong native update: %v %v", got, colour)
				}
			})
			if (calls == 1) != tc.accept || calls > 1 {
				t.Fatalf("native updates=%d, accepted=%v", calls, tc.accept)
			}
		})
	}
}

// Run the real injected script: string matching cannot show that theme/settings
// changes update the native shield without reading conversation contents.
func TestRecoveryCoverScriptTracksOnlyNativeCornerBackground(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required for the native cover script:", err)
	}
	script, err := json.Marshal(recoveryCoverScript)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", recoveryCoverScriptReceiver)
	command.Stdin = bytes.NewReader(script)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("actual native corner script: %v", err)
	}
}

const recoveryCoverScriptReceiver = `
const assert = require("node:assert/strict"), vm = require("node:vm");
const script = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
const results = [];
function receiver(options = {}) {
  const messages = [], frames = [], observations = [], observers = [], bridges = [], listeners = new Map();
  const root = { parentElement: null, colour: "rgb(248, 249, 250)" };
  const body = { parentElement: root, colour: "rgba(0, 0, 0, 0)" };
  let pointerValue=options.pointerValue||"",pointerPriority=options.pointerPriority||"";
  body.style={getPropertyValue(name){assert.equal(name,"pointer-events");return pointerValue;},getPropertyPriority(name){assert.equal(name,"pointer-events");return pointerPriority;},setProperty(name,value,priority){assert.equal(name,"pointer-events");pointerValue=value;pointerPriority=priority;},removeProperty(name){assert.equal(name,"pointer-events");pointerValue="";pointerPriority="";}};
  const point = { parentElement: body, colour: "rgba(0, 0, 0, 0)" };
  let stack=null;
  const document = { documentElement: root, body, elementFromPoint(x,y) { assert.equal(x,8); assert.equal(y,8); return options.missingPoint ? null : point; }, elementsFromPoint(x,y) { assert.equal(x,8); assert.equal(y,8); if(options.requirePointerRead){assert.equal(pointerValue,"auto","sampling must include inherited pointer-disabled layers");assert.equal(pointerPriority,"important");}if(options.throwPointerRead)throw Error("point sampling failed");return stack|| (options.missingPoint?[]:[point,body,root]); } };
  document.addEventListener=(name,callback,capture)=>{assert.equal(capture,true);assert.equal(listeners.has(name),false,"one listener per document");listeners.set(name,callback);};
  for (const node of [root,body,point]) for (const key of ["textContent","innerText","innerHTML"]) Object.defineProperty(node,key,{get(){throw Error("must not read page content");}});
  const window = {};
  for (const kind of options.bridges || ["wails"]) {
    const bridge = {};
    const send = function(message) { assert.equal(this,bridge,"bridge must retain receiver binding"); bridges.push(kind); messages.push(JSON.parse(message)); };
    if(kind==="wails") { bridge.invoke=send; window._wails=bridge; }
    if(kind==="webkit") { bridge.postMessage=send; window.webkit={messageHandlers:{external:bridge}}; }
    if(kind==="chrome") { bridge.postMessage=send; window.chrome={webview:bridge}; }
  }
  const getComputedStyle = (node) => ({backgroundColor:node.colour,backgroundImage:node.image||"none",opacity:node.opacity===undefined?"1":node.opacity});
  window.getComputedStyle=getComputedStyle;
  class MutationObserver {
    constructor(callback) { this.callback=callback; observers.push(this); }
    observe(target,config) { observations.push({target,config}); }
  }
  const context=vm.createContext({window,document,getComputedStyle,MutationObserver,requestAnimationFrame:callback=>frames.push(callback)});
  return {root,body,point,window,messages,frames,observations,bridges,listeners,
    stack(nodes){stack=nodes;},
    run(){return vm.runInContext(script,context,{timeout:1000});},
    notify(){for(const observer of observers) observer.callback([]);},
    dispatch(name){listeners.get(name)?.({});},
    frame(){for(const callback of frames.splice(0)) callback();}
  };
}
function same(actual,expected) { assert.deepEqual(JSON.parse(JSON.stringify(actual)),expected); }
function check(name,test) { try{test();results.push({name,passed:true});}catch(error){results.push({name,passed:false,error:error.stack});} }
check("opaque point background takes precedence over body and root",()=>{
  const r=receiver(); r.point.colour="rgb(0, 128, 255)"; same(r.run(),[0,128,255]);
  assert.equal(r.messages.length,0,"initial colour is returned before native draws the marker");
});
check("transparent ancestors and missing point use only a real opaque background",()=>{
  const r=receiver(); same(r.run(),[248,249,250]);
  r.body.colour="rgb(20, 21, 22)"; same(r.run(),[20,21,22]);
  const missing=receiver({missingPoint:true}); same(missing.run(),[248,249,250]);
  missing.root.colour="rgba(0, 0, 0, 0)"; assert.equal(missing.run(),null);
});
check("unknown or image backgrounds refuse to invent an opaque colour",()=>{
  for(const colour of ["red","rgb(1, 2)","rgb(0, 0, 300)","rgb(NaN, 0, 0)"]){
    const r=receiver(); r.point.colour=colour; assert.equal(r.run(),null,colour);
  }
  const r=receiver(); r.point.image="linear-gradient(red,blue)"; assert.equal(r.run(),null);
  r.point.image="none"; r.point.colour="rgb(1, 2, 3)"; r.body.opacity="0.5"; same(r.run(),[125,126,127]);
});
check("opaque rgba and current modern rgb serializations return integer channels",()=>{
  const r=receiver(); r.point.colour="rgba(0, 128, 255, 1)"; same(r.run(),[0,128,255]);
  r.point.colour="rgb(0 128 255 / 1)"; same(r.run(),[0,128,255]);
});
check("observers cover theme palette and settings without transcript subtree",()=>{
  const r=receiver(); r.run(); assert.equal(r.observations.length,2);
  const root=r.observations.find(item=>item.target===r.root),body=r.observations.find(item=>item.target===r.body);
  assert.ok(root&&body); same(root.config.attributeFilter,["class","data-palette"]); assert.equal(root.config.attributes,true);
  assert.equal(body.config.childList,true);
  assert.equal(r.observations.some(item=>item.config.subtree),false,"streaming content must not be watched");
  r.root.colour="rgb(10, 11, 12)"; r.notify(); r.notify(); assert.equal(r.frames.length,1); r.frame();
  same(r.messages,[{type:"zwai:render-cover",colour:[10,11,12]}]);
  r.notify(); r.frame(); assert.equal(r.messages.length,1,"unchanged colour must stay quiet");
  r.point.colour="rgb(20, 21, 22)"; r.notify(); r.frame(); same(r.messages[1],{type:"zwai:render-cover",colour:[20,21,22]});
  r.point.colour="rgba(0, 0, 0, 0)"; r.notify(); r.frame(); same(r.messages[2],{type:"zwai:render-cover",colour:[10,11,12]});
  r.run(); assert.equal(r.observations.length,2,"each probe must reuse one observer registration");
});
check("invalid mutation colour sends no forged completion or health receipt",()=>{
  const r=receiver(); r.run(); r.root.colour="rgba(0, 0, 0, 0)"; r.notify(); r.frame(); assert.equal(r.messages.length,0);
  r.root.colour="rgb(1, 2, 3)"; r.notify(); r.frame(); same(r.messages,[{type:"zwai:render-cover",colour:[1,2,3]}]);
});
check("settings enter and exit settle before the next five second probe",()=>{
  const r=receiver(); r.run(); same([...r.listeners.keys()].sort(),["animationend","transitionend"]);
  r.point.colour="rgb(20, 21, 22)"; r.body.opacity="0.5"; r.notify(); r.frame(); same(r.messages[0],{type:"zwai:render-cover",colour:[134,135,136]});
  r.body.opacity="1"; r.dispatch("animationend"); r.frame(); same(r.messages[1],{type:"zwai:render-cover",colour:[20,21,22]});
  r.point.colour="rgba(0, 0, 0, 0)"; r.body.opacity="0.5"; r.notify(); r.frame(); assert.equal(r.messages.length,3);
  r.body.opacity="1"; r.dispatch("transitionend"); r.frame(); same(r.messages[2],{type:"zwai:render-cover",colour:[248,249,250]});
  r.run(); assert.equal(r.listeners.size,2); assert.equal(r.observations.length,2);
  const fresh=receiver(); fresh.run(); assert.equal(fresh.listeners.size,2); assert.equal(fresh.observations.length,2);
});
check("modal black overlay composites over white and current FOFA background",()=>{
  const r=receiver(); r.point.colour="rgb(255,255,255)";
  const overlay={parentElement:r.body,colour:"rgba(0,0,0,0.6)"}; r.stack([overlay,r.point,r.body,r.root]); same(r.run(),[102,102,102]);
  r.point.colour="rgb(248,249,250)"; same(r.run(),[99,100,100]);
  overlay.opacity="0.5"; same(r.run(),[174,174,175]);
  overlay.colour="rgba(0,0,0,0)"; same(r.run(),[248,249,250]);
});
check("Radix pointer-disabled body is sampled synchronously and precisely restored",()=>{
  for(const [value,priority] of [["none",""],["none","important"],["auto","important"],["",""]]){
    const r=receiver({pointerValue:value,pointerPriority:priority,requirePointerRead:true});
    same(r.run(),[248,249,250]); assert.equal(r.body.style.getPropertyValue("pointer-events"),value); assert.equal(r.body.style.getPropertyPriority("pointer-events"),priority);
    assert.equal(r.frames.length,0,"temporary input admission must not cross an animation frame");
    const failed=receiver({pointerValue:value,pointerPriority:priority,requirePointerRead:true,throwPointerRead:true});
    assert.equal(failed.run(),null); assert.equal(failed.body.style.getPropertyValue("pointer-events"),value); assert.equal(failed.body.style.getPropertyPriority("pointer-events"),priority);
  }
});
check("fade opacity is applied once to the whole settings group and ancestors are deduplicated",()=>{
  const r=receiver(); r.point.colour="rgb(255,255,255)";
  const dialog={parentElement:r.body,colour:"rgb(255,255,255)",opacity:"0.5"};
  const sidebar={parentElement:dialog,colour:"rgb(20,40,60)"};
  r.stack([sidebar,dialog,sidebar,dialog,r.point,r.body,r.root]); same(r.run(),[138,148,158]);
  dialog.opacity="1"; same(r.run(),[20,40,60]);
  dialog.opacity="0"; same(r.run(),[255,255,255]);
});
for(const kinds of [["wails"],["webkit"],["chrome"],["wails","webkit","chrome"]]) check("bound bridge "+kinds.join(","),()=>{
  const r=receiver({bridges:kinds}); r.run(); r.root.colour="rgb(1, 2, 3)"; r.notify(); r.frame();
  assert.equal(r.messages.length,1); same(r.bridges,[kinds[0]]);
});
check("missing bridge still returns colour but installs no notification owner",()=>{
  const r=receiver({bridges:[]}); same(r.run(),[248,249,250]); assert.equal(r.observations.length,0); assert.equal(r.messages.length,0);
});
console.log(JSON.stringify({tests:results.length,failed:results.filter(result=>!result.passed).length,results}));
if(results.some(result=>!result.passed))process.exitCode=1;
`
