/* —— Мастер первого запуска ——
 * Подключается до app.js, как и llm.js, и пользуется их функциями только во
 * время работы. Потоки входа Polza.ai и ChatGPT общие с панелью настроек. */

const ONBOARDING_STEPS = ['welcome', 'llm', 'auth'];

const ONBOARDING_COPY = {
  welcome: { title: 'Настройка dzzzr', subtitle: 'Два шага до начала работы' },
  llm: { title: 'Подключение к модели', subtitle: 'Шаг 1 из 2 — как агент обращается к языковой модели' },
  auth: { title: 'Вход в Дозор', subtitle: 'Шаг 2 из 2 — игрок или организатор' },
};

/** В мастере плашки «задано снаружи» лежат в одном слоте на шаг, поэтому каждая
 * называет своё поле — в модалке слот отдельный на поле, и подпись не нужна. */
const LLM_OVERRIDE_FIELD_RU = {
  auth_method: 'способ подключения',
  base_url: 'base URL',
  model: 'модель',
  api_key: 'API-ключ',
};

let onboardingLastFocus = null;

function isOnboardingOpen() {
  const overlay = $('onboarding');
  return !!overlay && !overlay.hidden;
}

// Панель настроек (llm.js) узнаёт о мастере только через эти крючки.
llmPanelHooks.onboardingOpen = isOnboardingOpen;
llmPanelHooks.refreshOnboarding = (data, opts) => fillOnboardingLLM(data, opts);
llmPanelHooks.selectTab['onboarding-llm'] = (method) => selectOnboardingLLMTab(method);

/** Ошибка мастера живёт в подвале диалога, а не в тосте: мастер модальный, и
 * тост за ним легко пропустить. Пустой текст прячет плашку. Ссылка собирается
 * через DOM: escapeHtml не экранирует кавычки, а значение попало бы в href. */
function setOnboardingError(text, linkURL) {
  const box = $('onboarding-error');
  if (!box) return;
  box.textContent = '';
  const msg = String(text || '').trim();
  box.hidden = !msg;
  if (!msg) return;
  const p = document.createElement('p');
  p.append(msg);
  if (/^https?:\/\//i.test(String(linkURL || ''))) {
    p.append(' ');
    const a = document.createElement('a');
    a.href = linkURL;
    a.target = '_blank';
    a.rel = 'noopener';
    a.textContent = 'Открыть страницу проверки';
    p.append(a);
  }
  box.appendChild(p);
}

function renderOverridesInto(slotId, list, flags, labels) {
  const slot = $(slotId);
  if (!slot) return;
  slot.textContent = '';
  for (const o of Array.isArray(list) ? list : []) {
    slot.append(...overrideBadgeNodes(o, flags, labels[o.field] || o.field));
  }
}

function onboardingStepIndex(step) {
  const idx = ONBOARDING_STEPS.indexOf(step);
  return idx < 0 ? 0 : idx;
}

/** Рисует всё, что не зависит от данных шага: индикатор, панели, заголовки и
 * кнопки подвала. Состояние шага в индикаторе — ровно два класса. */
function renderOnboardingChrome() {
  const step = state.onboarding.step;
  const idx = onboardingStepIndex(step);
  const busy = state.onboarding.busy;

  for (const li of document.querySelectorAll('#onboarding-steps li[data-step]')) {
    const at = onboardingStepIndex(li.dataset.step);
    li.classList.toggle('is-active', at === idx);
    li.classList.toggle('is-done', at < idx);
  }
  for (const name of ONBOARDING_STEPS) {
    const pane = $(`onboarding-pane-${name}`);
    if (pane) pane.hidden = name !== step;
  }

  const copy = ONBOARDING_COPY[step] || ONBOARDING_COPY.welcome;
  const title = $('onboarding-title');
  if (title) title.textContent = copy.title;
  const subtitle = $('onboarding-subtitle');
  if (subtitle) subtitle.textContent = copy.subtitle;

  const back = $('btn-onboarding-back');
  if (back) back.disabled = busy || idx === 0;
  const next = $('btn-onboarding-next');
  if (next) {
    next.disabled = busy;
    next.type = step === 'auth' ? 'submit' : 'button';
    if (step === 'auth') next.setAttribute('form', 'onboarding-auth-form');
    else next.removeAttribute('form');
  }
  const nextLabel = $('onboarding-next-label');
  if (nextLabel) nextLabel.textContent = idx === ONBOARDING_STEPS.length - 1 ? (busy ? 'Проверяем…' : 'Войти и завершить') : 'Далее';
}

function setOnboardingBusy(busy) {
  state.onboarding.busy = !!busy;
  renderOnboardingChrome();
}

