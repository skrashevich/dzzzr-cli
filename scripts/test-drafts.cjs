const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../cmd/dzzzr/webui/drafts.js'), 'utf8');
function harness() {
  let params = { title: 'initial', question: 'text' };
  let server = { id: 'draft', documents: { doc: { id: 'doc', revision: 1, params: structuredClone(params), base: structuredClone(params) } }, active: 'doc' };
  let conflicts = [], status, beforePatch;
  const storage = new Map();
  const context = { structuredClone, setTimeout, clearTimeout, window: { confirm(text) { conflicts.push(text); return true; } }, sessionStorage: { getItem: k => storage.get(k), setItem: (k,v) => storage.set(k,v), removeItem: k => storage.delete(k) } };
  vm.runInNewContext(source, context);
  const sync = context.window.createDraftSync({
    read: () => structuredClone(params), render: p => { params = structuredClone(p); }, changed() {}, status: text => status = text,
    async api(url, opts) {
      if (opts?.method === 'PATCH') {
        if (beforePatch) { const fn=beforePatch;beforePatch=null;fn(); }
        if (server.documents.doc.revision !== opts.body.revision) throw Object.assign(new Error('conflict'),{status:409});
        Object.assign(server.documents.doc.params, opts.body.params);server.documents.doc.revision++;
      }
      return structuredClone(server);
    },
  });
  return { sync, server, conflicts, storage, edit(p) { Object.assign(params,p); }, get params() { return params; }, get status() { return status; }, beforePatch(fn) { beforePatch=fn; } };
}
test('different fields merge; same field requires an explicit choice',async()=>{
 const h=harness();await h.sync.attach(structuredClone(h.server));
 h.edit({title:'mine'});h.server.documents.doc.params.question='agent';h.server.documents.doc.revision++;
 await h.sync.flush();assert.equal(h.params.title,'mine');assert.equal(h.params.question,'agent');assert.equal(h.conflicts.length,0);
 h.edit({title:'mine again'});h.server.documents.doc.params.title='theirs';h.server.documents.doc.revision++;
 await h.sync.flush();assert.equal(h.conflicts.length,1);assert.match(h.conflicts[0],/mine again/);assert.equal(h.server.documents.doc.params.title,'mine again');
});
test('keeps edits typed while saving and retries a revision conflict',async()=>{
 const h=harness();await h.sync.attach(structuredClone(h.server));h.edit({title:'first'});
 h.beforePatch(()=>{h.edit({title:'second'});h.server.documents.doc.params.question='remote';h.server.documents.doc.revision++;});
 await h.sync.flush();assert.equal(h.server.documents.doc.params.title,'second');assert.equal(h.params.question,'remote');
});
test('recovers changes written before debounce after reload',async()=>{
 const h=harness();await h.sync.attach(structuredClone(h.server));h.edit({title:'recover'});h.sync.schedule();
 await h.sync.attach(structuredClone(h.server));await h.sync.flush();assert.equal(h.params.title,'recover');assert.equal(h.server.documents.doc.params.title,'recover');
});
test('equivalent object key order does not mark a saved document dirty',async()=>{
 const h=harness();h.server.documents.doc.base={question:'text',title:'initial'};
 await h.sync.attach(structuredClone(h.server));await h.sync.flush();assert.equal(h.status,'Совпадает с сохранённой игрой');
});
