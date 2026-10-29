const $ = (id) => document.getElementById(id);
const api = async (path, method = 'GET', body) => {
  const r = await fetch('/api/' + path, {method, headers: {'Content-Type': 'application/json'}, body: body ? JSON.stringify(body) : undefined});
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
};
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

// ---- minimal markdown renderer ----
function inline(s) {
  return esc(s).replace(/`([^`]+)`/g, '<code>$1</code>').replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>').replace(/\*([^*]+)\*/g, '<i>$1</i>');
}
function md(src) {
  const lines = src.split('\n'); let out = '', i = 0, list = null, para = [];
  const flush = () => { if (para.length) { out += '<p>' + inline(para.join(' ')) + '</p>'; para = []; } };
  const closeList = () => { if (list) { out += `</${list}>`; list = null; } };
  while (i < lines.length) {
    const l = lines[i];
    if (l.trim().startsWith('```')) {
      flush(); closeList(); const code = []; i++;
      while (i < lines.length && !lines[i].trim().startsWith('```')) code.push(lines[i++]);
      out += '<pre><code>' + esc(code.join('\n')) + '</code></pre>'; i++; continue;
    }
    let m;
    if ((m = l.match(/^(#{1,4})\s+(.*)/))) { flush(); closeList(); out += `<h${m[1].length + 2}>${inline(m[2])}</h${m[1].length + 2}>`; }
    else if ((m = l.match(/^\s*[-*]\s+(.*)/))) { flush(); if (list !== 'ul') { closeList(); out += '<ul>'; list = 'ul'; } out += `<li>${inline(m[1])}</li>`; }
    else if ((m = l.match(/^\s*\d+[.)]\s+(.*)/))) { flush(); if (list !== 'ol') { closeList(); out += '<ol>'; list = 'ol'; } out += `<li>${inline(m[1])}</li>`; }
    else if (!l.trim()) { flush(); closeList(); }
    else para.push(l.trim());
    i++;
  }
  flush(); closeList(); return out;
}

// ---- views ----
let view = 'home', exam = null, pollTimer = null, timerTimer = null, current = 0, clockSkew = 0;
function show(v) {
  view = v;
  for (const id of ['home', 'exam', 'result']) $(id).classList.toggle('hidden', id !== v);
  $('exam-info').classList.toggle('hidden', v !== 'exam');
  if (v !== 'exam') $('notes').classList.add('hidden');
}
function overlay(html, buttons = []) {
  $('overlay-box').innerHTML = html + '<div class="row">' + buttons.map((b, i) => `<button data-i="${i}" class="${b.cls || ''}">${b.label}</button>`).join('') + '</div>';
  $('overlay').classList.remove('hidden');
  $('overlay-box').querySelectorAll('button').forEach((el) => el.onclick = () => { $('overlay').classList.add('hidden'); buttons[el.dataset.i].fn && buttons[el.dataset.i].fn(); });
}
const confirmBox = (html, label, fn) => overlay(html, [{label, cls: 'danger', fn}, {label: 'Cancel', cls: 'ghost'}]);

// ---- home ----
async function refreshCluster() {
  if (view !== 'home') return;
  try {
    const s = await api('cluster');
    $('cluster-profile').textContent = '(colima profile: ' + s.profile + ')';
    const el = $('cluster-status');
    let txt, cls;
    if (!s.colimaInstalled || !s.kubectlInstalled) { txt = 'colima / kubectl not found in PATH (brew install colima kubectl)'; cls = 'bad'; }
    else if (s.busy) { txt = 'Working: colima ' + s.operation + '…'; cls = 'warn'; }
    else if (s.ready) { txt = '● Cluster ready'; cls = 'ok'; }
    else if (s.running) { txt = 'VM running, Kubernetes not ready yet…'; cls = 'warn'; }
    else { txt = '○ Cluster not running'; cls = 'bad'; }
    el.textContent = txt + (s.error && !s.busy ? ' — ' + s.error : ''); el.className = 'status ' + cls;
    $('cluster-nodes').textContent = s.nodes || '';
    $('c-start').disabled = s.busy || s.ready || !s.colimaInstalled;
    $('c-stop').disabled = s.busy || !s.running;
    $('c-delete').disabled = s.busy;
    const log = $('cluster-log');
    log.classList.toggle('hidden', !(s.log && s.log.length));
    log.textContent = (s.log || []).join('\n'); log.scrollTop = log.scrollHeight;
    document.querySelectorAll('.scn button').forEach((b) => b.disabled = !s.ready);
  } catch (e) { $('cluster-status').textContent = e.message; }
}
async function loadScenarios() {
  const j = await api('scenarios');
  $('scenario-errors').textContent = (j.errors || []).join('\n');
  $('scenarios').innerHTML = '';
  for (const s of j.scenarios || []) {
    const d = document.createElement('div'); d.className = 'scn';
    d.innerHTML = `<h3>${esc(s.title)}</h3><div class="tags"><span class="tag">${esc(s.difficulty || 'medium')}</span><span class="tag">${s.taskCount} tasks</span><span class="tag">${s.durationMin} min</span><span class="tag">pass ${s.passScore}%</span></div>
      <p>${esc(s.description || '')}</p><div class="small">${esc(s.domains || '')}</div><button disabled>Start exam</button>`;
    d.querySelector('button').onclick = () => startExam(s);
    $('scenarios').appendChild(d);
  }
}
function startExam(s) {
  const intro = `<h2>${esc(s.title)}</h2><div class="md">${md(s.instructions || '')}</div>
    <p>You have <b>${s.durationMin} minutes</b>. Pass score: <b>${s.passScore}%</b>.</p>`;
  overlay(intro, [{label: 'Begin exam', fn: async () => {
    overlay('<h2>Preparing environment…</h2><p>Running the scenario setup against the cluster. This can take a minute.</p>');
    try { exam = await api('exam/start', 'POST', {id: s.id}); $('overlay').classList.add('hidden'); enterExam(true); }
    catch (e) { overlay('<h2>Could not start</h2><pre class="error">' + esc(e.message) + '</pre>', [{label: 'Close'}]); }
  }}, {label: 'Cancel', cls: 'ghost'}]);
}

// ---- exam ----
const flags = () => JSON.parse(localStorage.getItem('flags') || '{}');
function enterExam(fresh) {
  show('exam'); clockSkew = exam.now - Date.now(); current = 0;
  $('exam-title').textContent = exam.scenario.title;
  if (fresh) localStorage.removeItem('flags');
  $('notes-text').value = fresh ? '' : localStorage.getItem('notes') || '';
  if (fresh) localStorage.removeItem('notes');
  renderNav(); renderTask();
  clearInterval(timerTimer); timerTimer = setInterval(tick, 500); tick();
  startTerm();
}
function tick() {
  const left = exam.startedAt + exam.durationMs - (Date.now() + clockSkew);
  const t = Math.max(0, Math.floor(left / 1000));
  $('timer').textContent = [t / 3600, (t / 60) % 60, t % 60].map((x) => String(Math.floor(x)).padStart(2, '0')).join(':');
  $('timer').classList.toggle('low', t < 600);
  if (left <= 0) { clearInterval(timerTimer); finishExam(true); }
}
function renderNav() {
  const f = flags(); $('task-nav').innerHTML = '';
  exam.tasks.forEach((t, i) => {
    const b = document.createElement('button'); b.textContent = t.number;
    b.className = (i === current ? 'active ' : '') + (f[t.number] ? 'flag' : '');
    b.onclick = () => { current = i; renderNav(); renderTask(); };
    $('task-nav').appendChild(b);
  });
}
function renderTask() {
  const t = exam.tasks[current], f = flags();
  $('task-view').innerHTML = `<h2>Task ${t.number}: ${esc(t.title)}</h2><div class="weight">Weight: ${t.weight}%</div><div class="md">${md(t.body)}</div>
    <button class="ghost flagbtn" style="color:#555">${f[t.number] ? 'Remove flag' : 'Flag for review'}</button>`;
  $('task-view').querySelector('.flagbtn').onclick = () => { const x = flags(); x[t.number] = !x[t.number]; localStorage.setItem('flags', JSON.stringify(x)); renderNav(); renderTask(); };
}
async function finishExam(auto) {
  const go = async () => {
    overlay('<h2>Grading…</h2><p>Verifying your work against the cluster.</p>');
    try { exam = await api('exam/finish', 'POST'); $('overlay').classList.add('hidden'); showResult(); }
    catch (e) { overlay('<h2>Error</h2><pre class="error">' + esc(e.message) + '</pre>', [{label: 'Close'}]); }
  };
  if (auto) { go(); return; }
  confirmBox('<h2>End the exam?</h2><p>Your answers will be graded now.</p>', 'End and grade', go);
}
function showResult() {
  show('result'); stopTerm(); clearInterval(timerTimer);
  const r = exam.result;
  let h = `<div class="res-head"><div class="score ${r.passed ? 'pass' : 'fail'}">${r.score}%</div><h2>${r.passed ? 'PASS 🎉' : 'FAIL'} <small>(required ${r.passScore}%)</small></h2>
    <button id="r-home">Back to scenarios</button></div>`;
  for (const t of r.tasks) {
    h += `<div class="res-task ${t.passed ? 'pass' : ''}"><b>${t.passed ? '✔' : '✘'} Task ${t.number}: ${esc(t.title)}</b> <span class="small">(${t.weight}%)</span>
      ${t.output ? `<pre>${esc(t.output)}</pre>` : ''}
      ${t.solution ? `<details><summary>Show solution</summary><div class="md">${md(t.solution)}</div></details>` : ''}</div>`;
  }
  $('result').innerHTML = h; $('result').scrollTop = 0;
  $('r-home').onclick = async () => { await api('exam/abort', 'POST'); exam = null; goHome(); };
}
function goHome() { show('home'); loadScenarios().then(refreshCluster); }

// ---- terminal ----
let term = null, fit = null, sock = null, wantTerm = false;
const enc = new TextEncoder();
function fitTerm() {
  if (!term || !$('terminal').offsetParent) return;
  try { fit.fit(); } catch (e) { return; }
  if (sock && sock.readyState === 1) sock.send(JSON.stringify({type: 'resize', cols: term.cols, rows: term.rows}));
}
function connectTerm() {
  if (!wantTerm) return;
  sock = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/term');
  sock.binaryType = 'arraybuffer';
  const s = sock;
  s.onopen = () => { term.reset(); fitTerm(); };
  s.onmessage = (e) => term.write(new Uint8Array(e.data));
  s.onclose = () => { if (sock === s) setTimeout(connectTerm, 1000); };
}
function startTerm() {
  if (!term) {
    term = new Terminal({fontFamily: 'ui-monospace, Menlo, monospace', fontSize: 13, cursorBlink: true, scrollback: 5000, theme: {background: '#0b0f14'}});
    fit = new FitAddon.FitAddon(); term.loadAddon(fit);
    term.open($('terminal'));
    term.onData((d) => { if (sock && sock.readyState === 1) sock.send(enc.encode(d)); });
    new ResizeObserver(fitTerm).observe($('terminal'));
  }
  wantTerm = true;
  if (!sock || sock.readyState > 1) connectTerm();
  fitTerm(); term.focus();
}
function stopTerm() { wantTerm = false; if (sock) { const s = sock; sock = null; s.close(); } }
$('btn-clear').onclick = () => { term.clear(); term.focus(); };
$('terminal').addEventListener('click', () => term && term.focus());

// ---- misc wiring ----
$('btn-finish').onclick = () => finishExam(false);
$('btn-notes').onclick = () => $('notes').classList.toggle('hidden');
$('notes-close').onclick = () => $('notes').classList.add('hidden');
$('notes-text').oninput = () => localStorage.setItem('notes', $('notes-text').value);
$('c-start').onclick = async () => { try { await api('cluster/start', 'POST'); } catch (e) { alert(e.message); } refreshCluster(); };
$('c-stop').onclick = () => confirmBox('<h2>Stop the cluster VM?</h2>', 'Stop', async () => { try { await api('cluster/stop', 'POST'); } catch (e) { alert(e.message); } refreshCluster(); });
$('c-delete').onclick = () => confirmBox('<h2>Delete the cluster VM?</h2><p>The colima profile and all its data will be removed.</p>', 'Delete', async () => { try { await api('cluster/delete', 'POST'); } catch (e) { alert(e.message); } refreshCluster(); });
(() => { // resizable divider
  let drag = false; $('divider').onmousedown = () => { drag = true; document.body.style.userSelect = 'none'; };
  window.onmouseup = () => { drag = false; document.body.style.userSelect = ''; };
  window.onmousemove = (e) => { if (drag) $('left').style.width = Math.min(Math.max(e.clientX, 260), innerWidth - 300) + 'px'; };
})();
window.addEventListener('beforeunload', (e) => { if (view === 'exam') e.preventDefault(); });

(async function init() {
  show('home');
  try {
    exam = await api('exam');
    if (exam.active) { if (exam.result) showResult(); else enterExam(false); }
    else await loadScenarios();
  } catch (e) { $('scenario-errors').textContent = e.message; }
  refreshCluster(); setInterval(refreshCluster, 4000);
})();
