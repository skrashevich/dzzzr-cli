/* —— Настройки LLM —— */

/* Панель «Настройки LLM» и общие для неё и мастера первого запуска потоки
 * входа (Polza.ai, ChatGPT). Файл подключается до app.js и опирается на его
 * функции ($, api, toast, escapeHtml, loadAgentConfig) и поля state только во
 * время работы, не при загрузке. Мастер (onboarding.js) подключается через
 * llmPanelHooks — без него панель работает сама по себе. */

const llmPanelHooks = {
  /** Мастер открыт поверх страницы (тогда он, а не панель, держит прокрутку). */
  onboardingOpen: () => false,
  /** Перерисовать шаг «Модель» мастера по свежему снимку настроек. */
  refreshOnboarding: null,
  /** Выбор вкладки провайдера по префиксу площадки: 'llm' | 'onboarding-llm'. */
  selectTab: { llm: (method) => selectLLMTab(method) },
};

/** Флагов командной строки для LLM в dzzzr нет: всё, что задано снаружи, — переменные окружения. */
const LLM_OVERRIDE_FLAGS = {};

const LLM_OVERRIDE_FIELDS = ['auth_method', 'base_url', 'model', 'api_key'];

const LLM_TRANSPORT_RU = {
  codex: 'подписка ChatGPT',
  apikey: 'OpenAI-совместимый провайдер',
};

/** Опрос статуса входа через ChatGPT: раз в 2 с, не дольше пяти минут — столько
 * же живёт поток на стороне сервера (codexLoginTTL). */
const CODEX_POLL_MS = 2000;
const CODEX_LOGIN_TTL_MS = 5 * 60 * 1000;

/** Вход через ChatGPT нарисован дважды: в модалке настроек и в мастере первого
 * запуска. Оба набора элементов всегда лежат в DOM, виден только один, а поток
 * входа на всё приложение один — поэтому каждая отрисовка обновляет обе
 * площадки, вместо второго поллера и второго state.codexFlow. */
const CODEX_TARGETS = [
  { status: 'llm-codex-status', progress: 'llm-codex-progress', manual: 'llm-codex-manual', code: 'llm-codex-code', login: 'btn-codex-login' },
  {
    status: 'onboarding-codex-status',
    progress: 'onboarding-codex-progress',
    manual: 'onboarding-codex-manual',
    code: 'onboarding-codex-code',
    login: 'btn-onboarding-codex-login',
  },
];

let llmLastFocus = null;

function transportLabel(method) {
  return LLM_TRANSPORT_RU[method] || LLM_TRANSPORT_RU.apikey;
}

/** Вкладка, открытая при загрузке: сохранённое значение важнее действующего,
 * чтобы форма показывала то, что она же и перезапишет. Пустое сохранённое
 * значение остаётся пустым — при сохранении оно не превратится в «apikey». */
/* Polza keeps its inputs separate from custom providers, including unsaved keys. */
const POLZA_BASE_URL = 'https://polza.ai/api/v1';
const POLZA_PANELS = ['llm', 'onboarding-llm'];
let polzaCatalog = null;
let polzaCatalogLoading = false;
/** Каталог не загрузился: повторно — только по кнопке, а не на каждой перерисовке. */
let polzaCatalogFailed = false;
let polzaFlow = null;
let polzaBusy = false;

function isPolzaURL(value) {
  return String(value || '').replace(/\/+$/, '') === POLZA_BASE_URL;
}

function polzaInitialMethod(data) {
  const method = initialAuthMethod(data) || String(data?.effective?.auth_method?.value || data?.agent?.auth_method || '');
  const base = data?.effective?.base_url?.value || data?.stored?.base_url || data?.agent?.base_url;
  if (method === 'apikey' && isPolzaURL(base)) return 'polza';
  const stored = data?.stored || {};
  const configured = stored.auth_method || stored.base_url || stored.model || stored.has_api_key ||
    data?.effective?.api_key?.has_value || data?.codex?.signed_in ||
    (data?.env_overrides || []).length || (method && !['codex', 'apikey'].includes(method));
  return configured ? (method || 'apikey') : 'polza';
}

/** Площадки Polza, которые есть в разметке: мастер может быть не подключён. */
function polzaPanels() {
  return POLZA_PANELS.filter((pre) => $(`${pre}-tab-polza`));
}

function polzaResult(pre, text, tone = '') {
  setOnboardingResult(`${pre}-polza-result`, text, tone);
}

/** Блок .onboarding-result скрыт, пока пуст, поэтому очистка — это пустой текст. */
function setOnboardingResult(id, text, tone) {
  const box = $(id);
  if (!box) return;
  box.className = 'onboarding-result' + (tone ? ` is-${tone}` : '');
  box.textContent = String(text || '');
}

