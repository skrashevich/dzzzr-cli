'use strict';

const API = '/api/v1';

const state = {
  chats: [],
  activeId: null,
  detail: null,
  auth: null,
  filesEnabled: false,
  es: null,
  streamBuf: '',
  agentRunning: false,
  agentStatus: { phase: '', message: '' },
  lastActivityAt: 0,
  activityTimer: null,
  runningPoll: null,
  pendingTools: [],
  approvalPrompt: null,
  searchQuery: '',
  attachments: [],
  // Настройки LLM (llm.js): снимок /llm/settings, выбранная вкладка, правки
  // формы и поток входа через ChatGPT.
  llm: null,
  llmAuth: '',
  llmEdited: { base_url: false, model: false },
  codexFlow: null,
  codexPoll: null,
  // Мастер первого запуска (onboarding.js).
  onboarding: { step: 'welcome', status: null, llmAuth: 'polza', role: '', busy: false },
};

const ROLE_RU = {
  user: 'вы',
  assistant: 'агент',
  tool: 'инструмент',
  system: 'система',
};

const $ = (id) => document.getElementById(id);

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s == null ? '' : String(s);
  return d.innerHTML;
}

function renderInlineMarkdown(s) {
  let x = escapeHtml(String(s ?? ''));
  x = x.replace(/`([^`]+)`/g, '<code class="md-code">$1</code>');
  x = x.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  x = x.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  x = x.replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
  return x;
}

function isTableRow(line) {
  return /^\s*\|.+\|\s*$/.test(line);
}

function isTableSeparator(line) {
  const t = line.trim();
  if (!t.includes('-')) return false;
  const inner = t.replace(/^\|/, '').replace(/\|$/, '');
  return inner.split('|').every((part) => /^[\s\-:]+$/.test(part.trim()));
}

function parseTableRow(line) {
  const t = line.trim().replace(/^\|/, '').replace(/\|$/, '');
  return t.split('|').map((c) => c.trim());
}

function renderMarkdownTable(lines) {
  if (!lines.length) return '';
  const header = parseTableRow(lines[0]);
  let bodyStart = 1;
  if (lines.length > 1 && isTableSeparator(lines[1])) bodyStart = 2;
  const body = [];
  for (let i = bodyStart; i < lines.length; i++) {
    if (!isTableRow(lines[i])) break;
    body.push(parseTableRow(lines[i]));
  }
  const cols = header.length;
  let html = '<div class="md-table-wrap"><table class="md-table"><thead><tr>';
  for (const c of header) html += `<th>${renderInlineMarkdown(c)}</th>`;
  html += '</tr></thead><tbody>';
  for (const row of body) {
    html += '<tr>';
    for (let c = 0; c < cols; c++) {
      html += `<td>${renderInlineMarkdown(row[c] ?? '')}</td>`;
    }
    html += '</tr>';
  }
  html += '</tbody></table></div>';
  return html;
}

function renderMarkdownList(lines, ordered) {
  const tag = ordered ? 'ol' : 'ul';
  let html = `<${tag} class="md-list">`;
  for (const line of lines) {
    const m = ordered ? line.match(/^\s*\d+\.\s+(.*)$/) : line.match(/^\s*[-*+]\s+(.*)$/);
    if (m) html += `<li>${renderInlineMarkdown(m[1])}</li>`;
  }
  html += `</${tag}>`;
  return html;
}

function renderMarkdown(text) {
  const raw = String(text ?? '').replace(/\r\n/g, '\n');
  const lines = raw.split('\n');
  const blocks = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (line.trim().startsWith('```')) {
      const fence = line.trim();
      const lang = fence.slice(3).trim();
      i++;
      const codeLines = [];
      while (i < lines.length && !lines[i].trim().startsWith('```')) {
        codeLines.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++;
      const code = escapeHtml(codeLines.join('\n'));
      const langAttr = lang ? ` data-lang="${escapeHtml(lang)}"` : '';
      blocks.push(`<pre class="md-pre"${langAttr}><code>${code}</code></pre>`);
      continue;
    }

    if (isTableRow(line)) {
      const tableLines = [];
      while (i < lines.length && (isTableRow(lines[i]) || isTableSeparator(lines[i]))) {
        tableLines.push(lines[i]);
        i++;
      }
      blocks.push(renderMarkdownTable(tableLines));
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      const level = heading[1].length;
      blocks.push(`<h${level} class="md-h${level}">${renderInlineMarkdown(heading[2])}</h${level}>`);
      i++;
      continue;
    }

    if (/^\s*[-*+]\s+/.test(line)) {
      const listLines = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        listLines.push(lines[i]);
        i++;
      }
      blocks.push(renderMarkdownList(listLines, false));
      continue;
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      const listLines = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        listLines.push(lines[i]);
        i++;
      }
      blocks.push(renderMarkdownList(listLines, true));
      continue;
    }

    if (line.trim() === '') {
      i++;
      continue;
    }

    const para = [];
    while (
      i < lines.length &&
      lines[i].trim() !== '' &&
      !lines[i].trim().startsWith('```') &&
      !isTableRow(lines[i]) &&
      !/^(#{1,6})\s+/.test(lines[i]) &&
      !/^\s*[-*+]\s+/.test(lines[i]) &&
      !/^\s*\d+\.\s+/.test(lines[i])
    ) {
      para.push(lines[i]);
      i++;
    }
    blocks.push(`<p class="md-p">${renderInlineMarkdown(para.join('\n')).replace(/\n/g, '<br>')}</p>`);
  }

  return blocks.join('\n');
}

