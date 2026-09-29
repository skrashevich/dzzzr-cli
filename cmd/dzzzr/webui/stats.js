// Statistics display adapted from dozor_stats.html.
// Original algorithm author: Sergey <sergey@luberg.me> Luberg.
(function(){
function initStats(root){
  'use strict';

  const $ = (s, r=root)=>r.querySelector(s);
  const esc = s => String(s ?? '').replace(/[&<>"']/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

  // ---------------------------------------------------------------
  // 1. ВРЕМЯ И ФОРМАТЫ
  // ---------------------------------------------------------------
  const pad2 = n => String(n).padStart(2, "0");
  function fmtHMS(sec){
    if(sec===null || sec===undefined || isNaN(sec)) return '';
    const neg = sec<0; sec = Math.round(Math.abs(sec));
    const h = Math.floor(sec/3600), m = Math.floor(sec%3600/60), s = sec%60;
    return (neg?'-':'')+pad2(h)+':'+pad2(m)+':'+pad2(s);
  }
  // Как в столбцах «Штраф» и «Бонусы» на сайте: 0:56, 1:05; секунды — только если они есть
  function fmtHM(sec){
    const neg = sec<0; sec = Math.round(Math.abs(sec));
    const h = Math.floor(sec/3600), m = Math.floor(sec%3600/60), s = sec%60;
    return (neg?'-':'')+h+':'+pad2(m)+(s?':'+pad2(s):'');
  }
  function fmtClock(ms){
    if(ms===null || ms===undefined) return '—';
    const d = new Date(ms);
    return pad2(d.getUTCHours())+':'+pad2(d.getUTCMinutes())+':'+pad2(d.getUTCSeconds());
  }
  function fmtDT(ms){
    if(ms===null || ms===undefined) return '—';
    const d = new Date(ms);
    return pad2(d.getUTCDate())+'.'+pad2(d.getUTCMonth()+1)+' '+fmtClock(ms);
  }
  function toLocalInput(ms){
    const d = new Date(ms);
    return d.getUTCFullYear()+'-'+pad2(d.getUTCMonth()+1)+'-'+pad2(d.getUTCDate())+'T'+fmtClock(ms);
  }
  function fmtMin(x){ return (Math.round(x*100)/100).toString().replace('.',','); }

  const defaultCfg = () => ({game:{}, levels:{}, teams:{}});
  // ---------------------------------------------------------------
  // 6. ОТРИСОВКА
  // ---------------------------------------------------------------
  const state = {game:null, cfg:defaultCfg(), res:null, sel:null, source:null, storeKey:null};

  const HOW_MARK = {credit:'з', avg:'ср', refuse:'о'};
  const HOW_TEXT = {
    code:'выполнен — введены все коды', credit:'засчитан организатором', avg:'засчитано среднее время',
    timeout:'не выполнен — вышел лимит времени', refuse:'не выполнен — отказ от уровня',
    stop:'не выполнен до стоп-игры', none:'не получен',
  };
  const ST_TEXT = {h0:'без подсказок', h1:'после первой подсказки', h2:'после второй подсказки', fail:'не выполнено', none:'не получено', stop:'не выполнено до стоп-игры'};

  function cellHtml(c, L){
    if(!c) return '<td class="lvcol"></td>';
    const stub = L.kind==='bonus' ? ' stubcol' : '';
    const key = `data-lv="${esc(L.name)}"`;
    if(L.kind==='through'){
      let txt = '', title = '';
      if(c.show){
        if(c.configured) txt = c.complete || c.bonusSec>0 ? fmtHM(c.bonusSec) : '';
        else if(c.nc||c.nb) txt = '✓'+(c.nb?' +'+c.nb+'б':'');
        title = 'кодов: '+c.nc+(c.nb?', бонусных: '+c.nb:'')+(c.rec&&c.rec.hintReq?', запрошено подсказок: '+c.rec.hintReq:'')+(c.configured?'':' — размер бонуса не задан в параметрах');
      }
      return `<td class="cell lvcol st-thr" ${key} title="${esc(title)}">${esc(txt)}</td>`;
    }
    if(!c.show) return `<td class="cell lvcol${stub}" ${key}></td>`;
    const sp = c.rec && c.rec.spoilers.size ? ' · спойлер: '+[...c.rec.spoilers.values()].map(s=>s.raw+' в '+fmtClock(s.t)).join(', ') : '';
    if(L.kind==='bonus'){
      // Как на сайте: у бонусного уровня в клетке бонус за выполнение, у невыполненного — пусто
      const ok = /^h/.test(c.st);
      const title = 'бонусный уровень, в зачёт не идёт · '+(ok?'выполнен за '+fmtHMS(c.sec):HOW_TEXT[c.how]||'')+(c.bonusSec?' · бонус '+fmtHM(c.bonusSec):'')+sp;
      return `<td class="cell lvcol${stub} st-bon${c.best?' best':''}" ${key} title="${esc(title)}">${ok?`<span class="v">${fmtHM(L.doneBonusSec)}</span>`:''}</td>`;
    }
    const cls = 'st-'+c.st;
    const mark = HOW_MARK[c.how] || (c.implicit ? 'т' : '') || (c.virtualIssue ? 'з' : '');
    const title = (ST_TEXT[c.st]||'') + (c.how && c.how!=='none' ? ' · '+HOW_TEXT[c.how] : '') + (c.rejectedFix ? ' · по первому вводу верного ответа, отклонённого движком' : '') + sp;
    return `<td class="cell lvcol${stub} ${cls}${c.best?' best':''}" ${key} title="${esc(title)}"><span class="v">${fmtHMS(c.sec)}</span>${mark||c.rejectedFix?`<span class="mark">${c.rejectedFix?'!':mark}</span>`:''}</td>`;
  }

  function levelHead(L){
    const stub = L.kind==='bonus' ? ' stubcol' : '';
    const sub = L.kind==='bonus' ? '(бонусное)' : L.kind==='through' ? '(бонусное сквозное)' : '';
    const top = L.num ? L.num : L.title;
    const title = L.num && L.title ? L.title : '';
    const sel = state.sel && state.sel.type==='level' && state.sel.level===L.name ? ' sel' : '';
    return `<th class="lvcol${stub}${sel}" data-lvh="${esc(L.name)}" title="${esc(L.name)}"><span class="n">${esc(top)}${title?'<br>'+esc(title):''}</span>${sub?`<span class="sub">${sub}</span>`:''}</th>`;
  }

  function renderBoard(){
    const {R, rows} = state.res;
    const lv = R.levels.filter(L=>L.kind!=='hidden');
    let h = '<thead><tr><th class="team-h plain">КОМАНДЫ</th>';
    h += lv.map(levelHead).join('');
    h += '<th class="plain">Штраф</th><th class="plain">Бонусы</th><th class="plain">Чистое время</th><th class="plain">Общее время</th><th class="plain" title="по общему / по чистому времени">Место</th><th class="plain">Отставание от лидера</th><th class="plain">Отставание от предыдущего</th></tr></thead><tbody>';
    for(const r of rows){
      h += `<tr data-team="${esc(r.team)}"><th><button data-teamb="${esc(r.team)}">${esc(r.team)}</button></th>`;
      for(const L of lv){
        const c = r.cells[L.name];
        let html = cellHtml(c, L);
        if(state.sel && state.sel.type==='cell' && state.sel.team===r.team && state.sel.level===L.name) html = html.replace('class="cell', 'class="cell selected');
        h += html;
      }
      const pen = fmtHM(r.add) + (r.penalty ? '+'+fmtHM(r.penalty) : '');
      const bon = fmtHM(r.thr) + (r.bonusOther ? '+'+fmtHM(r.bonusOther) : '');
      h += `<td class="tot">${pen}</td><td class="tot">${bon}</td><td class="tot">${fmtHMS(r.clean)}</td>`;
      h += `<td class="tot total">${fmtHMS(r.total)}</td><td class="tot place">${r.place} / ${r.placeClean}</td>`;
      h += `<td class="tot">${r.gapLeader===null?'-':fmtHMS(r.gapLeader)}</td><td class="tot">${r.gapPrev===null?'-':fmtHMS(r.gapPrev)}</td></tr>`;
    }
    h += '</tbody>';
    const t = $('#stats-board');
    t.innerHTML = h;
    t.classList.toggle('hide-levels', !state.showLevels);
    t.classList.toggle('hide-stubs', !state.showStubs);
  }

  function describeCell(team, L, c){
    const r = c.rec;
    const parts = [];
    let h = `<h3>${esc(team)} · ${esc(L.name)}<span class="tag">${L.kind==='main'?'зачётный':L.kind==='bonus'?'бонусный':'сквозной'}</span></h3>`;
    if(L.kind==='through'){
      h += '<div class="kv">';
      h += kv('Выдан', r&&r.issued!==null?fmtDT(r.issued):'—');
      h += kv('Принято кодов', (c.nc||0)+(L.reqThrough?' из '+L.reqThrough:'')) + kv('Бонусных кодов', c.nb||0);
      if(r && r.hintReq) h += kv('Запрошено подсказок', r.hintReq);
      h += kv('Бонус', fmtHM(c.bonusSec||0));
      h += '</div>';
      const parts = [];
      if(L.done) parts.push(fmtMin(L.done)+' мин за выполнение');
      if(L.perCode) parts.push('коды × '+fmtMin(L.perCode)+' мин');
      if(L.perBonus || L.codeVals.size) parts.push('бонусные коды'+(L.perBonus?' × '+fmtMin(L.perBonus)+' мин':'')+(L.codeVals.size?' (особые: '+esc(L.codeValsText)+')':''));
      if(L.hintPen) parts.push('− '+fmtMin(L.hintPen)+' мин за каждую запрошенную подсказку');
      h += `<div class="expl">Сквозной уровень идёт параллельно основной цепочке и в чистое время не входит. Бонус: ${parts.length?parts.join(', '):'не задан'}${L.cap>0?' (не больше '+fmtMin(L.cap)+' мин)':''} — размеры задаются в параметрах, в журнале их нет.</div>`;
      if(r) h += codesTable(r);
      return h;
    }
    h += '<div class="kv">';
    h += kv('Выдан', c.issued!==null ? fmtDT(c.issued)+(c.virtualIssue?' (при стоп-игре)':'') : '—');
    h += kv('Закрыт', c.end ? fmtDT(c.end) : (c.how==='stop' ? 'стоп-игра '+fmtClock(state.res.R.stopAt) : '—'));
    h += kv('Время в зачёт', c.show ? fmtHMS(c.countSec) : '—');
    h += kv('Добавочное', c.addSec ? fmtHM(c.addSec) : '—');
    h += kv('Лимит · подсказки', fmtMin(L.dur)+' · '+fmtMin(L.h1)+' / '+fmtMin(L.h2)+' мин');
    if(r){
      h += kv('Кодов принято', r.codes.size + (L.reqCodes ? ' из '+L.reqCodes : ''));
      if(r.bonus.size) h += kv('Бонусных кодов', r.bonus.size + (c.bonusSec ? ' → '+fmtHM(c.bonusSec) : ''));
      if(r.spoilerAt!==null) h += kv('Спойлер открыт', fmtClock(r.spoilerAt) + (c.issued!==null ? ' (+'+fmtHMS(c.spoilerElapsed)+')' : ''));
      if(r.timeoutAt!==null) h += kv('«Вышло время» в журнале', fmtClock(r.timeoutAt));
      if(r.order) h += kv('Порядок у команды', r.order+'-й уровень');
    }
    h += '</div>';
    const why = [];
    why.push('<b>'+esc(HOW_TEXT[c.how]||'')+'</b>'+(c.st && ST_TEXT[c.st] && /^h/.test(c.st) ? ', '+ST_TEXT[c.st] : '')+'.');
    if(c.how==='credit') why.push('В журнале «засчитан уровень» ('+fmtDT(r.override.logged)+') со временем '+fmtDT(r.override.at)+' — время уровня считается до этого момента, а не до выдачи следующего.');
    if(c.virtualIssue) why.push('Выдачи этого уровня в журнале нет: команда перестала заходить в движок, и следующий уровень движок выдал ей только при завершении игры ('+fmtDT(c.issued)+'). От этого момента и считается засчитанное время.');
    if(c.rejectedFix) why.push(`<b>Движок отклонял верный ответ спойлера.</b> Команда ввела «${esc(r.rejected.raw)}» в ${fmtClock(r.rejected.t)}${r.rejected.count>1?' (всего '+r.rejected.count+' раз)':''}, но он был засчитан как неверный. Позже этот же ответ принят как правильный, значит, в движке была ошибка. Время считается по первому вводу — ${fmtClock(c.rejectedFix.to)} вместо ${fmtClock(c.rejectedFix.from)} (отключается в параметрах).`);
    if(L.kind==='bonus') why.push('Бонусный уровень: в чистое время не входит, в клетке таблицы — бонус за выполнение.');
    if(c.how==='avg') why.push('В журнале «засчитано среднее время» ('+fmtDT(r.override.logged)+'): уровень закрыт на '+fmtDT(r.override.at)+', т.е. через '+fmtHMS(c.sec)+' после выдачи.');
    if(c.how==='refuse') why.push('Отказ от уровня в '+fmtClock(r.refusedAt)+' — уровень не пройден (п. 5.13), в зачёт идёт полный лимит '+fmtMin(L.dur)+' мин и добавочное время.');
    if(c.how==='timeout' && !c.implicit) why.push('Лимит '+fmtMin(L.dur)+' мин истёк в '+fmtClock(c.expiresAt)+'; в зачёт — полный лимит плюс добавочное '+fmtHM(c.addSec)+' (п. 6.5).');
    if(c.implicit) why.push('Записи «вышло время» нет — команда перестала обращаться к движку, но лимит истёк в '+fmtClock(c.end)+' до стоп-игры, поэтому уровень считается невыполненным.');
    if(c.how==='stop') why.push('К стоп-игре ('+fmtClock(state.res.R.stopAt)+') уровень не закрыт. В зачёт — '+(state.res.R.stopMode==='limit'?'полный лимит':'фактическое время до стоп-игры')+' плюс добавочное время.');
    if(c.how==='none') why.push(state.res.R.noneMode==='limit' ? 'Команда не дошла до уровня: в зачёт идёт полный лимит и добавочное время (настраивается в параметрах).' : state.res.R.noneMode==='add' ? 'Команда не дошла до уровня: начислено только добавочное время.' : 'Команда не дошла до уровня: не учитывается.');
    if(c.how==='code') why.push('Уровень закрыт в '+fmtClock(c.end)+(r.closedAt!==null ? ' — в этот момент команде выдан следующий уровень.' : ' — введён '+L.reqCodes+'-й из нужных кодов. Уровень был последним у команды, поэтому следующий не выдавался'+(r.timeoutAt!==null?', а записи «вышло время» после '+fmtClock(r.timeoutAt)+' — это таймер уже пройденного уровня':'')+'.'));
    if(r && r.badOverride) why.push('<b>Внимание:</b> '+esc(r.badOverride));
    h += '<div class="expl">'+why.join(' ')+'</div>';
    if(r && (r.codes.size || r.bonus.size || r.spoilers.size)) h += codesTable(r);
    return h;
  }
  function kv(k,v){ return `<div><div class="k">${esc(k)}</div><div class="v">${esc(String(v))}</div></div>`; }
  function codesTable(r){
    const list = [];
    r.spoilers.forEach(v=>list.push({t:v.t, elapsed:v.elapsed, code:v.raw, player:v.player, type:'spoiler', n:v.n, count:v.count}));
    r.codes.forEach(v=>list.push({t:v.t, elapsed:v.elapsed, code:v.raw, player:v.player, type:'code'}));
    r.bonus.forEach(v=>list.push({t:v.t, elapsed:v.elapsed, code:v.raw, player:v.player, type:'bonus'}));
    list.sort((a,b)=>a.t-b.t);
    const shown = list.slice(0, 80);
    const pill = x => x.type==='spoiler' ? `<span class="pill spoiler">спойлер${x.n?' '+esc(x.n):''}</span>`
                    : x.type==='bonus' ? '<span class="pill mid">бонусный</span>' : '<span class="pill ok">код</span>';
    let h = '<div class="mini-scroll"><table class="mini"><thead><tr><th>Время</th><th>+ от выдачи</th><th>Код</th><th>Тип</th><th>Игрок</th></tr></thead><tbody>';
    for(const x of shown) h += `<tr><td class="mono">${fmtClock(x.t)}</td><td class="mono">${r.issued!==null?fmtHMS(x.elapsed):''}</td><td class="mono">${esc(x.code)}${x.count>1?` <span class="subnote">×${x.count}</span>`:''}</td><td>${pill(x)}</td><td>${esc(x.player??'')}</td></tr>`;
    h += '</tbody></table></div>';
    if(list.length>shown.length) h += `<p class="subnote">…и ещё ${list.length-shown.length}</p>`;
    return h;
  }

  function describeLevel(L){
    const {rows, R} = state.res;
    let h = `<h3>${esc(L.name)}<span class="tag">${L.kind==='main'?'зачётный':L.kind==='bonus'?'бонусная заглушка':L.kind==='through'?'сквозной':'скрыт'}</span></h3>`;
    if(L.kind==='through'){
      h += '<div class="mini-scroll"><table class="mini"><thead><tr><th>Команда</th><th>Кодов</th><th>Бонусных</th><th>Бонус</th></tr></thead><tbody>';
      for(const r of rows){ const c=r.cells[L.name]; h += `<tr><td>${esc(r.team)}</td><td class="mono">${c?c.nc:0}</td><td class="mono">${c?c.nb:0}</td><td class="mono">${c?fmtHM(c.bonusSec):''}</td></tr>`; }
      return h+'</tbody></table></div>';
    }
    const avg = L.average;
    h += '<div class="kv">';
    h += kv('Лимит', fmtMin(L.dur)+' мин') + kv('Подсказки', fmtMin(L.h1)+' / '+fmtMin(L.h2)+' мин') + kv('Добавочное', fmtMin(L.add)+' мин');
    h += kv('Выполнили', L.completed+' из '+rows.length);
    h += kv('Среднее время', avg===null?'—':fmtHMS(avg));
    if(L.reqCodes) h += kv('Кодов для закрытия', L.reqCodes);
    h += '</div>';
    h += `<div class="expl">Источник лимита: <b>${esc(L.durSrc==='manual'?'задан вручную':L.durSrcText||'')}</b>. Среднее время — по командам, закрывшим уровень кодом или по решению организатора (п. 7.30.1, без «средних» и без не выполнивших).</div>`;
    const hasSpoiler = rows.some(r=>r.cells[L.name] && r.cells[L.name].rec && r.cells[L.name].rec.spoilers.size);
    h += '<div class="mini-scroll"><table class="mini"><thead><tr><th>Команда</th><th>Выдан</th>'+(hasSpoiler?'<th>Спойлер</th>':'')+'<th>Закрыт</th><th>Время</th><th>Итог</th></tr></thead><tbody>';
    const sorted = rows.map(r=>({team:r.team, c:r.cells[L.name]})).sort((a,b)=>(a.c&&a.c.rec&&a.c.rec.issued||Infinity)-(b.c&&b.c.rec&&b.c.rec.issued||Infinity));
    for(const x of sorted){
      const c = x.c; if(!c) continue;
      const ok = /^h/.test(c.st);
      const rec = c.rec;
      const sp = hasSpoiler ? `<td class="mono">${rec && rec.spoilerAt!==null ? fmtClock(rec.spoilerAt)+' · '+esc([...rec.spoilers.values()][0].raw) : '—'}</td>` : '';
      h += `<tr><td>${esc(x.team)}</td><td class="mono">${c.issued!=null?fmtClock(c.issued):'—'}</td>${sp}<td class="mono">${c.end?fmtClock(c.end):'—'}</td><td class="mono">${c.show?fmtHMS(c.sec):''}</td><td><span class="pill ${ok?'ok':c.st==='none'?'mid':'bad'}">${esc(HOW_TEXT[c.how]||'')}</span></td></tr>`;
    }
    return h+'</tbody></table></div>';
  }

  function describeTeam(team){
    const {rows, R} = state.res;
    const row = rows.find(r=>r.team===team);
    const tr = state.game.recs.get(team);
    let h = `<h3>${esc(team)}<span class="tag">место ${row?row.place:'—'}</span></h3>`;
    if(row){
      h += '<div class="kv">';
      h += kv('Чистое время', fmtHMS(row.clean)) + kv('Добавочное', fmtHM(row.add)) + kv('Штраф', fmtHM(row.penalty));
      h += kv('Бонусы сквозных', fmtHM(row.thr)) + kv('Бонусы уровней', fmtHM(row.lvlBonus)) + kv('Приквел', state.game.prequel.has(team) ? (row.prequel? fmtHM(row.prequel) : 'выполнен') : 'нет');
      h += kv('Общее время', fmtHMS(row.total));
      h += '</div>';
    }
    const chain = tr ? [...tr.values()].filter(r=>r.order).sort((a,b)=>a.order-b.order) : [];
    h += '<div class="subhead">Маршрут команды</div><div class="mini-scroll"><table class="mini"><thead><tr><th>#</th><th>Уровень</th><th>Выдан</th><th>Спойлер</th><th>Закрыт</th><th>Время</th><th>Итог</th></tr></thead><tbody>';
    for(const r of chain){
      const L = R.levels.find(x=>x.name===r.level);
      const c = row && row.cells[r.level];
      const ok = c && /^h/.test(c.st);
      h += `<tr><td class="mono">${r.order}</td><td>${esc(r.level)}${L&&L.kind==='bonus'?' <span class="pill mid">заглушка</span>':''}</td><td class="mono">${fmtClock(r.issued)}</td><td class="mono">${r.spoilerAt!==null?fmtClock(r.spoilerAt)+' · '+esc([...r.spoilers.values()][0].raw):''}</td><td class="mono">${c&&c.end?fmtClock(c.end):'—'}</td><td class="mono">${c&&c.show?fmtHMS(c.sec):''}</td><td>${c?`<span class="pill ${ok?'ok':'bad'}">${esc(HOW_TEXT[c.how]||'')}</span>`:''}</td></tr>`;
    }
    const plan = state.game.plan.get(team) || [];
    const missing = plan.filter(l=>!chain.some(r=>r.level===l));
    h += '</tbody></table></div>';
    if(missing.length) h += `<p class="subnote">Не получены по плану выдачи: ${missing.map(esc).join(', ')}.</p>`;
    return h;
  }

  function renderDetails(){
    const el = $('#stats-details');
    const s = state.sel;
    if(!s || !state.res){ el.className='card details empty'; el.innerHTML='Выберите клетку таблицы — здесь появится, из каких событий журнала получено время.'; return; }
    el.className = 'card details';
    const L = state.res.R.levels.find(x=>x.name===s.level);
    if(s.type==='cell'){
      const row = state.res.rows.find(r=>r.team===s.team);
      const c = row && row.cells[s.level];
      if(!row || !c || !L){ state.sel=null; return renderDetails(); }
      el.innerHTML = describeCell(s.team, L, c);
    } else if(s.type==='level'){
      if(!L){ state.sel=null; return renderDetails(); }
      el.innerHTML = describeLevel(L);
    } else if(s.type==='team'){
      el.innerHTML = describeTeam(s.team);
    }
  }

  function renderNotes(){
    const G = state.game, {R, rows} = state.res;
    const notes = [];
    const add = (sev, html)=>notes.push({sev, html});
    add('info', `Игра: <b>${fmtDT(G.startAt)}</b> — <b>${fmtDT(R.stopAt)}</b> (стоп-игра: ${R.stopAt===G.endAt ? esc(G.endSrc) : 'задана вручную'}). Команд: <b>${G.teams.length}</b>, уровней: <b>${R.levels.filter(l=>l.kind==='main').length}</b> зачётных, ${R.levels.filter(l=>l.kind==='bonus').length} бонусных, ${R.levels.filter(l=>l.kind==='through').length} сквозных.`);
    if(G.cutAt!==null) add('info', `До ${fmtDT(G.cutAt)} в журнале «удалена статистика» — более ранние игровые события (тестовые прогоны) не учитываются.`);

    const credits=[], avgs=[], bad=[], refuses=[], implicit=[], stops=[], lastDone=[], rejected=[], virtualIss=[];
    for(const row of rows){
      for(const L of R.levels){
        const c = row.cells[L.name]; if(!c || !c.rec) {
          const r0 = G.recs.get(row.team)?.get(L.name);
          if(r0 && r0.override && r0.issued===null) bad.push(`${esc(row.team)} — ${esc(L.name)}: «${r0.override.kind==='credit'?'засчитан уровень':'засчитано среднее время'}» на ${fmtDT(r0.override.at)}, но команда этот уровень не получала`);
          continue;
        }
        const r = c.rec;
        if(r.override && r.issued!==null && r.override.at<r.issued) bad.push(`${esc(row.team)} — ${esc(L.name)}: время зачёта ${fmtDT(r.override.at)} раньше выдачи уровня ${fmtDT(r.issued)}`);
        if(c.virtualIssue) virtualIss.push(`${esc(row.team)} — ${esc(L.name)}: засчитан на ${fmtDT(r.override.at)}, выдан при стоп-игре ${fmtClock(c.issued)} → ${fmtHMS(c.sec)}`);
        if(c.lastDone && r.timeoutAt!==null) lastDone.push(`${esc(row.team)} — ${esc(L.name)} (${fmtHMS(c.sec)})`);
        if(r.rejected && r.spoilerAt!==null) rejected.push(`${esc(row.team)} — ${esc(L.name)}: «${esc(r.rejected.raw)}» отклонён ${r.rejected.count>1?r.rejected.count+' раз, впервые ':''}в ${fmtClock(r.rejected.t)}, принят только в ${fmtClock(r.spoilerAt)}${c.rejectedFix?' — время посчитано по первому вводу ('+fmtHMS(c.sec)+')':''}`);
        if(r.override && r.issued===null && !c.virtualIssue) bad.push(`${esc(row.team)} — ${esc(L.name)}: «${r.override.kind==='credit'?'засчитан уровень':'засчитано среднее время'}» на ${fmtDT(r.override.at)}, но команда этот уровень не получала`);
        if(c.how==='credit') credits.push(L.name);
        if(c.how==='avg') avgs.push(`${esc(row.team)} — ${esc(L.name)} (${fmtHMS(c.sec)})`);
        if(r.refusedAt!==null) refuses.push(`${esc(row.team)} — ${esc(L.name)} в ${fmtClock(r.refusedAt)}${c.how==='avg'?' (потом засчитано среднее)':''}`);
        if(c.implicit && L.kind==='main') implicit.push(`${esc(row.team)} — ${esc(L.name)} (лимит истёк в ${fmtClock(c.end)})`);
        if(c.how==='stop' && L.kind==='main') stops.push(`${esc(row.team)} — ${esc(L.name)}`);
      }
    }
    if(bad.length) add('warn', `<b>Зачёты без основания — проигнорированы:</b> ${bad.join('; ')}.`);
    if(credits.length){
      const byLv = {}; credits.forEach(l=>byLv[l]=(byLv[l]||0)+1);
      add('info', `Организатор вручную засчитал уровни по указанному времени (событие «засчитан уровень»): ${Object.keys(byLv).map(l=>`<b>${esc(l)}</b> ×${byLv[l]}`).join(', ')}. Время этих уровней считается до указанного момента, а не до выдачи следующего.`);
    }
    if(avgs.length) add('info', `Засчитано среднее время: ${avgs.join('; ')}.`);
    if(refuses.length) add('info', `Отказ от уровня: ${refuses.join('; ')}.`);
    if(implicit.length) add('warn', `Нет записи «вышло время», хотя лимит истёк до стоп-игры (команда перестала заходить в движок) — уровень считается невыполненным: ${implicit.join('; ')}. Проверьте лимит этих уровней.`);
    if(stops.length) add('info', `Не выполнено до стоп-игры: ${stops.join('; ')}.`);
    if(rejected.length) add('warn', `<b>Движок отклонял верный ответ спойлера</b> (тот же ответ потом принят как правильный — похоже на ошибку в движке): ${rejected.join('; ')}.`);
    if(lastDone.length) add('info', `Последний уровень пройден кодами до истечения лимита; записи «вышло время» после этого — таймер уже пройденного уровня, их не учитываю: ${lastDone.join('; ')}.`);
    if(virtualIss.length) add('info', `Засчитан уровень, которого нет в журнале выдач (команда перестала заходить в движок, уровень выдан при завершении игры): ${virtualIss.join('; ')}.`);
    if(state.cfg.preset) add('info', `<b>Для этой игры применены параметры, подобранные под официальную статистику:</b> ${esc(state.cfg.preset)} Их можно изменить в «Параметрах подсчёта».`);

    const est = R.levels.filter(L=>(L.kind==='main'||L.kind==='bonus') && L.durSrc!=='manual' && L.durSrc && !/^timeout$/.test(L.durSrc));
    if(est.length) add('warn', `Таймаутов на этих уровнях не было, лимит восстановлен косвенно — сверьте с заданиями: ${est.map(L=>`<b>${esc(L.name)}</b> — ${fmtMin(L.dur)} мин`).join(', ')}. От лимита зависят цвет подсказок, время невыполненного уровня и добавочное; почему выбрано такое значение — в «Параметрах подсчёта».`);

    const thrCodes = rows.some(r=>R.levels.some(L=>L.kind==='through' && r.cells[L.name] && (r.cells[L.name].nc||r.cells[L.name].nb)));
    const thrSet = R.levels.some(L=>L.kind==='through' && (L.done>0||L.perCode>0||L.perBonus>0||L.codeVals.size>0));
    const lvlBonusLv = R.levels.filter(L=>L.kind!=='through' && G.teams.some(t=>G.recs.get(t)?.get(L.name)?.bonus.size));
    const lvlSet = lvlBonusLv.some(L=>L.perBonus>0 || L.codeVals.size>0);
    if((thrCodes && !thrSet) || (lvlBonusLv.length && !lvlSet)){
      add('warn', `<b>Размер бонусов в журнале не записывается.</b> ${thrCodes&&!thrSet?'Коды сквозных уровней приняты, но минуты за них не заданы. ':''}${lvlBonusLv.length&&!lvlSet?'Бонусные коды есть на уровнях: '+lvlBonusLv.map(L=>esc(L.name)).join(', ')+'. ':''}Укажите минуты в «Параметрах подсчёта», иначе столбец «Бонусы» будет нулевым.`);
    }
    if(G.prequel.size && !R.prequelMin) add('info', `Приквел выполнили: ${[...G.prequel].map(esc).join(', ')}. Бонус за приквел задаётся в параметрах (сейчас 0).`);
    const noShow = [...G.plannedTeams].filter(t=>!G.teams.includes(t));
    if(noShow.length) add('info', `Запланированы, но не получили ни одного уровня (в таблицу не включены): ${noShow.map(esc).join(', ')}.`);

    $('#stats-notes').innerHTML = notes.map(n=>`<div class="card note ${n.sev}"><span class="dot"></span><div>${n.html}</div></div>`).join('');
  }

  // ---------- параметры ----------
  function renderSettings(){
    const G = state.game, cfg = state.cfg, R = state.res.R;
    const g = cfg.game;
    $('#stats-cfg-game').innerHTML = `
      <label class="field"><span>Стоп-игра</span><input type="datetime-local" step="1" data-g="stopAt" value="${esc(g.stopAt || toLocalInput(G.endAt))}"></label>
      <label class="field"><span>Лимит уровня по умолчанию, мин</span><input type="number" min="1" step="1" data-g="defaultDur" value="${esc(g.defaultDur ?? '')}" placeholder="90"></label>
      <label class="field"><span>Бонус за приквел, мин</span><input type="number" min="0" step="1" data-g="prequelMin" value="${esc(g.prequelMin ?? '')}" placeholder="0"></label>
      <label class="field"><span>Неполученные уровни</span><select data-g="noneMode">
        <option value="limit"${R.noneMode==='limit'?' selected':''}>полный лимит + добавочное</option>
        <option value="add"${R.noneMode==='add'?' selected':''}>только добавочное</option>
        <option value="ignore"${R.noneMode==='ignore'?' selected':''}>не учитывать</option></select></label>
      <label class="field"><span>Уровень, не законченный к стоп-игре</span><select data-g="stopMode">
        <option value="actual"${R.stopMode==='actual'?' selected':''}>фактическое время + добавочное</option>
        <option value="limit"${R.stopMode==='limit'?' selected':''}>полный лимит + добавочное</option></select></label>
      <label class="field"><span>Верный ответ спойлера, отклонённый движком</span><select data-g="rejectedMode">
        <option value="first"${R.rejectedMode==='first'?' selected':''}>считать по первому вводу</option>
        <option value="ignore"${R.rejectedMode==='ignore'?' selected':''}>не учитывать</option></select></label>`;

    let h = '<thead><tr><th>Уровень</th><th>Тип</th><th>Лимит</th><th>1-я подск.</th><th>2-я подск.</th><th>Добавочное</th><th title="бонус за выполнение уровня (для сквозного — когда собраны все коды)">За выполнение</th><th title="для сквозных: минуты за каждый принятый код">За код</th><th>За бонусный код</th><th title="особая стоимость отдельных бонусных кодов, например: 4541DR2=5; 7D72R3=5">Особые коды</th><th title="вычитается из бонуса за каждую запрошенную подсказку">− за подсказку</th><th title="0 — без ограничения">Макс. бонус</th><th>Откуда лимит</th></tr></thead><tbody>';
    for(const L of R.levels){
      const o = cfg.levels[L.name] || {};
      const thr = L.kind==='through';
      const inp = (f, ph, dis)=>`<input type="number" min="0" step="any" data-lv="${esc(L.name)}" data-f="${f}" value="${esc(o[f] ?? '')}" placeholder="${esc(dis?'—':ph)}"${dis?' disabled':''}>`;
      const srcCls = L.durSrc==='timeout' || L.durSrc==='manual' ? 'src' : 'src est';
      h += `<tr><td class="lvname">${esc(L.name)}</td>
        <td><select data-lv="${esc(L.name)}" data-f="kind">
          <option value="main"${L.kind==='main'?' selected':''}>зачётный</option>
          <option value="bonus"${L.kind==='bonus'?' selected':''}>бонусный (не в зачёт)</option>
          <option value="through"${L.kind==='through'?' selected':''}>сквозной</option>
          <option value="hidden"${L.kind==='hidden'?' selected':''}>скрыть</option></select></td>
        <td>${inp('dur', fmtMin(L.autoDur).replace(',','.'), thr)}</td>
        <td>${inp('h1', fmtMin(L.h1).replace(',','.'), thr)}</td>
        <td>${inp('h2', fmtMin(L.h2).replace(',','.'), thr)}</td>
        <td>${inp('add', fmtMin(L.add).replace(',','.'), thr)}</td>
        <td>${inp('done', '0', false)}</td>
        <td>${inp('perCode', '0', !thr)}</td>
        <td>${inp('perBonus', '0', false)}</td>
        <td><input type="text" data-lv="${esc(L.name)}" data-f="codeVals" value="${esc(o.codeVals ?? '')}" placeholder="КОД=мин; …" style="width:150px"></td>
        <td>${inp('hintPen', '0', false)}</td>
        <td>${inp('cap', '0', false)}</td>
        <td class="${thr?'src':srcCls}">${thr ? 'сквозной — лимит не нужен' : esc(L.durSrc==='manual' ? 'задан вручную (авто: '+fmtMin(L.autoDur)+' мин)' : L.durSrcText||'')}</td></tr>`;
    }
    $('#stats-cfg-levels').innerHTML = h + '</tbody>';

    let t = '<thead><tr><th>Команда</th><th>Приквел</th><th>Штраф, мин</th><th>Бонус, мин</th><th>Скрыть</th></tr></thead><tbody>';
    for(const team of [...G.teams].sort((a,b)=>a.localeCompare(b,'ru'))){
      const o = cfg.teams[team] || {};
      t += `<tr><td class="lvname">${esc(team)}</td><td>${G.prequel.has(team)?'<span class="pill ok">выполнен</span>':'<span class="pill mid">нет</span>'}</td>
        <td><input type="number" min="0" step="any" data-team="${esc(team)}" data-f="penalty" value="${esc(o.penalty ?? '')}" placeholder="0"></td>
        <td><input type="number" min="0" step="any" data-team="${esc(team)}" data-f="bonus" value="${esc(o.bonus ?? '')}" placeholder="0"></td>
        <td><input type="checkbox" data-team="${esc(team)}" data-f="hidden"${o.hidden?' checked':''}></td></tr>`;
    }
    $('#stats-cfg-teams').innerHTML = t + '</tbody>';
  }

  // Обновить только плейсхолдеры/источники, не перерисовывая поля (чтобы не терять фокус)
  function refreshSettingsHints(){
    const R = state.res.R;
    root.querySelectorAll('#stats-cfg-levels input[data-lv]').forEach(inp=>{
      const L = R.levels.find(x=>x.name===inp.dataset.lv); if(!L) return;
      const f = inp.dataset.f, thr = L.kind==='through';
      const ph = {dur:L.autoDur, h1:L.autoH1, h2:L.autoH2, add:L.autoAdd}[f];
      if(ph!==undefined){ inp.disabled = thr; inp.placeholder = thr ? '—' : fmtMin(ph).replace(',','.'); }
      if(f==='perCode'){ inp.disabled = !thr; inp.placeholder = thr ? '0' : '—'; }
    });
  }

  function saveCfg(){
    try{ if(state.storeKey) localStorage.setItem(state.storeKey, JSON.stringify(state.cfg)); }catch(e){}
  }
  // The browser renders server results. Every parameter change is calculated in Go.
  let requestVersion = 0;
  let pendingController = null;
  function hydrate(report){
    const record = r => {
      for(const k of ['codes','bonus','spoilers','spoilerBad'])
        r[k] = new Map(Object.entries(r[k] || {}).sort((a,b)=>a[1].t-b[1].t));
      return r;
    };
    const g = report.game;
    g.recs = new Map(Object.entries(g.recs).map(([t,rs])=>[t,new Map(Object.entries(rs).map(([l,r])=>[l,record(r)]))]));
    g.plan = new Map(Object.entries(g.plan));
    g.prequel = new Set(g.prequel); g.plannedTeams = new Set(g.plannedTeams);
    for(const l of report.res.R.levels) l.codeVals = new Map(Object.entries(l.codeVals));
    for(const row of report.res.rows) for(const c of Object.values(row.cells)) if(c.rec) record(c.rec);
    return report;
  }
  async function requestReport(file, cfg, format, signal){
    const body = new FormData(); body.append('file', file);
    if(cfg) body.append('cfg', JSON.stringify(cfg));
    const response = await fetch('/api/v1/log-stat'+(format?'?format='+format:''), {method:'POST', body, signal});
    if(!response.ok){
      const error = await response.json().catch(()=>({error:'Не удалось рассчитать статистику.'}));
      throw new Error(error.error);
    }
    return format ? response.blob() : response.json();
  }
  async function recompute(opts={}){
    const version = ++requestVersion;
    pendingController?.abort(); pendingController = new AbortController();
    $('#stats-btn-export').disabled = true;
    try{
      const report = hydrate(await requestReport(state.file, state.cfg, '', pendingController.signal));
      if(version!==requestVersion) return;
      state.game = report.game; state.res = report.res;
      $('#stats-error').style.display = 'none';
      renderBoard(); renderNotes(); renderDetails();
      if(opts.settings) renderSettings(); else refreshSettingsHints();
    }catch(err){ if(err.name!=='AbortError') showError(err.message || String(err)); }
    finally{ if(version===requestVersion) $('#stats-btn-export').disabled = false; }
  }
  function showError(msg){
    const el = $('#stats-error'); el.textContent = msg; el.style.display = 'block';
  }

  // ---------------------------------------------------------------
  // 7. ЗАГРУЗКА ФАЙЛОВ
  // ---------------------------------------------------------------
  async function readFile(file){
    const version = ++requestVersion;
    pendingController?.abort(); pendingController = new AbortController();
    root.querySelectorAll('[data-needs-log]').forEach(el => el.disabled = true);
    try{
      let report = await requestReport(file, null, '', pendingController.signal);
      if(version!==requestVersion) return;
      const defaults = structuredClone(report.cfg);
      let saved;
      try{ saved = JSON.parse(localStorage.getItem(report.key)); }catch(e){}
      if(saved) report = await requestReport(file, saved, '', pendingController.signal);
      if(version!==requestVersion) return;
      hydrate(report);
      state.file = file; state.source = {name:file.name}; state.storeKey = report.key;
      state.defaultCfg = defaults; state.cfg = report.cfg; state.game = report.game; state.res = report.res; state.sel = null;
      $('#stats-file-name').textContent = file.name;
      $('#stats-file-meta').textContent = report.game.nRows.toLocaleString('ru-RU')+' строк · '+report.game.teams.length+' команд · '+fmtDT(report.game.startAt)+' — '+fmtDT(report.game.endAt);
      $('#stats-error').style.display = 'none';
      renderBoard(); renderNotes(); renderDetails(); renderSettings();
    }catch(err){ if(err.name!=='AbortError') showError(err.message || String(err)); }
    finally{ if(version===requestVersion) root.querySelectorAll('[data-needs-log]').forEach(el => el.disabled = !state.res); }
  }

  async function exportXlsx(){
    if(!state.file) return;
    $('#stats-btn-export').disabled = true;
    try{
      const blob = await requestReport(state.file, state.cfg, 'xlsx');
      const a = document.createElement('a'); a.href = URL.createObjectURL(blob);
      a.download = state.file.name.replace(/\.[^.]+$/, '')+'-статистика.xlsx'; a.click();
      setTimeout(()=>URL.revokeObjectURL(a.href),2000);
    }catch(err){ showError(err.message || String(err)); }
    finally{ $('#stats-btn-export').disabled = false; }
  }

  // ---------------------------------------------------------------
  // 9. СОБЫТИЯ
  // ---------------------------------------------------------------
  state.showLevels = true; state.showStubs = true;

  $('#stats-btn-upload').addEventListener('click', ()=>$('#stats-file-input').click());
  $('#stats-file-input').addEventListener('change', e=>{
    const f = e.target.files[0]; if(!f) return;
    readFile(f).catch(err=>showError(err.message||String(err)));
    e.target.value='';
  });
  $('#stats-btn-export').addEventListener('click', exportXlsx);
  $('#stats-toggle-levels').addEventListener('click', e=>{
    state.showLevels = !state.showLevels;
    e.target.textContent = state.showLevels ? 'скрыть результаты уровней' : 'показать результаты уровней';
    $('#stats-board').classList.toggle('hide-levels', !state.showLevels);
  });
  $('#stats-toggle-stubs').addEventListener('click', e=>{
    state.showStubs = !state.showStubs;
    e.target.textContent = state.showStubs ? 'скрыть бонусные уровни' : 'показать бонусные уровни';
    $('#stats-board').classList.toggle('hide-stubs', !state.showStubs);
  });

  $('#stats-board').addEventListener('click', e=>{
    const td = e.target.closest('td.cell');
    const th = e.target.closest('th[data-lvh]');
    const tb = e.target.closest('button[data-teamb]');
    if(td){
      const team = td.closest('tr').dataset.team, level = td.dataset.lv;
      state.sel = {type:'cell', team, level};
    } else if(th){
      state.sel = {type:'level', level:th.dataset.lvh};
    } else if(tb){
      state.sel = {type:'team', team:tb.dataset.teamb};
    } else return;
    renderBoard(); renderDetails();
    if(window.innerWidth < 900) $('#stats-details').scrollIntoView({behavior:'smooth', block:'start'});
  });

  function onCfgChange(e){
    const t = e.target;
    if(!state.game) return;
    if(t.dataset.g){
      if(t.value==='' ) delete state.cfg.game[t.dataset.g]; else state.cfg.game[t.dataset.g] = t.value;
    } else if(t.dataset.lv){
      const o = state.cfg.levels[t.dataset.lv] = state.cfg.levels[t.dataset.lv] || {};
      if(t.value==='') delete o[t.dataset.f]; else o[t.dataset.f] = t.value;
    } else if(t.dataset.team){
      const o = state.cfg.teams[t.dataset.team] = state.cfg.teams[t.dataset.team] || {};
      if(t.type==='checkbox') o[t.dataset.f] = t.checked;
      else if(t.value==='') delete o[t.dataset.f]; else o[t.dataset.f] = t.value;
    } else return;
    saveCfg();
    recompute({settings: t.tagName==='SELECT' && t.dataset.f==='kind'});
  }
  $('#stats-settings').addEventListener('change', onCfgChange);
  $('#stats-btn-reset-cfg').addEventListener('click', ()=>{
    if(!state.game) return;
    state.cfg = structuredClone(state.defaultCfg);
    try{ localStorage.removeItem(state.storeKey); }catch(e){}
    recompute({settings:true});
  });

  // перетаскивание файла на страницу
  let dragDepth = 0;
  root.addEventListener('dragenter', e=>{ if([...(e.dataTransfer?.types||[])].includes('Files')){ dragDepth++; root.classList.add('dragging'); } });
  root.addEventListener('dragleave', ()=>{ dragDepth=Math.max(0,dragDepth-1); if(!dragDepth) root.classList.remove('dragging'); });
  root.addEventListener('dragover', e=>e.preventDefault());
  root.addEventListener('drop', e=>{
    e.stopPropagation();
    e.preventDefault(); dragDepth=0; root.classList.remove('dragging');
    const f = e.dataTransfer?.files?.[0];
    if(f) readFile(f).catch(err=>showError(err.message||String(err)));
  });

  if(root === document.body && new URLSearchParams(location.search).has('standalone')) $('#stats-app-link').hidden = true;
  const initialName = root === document.body ? new URLSearchParams(location.search).get('log') : null;
  if(initialName){
    fetch('/stats-input').then(async response => {
      if(!response.ok) throw new Error('Не удалось загрузить журнал, указанный при запуске.');
      await readFile(new File([await response.blob()], initialName));
    }).catch(err => showError(err.message || String(err)));
  }
 }

  let mounting;
  window.dzzzrStats = {
    mount(root){
      if(mounting) return mounting;
      root.setAttribute('aria-busy', 'true');
      mounting = (async () => {
        const response = await fetch('stats.html');
        if(!response.ok) throw new Error('Не удалось открыть статистику. Нажмите «Статистика», чтобы повторить.');
        const page = new DOMParser().parseFromString(await response.text(), 'text/html');
        const content = page.querySelector('.wrap');
        const drop = page.querySelector('.drop-hint');
        if(!content || !drop) throw new Error('Страница статистики повреждена.');
        content.querySelector('#stats-app-link')?.remove();
        root.replaceChildren(drop, content);
        initStats(root);
      })().catch(error => {
        mounting = null;
        root.textContent = error.message;
        throw error;
      }).finally(() => root.removeAttribute('aria-busy'));
      return mounting;
    },
  };
  document.addEventListener('DOMContentLoaded', () => {
    if(document.body.classList.contains('stats-app')) initStats(document.body);
  });
})();