async function loadPolzaModels(retry = false) {
  if (polzaCatalogLoading || ((polzaCatalog || polzaCatalogFailed) && !retry)) return;
  polzaCatalogLoading = true;
  try {
    const data = await api('/llm/polza/models');
    if (!data.models?.length) throw new Error('Нет доступных моделей с поддержкой инструментов.');
    polzaCatalog = data;
    polzaCatalogFailed = false;
    for (const pre of POLZA_PANELS) {
      const select = $(`${pre}-polza-model`);
      if (!select) continue;
      const selected = select.value || (polzaInitialMethod(state.llm) === 'polza' ? state.llm?.stored?.model : '') || data.default_model;
      select.replaceChildren(...data.models.map((m) => new Option(m.name || m.id, m.id)));
      select.value = data.models.some(m => m.id === selected) ? selected : data.default_model;
      if (!select.value) select.selectedIndex = 0;
      select.disabled = false;
      $(`${pre}-polza-models-retry`).hidden = true;
    }
  } catch (e) {
    polzaCatalogFailed = true;
    for (const pre of polzaPanels()) {
      polzaResult(pre, `Модели: ${e.message || String(e)}`, 'err');
      $(`${pre}-polza-models-retry`).hidden = false;
    }
  } finally { polzaCatalogLoading = false; }
}

function polzaModel(pre) {
  const model = $(`${pre}-polza-model`)?.value || '';
  if (!model) throw new Error('Дождитесь загрузки и выберите модель.');
  return model;
}

function showPolzaBalance(pre, data) {
  const available = String(data.available ?? '—');
  const amount = String(data.amount ?? '—');
  const exhausted = Number(available) <= 0;
  polzaResult(pre, `Баланс: ${amount} ₽ · Доступно: ${available} ₽.${exhausted ? ' Пополните баланс или проверьте лимит ключа.' : ''}${data.warning ? ` ${data.warning}` : ''}`, exhausted ? '' : 'ok');
}

function setPolzaBusy(value) {
  polzaBusy = value;
  for (const pre of polzaPanels()) {
    for (const action of ['login', 'connect', 'check']) $(`${pre}-polza-${action}`).disabled = value || !!polzaFlow;
    $(`${pre}-polza-cancel`).hidden = !polzaFlow || polzaFlow.pre !== pre;
  }
}

async function refreshPolzaSettings(flow = null) {
  const data = await api('/llm/settings');
  if (flow && polzaFlow !== flow) return false;
  applyLLMSnapshot(data);
  llmPanelHooks.refreshOnboarding?.(data);
  for (const pre of polzaPanels()) {
    $(`${pre}-polza-key`).value = '';
    const select = $(`${pre}-polza-model`);
    if (polzaCatalog?.models.some(m => m.id === data?.stored?.model)) select.value = data.stored.model;
  }
  void loadAgentConfig();
  return true;
}

async function connectPolza(pre) {
  if (polzaBusy || polzaFlow) { polzaResult(pre, 'Сначала завершите или отмените текущее подключение.'); return false; }
  setPolzaBusy(true);
  try {
    const data = await api('/llm/polza/connect', { method: 'POST', body: {
      api_key: ($(`${pre}-polza-key`)?.value || '').trim(), model: polzaModel(pre),
    } });
    await refreshPolzaSettings();
    showPolzaBalance(pre, data);
    return true;
  } catch (e) { polzaResult(pre, e.message || String(e), 'err'); return false; }
  finally { setPolzaBusy(false); }
}

async function checkPolza(pre) {
  if (polzaBusy || polzaFlow) return;
  setPolzaBusy(true);
  polzaResult(pre, 'Проверяем сохранённое подключение и баланс…');
  try { showPolzaBalance(pre, await api('/llm/polza/check', { method: 'POST', body: {} })); }
  catch (e) { polzaResult(pre, e.message || String(e), 'err'); }
  finally { setPolzaBusy(false); }
}

async function cancelPolzaLogin() {
  const flow = polzaFlow;
  if (!flow) return;
  polzaFlow = null;
  clearTimeout(flow.timer);
  flow.popup?.close();
  $(`${flow.pre}-polza-authorize`).hidden = true;
  $(`${flow.pre}-polza-callback-details`).hidden = true;
  setPolzaBusy(false);
  polzaResult(flow.pre, 'Подключение отменено.');
  if (flow.id) {
    try { await api(`/llm/polza/login/${encodeURIComponent(flow.id)}`, { method: 'DELETE' }); }
    catch (e) { polzaResult(flow.pre, `Не удалось подтвердить отмену на сервере: ${e.message || e}`, 'err'); }
  }
}