function toast(msg, err) {
  const el = $('toast');
  el.textContent = msg;
  el.classList.toggle('err', !!err);
  el.classList.add('visible');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.remove('visible'), 4200);
}

async function api(path, opts = {}) {
  const init = {
    credentials: 'same-origin',
    headers: { ...(opts.headers || {}), Accept: 'application/json' },
    ...opts,
  };
  if (opts.body != null && typeof opts.body === 'object' && !(opts.body instanceof FormData)) {
    init.body = JSON.stringify(opts.body);
    init.headers['Content-Type'] = 'application/json';
  }
  const res = await fetch(`${API}${path}`, init);
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!res.ok) {
    const msg =
      typeof data === 'object' && data && (data.message || data.error || data.detail)
        ? String(data.message || data.error || data.detail)
        : `HTTP ${res.status}`;
    const err = new Error(msg);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

function parseSSEPayload(ev) {
  if (ev.data == null || ev.data === '') return {};
  try {
    return JSON.parse(ev.data);
  } catch {
    return { _raw: ev.data };
  }
}

/* ---------- авторизация одного города ---------- */

function isLoggedIn() {
  return !!state.auth?.has_session;
}

async function loadAuthStatus() {
  state.auth = await api('/auth/status');
  const cityEl = $('field-city');
  if (cityEl) cityEl.textContent = state.auth?.city || '—';
  renderAuth();
}

function renderAuth() {
  const box = $('auth-status');
  const form = $('login-form');
  const logoutBtn = $('btn-logout');
  const online = isLoggedIn();

  if (form) form.classList.toggle('is-collapsed', online);
  if (logoutBtn) logoutBtn.hidden = !online;
  if (!box) return;

  if (!state.auth) {
    box.innerHTML = '';
    return;
  }
  const city = String(state.auth.city || '');
  const login = String(state.auth.login || '').trim();
  const userText = login || (online ? '…' : '—');
  const title = login
    ? `${city} — ${login}`
    : online
      ? `${city} — сессия без имени`
      : `${city} — нет сессии`;
  box.innerHTML = `<li class="auth-session-chip is-active${online ? '' : ' is-offline'}" title="${escapeHtml(title)}">
      <span class="auth-session-dot" aria-hidden="true"></span>
      <span class="auth-session-city">${escapeHtml(city)}</span>
      <span class="auth-session-user${login ? '' : ' is-missing'}">${escapeHtml(userText)}</span>
    </li>`;
}

async function onLoginSubmit(ev) {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const login = String(fd.get('login') || '').trim();
  const password = String(fd.get('password') || '');
  if (!login || !password) return;
  try {
    state.auth = await api('/auth/login', { method: 'POST', body: { login, password } });
    ev.target.reset();
    renderAuth();
    toast(`Вход выполнен (${state.auth?.city || ''}).`);
    await loadGames();
  } catch (e) {
    toast(e.message || String(e), true);
  }
  refreshSendState();
}

async function logout() {
  try {
    state.auth = await api('/auth/logout', { method: 'POST', body: {} });
    toast('Выход выполнен.');
  } catch (e) {
    toast(e.message || String(e), true);
  }
  renderAuth();
  await loadGames();
  refreshSendState();
}

/* ---------- справочная информация ---------- */

async function loadAgentConfig() {
  const el = $('brand-model');
  try {
    const data = await api('/agent/config');
    state.filesEnabled = !!data?.files_enabled;
    if (!el) return;
    const model = String(data?.model || '').trim();
    if (model) {
      const base = String(data?.base_url || '').trim();
      el.textContent = model;
      el.title = base ? `Модель: ${model}\nAPI: ${base}` : `Модель: ${model}`;
      el.classList.remove('is-missing');
    } else {
      const err = String(data?.error || '').trim();
      el.textContent = err ? 'модель не настроена' : '—';
      el.title = err || 'Откройте ⚙ «Настройки LLM» или задайте DZZZR_LLM_API_KEY';
      el.classList.add('is-missing');
    }
  } catch (e) {
    state.filesEnabled = false;
    if (el) {
      el.textContent = 'модель ?';
      el.title = e.message || String(e);
      el.classList.add('is-missing');
    }
  }
}

/** Список игр города — только для справки: инструменты агента работают с
 *  текущей игрой независимо от того, что здесь показано. */
async function loadGames() {
  const list = $('games-list');
  if (!list) return;
  list.innerHTML = '<li>Загрузка…</li>';
  try {
    const data = await api('/catalog/games');
    const games = Array.isArray(data?.games) ? data.games : [];
    if (!games.length) {
      list.innerHTML = '<li>Игр не найдено</li>';
      return;
    }
    list.innerHTML = games
      .map((g) => {
        const when = String(g.start || g.date || '').trim();
        return `<li><strong>#${escapeHtml(g.id)}</strong> ${escapeHtml(g.name || '')}${
          when ? ` · ${escapeHtml(when)}` : ''
        }</li>`;
      })
      .join('');
  } catch (e) {
    list.innerHTML = `<li>${escapeHtml(e.message || String(e))}</li>`;
  }
}

/* ---------- права агента ---------- */

function getSelectedPolicy() {
  return ($('field-policy')?.value || 'approve').trim();
}

function syncPolicyFromDetail(detail) {
  const sel = $('field-policy');
  if (!sel) return;
  const policy = detail?.policy || 'approve';
  if ([...sel.options].some((o) => o.value === policy)) sel.value = policy;
  syncPolicyVisual();
}

function syncPolicyVisual() {
  const sel = $('field-policy');
  if (sel) sel.dataset.mode = sel.value || 'approve';
}

function flashPolicyApplied() {
  const sel = $('field-policy');
  if (!sel) return;
  sel.classList.remove('is-applied');
  void sel.offsetWidth;
  sel.classList.add('is-applied');
  window.setTimeout(() => sel.classList.remove('is-applied'), 700);
}

async function applyPolicy() {
  const policy = getSelectedPolicy();
  syncPolicyVisual();
  if (state.detail) state.detail.policy = policy;
  if (!state.activeId) return;
  try {
    const updated = await api(`/chats/${encodeURIComponent(state.activeId)}`, {
      method: 'PATCH',
      body: { policy },
    });
    if (updated && typeof updated === 'object') {
      state.detail = { ...state.detail, ...updated };
      syncPolicyFromDetail(state.detail);
    }
    flashPolicyApplied();
  } catch (e) {
    toast(e.message || String(e), true);
    if (state.detail?.policy) syncPolicyFromDetail(state.detail);
  }
}

/* ---------- чаты ---------- */

async function loadChats() {
  const q = state.searchQuery.trim();
  const path = q ? `/chats?q=${encodeURIComponent(q)}` : '/chats';
  const data = await api(path);
  state.chats = Array.isArray(data?.chats) ? data.chats : [];
  state.chats.sort((a, b) => {
    const ta = Date.parse(a.updated_at || 0) || 0;
    const tb = Date.parse(b.updated_at || 0) || 0;
    return tb - ta;
  });
  renderChatList();
}

function renderChatList() {
  const ul = $('chat-list');
  ul.innerHTML = '';
  for (const c of state.chats) {
    const id = String(c.id);
    const li = document.createElement('li');
    li.className = 'chat-list-item';

    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'chat-item' + (id === state.activeId ? ' active' : '');
    const title = c.title || `Чат ${id}`;
    const running = !!c.running;
    btn.innerHTML = `<span class="chat-item-title">${escapeHtml(title)}</span>
      <span class="chat-item-meta">
        ${running ? '<span class="dot-running" aria-hidden="true"></span>' : ''}
        <span>${escapeHtml(c.city || '')}</span>
      </span>`;
    btn.addEventListener('click', () => switchChat(id));

    const del = document.createElement('button');
    del.type = 'button';
    del.className = 'chat-item-delete btn btn-ghost';
    del.title = 'Удалить чат';
    del.setAttribute('aria-label', `Удалить чат: ${title}`);
    del.innerHTML = '<span aria-hidden="true">✕</span>';
    del.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      void deleteChat(id);
    });

    li.appendChild(btn);
    li.appendChild(del);
    ul.appendChild(li);
  }
}

