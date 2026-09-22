const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

// The three scripts share one global scope in the browser; the test loads them
// the same way, with app.js's boot() left out.
function setup() {
  const context = vm.createContext({
    console, setTimeout, clearTimeout, setInterval, clearInterval,
    window: {},
    location: { hash: '' },
    localStorage: { getItem: () => null, setItem() {} },
    requestAnimationFrame: () => 0,
    document: {
      documentElement: { dataset: {} },
      body: { dataset: {}, classList: { add() {}, remove() {} } },
      getElementById: () => null,
      querySelectorAll: () => [],
      addEventListener() {},
    },
  });
  const dir = path.join(__dirname, '../webui');
  for (const name of ['llm.js', 'onboarding.js', 'app.js']) {
    let source = fs.readFileSync(path.join(dir, name), 'utf8');
    if (name === 'app.js') source = source.replace(/boot\(\);\s*$/, '');
    vm.runInContext(source, context, { filename: name });
  }
  vm.runInContext(`
    var closed = false, error = '', calls = 0, valid = true, refreshed = [];
    var fields = {role: 'organizer', login: 'org', password: 'secret'};
    var form = {reportValidity: () => valid, requestSubmit: () => { calls++; }};
    document.getElementById = id => id === 'onboarding-auth-form' ? form : null;
    FormData = class { get(key) { return fields[key]; } };
    setOnboardingBusy = busy => { state.onboarding.busy = busy; };
    setOnboardingError = message => { error = message; };
    closeOnboarding = () => { closed = true; };
    afterOnboarding = async () => { refreshed.push('done'); };
    var requests = [];
    api = async (url, options) => { requests.push({url, options}); return {completed: true}; };
    state.onboarding.step = 'auth';
  `, context);
  return code => vm.runInContext(code, context);
}

test('auth next submits the required form instead of completing directly', async () => {
  const run = setup();
  await run('onboardingNext()');
  assert.equal(run('calls'), 1);
  assert.equal(run('requests.length'), 0);
});

test('invalid form cannot finish onboarding', async () => {
  const run = setup();
  run('valid = false');
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('requests.length'), 0);
  assert.equal(run('closed'), false);
});

test('organizer role posts role to complete and closes the wizard', async () => {
  const run = setup();
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('requests[0].url'), '/onboarding/complete');
  assert.deepEqual(JSON.parse(JSON.stringify(run('requests[0].options.body'))), {role: 'organizer', login: 'org', password: 'secret'});
  assert.equal(run('closed'), true);
  assert.deepEqual(JSON.parse(JSON.stringify(run('refreshed'))), ['done']);
});

test('missing role is refused before any request', async () => {
  const run = setup();
  run("fields.role = ''");
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('requests.length'), 0);
  assert.match(run('error'), /роль/i);
});

test('refused login keeps the wizard open with the server message', async () => {
  const run = setup();
  run("api = async () => { const e = new Error('неверный логин или пароль'); e.status = 401; throw e; }");
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('closed'), false);
  assert.equal(run('error'), 'неверный логин или пароль');
});

test('editor view preselects organizer, chat view player', () => {
  const run = setup();
  assert.equal(run('defaultOnboardingRole(true)'), 'organizer');
  assert.equal(run('defaultOnboardingRole(false)'), 'player');
});

test('model step does not advance until the model step is done', async () => {
  const run = setup();
  run(`
    state.onboarding.step = 'llm';
    saveOnboardingLLM = async () => true;
    goToOnboardingStep = async (step) => { state.onboarding.step = step; };
    api = async (url) => { requests.push({url}); return {steps: [{id: 'llm', done: false, detail: 'не задан ключ модели'}, {id: 'auth', done: false}]}; };
  `);
  await run('onboardingNext()');
  assert.equal(run('state.onboarding.step'), 'llm');
  assert.match(run('error'), /не задан ключ модели/);
  run("api = async () => ({steps: [{id: 'llm', done: true}, {id: 'auth', done: false}]})");
  await run('onboardingNext()');
  assert.equal(run('state.onboarding.step'), 'auth');
});

test('a failed Polza catalog is fetched again only on an explicit retry', async () => {
  const run = setup();
  run("var catalogCalls = 0; api = async (url) => { if (url === '/llm/polza/models') catalogCalls++; throw new Error('offline'); };");
  await run('loadPolzaModels()');
  await run('loadPolzaModels()');
  await run('loadPolzaModels()');
  assert.equal(run('catalogCalls'), 1);
  await run('loadPolzaModels(true)');
  assert.equal(run('catalogCalls'), 2);
});

test('boot still opens the wizard when an earlier step fails', async () => {
  const run = setup();
  run(`
    var opened = 0;
    initOnboarding = async () => { opened++; };
    bindUI = () => { throw new Error('boom'); };
  `);
  await run('boot().catch(() => {})');
  assert.equal(run('opened'), 1);
});

test('the model step shows a settings file the server could not read', () => {
  const run = setup();
  run(`
    var results = {};
    setOnboardingResult = (id, text, tone) => { results[id] = {text, tone}; };
    renderOverridesInto = () => {};
    renderOnboardingLLMTabs = () => {};
  `);
  run("fillOnboardingLLM({error: 'не удалось разобрать настройки LLM /x/settings.json'})");
  assert.match(run("results['onboarding-llm-result'].text"), /settings\.json/);
  assert.equal(run("results['onboarding-llm-result'].tone"), 'err');
});