async function pollPolzaLogin(flow) {
  if (polzaFlow !== flow) return;
  try {
    if (Date.now() > flow.deadline) throw new Error('Время ожидания истекло. Подключите аккаунт ещё раз.');
    const data = await api(`/llm/polza/login/${encodeURIComponent(flow.id)}`);
    if (polzaFlow !== flow) return;
    if (data.status === 'success') {
      if (!(await refreshPolzaSettings(flow))) return;
      polzaFlow = null;
      flow.popup?.close();
      $(`${flow.pre}-polza-authorize`).hidden = true;
      $(`${flow.pre}-polza-callback-details`).hidden = true;
      setPolzaBusy(false);
      await checkPolza(flow.pre);
      return;
    }
    if (data.status !== 'pending') throw new Error(data.error || 'Не удалось подключить аккаунт.');
    flow.timer = setTimeout(() => void pollPolzaLogin(flow), 1500);
  } catch (e) {
    if (polzaFlow !== flow) return;
    await cancelPolzaLogin();
    polzaResult(flow.pre, e.message || String(e), 'err');
  }
}

async function startPolzaLogin(pre) {
  if (polzaFlow || polzaBusy) return;
  let model;
  try { model = polzaModel(pre); } catch (e) { polzaResult(pre, e.message, 'err'); return; }
  // Open before awaiting the server so browser popup rules retain user activation.
  const popup = window.open('about:blank', '_blank');
  if (popup) popup.opener = null;
  const flow = { pre, popup, id: '', deadline: Date.now() + 10 * 60 * 1000 };
  polzaFlow = flow;
  setPolzaBusy(false);
  polzaResult(pre, 'Ожидаем вход в Polza.ai…');
  try {
    const data = await api('/llm/polza/login', { method: 'POST', body: { model } });
    if (polzaFlow !== flow) {
      await api(`/llm/polza/login/${encodeURIComponent(data.id)}`, { method: 'DELETE' });
      return;
    }
    flow.id = data.id;
    const url = new URL(data.authorize_url);
    if (url.protocol !== 'https:') throw new Error('Сервер вернул небезопасный адрес входа.');
    const link = $(`${pre}-polza-authorize`);
    link.href = url.href;
    link.hidden = false;
    $(`${pre}-polza-callback-details`).hidden = false;
    if (popup && !popup.closed) popup.location.href = url.href;
    void pollPolzaLogin(flow);
  } catch (e) {
    if (polzaFlow !== flow) return;
    await cancelPolzaLogin();
    polzaResult(pre, e.message || String(e), 'err');
  }
}

async function submitPolzaCode(pre) {
  const flow = polzaFlow;
  if (!flow?.id || flow.pre !== pre) return;
  try {
    await api(`/llm/polza/login/${encodeURIComponent(flow.id)}/code`, { method: 'POST', body: { code: ($(`${pre}-polza-code`).value || '').trim() } });
    $(`${pre}-polza-code`).value = '';
    // The existing poller is the single completion path.
  } catch (e) { if (polzaFlow === flow) polzaResult(pre, e.message || String(e), 'err'); }
}

function bindPolza() {
  for (const pre of polzaPanels()) {
    const selectTab = (method) => llmPanelHooks.selectTab[pre]?.(method);
    $(`${pre}-tab-polza`).addEventListener('click', () => selectTab('polza'));
    const methods = ['polza', 'codex', 'apikey'];
    for (const [index, method] of methods.entries()) {
      $(`${pre}-tab-${method}`).addEventListener('keydown', e => {
        if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
        e.preventDefault();
        const next = e.key === 'Home' ? 0 : e.key === 'End' ? 2 : (index + (e.key === 'ArrowRight' ? 1 : 2)) % 3;
        selectTab(methods[next]);
        $(`${pre}-tab-${methods[next]}`).focus();
      });
    }
    $(`${pre}-polza-login`).addEventListener('click', () => void startPolzaLogin(pre));
    $(`${pre}-polza-cancel`).addEventListener('click', () => void cancelPolzaLogin());
    $(`${pre}-polza-connect`).addEventListener('click', () => void connectPolza(pre));
    $(`${pre}-polza-check`).addEventListener('click', () => void checkPolza(pre));
    $(`${pre}-polza-submit`).addEventListener('click', () => void submitPolzaCode(pre));
    $(`${pre}-polza-models-retry`).addEventListener('click', () => void loadPolzaModels(true));
    $(`${pre}-polza-code`).addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); void submitPolzaCode(pre); } });
  }
}