async function deleteChat(chatId) {
  const chat = state.chats.find((c) => String(c.id) === chatId);
  const title = chat?.title || `Чат ${chatId}`;
  let msg = `Удалить «${title}»?`;
  if (chat?.running) msg += '\nАгент сейчас работает — выполнение будет остановлено.';
  if (!window.confirm(msg)) return;

  try {
    await api(`/chats/${encodeURIComponent(chatId)}`, { method: 'DELETE' });
    if (state.activeId === chatId) {
      hideApprovalBar();
      clearAgentStatus();
      state.agentRunning = false;
      await switchChat(null);
    }
    await loadChats();
    toast('Чат удалён.');
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

async function createChat() {
  try {
    const created = await api('/chats', { method: 'POST', body: { policy: getSelectedPolicy() } });
    const id = created?.id != null ? String(created.id) : null;
    if (!id) {
      toast('Не удалось создать чат: нет id в ответе', true);
      await loadChats();
      return;
    }
    await loadChats();
    await switchChat(id);
    toast('Новый чат создан.');
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

/** Создаёт чат, когда пользователь пишет, ничего не выбрав в сайдбаре. */
async function ensureActiveChat() {
  if (state.activeId) return state.activeId;
  try {
    const created = await api('/chats', { method: 'POST', body: { policy: getSelectedPolicy() } });
    const id = created?.id != null ? String(created.id) : null;
    if (!id) return null;
    await loadChats();
    await selectChat(id);
    return id;
  } catch (e) {
    toast(e.message || String(e), true);
    return null;
  }
}

function getLinesFromDetail() {
  const lines = state.detail?.lines;
  return Array.isArray(lines) ? lines : [];
}

function renderMessages() {
  const wrap = $('messages');
  wrap.innerHTML = '';
  renderSessionFiles();

  if (!state.activeId) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.innerHTML =
      '<p class="empty-hint-title">Чат не выбран</p>' +
      (isLoggedIn()
        ? '<p class="empty-hint">Нажмите <strong>Новый чат</strong> слева или просто напишите задачу агенту.</p>'
        : '<p class="empty-hint">Войдите на сайт в панели справа — иначе инструменты агента не увидят игру.</p>');
    wrap.appendChild(empty);
    return;
  }

  for (const line of getLinesFromDetail()) {
    const role = String(line.role || 'assistant').toLowerCase();
    const roleLabel = line.tool_name ? `инстр. ${line.tool_name}` : ROLE_RU[role] || role;
    const content = line.content ?? '';
    const div = document.createElement('div');
    div.className = `msg ${role}`;
    const body = role === 'assistant' || role === 'user' ? renderMarkdown(content) : escapeHtml(content);
    div.innerHTML = `<span class="msg-role">${escapeHtml(roleLabel)}</span><span class="msg-body">${body}</span>`;
    wrap.appendChild(div);
  }
  if (state.agentRunning || state.streamBuf) {
    const div = document.createElement('div');
    div.className = 'msg assistant streaming';
    div.id = 'msg-streaming';
    div.innerHTML = `<span class="msg-role">${escapeHtml(ROLE_RU.assistant)}</span><span class="msg-body">${renderMarkdown(
      state.streamBuf,
    )}</span>`;
    wrap.appendChild(div);
  }
  wrap.scrollTop = wrap.scrollHeight;
}

/* ---------- состояние выполнения ---------- */

const PILL_LABEL_RU = {
  start: 'Запуск',
  llm: 'Модель',
  llm_wait: 'Ожидание',
  tool: 'Инструмент',
  stream: 'Ответ',
  retry: 'Повтор',
  log: 'Агент',
};

function shortPillLabel(phase) {
  return PILL_LABEL_RU[phase] || 'Агент';
}

function markAgentActivity() {
  state.lastActivityAt = Date.now();
  const pill = $('running-pill');
  if (pill && !pill.hidden) pill.classList.add('is-active');
  const bar = $('agent-status-bar');
  if (bar && !bar.hidden) bar.classList.add('is-active');
}

function scheduleActivityDecay() {
  clearTimeout(state.activityTimer);
  state.activityTimer = setTimeout(() => {
    if (!state.agentRunning) return;
    if (Date.now() - state.lastActivityAt > 3500) {
      $('running-pill')?.classList.remove('is-active');
      $('agent-status-bar')?.classList.remove('is-active');
    } else {
      scheduleActivityDecay();
    }
  }, 3500);
}

function setAgentStatus(phase, message) {
  const text = String(message || '').trim();
  if (!text) return;
  state.agentStatus = { phase: phase || 'log', message: text };
  markAgentActivity();
  scheduleActivityDecay();

  const bar = $('agent-status-bar');
  const textEl = $('agent-status-text');
  const pillLabel = $('running-pill-label');
  if (bar) bar.hidden = false;
  if (textEl) textEl.textContent = text;
  if (pillLabel) pillLabel.textContent = shortPillLabel(phase);
}

function clearAgentStatus() {
  state.agentStatus = { phase: '', message: '' };
  clearTimeout(state.activityTimer);
  state.activityTimer = null;
  const bar = $('agent-status-bar');
  if (bar) {
    bar.classList.remove('is-active');
    bar.hidden = true;
  }
  $('running-pill')?.classList.remove('is-active');
  const pillLabel = $('running-pill-label');
  if (pillLabel) pillLabel.textContent = 'Агент';
}

function clearToolChips() {
  hideToolChipTooltip();
  $('tool-chips').innerHTML = '';
  state.pendingTools = [];
}

/* ---------- вложения ---------- */

function formatFileSize(bytes) {
  const n = Number(bytes) || 0;
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/* ---------- файлы сессии ---------- */

// renderSessionFiles lists the files the agent wrote to DZZZR_FILES_ROOT during
// the active chat, each a direct download link.
function renderSessionFiles() {
  const wrap = $('session-files');
  const list = $('session-files-list');
  if (!wrap || !list) return;
  const files = Array.isArray(state.detail?.files) ? state.detail.files : [];
  list.innerHTML = '';
  wrap.hidden = files.length === 0 || !state.activeId;
  for (const f of files) {
    const li = document.createElement('li');
    li.className = 'session-file';
    const link = document.createElement('a');
    link.className = 'session-file-link';
    link.href = `${API}/chats/${encodeURIComponent(state.activeId)}/files/${encodeURIComponent(f.name)}`;
    link.setAttribute('download', f.name);
    link.textContent = f.name;
    const meta = document.createElement('span');
    meta.className = 'session-file-meta';
    meta.textContent = f.tool ? `${formatFileSize(f.size)} · ${f.tool}` : formatFileSize(f.size);
    li.append(link, meta);
    // A scenario the agent just wrote is the one file the author may want to
    // look at by hand before it reaches the engine, so it gets a way across.
    if (/\.json$/i.test(f.name)) {
      const open = document.createElement('button');
      open.type = 'button';
      open.className = 'btn btn-ghost btn-xs session-file-open';
      open.textContent = 'В редакторе';
      open.title = 'Открыть этот файл на экране импорта сценария';
      open.addEventListener('click', () => {
        window.dzzzrEditor?.openScenarioFromChatFile(state.activeId, f.name);
      });
      li.appendChild(open);
    }
    list.appendChild(li);
  }
}

// onSessionFile folds a live «file» event into the open chat.
function onSessionFile(payload) {
  if (!payload || !payload.name || !state.detail) return;
  if (!Array.isArray(state.detail.files)) state.detail.files = [];
  if (state.detail.files.some((f) => f.path === payload.path)) return;
  state.detail.files.push(payload);
  renderSessionFiles();
  toast(`Агент сохранил файл: ${payload.name}`);
}

function renderAttachments() {
  const wrap = $('composer-attachments');
  if (!wrap) return;
  wrap.innerHTML = '';
  wrap.hidden = state.attachments.length === 0;
  state.attachments.forEach((att, idx) => {
    const chip = document.createElement('span');
    chip.className = `attachment-chip ${att.status}`;
    const name = document.createElement('span');
    name.className = 'attachment-chip-name';
    name.textContent = att.status === 'error' ? `${att.file.name} — ошибка` : att.file.name;
    name.title = att.status === 'error' ? att.error || '' : `${att.file.name} (${formatFileSize(att.file.size)})`;
    chip.appendChild(name);
    if (att.status === 'uploading') {
      const spinner = document.createElement('span');
      spinner.textContent = '…';
      chip.appendChild(spinner);
    }
    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'attachment-chip-remove';
    remove.setAttribute('aria-label', `Убрать файл ${att.file.name}`);
    remove.textContent = '×';
    remove.addEventListener('click', () => removeAttachment(idx));
    chip.appendChild(remove);
    wrap.appendChild(chip);
  });
}

function removeAttachment(idx) {
  state.attachments.splice(idx, 1);
  renderAttachments();
}

async function uploadAttachment(att) {
  att.status = 'uploading';
  renderAttachments();
  try {
    const chatId = state.activeId || (await ensureActiveChat());
    if (!chatId) throw new Error('Нет активного чата');
    const fd = new FormData();
    fd.append('file', att.file, att.file.name);
    const res = await api(`/chats/${encodeURIComponent(chatId)}/files`, { method: 'POST', body: fd });
    att.path = res.path;
    att.name = res.name || att.file.name;
    att.size = res.size;
    att.status = 'done';
  } catch (e) {
    att.status = 'error';
    att.error = e.message || String(e);
    toast(`Не удалось загрузить файл «${att.file.name}»: ${att.error}`, true);
  }
  renderAttachments();
}

function onFilesSelected(ev) {
  const files = Array.from(ev.target.files || []);
  ev.target.value = '';
  for (const file of files) {
    const att = { file, status: 'pending', path: '', name: file.name, size: file.size, error: '' };
    state.attachments.push(att);
    void uploadAttachment(att);
  }
  renderAttachments();
}

/* ---------- инструменты ---------- */

function buildToolChipMeta(p) {
  const name = String(p.name ?? p.tool ?? 'tool');
  const args = String(p.args ?? '').trim();
  return { name, action: args ? `${name} ${args}` : name, details: [] };
}

let toolTipAnchor = null;

function hideToolChipTooltip() {
  toolTipAnchor = null;
  const tip = $('tool-chip-tooltip');
  if (tip) tip.hidden = true;
}

function showToolChipTooltip(el, meta) {
  const tip = $('tool-chip-tooltip');
  if (!tip || !el) return;
  toolTipAnchor = el;
  const detailItems = meta.details.length ? meta.details.map((line) => `<li>${escapeHtml(line)}</li>`).join('') : '';
  tip.innerHTML = `<p class="tool-chip-tooltip-action">${escapeHtml(meta.action)}</p>${
    detailItems ? `<ul class="tool-chip-tooltip-details">${detailItems}</ul>` : ''
  }`;
  tip.hidden = false;
  positionToolChipTooltip(el);
}

function positionToolChipTooltip(el) {
  const tip = $('tool-chip-tooltip');
  if (!tip || tip.hidden || !el) return;
  const rect = el.getBoundingClientRect();
  const margin = 8;
  tip.style.left = '0';
  tip.style.top = '0';
  tip.hidden = false;
  const tipRect = tip.getBoundingClientRect();
  let left = rect.left + rect.width / 2 - tipRect.width / 2;
  let top = rect.top - tipRect.height - margin;
  if (top < margin) top = rect.bottom + margin;
  left = Math.max(margin, Math.min(left, window.innerWidth - tipRect.width - margin));
  top = Math.max(margin, Math.min(top, window.innerHeight - tipRect.height - margin));
  tip.style.left = `${Math.round(left)}px`;
  tip.style.top = `${Math.round(top)}px`;
}

function bindToolChipTooltip(el, meta) {
  const show = () => showToolChipTooltip(el, meta);
  const hide = () => hideToolChipTooltip();
  el.addEventListener('mouseenter', show);
  el.addEventListener('mouseleave', hide);
  el.addEventListener('focus', show);
  el.addEventListener('blur', hide);
  el.tabIndex = 0;
  el.setAttribute('role', 'button');
  el.setAttribute('aria-label', `${meta.name}: ${meta.action}`);
}

function onToolStart(p) {
  const meta = buildToolChipMeta(p);
  setAgentStatus('tool', `Вызов инструмента: ${meta.name}`);
  const el = document.createElement('span');
  el.className = 'tool-chip pending';
  el.innerHTML = `<span class="tool-chip-label">инстр.</span> <strong class="tool-chip-name">${escapeHtml(
    meta.name,
  )}</strong>`;
  bindToolChipTooltip(el, meta);
  $('tool-chips').appendChild(el);
  state.pendingTools.push({ name: meta.name, el, meta });
}

function onToolDone(p) {
  const name = String(p.name ?? p.tool ?? 'tool');
  const idx = state.pendingTools.findIndex((t) => t.name === name && !t.el.classList.contains('done'));
  if (idx >= 0) {
    state.pendingTools[idx].el.classList.remove('pending');
    state.pendingTools[idx].el.classList.add('done');
  }
  setAgentStatus('tool', `Готово: ${name}`);
}

/* ---------- поток событий ---------- */

function disconnectES() {
  if (state.es) {
    state.es.close();
    state.es = null;
  }
}

function handleStreamEvent(kind, payload) {
  switch (kind) {
    case 'status':
      setAgentStatus(payload.phase, payload.message);
      break;
    case 'assistant_text': {
      const piece = payload.text ?? payload.content ?? payload._raw ?? '';
      if (!state.streamBuf) setAgentStatus('stream', 'Модель сформировала ответ');
      state.streamBuf += String(piece);
      const el = $('msg-streaming');
      if (el) {
        el.innerHTML = `<span class="msg-role">${escapeHtml(
          ROLE_RU.assistant,
        )}</span><span class="msg-body">${renderMarkdown(state.streamBuf)}</span>`;
        el.parentElement.scrollTop = el.parentElement.scrollHeight;
      } else {
        renderMessages();
      }
      break;
    }
    case 'tool_start':
      onToolStart(payload);
      break;
    case 'tool_done':
      onToolDone(payload);
      break;
    case 'file':
      onSessionFile(payload);
      break;
    case 'report':
      setAgentStatus('log', String(payload.text || '').split('\n')[0]);
      break;
    case 'warning':
      toast(String(payload.message || 'Предупреждение'), true);
      break;
    case 'done':
      void finishAgentTurn();
      break;
    case 'error':
      toast(String(payload.message || 'Ошибка агента'), true);
      break;
    case 'approval_prompt':
      showApprovalPrompt(payload);
      break;
    case 'approval_resolved':
      hideApprovalBar();
      break;
    default:
      break;
  }
}

function wireSSE(es) {
  const kinds = [
    'assistant_text',
    'tool_start',
    'tool_done',
    'file',
    'status',
    'report',
    'warning',
    'done',
    'error',
    'approval_prompt',
    'approval_resolved',
  ];
  for (const k of kinds) {
    es.addEventListener(k, (e) => handleStreamEvent(k, parseSSEPayload(e)));
  }
  es.onerror = () => {
    /* браузер переподключится сам */
  };
}

function connectES(chatId) {
  disconnectES();
  const es = new EventSource(`${API}/chats/${encodeURIComponent(chatId)}/events`);
  state.es = es;
  wireSSE(es);
  es.addEventListener('open', () => {
    void syncApprovalPrompt(chatId);
  });
}

async function syncApprovalPrompt(chatId) {
  try {
    const prompt = await api(`/chats/${encodeURIComponent(chatId)}/approval`);
    if (state.activeId === chatId) showApprovalPrompt(prompt);
  } catch (e) {
    if (e?.status !== 404) toast(e.message || String(e), true);
  }
}

/* ---------- согласование ---------- */

function showApprovalPrompt(p) {
  state.approvalPrompt = p;
  const bar = $('approval-bar');
  const body = $('approval-body');
  if (!bar || !body) return;
  const args = String(p.args || '').trim();
  body.innerHTML = `<div class="approval-head">
      <span class="approval-kicker">Согласование</span>
      <span class="approval-tool">${escapeHtml(p.tool || '')}</span>
    </div>
    <p class="approval-action">${escapeHtml(p.action || p.tool || '')}</p>
    ${args ? `<ul class="approval-details"><li>${escapeHtml(args)}</li></ul>` : ''}`;
  bar.hidden = false;
  setApprovalButtonsDisabled(false);
  bar.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
}

function setApprovalButtonsDisabled(disabled) {
  $('approval-bar')
    ?.querySelectorAll('.approval-actions button')
    .forEach((btn) => {
      btn.disabled = disabled;
    });
}

function hideApprovalBar() {
  state.approvalPrompt = null;
  const bar = $('approval-bar');
  if (bar) {
    bar.hidden = true;
    setApprovalButtonsDisabled(false);
  }
}

async function postApproval(action) {
  if (!state.activeId) return;
  setApprovalButtonsDisabled(true);
  try {
    await api(`/chats/${encodeURIComponent(state.activeId)}/approval`, { method: 'POST', body: { action } });
    hideApprovalBar();
  } catch (e) {
    setApprovalButtonsDisabled(false);
    toast(e.message || String(e), true);
  }
}

/* ---------- ход агента ---------- */

function refreshSendState() {
  const hasChat = !!state.activeId;
  const busy = state.agentRunning;
  $('message-input').disabled = busy;
  $('btn-send').disabled = busy;
  const attachBtn = $('btn-attach');
  if (attachBtn) attachBtn.disabled = busy || !state.filesEnabled;
  $('btn-export').disabled = !hasChat;
  $('btn-cancel').disabled = !hasChat || !busy;
  const policySel = $('field-policy');
  if (policySel) {
    policySel.disabled = !hasChat;
    syncPolicyVisual();
  }
  const pill = $('running-pill');
  if (pill) pill.hidden = !busy;
  if (!busy) {
    clearAgentStatus();
  } else if (!state.agentStatus.message) {
    setAgentStatus('start', 'Агент работает…');
  }
  renderComposerPlaceholder();
}

function renderComposerPlaceholder() {
  const input = $('message-input');
  if (!input) return;
  if (state.agentRunning) {
    input.placeholder = state.agentStatus.message || 'Агент отвечает…';
    return;
  }
  if (state.detail?.draft_id) {
    input.placeholder = 'Что изменить в общем черновике? (Enter — отправить)';
    return;
  }
  if (!isLoggedIn()) {
    input.placeholder = 'Войдите на сайт, чтобы агент видел игру…';
    return;
  }
  input.placeholder = 'Напишите задачу агенту… (Enter — отправить)';
}

function stopRunningPoll() {
  if (state.runningPoll) {
    clearInterval(state.runningPoll);
    state.runningPoll = null;
  }
}

function startRunningPoll(chatId) {
  stopRunningPoll();
  state.runningPoll = setInterval(() => void pollAgentRunning(chatId), 2000);
}

async function pollAgentRunning(chatId) {
  if (!state.agentRunning || state.activeId !== chatId) {
    stopRunningPoll();
    return;
  }
  try {
    const detail = await api(`/chats/${encodeURIComponent(chatId)}`);
    if (!detail.running) {
      state.detail = detail;
      await finishAgentTurn();
    }
  } catch {
    /* переходные ошибки не показываем */
  }
}

async function finishAgentTurn() {
  stopRunningPoll();
  state.streamBuf = '';
  clearToolChips();
  hideApprovalBar();
  state.agentRunning = false;
  try {
    await loadChats();
    if (state.activeId) {
      state.detail = await api(`/chats/${encodeURIComponent(state.activeId)}`);
      renderMessages();
      state.agentRunning = !!state.detail?.running;
    }
  } catch (e) {
    toast(e.message || String(e), true);
    state.agentRunning = false;
  }
  refreshSendState();
}

async function sendMessage() {
  const input = $('message-input');
  const text = input.value.trim();
  if (state.agentRunning) return;
  if (state.attachments.some((a) => a.status === 'uploading')) {
    toast('Файлы ещё загружаются…', true);
    return;
  }
  const failed = state.attachments.filter((a) => a.status === 'error');
  const ready = state.attachments.filter((a) => a.status === 'done');
  if (!text && ready.length === 0) return;
  if (!state.activeId) {
    const id = await ensureActiveChat();
    if (!id) return;
  }
  clearToolChips();
  try {
    await window.dzzzrEditor?.flushDraft();
    const files = ready.map((a) => ({ path: a.path, name: a.name }));
    const detail = await api(`/chats/${encodeURIComponent(state.activeId)}/messages`, {
      method: 'POST',
      body: { content: text, files },
    });
    input.value = '';
    state.attachments = failed;
    renderAttachments();
    state.detail = detail;
    state.agentRunning = true;
    state.streamBuf = '';
    setAgentStatus('start', 'Запуск агента…');
    startRunningPoll(state.activeId);
    renderMessages();
    refreshSendState();
    await loadChats();
  } catch (e) {
    toast(e.message || String(e), true);
    state.agentRunning = false;
    refreshSendState();
  }
}

async function cancelAgent() {
  if (!state.activeId) return;
  try {
    await api(`/chats/${encodeURIComponent(state.activeId)}/cancel`, { method: 'POST' });
    toast('Отменено.');
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

function exportChat(format) {
  if (!state.activeId) return;
  window.open(
    `${API}/chats/${encodeURIComponent(state.activeId)}/export?format=${encodeURIComponent(format)}`,
    '_blank',
  );
}

function toggleTheme() {
  const root = document.documentElement;
  const next = root.dataset.theme === 'light' ? 'dark' : 'light';
  root.dataset.theme = next;
  localStorage.setItem('dzzzr-theme', next);
}

/** Переключает чат, сбрасывая вложения — в отличие от selectChat, который
 *  ensureActiveChat вызывает посреди загрузки файлов. */
async function switchChat(chatId) {
  state.attachments = [];
  renderAttachments();
  await selectChat(chatId);
}

async function selectChat(chatId) {
  state.activeId = chatId;
  renderChatList();
  disconnectES();
  clearToolChips();
  hideApprovalBar();
  state.streamBuf = '';
  if (!chatId) {
    state.detail = null;
    renderMessages();
    refreshSendState();
    return;
  }
  try {
    state.detail = await api(`/chats/${encodeURIComponent(chatId)}`);
    syncPolicyFromDetail(state.detail);
    state.agentRunning = !!state.detail?.running;
    if (state.agentRunning) {
      setAgentStatus('llm_wait', 'Агент выполняет задачу…');
      startRunningPoll(chatId);
    }
    renderMessages();
    connectES(chatId);
    await syncApprovalPrompt(chatId);
    refreshSendState();
  } catch (e) {
    toast(e.message || String(e), true);
    state.detail = null;
    renderMessages();
    refreshSendState();
  }
}

function bindUI() {
  $('btn-new-chat').addEventListener('click', () => void createChat());
  $('btn-send').addEventListener('click', () => void sendMessage());
  $('btn-attach')?.addEventListener('click', () => $('file-input').click());
  $('file-input')?.addEventListener('change', onFilesSelected);
  $('btn-logout').addEventListener('click', () => void logout());
  $('btn-export').addEventListener('click', () => exportChat('markdown'));
  $('btn-cancel').addEventListener('click', () => void cancelAgent());
  $('btn-theme').addEventListener('click', () => toggleTheme());
  $('btn-approval-yes')?.addEventListener('click', () => void postApproval('yes'));
  $('btn-approval-no')?.addEventListener('click', () => void postApproval('no'));
  $('btn-approval-quit')?.addEventListener('click', () => void postApproval('quit'));
  $('login-form').addEventListener('submit', onLoginSubmit);
  $('field-policy')?.addEventListener('change', () => void applyPolicy());
  $('games-info')?.addEventListener('toggle', (e) => {
    if (e.target.open) void loadGames();
  });
  let searchTimer;
  $('chat-search')?.addEventListener('input', (e) => {
    state.searchQuery = e.target.value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => void loadChats(), 200);
  });
  $('message-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void sendMessage();
    }
  });
  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'n') {
      e.preventDefault();
      void createChat();
    }
    if ((e.metaKey || e.ctrlKey) && e.key === 'f') {
      e.preventDefault();
      $('chat-search')?.focus();
    }
    if (e.key === 'Escape' && state.agentRunning) void cancelAgent();
  });
}

