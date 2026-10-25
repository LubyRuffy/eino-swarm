package desktop

import (
	"encoding/json"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Native code installs the opaque sibling cover before evaluating the paint
// challenge. Compose the app's solid backgrounds and modal opacity groups;
// unknown backgrounds cannot supply a new cover colour. Observe theme
// attributes, body portals and completed transitions, never conversation text.
const recoveryCoverScript = `(()=>{
const root=document.documentElement;
const read=()=>{
  const body=document.body,prior=body?.style.getPropertyValue("pointer-events"),priority=body?.style.getPropertyPriority("pointer-events");
  let nodes;
  try{
    if(body)body.style.setProperty("pointer-events","auto","important");
    nodes=document.elementsFromPoint(8,8);
  }catch{return null;}finally{
    if(body){if(prior)body.style.setProperty("pointer-events",prior,priority);else body.style.removeProperty("pointer-events");}
  }
  if(!nodes.length)for(let e=document.body||root;e;e=e.parentElement)nodes.push(e);
  const seen=new Set(),styles=new Map();
  const style=e=>{if(!styles.has(e))styles.set(e,getComputedStyle(e));return styles.get(e);};
  const groups=[{node:null,opacity:1,rgb:[0,0,0],alpha:0}];
  const append=(front,rgb,alpha)=>{const remaining=1-front.alpha;for(let i=0;i<3;i++)front.rgb[i]+=rgb[i]*remaining;front.alpha+=alpha*remaining;};
  const close=()=>{const back=groups.pop();append(groups[groups.length-1],back.rgb.map(v=>v*back.opacity),back.alpha*back.opacity);};
  for(const node of nodes){
    if(seen.has(node))continue;seen.add(node);
    const path=[];
    for(let e=node;e;e=e.parentElement){const opacity=Number(style(e).opacity);if(!Number.isFinite(opacity)||opacity<0||opacity>1)return null;if(opacity!==1)path.unshift({node:e,opacity});}
    let common=0;while(common<path.length&&common+1<groups.length&&groups[common+1].node===path[common].node)common++;
    while(groups.length>common+1)close();
    for(let i=common;i<path.length;i++)groups.push({...path[i],rgb:[0,0,0],alpha:0});
    if(groups.some(group=>group.opacity===0||group.alpha===1))continue;
    const s=style(node);if(s.backgroundImage!=="none")return null;
    const match=s.backgroundColor.match(/^rgba?\(([^)]+)\)$/);if(!match)return null;
    const values=match[1].trim().split(/[\s,/]+/).map(Number);if(values.length!==3&&values.length!==4)return null;
    if(values.some(v=>!Number.isFinite(v)))return null;
    const alpha=values.length===4?values[3]:1;
    if(alpha<0||alpha>1||values.slice(0,3).some(v=>v<0||v>255))return null;
    append(groups[groups.length-1],values.slice(0,3).map(v=>v*alpha),alpha);
  }
  while(groups.length>1)close();
  return groups[0].alpha===1?groups[0].rgb.map(Math.round):null;
};
const colour=read(),key=JSON.stringify(colour);
const send=window._wails?.invoke?.bind(window._wails)||window.webkit?.messageHandlers?.external?.postMessage?.bind(window.webkit.messageHandlers.external)||window.chrome?.webview?.postMessage?.bind(window.chrome.webview);
if(!send||!root||!document.body)return colour;
let owner=window.__zwaiRecoveryCover;if(owner){owner.last=key;return colour;}
owner={last:key,pending:false};window.__zwaiRecoveryCover=owner;
const schedule=()=>{if(owner.pending)return;owner.pending=true;requestAnimationFrame(()=>{owner.pending=false;const next=read(),value=JSON.stringify(next);if(!next||value===owner.last)return;owner.last=value;send(JSON.stringify({type:"zwai:render-cover",colour:next}));});};
const observer=new MutationObserver(schedule);document.addEventListener("animationend",schedule,true);document.addEventListener("transitionend",schedule,true);
owner.observer=observer;observer.observe(root,{attributes:true,attributeFilter:["class","data-palette"]});observer.observe(document.body,{childList:true});
return colour;
})()`

func recoveryCoverMessageHandler(expected string, window application.Window, message string, origin *application.OriginInfo, apply func(application.Window, [3]byte)) {
	if window == nil || len(message) > 256 || origin == nil || !origin.IsMainFrame ||
		!sameDirectoryOrigin(expected, origin.Origin) || (origin.TopOrigin != "" && !sameDirectoryOrigin(expected, origin.TopOrigin)) {
		return
	}
	var receipt struct {
		Type   string `json:"type"`
		Colour []*int `json:"colour"`
	}
	if json.Unmarshal([]byte(message), &receipt) != nil || receipt.Type != "zwai:render-cover" || len(receipt.Colour) != 3 {
		return
	}
	var colour [3]byte
	for i, channel := range receipt.Colour {
		if channel == nil || *channel < 0 || *channel > 255 {
			return
		}
		colour[i] = byte(*channel)
	}
	apply(window, colour)
}