function initialAuthMethod(data) {
  const stored = String(data?.stored?.auth_method || '').trim();
  if (stored) return stored;
  const effective = String(data?.effective?.auth_method?.value || '').trim();
  if (effective === 'codex' || effective === 'apikey') return effective;
  const resolved = String(data?.agent?.auth_method || '').trim();
  return resolved === 'codex' || resolved === 'apikey' ? resolved : '';
}

function isModalOpen() {
  const modal = $('llm-modal');
  return !!modal && !modal.hidden;
}

/** Виден ли элемент пользователю. У скрытого поддерева нет прямоугольников — чем
 * бы его ни прятали: атрибутом hidden, display:none или закрытым <details>. */
function isElementVisible(el) {
  return !!el && el.getClientRects().length > 0;
}

/** Видимые фокусируемые элементы контейнера. Ловушка фокуса нужна и модалке
 * настроек LLM, и мастеру первого запуска, поэтому контейнер приходит
 * аргументом, а не прибит к одному диалогу. */
function modalFocusable(container) {
  if (!container) return [];
  const sel =
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])';
  return [...container.querySelectorAll(sel)].filter(isElementVisible);
}

function trapModalFocus(e, container) {
  const items = modalFocusable(container);
  if (!items.length) return;
  const first = items[0];
  const last = items[items.length - 1];
  const active = document.activeElement;
  if (e.shiftKey && (active === first || !items.includes(active))) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && active === last) {
    e.preventDefault();
    first.focus();
  }
}

function onLLMModalKeydown(e) {
  if (!isModalOpen()) return;
  if (e.key === 'Escape') {
    e.preventDefault();
    e.stopPropagation();
    closeLLMModal();
    return;
  }
  if (e.key === 'Tab') {
    e.stopPropagation();
    trapModalFocus(e, $('llm-modal-dialog'));
  }
}

async function openLLMModal() {
  const modal = $('llm-modal');
  if (!modal || !modal.hidden) return;
  llmLastFocus = document.activeElement;
  modal.hidden = false;
  document.body.classList.add('is-modal-open');
  document.addEventListener('keydown', onLLMModalKeydown, true);
  renderLLMSettings();
  try {
    const data = await api('/llm/settings');
    applyLLMSnapshot(data);
  } catch (e) {
    toast(`Настройки LLM: ${e.message || String(e)}`, true);
  }
  const items = modalFocusable($('llm-modal-dialog'));
  (items[0] || modal).focus?.();
}

function closeLLMModal() {
  const modal = $('llm-modal');
  if (!modal || modal.hidden) return;
  modal.hidden = true;
  // Мастер первого запуска мог остаться открытым под модалкой: прокрутку
  // возвращает тот, кто закрывается последним.
  if (!llmPanelHooks.onboardingOpen()) document.body.classList.remove('is-modal-open');
  document.removeEventListener('keydown', onLLMModalKeydown, true);
  llmLastFocus?.focus?.();
  llmLastFocus = null;
}

/** Принимает свежий снимок настроек. keepEdits оставляет незасохранённый ввод —
 * его используют обновления, вызванные входом/выходом ChatGPT, а не формой. */
function applyLLMSnapshot(data, opts = {}) {
  state.llm = data || null;
  if (!opts.keepEdits) {
    state.llmEdited = { base_url: false, model: false };
    state.llmAuth = polzaInitialMethod(data);
    const key = $('llm-api-key');
    if (key) key.value = '';
  }
  renderLLMSettings();
}

function setTabState(btn, active, reachable) {
  if (!btn) return;
  btn.classList.toggle('is-active', active);
  btn.setAttribute('aria-selected', active ? 'true' : 'false');
  btn.tabIndex = active || reachable ? 0 : -1;
}

function selectLLMTab(method) {
  if (polzaFlow && method !== 'polza') void cancelPolzaLogin();
  state.llmAuth = method;
  renderLLMSettings();
  const pane = $(`llm-pane-${method}`);
  pane?.focus?.();
}

/** Узлы плашки «значение задано снаружи». Одна и та же лексика нужна и модалке
 * настроек (по слоту на поле, без подписи), и мастеру (общий слот, поэтому
 * каждая плашка называет своё поле). */
function overrideBadgeNodes(o, flags, fieldLabel) {
  const flag = flags[o.field];
  const source =
    o.source === 'flag'
      ? flag
        ? `флагом ${flag}`
        : 'флагом командной строки'
      : `переменной ${o.env_var || 'окружения'}`;
  const badge = document.createElement('span');
  badge.className = 'llm-override' + (o.shadows_stored ? ' is-shadowing' : '');
  badge.textContent = fieldLabel ? `${fieldLabel}: переопределено ${source}` : `переопределено ${source}`;
  const nodes = [badge];
  if (o.shadows_stored) {
    const note = document.createElement('span');
    note.className = 'llm-override-note';
    note.textContent = 'сохранённое здесь значение не применяется';
    nodes.push(note);
  }
  return nodes;
}