// dzzzrChat is everything the editor (editor.js) is allowed to reach into.
// The two views share one page, one HTTP helper and one theme; naming the
// seam keeps editor.js from guessing at this file's internals.
window.dzzzrChat = {
  api,
  toast,
  toggleTheme,
  activeDraft: () => state.detail?.draft_id,
  // openChat switches to the chat view and opens one chat, which is what a
  // handoff from the editor ends with.
  async openChat(chatId) {
    await window.dzzzrEditor?.setMode('chat');
    await loadChats();
    await selectChat(String(chatId));
  },
};

async function boot() {
  try {
    const savedTheme = localStorage.getItem('dzzzr-theme');
    if (savedTheme) document.documentElement.dataset.theme = savedTheme;
    bindUI();
    bindLLMSettings();
    bindOnboarding();
    syncPolicyVisual();
    window.addEventListener(
      'scroll',
      () => {
        if (toolTipAnchor) positionToolChipTooltip(toolTipAnchor);
      },
      true,
    );
    window.addEventListener('resize', () => {
      if (toolTipAnchor) positionToolChipTooltip(toolTipAnchor);
    });
    requestAnimationFrame(() => document.body.classList.add('is-ready'));
    await loadAgentConfig();
    try {
      await loadAuthStatus();
    } catch (e) {
      toast(`Статус авторизации: ${e.message || String(e)}`, true);
    }
    try {
      await loadChats();
    } catch (e) {
      toast(`Чаты: ${e.message || String(e)}`, true);
    }
    if (state.chats.length && !state.activeId) {
      await selectChat(String(state.chats[0].id));
    } else {
      refreshSendState();
    }
  } finally {
    // Мастер открывается и тогда, когда что-то выше сломалось: без него
    // первый запуск остался бы без способа настроить модель и вход.
    await initOnboarding();
  }
}

boot();