async function goToOnboardingStep(step) {
  state.onboarding.step = step;
  setOnboardingError('');
  renderOnboardingChrome();
  $(`onboarding-pane-${step}`)?.focus?.();
  if (step === 'llm') {
    renderOnboardingLLMTabs();
    await loadOnboardingLLM();
  } else if (step === 'auth') {
    await prefillOnboardingAuth();
  }
}

/* —— Шаг «Модель» —— */

function onboardingLLMTab() {
  return state.onboarding.llmAuth;
}

function renderOnboardingLLMTabs() {
  const method = onboardingLLMTab();
  for (const kind of ['polza', 'codex', 'apikey']) {
    setTabState($(`onboarding-llm-tab-${kind}`), method === kind, false);
    const pane = $(`onboarding-llm-pane-${kind}`);
    if (pane) pane.hidden = method !== kind;
  }
  if (method === 'polza') void loadPolzaModels();
  const test = $('btn-onboarding-llm-test');
  if (test) test.textContent = method === 'polza' ? 'Проверить баланс' : 'Проверить настройки';
}

function selectOnboardingLLMTab(method) {
  if (polzaFlow && method !== 'polza') void cancelPolzaLogin();
  state.onboarding.llmAuth = method;
  renderOnboardingLLMTabs();
  $(`onboarding-llm-pane-${method}`)?.focus?.();
}

function onboardingKeyHint(keyStatus) {
  const c = keyStatus || {};
  if (!c.has_value) {
    return 'Ключ пока не сохранён. Подойдёт любой провайдер с OpenAI-совместимым API — ключ остаётся на этой машине.';
  }
  const masked = String(c.masked || '').trim() || '••••';
  if (c.source === 'env') {
    return `Ключ ${masked} задан переменной ${c.env_var || 'окружения'} — поле ниже его не перебьёт.`;
  }
  return `Ключ ${masked} уже сохранён. Пустое поле его не затирает.`;
}

/** Поля заполняются сохранённым (stored), как и модалка настроек: мастер
 * сохраняет ровно то, что в полях, и не должен вписывать в файл значение,
 * пришедшее из окружения или из умолчания, — иначе оно окажется прибитым
 * навсегда и переживёт и контейнер, и смену умолчания. Действующее значение
 * остаётся видимым как подсказка поля: пустое поле показывает то, что будет
 * использовано, и пустым же уходит на сервер — выбор возвращается окружению. */
function fillOnboardingLLM(data, opts = {}) {
  const effective = data?.effective || {};
  const stored = data?.stored || {};
  const defaults = data?.defaults || {};
  const keyStatus = effective.api_key || {};
  if (!opts.keepEdits) {
    state.onboarding.llmAuth = polzaInitialMethod(data);
  }

  const base = $('onboarding-llm-base-url');
  if (base) {
    const shown = String(effective.base_url?.value || defaults.base_url || '').trim();
    if (shown) base.placeholder = shown;
    if (!opts.keepEdits) base.value = String(stored.base_url || '');
  }
  const model = $('onboarding-llm-model');
  if (model) {
    const shown = String(effective.model?.value || defaults.model || '').trim();
    if (shown) model.placeholder = shown;
    if (!opts.keepEdits) model.value = String(stored.model || '');
  }
  const key = $('onboarding-llm-api-key');
  if (key) {
    if (!opts.keepEdits) key.value = '';
    key.placeholder = keyStatus.has_value ? String(keyStatus.masked || '••••') : 'sk-…';
  }
  const hint = $('onboarding-llm-hint');
  if (hint) hint.textContent = onboardingKeyHint(keyStatus);

  renderOverridesInto('onboarding-llm-overrides', data?.env_overrides, LLM_OVERRIDE_FLAGS, LLM_OVERRIDE_FIELD_RU);
  renderOnboardingLLMTabs();
  // Файл настроек не читается — шаг сохранит его заново; сказать об этом сразу,
  // а не после «Далее».
  if (data?.error) {
    setOnboardingResult('onboarding-llm-result', `${data.error}. Сохранение на этом шаге заменит файл.`, 'err');
  }
}

async function loadOnboardingLLM() {
  try {
    const data = await api('/llm/settings');
    // Снимок один на приложение: applyLLMSnapshot заодно рисует статус входа
    // через ChatGPT — и в модалке, и здесь.
    applyLLMSnapshot(data);
    fillOnboardingLLM(data);
  } catch (e) {
    setOnboardingError(`Настройки LLM: ${e.message || String(e)}`);
  }
}

/** Шаг обязан оставить настройку записанной, а не только показанной, поэтому
 * переход дальше начинается с сохранения. Для вкладки подписки поля
 * OpenAI-провайдера очищаются: подписка их не использует, а сохранённая модель
 * провайдера подменила бы модель подписки. */
