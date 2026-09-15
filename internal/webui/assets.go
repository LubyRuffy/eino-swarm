// assets.go — embedded single-page webui (indexHTML) and small helpers.
package webui

import (
	"bytes"
	"io"
	"os"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func osStat(p string) (interface{ Size() int64 }, error) {
	fi, err := os.Stat(p)
	return fileInfo{fi}, err
}

type fileInfo struct{ os.FileInfo }

func (f fileInfo) Size() int64 { return 0 }

const indexHTML = `<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<title>zwai — agent swarm</title>
<style>
  :root { --bg:#0d1b1e; --panel:#122428; --cyan:#39c5cf; --dim:#8aa; --ok:#42d392; --err:#ff6b6b; --tool:#ffb86c; }
  * { box-sizing:border-box; margin:0; }
  body { background:var(--bg); color:#dde; font:14px/1.55 -apple-system,"PingFang SC",Menlo,monospace; height:100vh; display:flex; flex-direction:column; }
  header { padding:8px 14px; border-bottom:1px solid #1d3a40; display:flex; gap:18px; align-items:center; }
  header b { color:var(--cyan); }
  header .keys { color:#5a7a80; font-size:12px; }
  main { flex:1; display:flex; min-height:0; }
  #left { flex:3; overflow-y:auto; padding:12px 16px; border-right:1px solid #1d3a40; }
  #right { flex:1; overflow-y:auto; padding:12px 14px; min-width:260px; }
  h2 { color:var(--cyan); font-size:13px; margin:0 0 8px; }
  .agent { margin-bottom:14px; }
  .agent h3 { font-size:12px; color:var(--cyan); margin:0 0 4px; }
  .blk { margin:4px 0 4px 10px; padding:4px 8px; border-left:2px solid #2a4a52; }
  .blk.thinking { color:#9ab; font-style:italic; background:#0f2024; white-space:pre-wrap; }
  .blk.answer { white-space:pre-wrap; }
  .blk.tool { color:var(--tool,#ffb86c); background:#152225; }
  .blk .res { color:#9d9; margin-top:2px; font-size:12px; white-space:pre-wrap; }
  .line { margin:2px 0; }
  .tag { color:var(--cyan); font-weight:600; }
  .spawn { color:#39c5cf; }
  .fin-ok { color:var(--ok); }
  .fin-err { color:var(--err); }
  .delta { color:#bde; }
  footer { padding:8px 14px; border-top:1px solid #1d3a40; display:flex; gap:10px; }
  input { flex:1; background:#0f2024; border:1px solid #1d3a40; color:#dde; padding:8px 12px; border-radius:6px; font:inherit; }
  button { background:#173c44; color:var(--cyan); border:0; padding:8px 18px; border-radius:6px; cursor:pointer; }
  .ts { color:#51707a; font-size:11px; margin-right:8px; }
  summary { cursor:pointer; color:#9ab; }
</style>
</head>
<body>
<header><b>zwai</b><span class="keys" id="status">connecting…</span></header>
<main>
  <div id="left"><h2>MANAGER</h2><div id="agents"></div></div>
  <div id="right"><h2>SUB-AGENTS</h2><div id="roster"></div></div>
</main>
<footer>
  <input id="task" placeholder="任务目标…（回车发送）">
  <button onclick="start()">启动</button>
</footer>
<script>
const agents = new Map();          // agent_id -> {el, blocks}
const order = [];
let leftEl, rosterEl;

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}
function agentEl(id, role) {
  let a = agents.get(id);
  if (a) return a;
  const wrap = el('div','agent');
  wrap.appendChild(el('h3', null, (id==='manager'?'MANAGER':id)));
  const body = el('div');
  wrap.appendChild(body);
  document.getElementById('agents').appendChild(wrap);
  a = {wrap, body};
  agents.set(id, a);
  return a;
}
function blockFor(a, kind, key) {
  const k = kind+':'+key;
  let blk = a.blocks.get(k);
  if (!blk) {
    const cls = kind==='reasoning' ? 'blk thinking' : (kind==='tool' ? 'blk tool' : 'blk answer');
    blk = {el: el('div', cls), final:false};
    a.blocks.set(k, blk);
    a.body.appendChild(blk.el);
  }
  return blk;
}
function rosterEntry(id, status, tail) {
  let r = roster.get(id);
  if (!r) {
    r = {el: el('div','line'), id};
    r.el.appendChild(el('span','tag', id));
    r.status = el('span', null, '');
    r.el.appendChild(r.status);
    r.tail = el('div', null, '');
    r.tail.style.cssText = 'color:#7a9aa0;font-size:12px;margin-left:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;';
    r.el.appendChild(r.tail);
    roster.set(id, r);
    document.getElementById('roster').appendChild(r.el);
  }
  if (status) { r.status.textContent = '  ' + status; }
  if (tail !== undefined && tail !== null && tail !== '') {
    const t = tail.split('\n').pop();
    r.tail.textContent = '└ ' + (t.length>90 ? t.slice(0,90)+'…' : t);
  }
  return r;
}
const roster = new Map();

function handle(n) {
  const a = agentEl(n.agent_id, n.role);
  a.blocks = a.blocks || new Map();
  rosterEntry(n.agent_id, null, null);
  const ts = el('span','ts', n.time||'');
  switch (n.kind) {
    case 'spawned':
      rosterEntry(n.agent_id, 'spawned', n.role);
      break;
    case 'reasoning_delta':
      blk = blockFor(a,'reasoning', n.agent_id);
      blk.el.textContent = '💭 ' + n.text.slice(-600);
      rosterEntry(n.agent_id, '💭 thinking', n.text);
      break;
    case 'delta': {
      const blk = blockFor(a,'answer', n.agent_id);
      blk.el.textContent = n.text.slice(-2000);
      blk.el.appendChild(ts.cloneNode ? ts : el('span'));
      rosterEntry(n.agent_id, '▌ streaming', n.text);
      break;
    }
    case 'tool_call':
      rosterEntry(n.agent_id, '⚙ '+n.text.slice(0,60), '');
      break;
    case 'tool_result':
      rosterEntry(n.agent_id, '← result', n.text);
      break;
    case 'agent_message': {
      const blk = blockFor(a,'answer', n.agent_id);
      blk.el.textContent = n.text;
      if (n.agent_id === 'manager') { /* keep visible */ }
      break;
    }
    case 'finished':
      rosterEntry(n.agent_id, n.err ? ('✗ '+n.err) : '✓ done', n.text);
      break;
    case 'done':
      document.getElementById('status').textContent = 'done';
      break;
    case 'error':
      rosterEntry(n.agent_id, '✗ error', n.err || n.text);
      document.getElementById('status').textContent = 'error';
      break;
  }
}

async function start() {
  const t = document.getElementById('task').value.trim();
  if (!t) return;
  await fetch('/run', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({task: t})});
}

const es = new EventSource('/events');
es.onopen = () => document.getElementById('status').textContent = 'connected';
es.onerror = () => document.getElementById('status').textContent = 'disconnected';
es.onmessage = e => { try { handle(JSON.parse(e.data)); } catch(err) {} };

// task input from URL ?task=
const q = new URLSearchParams(location.search).get('task');
if (q) { document.getElementById('task').value = q; start(); }
</script>
</body>
</html>`
