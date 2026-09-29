'use strict';
// The existing editor talks to Go/WASM through the same API as native web.
const local = parent.dzzzrLocalWorkspace;
let assets = {}, assetsGame = null;
window.closeAuthPopovers = () => {};
window.dzzzrLocalEditor = {
 resolveAsset(raw) {
  if (!raw?.startsWith('asset:')) return raw;
  const key = raw.slice(6).split('#')[0], a = assets[key];
  if (a?.data_base64) return `data:${a.media_type};base64,${a.data_base64}`;
  return a?.url || raw;
 }
};
async function api(path,options={}) {
 let body=options.body;
 if (body instanceof FormData) {const file=body.get('file');body={name:file.name,data:await local.encode(file)}}
 const result=await local.api(path,{...options,body});
 const match=path.match(/^\/admin\/games\/(\d+)$/);
 const game=match?Number(match[1]):result?.game_id;
 if (game && game!==assetsGame && (!options.method || options.method==='GET' || path==='/admin/drafts/open')) {
  const scenario=await local.api(`/admin/games/${game}/assets`);assets=scenario.assets||{};assetsGame=game;
 }
 if (path.endsWith('/files')) {
  const base=path.slice(0,-6);const scenario=await local.api(base+'/assets');assets=scenario.assets||{};
 }
 return result;
}
function toast(text,error=false) {const el=document.getElementById('toast');el.textContent=text;el.classList.add('is-visible');el.classList.toggle('is-error',error);setTimeout(()=>el.classList.remove('is-visible'),6000)}
window.dzzzrChat={api,toast,activeDraft:()=>local.activeDraft(),openChat:async id=>{await local.openAgent(id)}};
const nativeFetch=window.fetch.bind(window);
window.fetch=async(url,options={})=>{
 if (typeof url==='string' && url.startsWith('/api/v1/admin/')) {
  try {const result=await api(url.slice(7),options);return new Response(JSON.stringify(result),{headers:{'Content-Type':'application/json'}})}
  catch(e){return new Response(JSON.stringify({error:e.message}),{status:e.status||400,headers:{'Content-Type':'application/json'}})}
 }
 return nativeFetch(url,options);
};
document.body.classList.add('is-ready');
document.documentElement.dataset.theme=local.theme();
window.addEventListener('DOMContentLoaded',()=>{
 document.querySelector('.brand-tag').textContent='локальный редактор';
 document.getElementById('btn-editor-save').textContent='Сохранить локально';
 document.querySelector('.editor-dialog-hint').textContent='Откройте JSON-сценарий dzzzr-scenario как новую локальную игру. HTML, вложения и метаданные сохранятся.';
 const apply=document.getElementById('btn-import-apply');if(apply)apply.textContent='Открыть в редакторе';
 const id=document.getElementById('import-game-id');if(id){id.placeholder='Новая локальная игра';id.closest('label').hidden=true}
 const map=document.getElementById('import-level-ids');if(map)map.closest('label').hidden=true;
});