async function saveOnboardingLLM() {
  if (onboardingLLMTab() === 'polza') return connectPolza('onboarding-llm');
  const isCodex = onboardingLLMTab() === 'codex';
  try {
    const data = await api('/llm/settings', {
      method: 'PUT',
      body: {
        auth_method: isCodex ? 'codex' : 'apikey',
        base_url: isCodex ? '' : ($('onboarding-llm-base-url')?.value || '').trim(),
        model: isCodex ? '' : ($('onboarding-llm-model')?.value || '').trim(),
        api_key: isCodex ? '' : ($('onboarding-llm-api-key')?.value || '').trim(),
        clear_api_key: false,
      },
    });
    applyLLMSnapshot(data);
    fillOnboardingLLM(data);
    void loadAgentConfig();
    return true;
  } catch (e) {
    setOnboardingError(e.message || String(e));
    return false;
  }
}

/** Проверка ничего не сохраняет: она показывает, что резолвится из уже
 * сохранённого и окружения, поэтому введённое, но не сохранённое сюда не
 * попадёт — и поля не затираются, чтобы ввод не пропал. */
async function testOnboardingLLM() {
  if (onboardingLLMTab() === 'polza') { await checkPolza('onboarding-llm'); return; }
  setOnboardingResult('onboarding-llm-result', 'Проверяем сохранённые настройки…', '');
  try {
    const data = await api('/llm/settings');
    applyLLMSnapshot(data, { keepEdits: true });
    const agent = data?.agent || {};
    const err = String(agent.error || '').trim();
    if (err) {
      setOnboardingResult('onboarding-llm-result', err, 'err');
      return;
    }
    const where = String(agent.base_url || '').trim() || 'адрес по умолчанию';
    const model = String(agent.model || '').trim() || '—';
    setOnboardingResult(
      'onboarding-llm-result',
      `Настройки определены: ${transportLabel(agent.auth_method)} · модель ${model} · ${where}. Запрос к модели не выполнялся.`,
      'ok',
    );
  } catch (e) {
    setOnboardingResult('onboarding-llm-result', e.message || String(e), 'err');
  }
}

/* —— Шаг «Вход» —— */

/** Открыт ли редактор: организатору, пришедшему за редактором, не нужен вход игрока. */
function isEditorView() {
  return document.body?.dataset?.mode === 'editor' || String(location.hash || '').startsWith('#/editor');
}

function defaultOnboardingRole(isEditor) {
  return isEditor ? 'organizer' : 'player';
}

/** Город один на запуск (-city), поэтому он только показывается. Роль
 * выбирается заново при каждом входе на шаг, если пользователь её не менял. */
async function prefillOnboardingAuth() {
  if (!state.onboarding.role) state.onboarding.role = defaultOnboardingRole(isEditorView());
  const radio = $(`onboarding-auth-role-${state.onboarding.role}`);
  if (radio) radio.checked = true;
  const city = $('onboarding-auth-city');
  if (!city) return;
  try {
    const status = state.auth || (await api('/auth/status'));
    city.textContent = String(status?.city || '—');
  } catch {
    city.textContent = '—';
  }
}

async function onOnboardingAuthSubmit(ev) {
  ev.preventDefault();
  if (state.onboarding.busy) return;
  const form = $('onboarding-auth-form');
  if (!form.reportValidity()) return;
  const fd = new FormData(form);
  const role = String(fd.get('role') || '').trim();
  const login = String(fd.get('login') || '').trim();
  const password = String(fd.get('password') || '');
  if (role !== 'player' && role !== 'organizer') {
    setOnboardingError('Выберите роль: игрок или организатор.');
    return;
  }
  if (!login || !password) {
    setOnboardingError('Заполните логин и пароль.');
    return;
  }
  await finishOnboarding({ role, login, password });
}

/* —— Переходы, открытие и завершение —— */

async function onboardingNext() {
  if (state.onboarding.busy) return;
  const step = state.onboarding.step;
  if (step === 'auth') {
    $('onboarding-auth-form')?.requestSubmit();
    return;
  }
  setOnboardingError('');
  setOnboardingBusy(true);
  try {
    if (step === 'llm' && !((await saveOnboardingLLM()) && (await onboardingLLMReady()))) return;
  } finally {
    setOnboardingBusy(false);
  }
  await goToOnboardingStep(ONBOARDING_STEPS[onboardingStepIndex(step) + 1]);
}

/** Сохранить мало: на шаге «Модель» подписка может быть выбрана без входа, а
 * ключ — не задан. Дальше пускает только сервер, когда агент получит модель;
 * причину отказа он называет сам. */
async function onboardingLLMReady() {
  try {
    const status = await api('/onboarding');
    state.onboarding.status = status;
    const step = (status?.steps || []).find((s) => s.id === 'llm');
    if (step?.done) return true;
    setOnboardingError(`Модель ещё не настроена: ${step?.detail || 'проверьте настройки'}. Можно нажать «Настроить позже».`);
  } catch (e) {
    setOnboardingError(e.message || String(e));
  }
  return false;
}

