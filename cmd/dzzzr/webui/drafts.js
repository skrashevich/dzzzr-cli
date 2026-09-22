'use strict';

// One active document, backed by the server. Navigation awaits flush(); typing
// also keeps a recovery copy before the debounce so a reload cannot lose it.
window.createDraftSync = function ({ api, read, render, status, changed }) {
  let draft = null;
  let doc = null;
  let base = {};
  let timer;
  let pending = Promise.resolve();
  let stopped = false;
  const clone = (x) => structuredClone(x);
  const canonical = (value) => {
    if (Array.isArray(value)) return value.map(canonical);
    if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonical(value[key])]));
    return value;
  };
  const equal = (a, b) => JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));
  const key = () => `dzzzr-draft-recovery:${draft.id}:${doc.id}`;
  const diff = (before, after) => {
    const patch = {};
    for (const field of new Set([...Object.keys(before), ...Object.keys(after)])) {
      if (!equal(before[field], after[field])) patch[field] = after[field] ?? null;
    }
    return patch;
  };
  const remember = () => {
    if (!doc) return;
    sessionStorage.setItem(key(), JSON.stringify({ base, params: read() }));
  };
  function merge(before, local, remote) {
    const result = clone(remote);
    for (const field of new Set([...Object.keys(before), ...Object.keys(local)])) {
      if (equal(before[field], local[field])) continue;
      if (!equal(before[field], remote[field]) && !equal(local[field], remote[field])) {
        const mine = JSON.stringify(local[field] ?? 'пусто');
        const theirs = JSON.stringify(remote[field] ?? 'пусто');
        if (!window.confirm(`Поле «${field}» изменено одновременно.\n\nВаш вариант: ${mine}\n\nОбщий черновик: ${theirs}\n\nОК — оставить ваш вариант; Отмена — принять общий.`)) continue;
      }
      if (local[field] === undefined) delete result[field];
      else result[field] = clone(local[field]);
    }
    return result;
  }
  function notify() {
    changed(draft, doc);
    const dirty = !equal(doc.base, read());
    status(dirty ? 'Черновик сохранён · есть правки для игры' : 'Совпадает с сохранённой игрой');
  }
  async function sync() {
    if (!doc || stopped) return;
    for (let attempt = 0; attempt < 8; attempt++) {
      remember();
      const latest = await api(`/admin/drafts/${draft.id}`);
      const remote = latest.documents[doc.id];
      if (!remote) throw new Error('Документ черновика не найден');
      let local = read();
      if (remote.revision !== doc.revision) {
        local = merge(base, local, remote.params);
        draft = latest; doc = remote; base = clone(remote.params);
        render(local);
      } else draft = latest;
      const patch = diff(base, local);
      if (!Object.keys(patch).length) {
        sessionStorage.removeItem(key());
        notify();
        return;
      }
      status('Сохраняю черновик…');
      try {
        const saved = await api(`/admin/drafts/${draft.id}/documents/${doc.id}`, {
          method: 'PATCH', body: { revision: doc.revision, params: patch },
        });
        // Fields typed while the request was in flight still belong to the user.
        draft = saved; doc = saved.documents[doc.id]; base = clone(doc.params);
        if (equal(local, read())) {
          sessionStorage.removeItem(key());
          notify();
          return;
        }
      } catch (e) {
        if (e.status !== 409) throw e;
      }
    }
    throw new Error('Черновик меняется одновременно. Повторите синхронизацию.');
  }
  function flush() {
    clearTimeout(timer);
    pending = pending.catch(() => {}).then(sync).catch((e) => {
      status(`Черновик не синхронизирован: ${e.message}`);
      throw e;
    });
    return pending;
  }
  function schedule() {
    if (!doc) return;
    try { remember(); } catch (e) { status(`Не удалось сохранить резервную копию: ${e.message}`); }
    status('Есть изменения · сохраняю черновик…');
    clearTimeout(timer);
    timer = setTimeout(() => void flush().catch(() => {}), 450);
  }
  async function attach(data, documentID) {
    clearTimeout(timer);
    stopped = false;
    draft = data; doc = data.documents[documentID ?? data.active];
    base = clone(doc.params);
    let params = clone(base);
    const recovery = sessionStorage.getItem(key());
    if (recovery) {
      const saved = JSON.parse(recovery);
      params = merge(saved.base, saved.params, base);
    }
    render(params);
    notify();
    if (!equal(params, base)) schedule();
  }
  async function publish() {
    await flush();
    stopped = true;
    try {
      const saved = await api(`/admin/drafts/${draft.id}/documents/${doc.id}/publish`, {
        method: 'POST', body: { revision: doc.revision },
      });
      await attach(saved, doc.id);
      return saved;
    } finally { stopped = false; }
  }
  return {
    schedule, flush, attach, publish,
    async pause() { stopped = true; await pending.catch(() => {}); },
    get draft() { return draft; }, get doc() { return doc; },
    async detach() { await flush(); draft = null; doc = null; base = {}; },
  };
};