function renderLLMOverrides(list) {
  for (const field of LLM_OVERRIDE_FIELDS) {
    const slot = $(`llm-override-${field}`);
    if (slot) slot.innerHTML = '';
  }
  for (const o of Array.isArray(list) ? list : []) {
    const slot = $(`llm-override-${o.field}`);
    if (!slot) continue;
    slot.append(...overrideBadgeNodes(o, LLM_OVERRIDE_FLAGS, ''));
  }
}

function formatCodexExpiry(raw) {
  const ts = Date.parse(String(raw || ''));
  if (!Number.isFinite(ts)) return String(raw || '');
  return new Date(ts).toLocaleString('ru-RU');
}

function renderCodexStatus(codex) {
  const c = codex || {};
  const rows = [];
  let cls = 'llm-codex-status';
  let title = 'Не выполнен вход';
  if (c.signed_in) {
    cls += c.expired ? ' is-warn' : ' is-ok';
    title = c.expired ? 'Вход выполнен, срок действия истёк' : 'Вход выполнен';
    if (c.account_id) rows.push(`Аккаунт: ${c.account_id}`);
    if (c.expires_at) rows.push(`Действует до: ${formatCodexExpiry(c.expires_at)}`);
  } else if (c.cli_signed_in) {
    // Своего входа нет, но Codex CLI на этой машине вошёл — агент возьмёт его.
    cls += ' is-ok';
    title = 'Используется вход Codex CLI';
    rows.push('Отдельный вход через dzzzr не обязателен.');
    if (c.cli_path) rows.push(`Файл Codex CLI: ${c.cli_path}`);
  } else {
    cls += ' is-off';
  }
  if (c.error) {
    cls += ' is-warn';
    rows.push(`Ошибка: ${c.error}`);
  }
  if (c.path) rows.push(`Файл: ${c.path}`);
  const html = `<p class="llm-codex-title">${escapeHtml(title)}</p>${
    rows.length ? `<ul class="llm-codex-rows">${rows.map((r) => `<li>${escapeHtml(r)}</li>`).join('')}</ul>` : ''
  }`;
  for (const target of CODEX_TARGETS) {
    const box = $(target.status);
    if (!box) continue;
    box.className = cls;
    box.innerHTML = html;
  }
}

function renderLLMSettings() {
  const data = state.llm;
  const defaults = data?.defaults || {};
  const stored = data?.stored || {};
  const method = state.llmAuth;
  const isCodex = method === 'codex';
  const isOther = method !== '' && method !== 'codex' && method !== 'apikey' && method !== 'polza';

  setTabState($('llm-tab-codex'), isCodex, isOther);
  setTabState($('llm-tab-apikey'), method === 'apikey' || !method, isOther);
  setTabState($('llm-tab-polza'), method === 'polza', isOther);
  if ($('llm-pane-polza')) $('llm-pane-polza').hidden = method !== 'polza';
  if (method === 'polza') void loadPolzaModels();
  const codexPane = $('llm-pane-codex');
  const apikeyPane = $('llm-pane-apikey');
  if (codexPane) codexPane.hidden = !isCodex;
  if (apikeyPane) apikeyPane.hidden = isCodex || isOther || method === 'polza';

  const note = $('llm-transport-note');
  if (note) {
    note.hidden = !isOther;
    note.textContent = isOther
      ? `Сохранён транспорт «${method}» — он настраивается через DZZZR_LLM_PROVIDER. Выбор вкладки заменит его.`
      : '';
  }

  const baseInput = $('llm-base-url');
  if (baseInput) {
    baseInput.placeholder = defaults.base_url || '';
    if (!state.llmEdited.base_url) baseInput.value = stored.base_url || '';
  }
  const modelInput = $('llm-model');
  if (modelInput) {
    modelInput.placeholder = defaults.model || '';
    if (!state.llmEdited.model) modelInput.value = stored.model || '';
  }
  const keyInput = $('llm-api-key');
  const keyHint = $('llm-api-key-hint');
  if (keyInput) {
    keyInput.placeholder = stored.has_api_key ? stored.api_key_masked || '••••' : 'sk-…';
  }
  if (keyHint) {
    keyHint.textContent = stored.has_api_key
      ? 'Ключ сохранён. Пустое поле при сохранении его не затирает.'
      : 'Ключ не сохранён.';
  }
  const clearBtn = $('btn-llm-clear-key');
  if (clearBtn) clearBtn.disabled = !stored.has_api_key;

  renderLLMOverrides(data?.env_overrides);
  renderCodexStatus(data?.codex);

  const pending = !!state.codexFlow;
  for (const target of CODEX_TARGETS) {
    const loginBtn = $(target.login);
    if (loginBtn) loginBtn.disabled = pending;
  }
  const logoutBtn = $('btn-codex-logout');
  if (logoutBtn) logoutBtn.disabled = !data?.codex?.signed_in;
  const cancelBtn = $('btn-codex-cancel');
  if (cancelBtn) cancelBtn.hidden = !pending;

  const agent = data?.agent || {};
  const summary = $('llm-summary');
  if (summary) {
    const model = String(agent.model || '').trim() || '—';
    const where = String(agent.base_url || '').trim() || transportLabel(agent.auth_method || method);
    summary.textContent = `Сейчас используется: ${model} · ${where}`;
  }
  const pathEl = $('llm-settings-path');
  if (pathEl) {
    pathEl.textContent = data?.settings_path ? `Файл настроек: ${data.settings_path}` : '';
  }

  const alert = $('llm-alert');
  if (alert) {
    // Нечитаемый файл настроек ломает и чтение, и резолв конфига агента — один
    // и тот же текст приходит дважды, показывать его дважды незачем.
    const problems = [...new Set([data?.error, agent.error].map((x) => String(x || '').trim()).filter(Boolean))];
    alert.hidden = problems.length === 0;
    alert.innerHTML = problems.map((p) => `<p>${escapeHtml(p)}</p>`).join('');
  }
}

