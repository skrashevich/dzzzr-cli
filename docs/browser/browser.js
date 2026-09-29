'use strict';
window.dzzzrBrowserWorkspace = true;
const q = s=>document.querySelector(s);
const decode = s=>Uint8Array.from(atob(s),c=>c.charCodeAt(0));
async function encode(file){const a=new Uint8Array(await file.arrayBuffer());let s='';for(let i=0;i<a.length;i+=32768)s+=String.fromCharCode(...a.subarray(i,i+32768));return btoa(s)}
function download(name,blob){const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(a.href),5000)}
const ready = (async()=>{const go=new Go();const response=await fetch('browser.wasm');if(!response.ok)throw new Error('Не удалось загрузить Go-модуль');const {instance}=await WebAssembly.instantiate(await response.arrayBuffer(),go.importObject);go.run(instance).catch(showError);})();
async function rawCall(request){const out=JSON.parse(await window.dzzzrBrowserCall(JSON.stringify(request)));if(out.error){const e=new Error(out.error);e.status=out.status;e.data=out;e.result=out.result;throw e}return out.result}
const dbReady=new Promise((resolve,reject)=>{const request=indexedDB.open('dzzzr-browser-workspace',1);request.onupgradeneeded=()=>request.result.createObjectStore('state');request.onsuccess=()=>resolve(request.result);request.onerror=()=>reject(request.error)});
async function stored(key){const db=await dbReady;return new Promise((resolve,reject)=>{const r=db.transaction('state').objectStore('state').get(key);r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error)})}
async function store(key,value){const db=await dbReady;return new Promise((resolve,reject)=>{const tx=db.transaction('state','readwrite');tx.objectStore('state').put(value,key);tx.oncomplete=()=>resolve();tx.onerror=()=>reject(tx.error);tx.onabort=()=>reject(tx.error||new Error('Сохранение отменено'))})}
let sessions={general:[]}, selectedSession='general', persistQueue=Promise.resolve();
const workspaceReady=ready.then(async()=>{const state=await stored('workspace');if(state)await rawCall({action:'workspace_restore',args:state});sessions=await stored('chats')||{general:[]};sessions.general??=[];});
async function persistWorkspace(){const task=persistQueue.catch(()=>{}).then(async()=>{const state=await rawCall({action:'workspace_export'});await store('workspace',state)});persistQueue=task;await task}
async function call(request){await workspaceReady;const value=await rawCall(request);if(request.action==='editor_api' && request.method && request.method!=='GET' || request.action==='workspace_restore')await persistWorkspace();return value}
window.dzzzrLocalWorkspace={
 encode,activeDraft:()=>selectedSession==='general'?'':selectedSession,
 theme:()=>document.documentElement.dataset.theme||(matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light'),
 api:(path,options={})=>call({action:'editor_api',path,method:options.method||'GET',body:options.body}),
 openAgent:async id=>{sessions[id]??=[];selectedSession=id;renderSessions();page('agent')},
};
function showError(e){const el=q('#browser-status');if(el)el.textContent=e.message||String(e)}
window.dzzzrBrowserStatus=s=>{q('#agent-status').textContent=s};
let active=null,searchArgs={},next=null;
window.dzzzrStatsOffline={
 async request(file,cfg,format){
  await ready;
  const data=await encode(file);
  if(format){
   const result=JSON.parse(window.dzzzrOfflineCalculate(file.name,data,JSON.stringify(cfg),format));if(result.error)throw new Error(result.error);
   if(format==='xlsx')return new Blob([decode(result.xlsx)],{type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'});
   if(format==='html'){
    const assets=await Promise.all(['stats-template.html','stats.css','stats.js','stats-offline.js','wasm_exec.js','GO-LICENSE'].map(async name=>{const r=await fetch(name);if(!r.ok)throw new Error('Не удалось загрузить '+name);return r.text()}));
    const wasm=await fetch('offline.wasm');if(!wasm.ok)throw new Error('Не удалось загрузить офлайн-модуль');
    const payload=JSON.stringify({name:file.name,data,cfg}).replace(/</g,'\\u003c');
    const close='</scr'+'ipt>';
    const scripts='<script type="application/json" id="stats-offline-data">'+payload+close+'<script type="application/octet-stream" id="stats-offline-wasm">'+await encode(await wasm.blob())+close+'<script>/*\n'+assets[5]+'*/\n'+assets[4]+close+'<script>'+assets[3]+close+'<script>'+assets[2]+close;
    let html=assets[0].replace('<script src="stats.js" defer></script>',scripts).replace('<link rel="stylesheet" href="stats.css">','<style>'+assets[1]+'</style>').replace('<body class="stats-app">','<body class="stats-app stats-offline">'+result.html);
    return new Blob([html],{type:'text/html;charset=utf-8'});
   }
  }
  let payload;
  if(!active || active.file!==file){await call({action:'upload',name:file.name,data});payload=await call({action:'import',name:file.name,data});active={file,id:payload.log_id,revision:payload.revision};}
  else payload=await call(cfg?{action:'configure',log_id:active.id,revision:active.revision,cfg}:{action:'report',log_id:active.id});
  active.revision=payload.revision;payload.report.defaults=payload.defaults;
  refreshFiles().catch(showError);
  return payload.report;
 }
};
function page(name){q('body').dataset.browserPage=name;for(const b of document.querySelectorAll('[data-page]'))b.setAttribute('aria-pressed',String(b.dataset.page===name));q('body > .wrap').hidden=name!=='stats';for(const el of document.querySelectorAll('.browser-panel'))el.hidden=el.id!=='browser-'+name;if(name==='stats')window.dzzzrStats?.reload();if(name==='editor'){
 const frame=q('#editor-frame');if(!frame.src)frame.src='editor.html#/editor';
 else frame.contentWindow.document.documentElement.dataset.theme=window.dzzzrLocalWorkspace.theme();
}}
async function refreshFiles(){const names=await call({action:'files'});q('#file-list').replaceChildren();for(const name of names){const li=document.createElement('li');li.textContent=name;const b=document.createElement('button');b.textContent='Скачать';b.onclick=async()=>{try{const r=await call({action:'download',name});download(name,new Blob([decode(r.data)]))}catch(e){showError(e)}};li.append(b);q('#file-list').append(li)}for(const [selector,filter] of [['#pdf-file',n=>/\.pdf$/i.test(n)],['#scenario-file',n=>/\.json$/i.test(n)]]){const select=q(selector),old=select.value;select.replaceChildren();for(const n of names.filter(filter)){select.add(new Option(n,n))}if(names.includes(old))select.value=old}}
async function search(){if(!active)throw new Error('Сначала загрузите журнал');const r=await call({action:'tool',name:'stats_search',args:{log_id:active.id,...searchArgs,offset:next||0,limit:30}});q('#search-result').textContent=JSON.stringify(r,null,2);next=r.next_offset??null;q('#search-next').hidden=next===null}
function inlineAnswer(parent, text){
 const tokens=/\*\*([^*]+)\*\*|`([^`]+)`/g;
 let at=0,match;
 while((match=tokens.exec(text))){
  parent.append(document.createTextNode(text.slice(at,match.index)));
  const node=document.createElement(match[1]?'strong':'code');
  node.textContent=match[1]||match[2];
  parent.append(node);
  at=tokens.lastIndex;
 }
 parent.append(document.createTextNode(text.slice(at)));
}
function formatAnswer(parent,text){
 let list=null,code=null;
 for(const line of text.replace(/\r\n/g,'\n').split('\n')){
  if(/^```/.test(line)){
   if(code){code=null}else{const pre=document.createElement('pre');code=document.createElement('code');pre.append(code);parent.append(pre)}
   list=null;continue;
  }
  if(code){code.textContent+=line+'\n';continue}
  if(!line.trim()){list=null;continue}
  const bullet=line.match(/^\s*[-*]\s+(.+)$/);
  if(bullet){
   if(!list){list=document.createElement('ul');parent.append(list)}
   const item=document.createElement('li');inlineAnswer(item,bullet[1]);list.append(item);continue;
  }
  list=null;
  const heading=line.match(/^#{1,3}\s+(.+)$/);
  const block=document.createElement(heading?'h3':'p');
  inlineAnswer(block,heading?heading[1]:line);
  parent.append(block);
 }
}
function message(role,text){
 const el=document.createElement('div');
 el.className='browser-message '+role;
 if(role==='assistant')formatAnswer(el,text);else el.textContent=text;
 const transcript=q('#agent-transcript');transcript.append(el);transcript.scrollTop=transcript.scrollHeight;
}
function renderSessions(){
 const select=q('#agent-session');select.replaceChildren();for(const id of Object.keys(sessions))select.add(new Option(id==='general'?'Общий диалог':`Черновик ${id}`,id));select.value=selectedSession;
 q('#agent-open-editor').hidden=selectedSession==='general';q('#agent-transcript').replaceChildren();for(const m of sessions[selectedSession]||[])message(m.role,m.content);
}
async function openEditorDraft(){page('editor');const frame=q('#editor-frame');if(!frame.contentWindow.dzzzrEditor)await new Promise(resolve=>frame.addEventListener('load',resolve,{once:true}));await frame.contentWindow.dzzzrEditor.openLocalDraft(selectedSession)}
let approvalCallback=null;
function finishApproval(yes){const fn=approvalCallback;approvalCallback=null;q('#agent-approval').hidden=true;if(fn)fn(yes)}
window.dzzzrBrowserApprove=(name,args,callback)=>{q('#approval-title').textContent='Разрешить '+name+'?';q('#approval-args').textContent=args;approvalCallback=callback;q('#agent-approval').hidden=false};
window.dzzzrBrowserEvent=raw=>{
 const e=JSON.parse(raw);if(e.type==='report')q('#agent-report').textContent=e.report;
 if(e.type==='warning')q('#agent-status').textContent=e.message;
 if(e.type==='tool_start'||e.type==='tool_done'){
  const details=document.createElement('details');const summary=document.createElement('summary');summary.textContent=e.name+(e.type==='tool_start'?' · вызов':e.error?' · ошибка':' · результат');const pre=document.createElement('pre');pre.textContent=e.type==='tool_start'?e.args:e.result;details.append(summary,pre);q('#agent-trace').append(details);
  if(e.type==='tool_done')persistWorkspace().catch(showError);
 }
};
document.addEventListener('DOMContentLoaded',()=>{
 q('#stats-app-link')?.remove();q('.casebar p').textContent='Загрузите XLSX, JSON или CSV: расчёт выполняется здесь в Go. Статистика и поиск доступны агенту в этой вкладке. Экспорт HTML сохраняет независимый офлайн-отчёт.';
 for(const b of document.querySelectorAll('[data-page]'))b.onclick=()=>page(b.dataset.page);
 q('#browser-upload').onchange=async e=>{try{for(const f of e.target.files)await call({action:'upload',name:f.name,data:await encode(f)});await refreshFiles()}catch(e){showError(e)}};
 q('#search-form').onsubmit=async e=>{e.preventDefault();searchArgs=Object.fromEntries(new FormData(e.target));next=null;try{await search()}catch(e){showError(e)}};q('#search-next').onclick=()=>search().catch(showError);
 q('#pdf-form').onsubmit=async e=>{e.preventDefault();try{const f=new FormData(e.target);const r=await call({action:'tool',name:'index_pdf',args:{path:q('#pdf-file').value,start_page:Number(f.get('start_page')||1),end_page:Number(f.get('end_page')||0)}});q('#pdf-result').textContent=JSON.stringify(r,null,2)}catch(e){showError(e)}};
 q('#scenario-open').onclick=async()=>{try{const r=await call({action:'download',name:q('#scenario-file').value});q('#scenario-json').value=new TextDecoder().decode(decode(r.data))}catch(e){showError(e)}};
 q('#scenario-check').onclick=async()=>{try{const name='scenario-edited.json';await call({action:'upload',name,data:await encode(new Blob([q('#scenario-json').value]))});const r=await call({action:'tool',name:'validate_scenario',args:{path:name,output_path:'scenario-validated.json'}});q('#scenario-result').textContent=JSON.stringify(r,null,2);const file=await call({action:'download',name:'scenario-validated.json'});download('scenario-validated.json',new Blob([decode(file.data)],{type:'application/json'}));await refreshFiles()}catch(e){q('#scenario-result').textContent=e.message}};
 q('#scenario-schema').onclick=async()=>{try{q('#scenario-result').textContent=JSON.stringify(await call({action:'schema'}),null,2)}catch(e){showError(e)}};
 q('#agent-session').onchange=e=>{selectedSession=e.target.value;renderSessions()};
 q('#agent-open-editor').onclick=()=>openEditorDraft().catch(showError);
 q('#workspace-backup').onclick=async()=>{try{await q('#editor-frame').contentWindow.dzzzrEditor?.flushDraft();const state=await call({action:'workspace_export'});download('dzzzr-workspace.json',new Blob([JSON.stringify(state,null,2)],{type:'application/json'}))}catch(e){showError(e)}};
 q('#workspace-restore').onchange=async e=>{try{const f=e.target.files[0];if(!f)return;if(!confirm('Заменить локальные игры и черновики данными резервной копии? Сначала скачайте текущую копию.'))return;await call({action:'workspace_restore',args:JSON.parse(await f.text())});q('#editor-frame').src='editor.html#/editor';sessions={general:[]};selectedSession='general';await store('chats',sessions);renderSessions()}catch(e){showError(e)}};
 q('#agent-cancel').onclick=()=>{finishApproval(false);window.dzzzrBrowserCancel?.()};
 q('#approval-yes').onclick=()=>finishApproval(true);q('#approval-no').onclick=()=>finishApproval(false);
 q('#agent-form').onsubmit=async e=>{
  e.preventDefault();const text=q('#agent-prompt').value.trim();if(!text)return;
  const session=selectedSession;const content=text+(active?'\nТекущий журнал log_id='+active.id:'');
  sessions[session]??=[];sessions[session].push({role:'user',content});message('user',text);
  q('#agent-send').disabled=true;q('#agent-session').disabled=true;q('#agent-cancel').disabled=false;q('#agent-trace').replaceChildren();q('#agent-report').textContent='';
  try{
   await q('#editor-frame').contentWindow.dzzzrEditor?.flushDraft();
   await store('chats',sessions);
   const extra=q('#llm-extra').value.trim();
   const result=await call({action:'chat',llm:{endpoint:q('#llm-endpoint').value,key:q('#llm-key').value,model:q('#llm-model').value,policy:q('#llm-policy').value,draft_id:session==='general'?'':session,max_turns:Number(q('#llm-turns').value),request_timeout_seconds:Number(q('#llm-timeout').value),extra_body:extra?JSON.parse(extra):{}},messages:sessions[session]});
   const messages=result.Messages;const target=result.draft_id||session;
   sessions[target]=messages;selectedSession=target;await store('chats',sessions);await persistWorkspace();renderSessions();
   q('#agent-report').textContent=result.Report||'';
   if(result.log_id){const source=await call({action:'log_source',log_id:result.log_id});const file=new File([decode(source.data)],source.name);active={id:result.log_id,revision:source.revision,file};await window.dzzzrStats.load(file)}
   q('#agent-prompt').value='';await refreshFiles();q('#agent-status').textContent='Готово';
  }catch(e){q('#agent-status').textContent=e.message;if(e.result?.draft_id){selectedSession=e.result.draft_id;sessions[selectedSession]=e.result.Messages||sessions[session];renderSessions();await store('chats',sessions)}await persistWorkspace().catch(showError)}
  finally{finishApproval(false);q('#agent-send').disabled=false;q('#agent-session').disabled=false;q('#agent-cancel').disabled=true}
 };
 workspaceReady.then(()=>{renderSessions();q('#editor-frame').src='editor.html#/editor'}).catch(showError);
 workspaceReady.then(()=>{q('#browser-status').textContent='Готово · игры и черновики сохраняются в браузере'}).catch(showError);
 if('serviceWorker' in navigator)navigator.serviceWorker.register('sw.js').catch(()=>{});
});
