'use strict';
window.dzzzrBrowserWorkspace = true;
const q = s=>document.querySelector(s);
const decode = s=>Uint8Array.from(atob(s),c=>c.charCodeAt(0));
async function encode(file){const a=new Uint8Array(await file.arrayBuffer());let s='';for(let i=0;i<a.length;i+=32768)s+=String.fromCharCode(...a.subarray(i,i+32768));return btoa(s)}
function download(name,blob){const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(a.href),5000)}
const ready = (async()=>{const go=new Go();const response=await fetch('browser.wasm');if(!response.ok)throw new Error('Не удалось загрузить Go-модуль');const {instance}=await WebAssembly.instantiate(await response.arrayBuffer(),go.importObject);go.run(instance).catch(showError);})();
async function call(request){await ready;const out=JSON.parse(await window.dzzzrBrowserCall(JSON.stringify(request)));if(out.error)throw new Error(out.error);return out.result}
function showError(e){const el=q('#browser-status');if(el)el.textContent=e.message||String(e)}
window.dzzzrBrowserStatus=s=>{q('#agent-status').textContent=s};
let active=null,chatHistory=[],searchArgs={},next=null;
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
function page(name){for(const b of document.querySelectorAll('[data-page]'))b.setAttribute('aria-pressed',String(b.dataset.page===name));q('body > .wrap').hidden=name!=='stats';for(const el of document.querySelectorAll('.browser-panel'))el.hidden=el.id!=='browser-'+name;if(name==='stats')window.dzzzrStats?.reload();}
async function refreshFiles(){const names=await call({action:'files'});q('#file-list').replaceChildren();for(const name of names){const li=document.createElement('li');li.textContent=name;const b=document.createElement('button');b.textContent='Скачать';b.onclick=async()=>{try{const r=await call({action:'download',name});download(name,new Blob([decode(r.data)]))}catch(e){showError(e)}};li.append(b);q('#file-list').append(li)}for(const [selector,filter] of [['#pdf-file',n=>/\.pdf$/i.test(n)],['#scenario-file',n=>/\.json$/i.test(n)]]){const select=q(selector),old=select.value;select.replaceChildren();for(const n of names.filter(filter)){select.add(new Option(n,n))}if(names.includes(old))select.value=old}}
async function search(){if(!active)throw new Error('Сначала загрузите журнал');const r=await call({action:'tool',name:'stats_search',args:{log_id:active.id,...searchArgs,offset:next||0,limit:30}});q('#search-result').textContent=JSON.stringify(r,null,2);next=r.next_offset??null;q('#search-next').hidden=next===null}
function message(role,text){const el=document.createElement('div');el.className='browser-message '+role;el.textContent=text;q('#agent-transcript').append(el)}
document.addEventListener('DOMContentLoaded',()=>{
 q('#stats-app-link')?.remove();q('.casebar p').textContent='Загрузите XLSX, JSON или CSV: расчёт выполняется здесь в Go. Статистика и поиск доступны агенту в этой вкладке. Экспорт HTML сохраняет независимый офлайн-отчёт.';
 for(const b of document.querySelectorAll('[data-page]'))b.onclick=()=>page(b.dataset.page);
 q('#browser-upload').onchange=async e=>{try{for(const f of e.target.files)await call({action:'upload',name:f.name,data:await encode(f)});await refreshFiles()}catch(e){showError(e)}};
 q('#search-form').onsubmit=async e=>{e.preventDefault();searchArgs=Object.fromEntries(new FormData(e.target));next=null;try{await search()}catch(e){showError(e)}};q('#search-next').onclick=()=>search().catch(showError);
 q('#pdf-form').onsubmit=async e=>{e.preventDefault();try{const f=new FormData(e.target);const r=await call({action:'tool',name:'index_pdf',args:{path:q('#pdf-file').value,start_page:Number(f.get('start_page')||1),end_page:Number(f.get('end_page')||0)}});q('#pdf-result').textContent=JSON.stringify(r,null,2)}catch(e){showError(e)}};
 q('#scenario-open').onclick=async()=>{try{const r=await call({action:'download',name:q('#scenario-file').value});q('#scenario-json').value=new TextDecoder().decode(decode(r.data))}catch(e){showError(e)}};
 q('#scenario-check').onclick=async()=>{try{const name='scenario-edited.json';await call({action:'upload',name,data:await encode(new Blob([q('#scenario-json').value]))});const r=await call({action:'tool',name:'validate_scenario',args:{path:name,output_path:'scenario-validated.json'}});q('#scenario-result').textContent=JSON.stringify(r,null,2);const file=await call({action:'download',name:'scenario-validated.json'});download('scenario-validated.json',new Blob([decode(file.data)],{type:'application/json'}));await refreshFiles()}catch(e){q('#scenario-result').textContent=e.message}};
 q('#scenario-schema').onclick=async()=>{try{q('#scenario-result').textContent=JSON.stringify(await call({action:'schema'}),null,2)}catch(e){showError(e)}};
 q('#agent-cancel').onclick=()=>window.dzzzrBrowserCancel?.();
 q('#agent-form').onsubmit=async e=>{e.preventDefault();const text=q('#agent-prompt').value.trim();if(!text)return;const content=text+(active?'\nТекущий журнал log_id='+active.id:'');message('user',text);q('#agent-send').disabled=true;q('#agent-cancel').disabled=false;try{const messages=await call({action:'chat',llm:{endpoint:q('#llm-endpoint').value,key:q('#llm-key').value,model:q('#llm-model').value},messages:[...chatHistory,{role:'user',content}]});chatHistory=messages;message('assistant',messages.at(-1).content);
 for(const m of [...messages].reverse()){
  if(m.role!=='tool')continue;
  let result;try{result=JSON.parse(m.content)}catch{continue}
  if(result.log_id && result.log_id!==active?.id){
   const source=await call({action:'log_source',log_id:result.log_id});
   const file=new File([decode(source.data)],source.name);
   active={id:result.log_id,revision:source.revision,file};
   await window.dzzzrStats.load(file);
  }
  if(result.log_id)break;
 }
q('#agent-prompt').value='';await refreshFiles();q('#agent-status').textContent='Готово'}catch(e){q('#agent-status').textContent=e.message}finally{q('#agent-send').disabled=false;q('#agent-cancel').disabled=true}};
 ready.then(()=>{q('#browser-status').textContent='Готово · файлы остаются в памяти этой вкладки'}).catch(showError);
 if('serviceWorker' in navigator)navigator.serviceWorker.register('sw.js').catch(()=>{});
});
