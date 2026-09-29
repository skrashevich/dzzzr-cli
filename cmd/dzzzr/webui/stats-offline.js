// Transport for exported HTML. All parsing, scoring and XLSX export use Go WASM.
(function(){
  'use strict';
  const payload = JSON.parse(document.getElementById('stats-offline-data').textContent);
  const decode = s => Uint8Array.from(atob(s), c => c.charCodeAt(0));
  async function encode(file){
    const bytes = new Uint8Array(await file.arrayBuffer());
    let binary = '';
    for(let i=0;i<bytes.length;i+=32768) binary += String.fromCharCode(...bytes.subarray(i,i+32768));
    return btoa(binary);
  }
  let ready;
  function calculator(){
    return ready ??= (async () => {
      const go = new Go();
      const {instance} = await WebAssembly.instantiate(decode(document.getElementById('stats-offline-wasm').textContent), go.importObject);
      // Go stays alive to service subsequent parameter changes.
      go.run(instance).catch(error => console.error(error));
      if(typeof window.dzzzrOfflineCalculate !== 'function') throw new Error('Не удалось запустить офлайн-расчёт.');
    })();
  }
  window.dzzzrStatsOffline = {
    initialFile: new File([decode(payload.data)], payload.name),
    initialConfig: payload.cfg,
    async request(file, cfg, format){
      await calculator();
      const result = JSON.parse(window.dzzzrOfflineCalculate(file.name, await encode(file), JSON.stringify(cfg), format));
      if(result.error) throw new Error(result.error);
      if(format === 'html'){
        const copy = document.documentElement.cloneNode(true);
        copy.querySelector('body').classList.remove('stats-interactive');
        copy.querySelector('#stats-static').outerHTML = result.html;
        copy.querySelector('#stats-offline-data').textContent = JSON.stringify({name:file.name, data:await encode(file), cfg}).replace(/</g, '\\u003c');
        return new Blob(['<!doctype html>\n'+copy.outerHTML], {type:'text/html;charset=utf-8'});
      }
      return format === 'xlsx' ? new Blob([decode(result.xlsx)], {type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'}) : result;
    },
  };
})();