function setLLMBusy(busy) {
  for (const id of ['btn-llm-save', 'btn-llm-reset', 'btn-llm-clear-key']) {
    const el = $(id);
    if (el) el.disabled = busy;
  }
  if (!busy) renderLLMSettings();
}

function llmFormBody(extra) {
  return {
    auth_method: state.llmAuth,
    base_url: ($('llm-base-url')?.value || '').trim(),
    model: ($('llm-model')?.value || '').trim(),
    api_key: ($('llm-api-key')?.value || '').trim(),
    clear_api_key: false,
    ...extra,
  };
}

async function putLLMSettings(extra, successMsg) {
  setLLMBusy(true);
  try {
    const data = await api('/llm/settings', { method: 'PUT', body: llmFormBody(extra) });
    applyLLMSnapshot(data);
    toast(successMsg);
    void loadAgentConfig();
  } catch (e) {
    toast(e.message || String(e), true);
  } finally {
    setLLMBusy(false);
  }
}

async function saveLLMSettings() {
  if (state.llmAuth === 'polza') { await connectPolza('llm'); return; }
  await putLLMSettings(undefined, 'Настройки LLM сохранены.');
}

async function clearLLMAPIKey() {
  if (!window.confirm('Удалить сохранённый API-ключ?')) return;
  await putLLMSettings({ api_key: '', clear_api_key: true }, 'API-ключ удалён.');
}

async function resetLLMSettings() {
  if (!window.confirm('Сбросить все сохранённые настройки LLM?')) return;
  await cancelPolzaLogin();
  for (const pre of polzaPanels()) {
    $(`${pre}-polza-key`).value = '';
    polzaResult(pre, '');
  }
  setLLMBusy(true);
  try {
    const data = await api('/llm/settings', { method: 'DELETE' });
    applyLLMSnapshot(data);
    toast('Настройки LLM сброшены.');
    void loadAgentConfig();
  } catch (e) {
    toast(e.message || String(e), true);
  } finally {
    setLLMBusy(false);
  }
}

/** Ссылка собирается через DOM, а не через innerHTML: escapeHtml не экранирует
 * кавычки, а здесь значение попало бы в атрибут href. */
function setCodexProgress(text, linkURL) {
  const withLink = /^https?:\/\//i.test(String(linkURL || ''));
  for (const target of CODEX_TARGETS) {
    const el = $(target.progress);
    if (!el) continue;
    el.textContent = '';
    el.hidden = !text;
    if (!text) continue;
    el.append(text);
    if (!withLink) continue;
    el.append(' Если вкладка не открылась — ');
    const a = document.createElement('a');
    a.href = linkURL;
    a.target = '_blank';
    a.rel = 'noopener';
    a.textContent = 'откройте ссылку вручную';
    el.append(a, '.');
  }
}

function stopCodexPoll() {
  if (state.codexPoll) {
    clearInterval(state.codexPoll);
    state.codexPoll = null;
  }
}

