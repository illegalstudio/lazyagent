package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/illegalstudio/lazyagent/internal/apiauth"
)

// playgroundHTML is rendered with PBKDF2 parameters injected as JS constants
// so the in-page client uses exactly the same algorithm as the server.
const playgroundHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>lazyagent API</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, monospace;
         background: #1e1e2e; color: #cdd6f4; padding: 2rem; line-height: 1.6; }
  h1 { color: #89b4fa; margin-bottom: .5rem; font-size: 1.5rem; }
  p.sub { color: #6c7086; margin-bottom: 1rem; font-size: .9rem; }
  .auth { background: #313244; border-radius: 8px; padding: 1rem 1.25rem; margin-bottom: 1.5rem; }
  .auth label { display:block; font-size:.85rem; color:#a6adc8; margin-bottom:.35rem; }
  .auth input[type=password] { width: 100%; padding: .5rem .75rem; border-radius: 6px;
     border: 1px solid #45475a; background: #181825; color: #cdd6f4; font-family: monospace;
     font-size: .9rem; }
  .auth .row { display: flex; gap: .5rem; align-items: center; margin-top: .5rem; }
  .auth button { background: #89b4fa; color: #1e1e2e; border: none; padding: .5rem 1rem;
     border-radius: 6px; cursor: pointer; font-weight: 600; }
  .auth button:disabled { opacity: .5; cursor: wait; }
  .auth .status { font-size: .8rem; color: #6c7086; }
  .auth .status.ready { color: #a6e3a1; }
  .auth .status.fail { color: #f38ba8; }
  .auth code { background: #181825; padding: 1px 6px; border-radius: 4px;
     font-size: .8rem; color: #f5e0dc; word-break: break-all; }
  .endpoint { background: #313244; border-radius: 8px; padding: 1rem 1.25rem;
              margin-bottom: 1rem; cursor: pointer; transition: background .15s; }
  .endpoint:hover { background: #45475a; }
  .endpoint.locked { opacity: .5; cursor: not-allowed; }
  .method { display: inline-block; font-weight: 700; font-size: .8rem; padding: 2px 8px;
            border-radius: 4px; margin-right: .75rem; }
  .get { background: #a6e3a1; color: #1e1e2e; }
  .put { background: #89b4fa; color: #1e1e2e; }
  .delete { background: #f38ba8; color: #1e1e2e; }
  .path { color: #f5e0dc; font-family: monospace; }
  .desc { color: #a6adc8; font-size: .85rem; margin-top: .25rem; }
  #output { margin-top: 2rem; }
  #output h2 { color: #f9e2af; font-size: 1rem; margin-bottom: .5rem; }
  pre { background: #181825; border-radius: 8px; padding: 1rem; overflow-x: auto;
        font-size: .85rem; color: #cdd6f4; max-height: 60vh; overflow-y: auto; }
  .sse-log { white-space: pre-wrap; }
  .sse-badge { background: #f38ba8; color: #1e1e2e; }
  #sse-status { display: inline-block; padding: 2px 8px; border-radius: 4px;
                font-size: .75rem; font-weight: 700; margin-left: .5rem; }
  .connected { background: #a6e3a1; color: #1e1e2e; }
  .disconnected { background: #f38ba8; color: #1e1e2e; }
  button.stop { background: #f38ba8; color: #1e1e2e; border: none; padding: 4px 12px;
                border-radius: 4px; cursor: pointer; font-weight: 600; margin-left: .5rem; }
</style>
</head>
<body>
<h1>lazyagent API</h1>
<p class="sub">Enter your passphrase below — the same one in your config — to derive the bearer token. Your passphrase never leaves this page; only the derived token is sent to the server.</p>

<div class="auth">
  <label for="pp">API passphrase</label>
  <input type="password" id="pp" placeholder="Type your passphrase…" autocomplete="off">
  <div class="row">
    <button id="unlock" onclick="unlock()">Unlock</button>
    <span class="status" id="auth-status">Locked</span>
  </div>
  <div class="row" id="token-row" style="display:none">
    <span class="status">Token:</span>
    <code id="token-display"></code>
  </div>
</div>

<div class="endpoint locked" data-locked="1" onclick="fetchEndpoint('/api/sessions')">
  <span class="method get">GET</span>
  <span class="path">/api/sessions</span>
  <span class="path" style="color:#6c7086">?search=&amp;filter=</span>
  <div class="desc">List all visible sessions (filterable by search query and activity type)</div>
</div>

<div class="endpoint locked" data-locked="1" onclick="fetchEndpoint('/api/sessions/{id}')">
  <span class="method get">GET</span>
  <span class="path">/api/sessions/{id}</span>
  <div class="desc">Get full session detail (tokens, tools, conversation)</div>
</div>

<div class="endpoint locked" data-locked="1" onclick="renameSession()">
  <span class="method put">PUT</span>
  <span class="path">/api/sessions/{id}/name</span>
  <div class="desc">Rename a session (JSON body: {"name": "..."}). Empty name resets.</div>
</div>

<div class="endpoint locked" data-locked="1" onclick="deleteSessionName()">
  <span class="method delete">DELETE</span>
  <span class="path">/api/sessions/{id}/name</span>
  <div class="desc">Remove custom name from a session (reset to path)</div>
</div>

<div class="endpoint locked" data-locked="1" onclick="fetchEndpoint('/api/stats')">
  <span class="method get">GET</span>
  <span class="path">/api/stats</span>
  <div class="desc">Summary stats: total sessions, active count, time window</div>
</div>

<div class="endpoint locked" data-locked="1" onclick="fetchEndpoint('/api/config')">
  <span class="method get">GET</span>
  <span class="path">/api/config</span>
  <div class="desc">Current lazyagent configuration</div>
</div>

<div class="endpoint locked" data-locked="1" onclick="toggleSSE()">
  <span class="method get sse-badge">SSE</span>
  <span class="path">/api/events</span>
  <span id="sse-status" class="disconnected">disconnected</span>
  <button class="stop" id="sse-stop" style="display:none" onclick="event.stopPropagation(); stopSSE()">Stop</button>
  <div class="desc">Real-time event stream (Server-Sent Events). Pushes session updates automatically.</div>
</div>

<div id="output"></div>

<script>
// PBKDF2 parameters — MUST match the server constants in internal/apiauth/derive.go.
const KDF_SALT = "__SALT__";
const KDF_ITER = __ITER__;
const KDF_LEN  = __LEN__;

let bearerToken = "";  // populated by unlock()
let evtSource = null;

async function deriveToken(passphrase) {
  passphrase = passphrase.trim();
  if (!passphrase) return "";
  const enc = new TextEncoder();
  const baseKey = await crypto.subtle.importKey(
    "raw", enc.encode(passphrase), { name: "PBKDF2" }, false, ["deriveBits"]
  );
  const bits = await crypto.subtle.deriveBits(
    { name: "PBKDF2", hash: "SHA-256", salt: enc.encode(KDF_SALT), iterations: KDF_ITER },
    baseKey,
    KDF_LEN * 8
  );
  return base64url(new Uint8Array(bits));
}

function base64url(bytes) {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

async function unlock() {
  const btn = document.getElementById("unlock");
  const status = document.getElementById("auth-status");
  const passphrase = document.getElementById("pp").value;
  if (!passphrase.trim()) { status.textContent = "Enter a passphrase"; status.className = "status fail"; return; }
  btn.disabled = true;
  status.textContent = "Deriving token…"; status.className = "status";
  try {
    const t = await deriveToken(passphrase);
    // Probe an endpoint to confirm the token is correct.
    const r = await fetch("/api/stats", { headers: { "Authorization": "Bearer " + t } });
    if (r.status === 401) {
      status.textContent = "Wrong passphrase"; status.className = "status fail";
      btn.disabled = false; return;
    }
    if (!r.ok) {
      status.textContent = "Server error " + r.status; status.className = "status fail";
      btn.disabled = false; return;
    }
    bearerToken = t;
    status.textContent = "Unlocked"; status.className = "status ready";
    document.getElementById("token-display").textContent = t;
    document.getElementById("token-row").style.display = "flex";
    document.querySelectorAll(".endpoint.locked").forEach(el => {
      el.classList.remove("locked");
      el.removeAttribute("data-locked");
    });
  } catch (e) {
    status.textContent = "Error: " + e.message; status.className = "status fail";
  } finally {
    btn.disabled = false;
  }
}

document.addEventListener("keydown", function(e) {
  if (e.key === "Enter" && document.activeElement && document.activeElement.id === "pp") {
    e.preventDefault(); unlock();
  }
});

function requireToken() {
  if (!bearerToken) {
    showResult("(locked)", { error: "Unlock with your passphrase first." });
    return false;
  }
  return true;
}

async function fetchEndpoint(path) {
  if (!requireToken()) return;
  stopSSE();
  if (path.includes('{id}')) {
    try {
      const res = await fetch('/api/sessions', { headers: { "Authorization": "Bearer " + bearerToken } });
      const sessions = await res.json();
      if (sessions.length === 0) {
        showResult(path, {error: 'No sessions available. Start a Claude Code session first.'});
        return;
      }
      path = '/api/sessions/' + sessions[0].session_id;
    } catch(e) {
      showResult(path, {error: e.message});
      return;
    }
  }
  try {
    const res = await fetch(path, { headers: { "Authorization": "Bearer " + bearerToken } });
    const data = await res.json();
    showResult(path, data);
  } catch(e) {
    showResult(path, {error: e.message});
  }
}

function showResult(path, data) {
  var out = document.getElementById('output');
  out.textContent = '';
  var h = document.createElement('h2');
  h.textContent = path;
  var pre = document.createElement('pre');
  pre.textContent = JSON.stringify(data, null, 2);
  out.appendChild(h);
  out.appendChild(pre);
}

function toggleSSE() {
  if (!requireToken()) return;
  if (evtSource) { stopSSE(); return; }
  document.getElementById('output').innerHTML =
    '<h2>/api/events (live)</h2><pre class="sse-log" id="sse-log"></pre>';
  document.getElementById('sse-status').textContent = 'connecting…';
  document.getElementById('sse-status').className = 'disconnected';
  document.getElementById('sse-stop').style.display = 'inline-block';

  // EventSource cannot send Authorization headers; use ?token= fallback.
  evtSource = new EventSource('/api/events?token=' + encodeURIComponent(bearerToken));
  evtSource.addEventListener('update', function(e) {
    document.getElementById('sse-status').textContent = 'connected';
    document.getElementById('sse-status').className = 'connected';
    const log = document.getElementById('sse-log');
    const ts = new Date().toLocaleTimeString();
    const data = JSON.parse(e.data);
    log.textContent = '[' + ts + ']\n' + JSON.stringify(data, null, 2);
  });
  evtSource.onerror = function() {
    document.getElementById('sse-status').textContent = 'disconnected';
    document.getElementById('sse-status').className = 'disconnected';
  };
}

async function getFirstSessionId() {
  const res = await fetch('/api/sessions', { headers: { "Authorization": "Bearer " + bearerToken } });
  const sessions = await res.json();
  if (sessions.length === 0) return null;
  return sessions[0].session_id;
}

async function renameSession() {
  if (!requireToken()) return;
  stopSSE();
  try {
    const id = await getFirstSessionId();
    if (!id) { showResult('PUT /api/sessions/{id}/name', {error: 'No sessions available.'}); return; }
    var name = prompt('Enter custom name (empty to reset):');
    if (name === null) return;
    const res = await fetch('/api/sessions/' + id + '/name', {
      method: 'PUT',
      headers: {'Content-Type': 'application/json', "Authorization": "Bearer " + bearerToken},
      body: JSON.stringify({name: name})
    });
    showResult('PUT /api/sessions/' + id + '/name', await res.json());
  } catch(e) { showResult('PUT /api/sessions/{id}/name', {error: e.message}); }
}

async function deleteSessionName() {
  if (!requireToken()) return;
  stopSSE();
  try {
    const id = await getFirstSessionId();
    if (!id) { showResult('DELETE /api/sessions/{id}/name', {error: 'No sessions available.'}); return; }
    const res = await fetch('/api/sessions/' + id + '/name', {
      method: 'DELETE',
      headers: { "Authorization": "Bearer " + bearerToken }
    });
    showResult('DELETE /api/sessions/' + id + '/name', await res.json());
  } catch(e) { showResult('DELETE /api/sessions/{id}/name', {error: e.message}); }
}

function stopSSE() {
  if (evtSource) { evtSource.close(); evtSource = null; }
  document.getElementById('sse-status').textContent = 'disconnected';
  document.getElementById('sse-status').className = 'disconnected';
  document.getElementById('sse-stop').style.display = 'none';
}
</script>
</body>
</html>`

func (s *Server) handlePlayground(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderedPlayground))
}

// renderedPlayground is the playground HTML with KDF parameters substituted in.
// Computed once at package init since the parameters are constants.
var renderedPlayground = strings.NewReplacer(
	"__SALT__", apiauth.Salt,
	"__ITER__", strconv.Itoa(apiauth.Iterations),
	"__LEN__", strconv.Itoa(apiauth.KeyLength),
).Replace(playgroundHTML)