/** Редактору модель не нужна: организатор может отложить её настройку. */
async function skipOnboardingLLM() {
  if (state.onboarding.busy) return;
  await goToOnboardingStep('auth');
}

async function onboardingBack() {
  if (state.onboarding.busy) return;
  const idx = onboardingStepIndex(state.onboarding.step);
  if (idx === 0) return;
  await goToOnboardingStep(ONBOARDING_STEPS[idx - 1]);
}

function onOnboardingKeydown(e) {
  if (!isOnboardingOpen()) return;
  if (e.key === 'Escape') {
    // Первый запуск нельзя закрыть до успешного входа.
    if (state.onboarding.status?.required) return;
    e.preventDefault();
    e.stopPropagation();
    closeOnboarding();
    return;
  }
  if (e.key === 'Tab') {
    e.stopPropagation();
    trapModalFocus(e, $('onboarding')?.querySelector('.onboarding-dialog'));
  }
}

async function openOnboarding() {
  const overlay = $('onboarding');
  if (!overlay || !overlay.hidden) return;
  onboardingLastFocus = document.activeElement;
  overlay.hidden = false;
  document.body.classList.add('is-modal-open');
  document.addEventListener('keydown', onOnboardingKeydown, true);
  for (const id of ['onboarding-llm-result', 'onboarding-auth-status']) {
    setOnboardingResult(id, '', '');
  }
  await goToOnboardingStep('welcome');
}

function closeOnboarding() {
  const overlay = $('onboarding');
  if (!overlay || overlay.hidden) return;
  overlay.hidden = true;
  // Модалка настроек LLM могла открыться поверх: прокрутку возвращает тот, кто
  // закрывается последним.
  if (!isModalOpen()) document.body.classList.remove('is-modal-open');
  document.removeEventListener('keydown', onOnboardingKeydown, true);
  setOnboardingError('');
  onboardingLastFocus?.focus?.();
  onboardingLastFocus = null;
}

async function finishOnboarding(credentials) {
  setOnboardingError('');
  setOnboardingBusy(true);
  try {
    state.onboarding.status = await api('/onboarding/complete', {
      method: 'POST',
      body: credentials,
    });
  } catch (e) {
    setOnboardingError(e.message || String(e));
    return;
  } finally {
    setOnboardingBusy(false);
  }
  const pwd = $('onboarding-auth-password');
  if (pwd) pwd.value = '';
  closeOnboarding();
  await afterOnboarding();
}

/** После входа страница должна показать новое состояние: игрока в шапке чата,
 * организатора в редакторе, модель в сайдбаре. */
async function afterOnboarding() {
  await Promise.allSettled([
    loadAuthStatus(),
    loadAgentConfig(),
    window.dzzzrEditor?.refreshAdmin?.(),
  ]);
}

/** Статус мастера на старте. Ни сеть, ни битый файл состояния не должны запирать
 * приложение: мастер просто не открывается, а консоль остаётся рабочей. */
async function initOnboarding() {
  try {
    const status = await api('/onboarding');
    state.onboarding.status = status;
    if (status?.required) await openOnboarding();
  } catch (e) {
    toast(`Мастер настройки: ${e.message || String(e)}`, true);
  }
}

function bindOnboarding() {
  $('btn-onboarding-open')?.addEventListener('click', () => void openOnboarding());
  $('btn-onboarding-next')?.addEventListener('click', () => {
    if (state.onboarding.step !== 'auth') void onboardingNext();
  });
  $('btn-onboarding-back')?.addEventListener('click', () => void onboardingBack());
  $('onboarding-llm-tab-codex')?.addEventListener('click', () => selectOnboardingLLMTab('codex'));
  $('onboarding-llm-tab-apikey')?.addEventListener('click', () => selectOnboardingLLMTab('apikey'));
  $('btn-onboarding-llm-test')?.addEventListener('click', () => void testOnboardingLLM());
  $('btn-onboarding-llm-skip')?.addEventListener('click', () => void skipOnboardingLLM());
  for (const role of ['player', 'organizer']) {
    $(`onboarding-auth-role-${role}`)?.addEventListener('change', () => {
      state.onboarding.role = role;
    });
  }
  $('btn-onboarding-codex-login')?.addEventListener('click', () => void startCodexLogin(false));
  $('btn-onboarding-codex-code-submit')?.addEventListener('click', () => void submitCodexCode());
  $('onboarding-codex-code')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void submitCodexCode();
    }
  });
  $('onboarding-auth-form')?.addEventListener('submit', (e) => void onOnboardingAuthSubmit(e));
}
