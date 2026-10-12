'use strict';
const $ = id => document.getElementById(id);
let token = '', runs = [], current = '', profile = null, selection = null, logText = '', revision = 0, logRevision = 0;
const node = (tag, text, cls) => { const e = document.createElement(tag); if(text !== undefined) e.textContent = text; if(cls) e.className = cls; return e; };
const status = s => (s || 'UNKNOWN').replace(/^RUN_STATUS_/, '');
const duration = n => n == null ? 'unknown' : Number(n) < 1e6 ? `${(Number(n)/1e3).toFixed(1)}µs` : Number(n) < 1e9 ? `${(Number(n)/1e6).toFixed(1)}ms` : `${(Number(n)/1e9).toFixed(2)}s`;
const nanos = n => BigInt(n || '0');
const started = n => { const date = new Date(Number(nanos(n)/1000000n)); return Number.isNaN(date.getTime()) ? 'Unknown start' : date.toLocaleString(); };
const notice = text => { $('notice').textContent = text; };
async function api(path, options = {}) {
 const headers = new Headers(options.headers); if(token) headers.set('Authorization', `Bearer ${token}`);
 const response = await fetch(path, {...options, headers});
 if(!response.ok) { let message = `HTTP ${response.status}`; try { message = (await response.json()).error || message; } catch {} if(response.status === 401) $('token-dialog').showModal(); throw new Error(message); }
 return response;
}
async function refresh() {
 try { runs = await (await api('/api/runs')).json(); renderRuns(); if(!current) { const linked = new URL(location.href).searchParams.get('run'); if(linked) await selectRun(linked); else if(runs.length) await selectRun(runs[0].id); } } catch(e) { notice(e.message); }
}
function renderRuns() {
 $('runs').replaceChildren(); const query = $('filter').value.toLowerCase();
 for(const run of runs.filter(r => `${r.target} ${r.id}`.toLowerCase().includes(query))) {
  const button = node('button', undefined, `run${run.id === current ? ' selected' : ''}`);
  button.append(node('strong',run.target), node('small', `${status(run.status)} · ${duration(run.durationNanos)}`),node('small',started(run.startedAtUnixNanos)),node('small',run.id));
  button.onclick = () => selectRun(run.id); $('runs').append(button);
 }
 if(!runs.length) $('runs').append(node('p','No profiles yet. Upload your first build.','hint'));
}
async function selectRun(id) {
 const request = ++revision; ++logRevision; current = id; profile = null; selection = null; logText = ''; renderLogs(); $('detail').hidden = true; $('empty').hidden = false; renderRuns();
 try {
  const data = await (await api(`/api/runs/${encodeURIComponent(id)}`)).json(); if(request !== revision) return; profile = data;
  const link = new URL(location.href); link.searchParams.set('run', id); history.replaceState(null, '', link);
  $('empty').hidden = true; $('detail').hidden = false; const m = profile.manifest;
  $('build-id').textContent = m.buildId; $('target-name').textContent = m.targets?.[m.mainTargetId]?.name || '(no target)';
  $('metrics').replaceChildren();
  for(const [label,value] of [['Status',status(m.status)],['Wall time',duration(profile.runDurationNanos)],['Targets',Object.keys(m.targets || {}).length],['Steps',Object.keys(m.commands || {}).length]]) { const item=node('div',undefined,'metric'); item.append(node('small',label),node('strong',value)); $('metrics').append(item); }
  $('failure').textContent = m.failure?.errorMessage || ''; $('log-title').textContent = 'Select a target or step to inspect logs'; renderTargets(); notice('');
 } catch(e) { if(request === revision) notice(e.message); }
}
function renderTargets() {
 if(!profile) return; $('targets').replaceChildren(); const m = profile.manifest, query = $('step-filter').value.toLowerCase();
 const commandIDs = Object.keys(m.commands || {}).sort((a,b) => {
  if($('order').value === 'slowest') return Number(profile.commandDurationNanos?.[b] ?? -1)-Number(profile.commandDurationNanos?.[a] ?? -1) || a.localeCompare(b);
  const diff = nanos(m.commands[a].startedAtUnixNanos)-nanos(m.commands[b].startedAtUnixNanos); return diff < 0n ? -1 : diff > 0n ? 1 : a.localeCompare(b);
 });
 const targets = $('order').value === 'slowest' ? [['','All steps ranked by duration']] : Object.entries(m.targets || {}).map(([id,t]) => [id,t.name || id]);
 if($('order').value !== 'slowest' && commandIDs.some(id => !m.commands[id].targetId)) targets.push(['','Auxiliary steps']);
 for(const [id,name] of targets) {
  const ids = commandIDs.filter(cid => ($('order').value === 'slowest' || (m.commands[cid].targetId || '') === id) && `${m.commands[cid].name} ${cid}`.toLowerCase().includes(query));
  if(query && !ids.length) continue;
  const block = node('section',undefined,'target'), heading = node('div',undefined,'target-header');
  const title = node('button',name); title.onclick = () => { if(id) selectLogs('target',id,name); }; heading.append(title);
  if(id) { const t = m.targets[id]; heading.append(node('span',`${duration(profile.targetDurationNanos?.[id])} · ${status(t.status)} · ${t.finalPlatform || t.initialPlatform || ''}`,'target-meta'),node('span',`${id}${t.overrideArgs?.length ? ' · '+t.overrideArgs.join(' ') : ''}`,'target-meta')); }
  block.append(heading);
  if(id && m.targets[id].dependsOn?.length) { const deps = node('div','Depends on: ','deps'); for(const dep of m.targets[id].dependsOn) { const b=node('button',m.targets[dep]?.name || dep); b.onclick=()=>selectLogs('target',dep,m.targets[dep]?.name || dep); deps.append(b); } block.append(deps); }
  for(const cid of ids) {
   const c = m.commands[cid], d = profile.commandDurationNanos?.[cid], button = node('button',undefined,'step');
   const label = node('span',c.name || cid,'step-name'); label.append(node('small',`${status(c.status)}${c.isCached ? ' · CACHED' : ''} · ${m.targets?.[c.targetId]?.name || c.category || 'auxiliary'}${c.sourceLocation ? ` · ${c.sourceLocation.file}:${c.sourceLocation.startLine}` : ''}${c.errorMessage ? ' · '+c.errorMessage : ''}`,c.isCached ? 'cached' : status(c.status)==='FAILURE' ? 'bad' : ''));
   const track = node('div',undefined,'track'), bar = node('div',undefined,'bar');
   // Subtract integer timestamps before converting to Number to preserve precision.
   const span = nanos(m.endedAtUnixNanos)-nanos(m.startedAtUnixNanos);
   if(span > 0n && d != null) { const offset = nanos(c.startedAtUnixNanos)-nanos(m.startedAtUnixNanos); bar.style.marginLeft = `${Math.max(0,Math.min(100,Number(offset)*100/Number(span)))}%`; bar.style.width = `${Math.max(.5,Math.min(100,Number(d)*100/Number(span)))}%`; }
   track.append(bar); button.append(label,track,node('span',duration(d),'duration')); button.onclick = () => selectLogs('command',cid,c.name || cid); block.append(button);
  }
  $('targets').append(block);
 }
}
async function selectLogs(kind,id,name) {
 selection = {kind,id,name}; $('log-title').textContent = name; await loadLogs();
}
async function loadLogs() {
 if(!selection) return; const request = ++logRevision, run = current;
 const query = new URLSearchParams({[selection.kind]:selection.id,stream:$('stream').value,dependencies:String($('dependencies').checked)});
 $('logs').textContent = 'Loading…';
 try { const text = await (await api(`/api/runs/${encodeURIComponent(run)}/logs?${query}`)).text(); if(request !== logRevision || run !== current) return; logText = text.replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)/g,'').replace(/\x1b\[[0-?]*[ -/]*[@-~]/g,'').replace(/[\x00-\x08\x0b-\x1f\x7f]/g,''); renderLogs(); } catch(e) { if(request === logRevision) { $('logs').textContent = ''; notice(e.message); } }
}
function renderLogs() { const query = $('log-search').value.toLowerCase(); $('logs').textContent = (query ? logText.split('\n').filter(line=>line.toLowerCase().includes(query)).join('\n') : logText) || 'No output for this selection.'; }
$('upload').onchange = async () => { const file = $('upload').files[0]; if(!file) return; $('upload').disabled=true; notice('Uploading profile…'); try { const summary = await (await api('/api/runs',{method:'POST',headers:{'Content-Type':'application/json'},body:file})).json(); await refresh(); await selectRun(summary.id); notice(`Imported ${summary.id}`); } catch(e) { notice(e.message); } finally { $('upload').disabled=false; $('upload').value=''; } };
$('export').onclick = async () => { try { const blob = await (await api(`/api/runs/${encodeURIComponent(current)}/export`)).blob(); const url = URL.createObjectURL(blob), a=node('a'); a.href=url; a.download='profile.json'; a.click(); setTimeout(()=>URL.revokeObjectURL(url),1000); } catch(e) { notice(e.message); } };
$('connect').onclick=()=>$('token-dialog').showModal();
$('save-token').onclick=()=>{ token=$('token').value; $('token').value=''; current=''; refresh(); };
$('refresh').onclick=async()=>{ await refresh(); if(current) await selectRun(current); };
$('filter').oninput=renderRuns; $('step-filter').oninput=renderTargets; $('order').onchange=renderTargets; $('stream').onchange=loadLogs; $('dependencies').onchange=loadLogs; $('log-search').oninput=renderLogs;
refresh();