async function refreshLLMAfterAuth() {
  try {
    const data = await api('/llm/settings');
    applyLLMSnapshot(data, { keepEdits: true });
    // Вход мог быть начат изнутри мастера: тогда его плашки и подсказки описывают
    // состояние до входа. Снимок уже здесь — второй запрос за тем же не нужен.
    if (llmPanelHooks.onboardingOpen()) llmPanelHooks.refreshOnboarding?.(data, { keepEdits: true });
  } catch (e) {
    toast(`Настройки LLM: ${e.message || String(e)}`, true);
  }
  void loadAgentConfig();
}

/** Успешный вход обязан ещё и записать транспорт: автовыбор подписки на стороне
 * сервера срабатывает, только когда не задано вообще ничего (resolveLLMConfig), и у
 * пользователя с сохранённым api_key вход без записи оставил бы агента на старом
 * провайдере, пока панель показывает «Вход выполнен».
 *
 * Пишутся сохранённые base_url/model, а не содержимое формы: поток мог
 * завершиться, пока пользователь правил поля, и сохранять его черновик молча — нет. */
async function persistCodexTransport() {
  // Без снимка писать нельзя: PUT перезаписывает base_url и model как есть, так
  // что пустые поля затёрли бы сохранённое. Начальный GET мог упасть — модалка
  // при этом открыта и вход доступен, — поэтому снимок сначала добирается.
  if (!state.llm) {
    try {
      applyLLMSnapshot(await api('/llm/settings'), { keepEdits: true });
    } catch (e) {
      return e.message || String(e);
    }
    if (!state.llm) return 'не удалось прочитать текущие настройки';
  }
  const stored = state.llm.stored || {};
  if (String(stored.auth_method || '') === 'codex') return '';
  try {
    await api('/llm/settings', {
      method: 'PUT',
      body: {
        auth_method: 'codex',
        // base_url подписка не использует, и он переживёт обратное переключение
        // на провайдера — а вот model очищается намеренно: имя модели другого
        // провайдера подписке не подходит, и пусть лучше она возьмёт свою
        // модель по умолчанию, чем сохранённое имя, которое она не обслуживает.
        base_url: stored.base_url || '',
        model: '',
        api_key: '',
        clear_api_key: false,
      },
    });
    return '';
  } catch (e) {
    return e.message || String(e);
  }
}

async function finishCodexFlow(ok, message) {
  stopCodexPoll();
  state.codexFlow = null;
  setCodexProgress('');
  // Тост один на всё завершение: #toast — единственный элемент с перезаписью
  // текста, поэтому предупреждение, показанное перед сообщением об успехе,
  // прожило бы нулевое время и пользователь увидел бы только «Вход выполнен».
  const failedToSave = ok ? await persistCodexTransport() : '';
  if (!ok) {
    toast(message || 'Вход через ChatGPT не удался.', true);
  } else if (failedToSave) {
    toast(`Вход выполнен, но транспорт не сохранён: ${failedToSave}`, true);
  } else {
    toast('Вход через ChatGPT выполнен.');
  }
  await refreshLLMAfterAuth();
}

function startCodexPoll() {
  stopCodexPoll();
  state.codexPoll = setInterval(() => void pollCodexFlow(), CODEX_POLL_MS);
}

async function pollCodexFlow() {
  const flow = state.codexFlow;
  if (!flow) {
    stopCodexPoll();
    return;
  }
  if (Date.now() > flow.deadline) {
    await finishCodexFlow(false, 'Вход через ChatGPT не завершён за 5 минут.');
    return;
  }
  try {
    const snap = await api(`/llm/codex/login/${encodeURIComponent(flow.id)}`);
    if (snap?.status === 'success') {
      await finishCodexFlow(true);
    } else if (snap?.status === 'error') {
      await finishCodexFlow(false, snap.error);
    }
  } catch (e) {
    if (e.status === 404) {
      await finishCodexFlow(false, 'Поток входа больше не существует.');
    }
    /* остальные ошибки считаем временными и продолжаем опрос */
  }
}

