package api

import "net/http"

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
  p.sub { color: #6c7086; margin-bottom: 2rem; font-size: .9rem; }
  .endpoint { background: #313244; border-radius: 8px; padding: 1rem 1.25rem;
              margin-bottom: 1rem; cursor: pointer; transition: background .15s; }
  .endpoint:hover { background: #45475a; }
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
<p class="sub">Click an endpoint to test it. All responses are JSON.</p>

<div class="endpoint" onclick="fetchEndpoint('/api/sessions')">
  <span class="method get">GET</span>
  <span class="path">/api/sessions</span>
  <span class="path" style="color:#6c7086">?search=&amp;filter=</span>
  <div class="desc">List all visible sessions (filterable by search query and activity type)</div>
</div>

<div class="endpoint" onclick="fetchEndpoint('/api/sessions/{id}')">
  <span class="method get">GET</span>
  <span class="path">/api/sessions/{id}</span>
  <div class="desc">Get full session detail (tokens, tools, conversation)</div>
</div>

<div class="endpoint" onclick="renameSession()">
  <span class="method put">PUT</span>
  <span class="path">/api/sessions/{id}/name</span>
  <div class="desc">Rename a session (JSON body: {"name": "..."}). Empty name resets.</div>
</div>

<div class="endpoint" onclick="deleteSessionName()">
  <span class="method delete">DELETE</span>
  <span class="path">/api/sessions/{id}/name</span>
  <div class="desc">Remove custom name from a session (reset to path)</div>
</div>

<div class="endpoint" onclick="fetchEndpoint('/api/stats')">
  <span class="method get">GET</span>
  <span class="path">/api/stats</span>
  <div class="desc">Summary stats: total sessions, active count, time window</div>
</div>

<div class="endpoint" onclick="fetchEndpoint('/api/config')">
  <span class="method get">GET</span>
  <span class="path">/api/config</span>
  <div class="desc">Current lazyagent configuration</div>
</div>

<div class="endpoint" onclick="toggleSSE()">
  <span class="method get sse-badge">SSE</span>
  <span class="path">/api/events</span>
  <span id="sse-status" class="disconnected">disconnected</span>
  <button class="stop" id="sse-stop" style="display:none" onclick="event.stopPropagation(); stopSSE()">Stop</button>
  <div class="desc">Real-time event stream (Server-Sent Events). Pushes session updates automatically.</div>
</div>

<div id="output"></div>

<script>
let evtSource = null;

async function fetchEndpoint(path) {
  stopSSE();
  if (path.includes('{id}')) {
    // Fetch sessions first to get a real ID.
    try {
      const res = await fetch('/api/sessions');
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
    const res = await fetch(path);
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
  if (evtSource) { stopSSE(); return; }
  document.getElementById('output').innerHTML =
    '<h2>/api/events (live)</h2><pre class="sse-log" id="sse-log"></pre>';
  document.getElementById('sse-status').textContent = 'connecting…';
  document.getElementById('sse-status').className = 'disconnected';
  document.getElementById('sse-stop').style.display = 'inline-block';

  evtSource = new EventSource('/api/events');
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
  const res = await fetch('/api/sessions');
  const sessions = await res.json();
  if (sessions.length === 0) return null;
  return sessions[0].session_id;
}

async function renameSession() {
  stopSSE();
  try {
    const id = await getFirstSessionId();
    if (!id) { showResult('PUT /api/sessions/{id}/name', {error: 'No sessions available.'}); return; }
    var name = prompt('Enter custom name (empty to reset):');
    if (name === null) return;
    const res = await fetch('/api/sessions/' + id + '/name', {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({name: name})
    });
    showResult('PUT /api/sessions/' + id + '/name', await res.json());
  } catch(e) { showResult('PUT /api/sessions/{id}/name', {error: e.message}); }
}

async function deleteSessionName() {
  stopSSE();
  try {
    const id = await getFirstSessionId();
    if (!id) { showResult('DELETE /api/sessions/{id}/name', {error: 'No sessions available.'}); return; }
    const res = await fetch('/api/sessions/' + id + '/name', {method: 'DELETE'});
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
	w.Write([]byte(playgroundHTML))
}