async function startCodexLogin(noBrowser) {
  if (state.codexFlow) return;
  // Вкладка открывается синхронно, до запроса: window.open после await уже вне
  // пользовательского жеста, и блокировщик всплывающих окон его отклонит.
  // Адрес известен только после ответа сервера, поэтому окно открывается пустым
  // и переадресуется ниже — а значит нужен handle, и флаг 'noopener' здесь не
  // годится: с ним window.open по спецификации возвращает null. Связь рвётся
  // вручную (popup.opener = null), пока вкладка ещё about:blank.
  const popup = noBrowser ? null : window.open('', '_blank');
  try {
    const flow = await api('/llm/codex/login', { method: 'POST', body: { no_browser: !!noBrowser } });
    if (!flow?.id) throw new Error('Сервер не вернул идентификатор входа');
    state.codexFlow = { id: flow.id, deadline: Date.now() + CODEX_LOGIN_TTL_MS };
    const url = String(flow.authorize_url || '');
    if (popup && url) {
      popup.opener = null;
      popup.location.replace(url);
    } else if (popup) {
      popup.close();
    }
    if (noBrowser) {
      for (const target of CODEX_TARGETS) {
        const manual = $(target.manual);
        if (manual) manual.open = true;
      }
      setCodexProgress('Откройте ссылку, завершите вход и вставьте redirect URL ниже.', url);
    } else {
      setCodexProgress('Ожидаем завершения входа в браузере…', url);
    }
    startCodexPoll();
    renderLLMSettings();
  } catch (e) {
    popup?.close();
    toast(e.message || String(e), true);
    // 409 — занят порт 1455 под redirect: остаётся ручной путь без слушателя.
    if (e.status === 409 && !noBrowser) {
      await startCodexLogin(true);
    }
  }
}

async function submitCodexCode() {
  // Полей ввода кода два — в модалке и в мастере; отправляем то, которое видит
  // пользователь. «Первое непустое» отправило бы текст, оставшийся на закрытой
  // площадке от прошлой неудачной попытки, вместо только что вставленного.
  const input = CODEX_TARGETS.map((t) => $(t.code)).find(isElementVisible) || $(CODEX_TARGETS[0].code);
  const code = (input?.value || '').trim();
  if (!state.codexFlow) {
    toast('Сначала нажмите «Войти через ChatGPT».', true);
    return;
  }
  if (!code) {
    toast('Вставьте redirect URL или код.', true);
    return;
  }
  const id = state.codexFlow.id;
  try {
    await api(`/llm/codex/login/${encodeURIComponent(id)}/code`, { method: 'POST', body: { code } });
    await finishCodexFlow(true);
  } catch (e) {
    toast(e.message || String(e), true);
  } finally {
    // Поле очищается и после отказа: иначе отклонённое значение осталось бы
    // лежать и ушло бы снова, откуда бы следующую отправку ни начали.
    if (input) input.value = '';
  }
}

async function cancelCodexLogin() {
  const flow = state.codexFlow;
  if (!flow) return;
  stopCodexPoll();
  state.codexFlow = null;
  setCodexProgress('');
  try {
    await api(`/llm/codex/login/${encodeURIComponent(flow.id)}`, { method: 'DELETE' });
  } catch {
    /* поток мог уже завершиться сам — отменять нечего */
  }
  toast('Вход отменён.');
  renderLLMSettings();
}

async function codexLogout() {
  if (!window.confirm('Выйти из аккаунта ChatGPT?')) return;
  try {
    const codex = await api('/llm/codex/logout', { method: 'POST' });
    if (state.llm) state.llm.codex = codex;
    renderLLMSettings();
    toast('Выход из ChatGPT выполнен.');
    await refreshLLMAfterAuth();
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

function bindLLMSettings() {
  $('btn-llm-settings')?.addEventListener('click', () => void openLLMModal());
  $('btn-llm-close')?.addEventListener('click', () => closeLLMModal());
  $('btn-llm-cancel')?.addEventListener('click', () => closeLLMModal());
  $('llm-modal')?.addEventListener('mousedown', (e) => {
    if (e.target === e.currentTarget) closeLLMModal();
  });
  $('llm-tab-codex')?.addEventListener('click', () => selectLLMTab('codex'));
  $('llm-tab-apikey')?.addEventListener('click', () => selectLLMTab('apikey'));
  bindPolza();
  $('llm-base-url')?.addEventListener('input', () => {
    state.llmEdited.base_url = true;
  });
  $('llm-model')?.addEventListener('input', () => {
    state.llmEdited.model = true;
  });
  $('btn-llm-save')?.addEventListener('click', () => void saveLLMSettings());
  $('btn-llm-reset')?.addEventListener('click', () => void resetLLMSettings());
  $('btn-llm-clear-key')?.addEventListener('click', () => void clearLLMAPIKey());
  $('btn-codex-login')?.addEventListener('click', () => void startCodexLogin(false));
  $('btn-codex-logout')?.addEventListener('click', () => void codexLogout());
  $('btn-codex-cancel')?.addEventListener('click', () => void cancelCodexLogin());
  $('btn-codex-code-submit')?.addEventListener('click', () => void submitCodexCode());
  $('llm-codex-code')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void submitCodexCode();
    }
  });
}
