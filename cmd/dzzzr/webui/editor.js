'use strict';

// The manual game editor. It sits beside the agent chat rather than instead of
// it: both views share one page, one organizer session and one set of field
// names, because the form is drawn from /api/v1/admin/schema — the very schema
// the agent's admin_create_level tool accepts. An author who lays a level out
// by hand and an author who dictates it to the agent are editing the same
// thing.

// Everything lives inside one function because app.js and this file are two
// plain scripts sharing a single global scope: a top-level `const api` here
// would collide with app.js's own, and the whole editor would die on load.
// The only thing that leaves is window.dzzzrEditor.
(function () {
  const { api, toast } = window.dzzzrChat;

  const el = (id) => document.getElementById(id);

  const ed = {
    mode: 'chat',
    spec: null, // {game:{form,schema,clearable}, level:{…}}
    admin: null, // {city, login, has_admin}
    adminError: '',
    games: [],
    gameID: null,
    // gamesError and levelsError keep the reason a list came back empty, so
    // the sidebar can say «движок ответил 404» instead of «игр не найдено».
    gamesError: '',
    levels: [],
    levelsError: '',
    levelID: null,
    tab: 'game',
    // original is what the engine last told us; it is what «Отменить правки»
    // restores and what the empty-means-clear rule compares against.
    original: { game: {}, level: {} },
    booted: false,
    // booting is the one boot the editor ever runs, so the chat bridge can
    // wait for it instead of racing the schema and status requests.
    booting: null,
    dirty: false,
    scenario: null,
  };

  // LONG_TEXT_ROWS gives the tall fields more room than the two lines a plain
  // textarea starts with; a level's question is routinely a page of HTML.
  const LONG_TEXT_ROWS = 8;

  // ROW_COLUMNS describes the repeatable tables. The keys match the array field
  // types the server's form spec emits, and the column names match the object
  // properties of the schema items, so a renamed field shows up as an empty
  // column rather than as silently dropped data.
  const ROW_COLUMNS = {
    codes: [
      { name: 'code', label: 'Код', type: 'text', width: '2fr' },
      { name: 'synonyms', label: 'Синонимы (через #)', type: 'text', width: '2fr' },
      { name: 'danger', label: 'Сложность', type: 'danger', width: '1fr' },
      { name: 'sector', label: 'Сектор', type: 'int', width: '1fr' },
    ],
    bonus_codes: [
      { name: 'code', label: 'Код', type: 'text', width: '2fr' },
      { name: 'synonyms', label: 'Синонимы (через #)', type: 'text', width: '2fr' },
      { name: 'danger', label: 'Сложность', type: 'danger', width: '1fr' },
      { name: 'minutes', label: 'Минуты', type: 'int', width: '1fr' },
    ],
    fake_codes: [
      { name: 'code', label: 'Код', type: 'text', width: '2fr' },
      { name: 'synonyms', label: 'Синонимы (через #)', type: 'text', width: '2fr' },
      { name: 'penalty', label: 'Штраф, мин', type: 'int', width: '1fr' },
    ],
    spoilers: [
      { name: 'code', label: 'Код', type: 'text', width: '1.5fr' },
      { name: 'synonyms', label: 'Синонимы (через #)', type: 'text', width: '1.5fr' },
      { name: 'penalty', label: 'Штраф, мин', type: 'int', width: '1fr' },
      { name: 'text', label: 'Текст спойлера', type: 'text', width: '3fr' },
    ],
  };

  // DANGER_VALUES is the difficulty vocabulary the engine accepts. An empty
  // value and "null" both mean "не указана"; the engine stores them the same way
  // and the schema lists both, so the control offers one entry for the pair.
  const DANGER_VALUES = ['', '1', '1+', '2', '2+', '3', '3+'];

  // BULK_HINT explains the one-line-per-code format under each table.
  const BULK_HINT = {
    codes: 'КОД#синоним | сложность | сектор',
    bonus_codes: 'КОД#синоним | сложность | минуты',
    fake_codes: 'КОД#синоним | штраф',
    spoilers: 'КОД#синоним | штраф | текст спойлера',
  };

  // ---------------------------------------------------------------- mode

  // setMode swaps the whole page between the chat and the editor. The two views
  // share the sidebar and the window, so the switch is a body attribute the
  // stylesheet reads rather than a re-render.
  function setMode(mode) {
    ed.mode = mode === 'editor' ? 'editor' : 'chat';
    document.body.dataset.mode = ed.mode;
    for (const tab of document.querySelectorAll('.mode-tab')) {
      const active = tab.dataset.mode === ed.mode;
      tab.classList.toggle('is-active', active);
      tab.setAttribute('aria-selected', active ? 'true' : 'false');
    }
    if (ed.mode === 'editor' && !ed.booted) {
      ed.booted = true;
      ed.booting = bootEditor();
    }
    // The caller may need the editor to be usable before it acts on it; a mode
    // switch that changes nothing resolves immediately.
    return ed.booting ?? Promise.resolve();
  }

  // ---------------------------------------------------------------- helpers

  function setIssues(node, lines, kind) {
    node.innerHTML = '';
    node.hidden = !lines || lines.length === 0;
    node.classList.toggle('is-ok', kind === 'ok');
    node.classList.toggle('is-err', kind !== 'ok');
    if (node.hidden) return;
    const ul = document.createElement('ul');
    for (const line of lines) {
      const li = document.createElement('li');
      li.textContent = line;
      ul.appendChild(li);
    }
    node.appendChild(ul);
  }

  function markDirty(on) {
    ed.dirty = !!on;
    const node = el('editor-dirty');
    if (node) node.hidden = !on;
  }

  // confirmDiscard guards every path that rebuilds the form from what the
  // engine last said. Without it the «Есть несохранённые правки» banner warned
  // about a loss and was then cleared as part of causing it: switching tabs or
  // picking another level threw the work away without a word.
  function confirmDiscard() {
    if (!ed.dirty) return true;
    return window.confirm('В форме есть несохранённые правки. Отбросить их?');
  }

  // ISSUE_PATH picks the «поле[строка].колонка» or «поле» a validator message
  // opens with. gamesource writes its findings in English and the command-line
  // validator prints them word for word; translating them here would be a
  // second source of truth that drifts. Naming the control instead costs
  // nothing and cannot drift, because the names come from the very form spec
  // the page is drawn from.
  const ISSUE_PATH = /^([a-z_]+)(?:\[(\d+)\])?(?:\.([a-z_]+))?\b/;

  // fieldLabel finds what the form calls a parameter, or null when the name is
  // not one the form shows.
  function fieldLabel(name) {
    for (const group of currentSpec()?.form?.groups ?? []) {
      for (const field of group.fields ?? []) {
        if (field.name === name) return field.label || field.name;
      }
    }
    return null;
  }

  // humanizeIssue prefixes a validator line with the control it is about.
  // «codes[2].danger is required» reads as «Коды, строка 3 — «Сложность»:
  // codes[2].danger is required», so an author knows where to look without the
  // message itself being rewritten.
  function humanizeIssue(line) {
    const m = ISSUE_PATH.exec(line);
    if (!m) return line;
    const [, name, index, column] = m;
    const label = fieldLabel(name);
    if (!label) return line;
    const parts = [label];
    if (index != null) parts.push(`строка ${Number(index) + 1}`);
    if (column) {
      const col = (ROW_COLUMNS[name] ?? []).find((c) => c.name === column);
      parts.push(`«${col ? col.label : column}»`);
    }
    return `${parts.join(', ')}: ${line}`;
  }

  // ---------------------------------------------------------------- auth

  async function loadAdminStatus() {
    ed.admin = await api('/admin/status');
    renderAdminAuth();
  }

  function renderAdminAuth() {
    const form = el('admin-login-form');
    const out = el('btn-admin-logout');
    const status = el('admin-auth-status');
    const signed = !!ed.admin?.has_admin;
    form.hidden = signed;
    out.hidden = !signed;
    status.classList.toggle('is-err', !!ed.adminError);
    if (ed.adminError) {
      status.textContent = ed.adminError;
    } else {
      status.textContent = signed
        ? `${ed.admin.city}: организатор ${ed.admin.login}`
        : `${ed.admin?.city ?? '—'}: организатор не задан`;
    }
    el('editor-empty').hidden = signed;
    el('editor-sheet').hidden = !signed;
    el('editor-actions').hidden = !signed;
    el('editor-levels-block').hidden = !signed || ed.gameID == null;
  }

  async function onAdminLogin(event) {
    event.preventDefault();
    const form = event.target;
    const body = {
      login: form.elements.login.value.trim(),
      password: form.elements.password.value,
    };
    try {
      ed.admin = await api('/admin/login', { method: 'POST', body });
      form.elements.password.value = '';
      ed.adminError = '';
      renderAdminAuth();
      toast('Вход организатора выполнен');
      await loadGames();
    } catch (e) {
      // A run started with -admin-login already carries credentials, so a
      // refused browser login leaves has_admin true and the panel would
      // otherwise look like nothing happened.
      ed.adminError = `Вход организатора: ${e.message || String(e)}`;
      renderAdminAuth();
      toast(ed.adminError, true);
    }
  }

  async function onAdminLogout() {
    try {
      ed.admin = await api('/admin/logout', { method: 'POST', body: {} });
      ed.adminError = '';
      ed.gamesError = '';
      ed.games = [];
      ed.gameID = null;
      ed.original.game = {};
      resetLevelState();
      gameActionsEnabled(false);
      renderGameList();
      el('editor-form').innerHTML = '';
      renderAdminAuth();
      toast('Организатор отключён');
    } catch (e) {
      toast(`Выход организатора: ${e.message || String(e)}`, true);
    }
  }

  // ---------------------------------------------------------------- games

  async function loadGames() {
    if (!ed.admin?.has_admin) {
      ed.games = [];
      ed.gamesError = '';
      renderGameList();
      return;
    }
    try {
      const data = await api('/admin/games');
      ed.games = Array.isArray(data?.games) ? data.games : [];
      ed.gamesError = '';
    } catch (e) {
      ed.games = [];
      // The reason has to stay on screen. A toast fades, and «Игр не найдено»
      // then reads as «в городе нет игр» when what actually happened is that
      // the engine refused or was not reached at all — a wrong -base-url looks
      // exactly like an empty city otherwise.
      ed.gamesError = e.message || String(e);
      toast(`Список игр: ${ed.gamesError}`, true);
    }
    renderGameList();
  }

  function renderGameList() {
    const list = el('editor-game-list');
    list.innerHTML = '';
    if (ed.gamesError) {
      const li = document.createElement('li');
      li.className = 'editor-list-empty editor-list-error';
      li.textContent = ed.gamesError;
      list.appendChild(li);
      return;
    }
    for (const g of ed.games) {
      const li = document.createElement('li');
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'editor-item';
      btn.classList.toggle('is-active', g.id === ed.gameID);
      const name = document.createElement('span');
      name.className = 'editor-item-name';
      name.textContent = g.name || `Игра ${g.id}`;
      const meta = document.createElement('span');
      meta.className = 'editor-item-meta';
      meta.textContent = [g.date, g.status].filter(Boolean).join(' · ') || `#${g.id}`;
      btn.append(name, meta);
      btn.addEventListener('click', () => void selectGame(g.id));
      li.appendChild(btn);
      list.appendChild(li);
    }
    if (!ed.games.length) {
      const li = document.createElement('li');
      li.className = 'editor-list-empty';
      li.textContent = 'Игр не найдено';
      list.appendChild(li);
    }
  }

  // resetLevelState drops everything that belonged to the previous game's
  // levels. Leaving any of it behind is how the «Уровень» tab kept showing one
  // game's level under another game's heading — and «Сохранить» there, with no
  // level id, would have created a copy of it in the wrong game.
  function resetLevelState() {
    ed.levelID = null;
    ed.levels = [];
    ed.levelsError = '';
    ed.original.level = {};
    el('editor-tab-level').disabled = true;
    renderLevelList();
  }

  // gameActionsEnabled switches everything that needs a game already on the
  // engine: a game being composed has no id to copy, delete, export or hand to
  // the agent.
  function gameActionsEnabled(on) {
    for (const id of ['btn-game-copy', 'btn-game-delete', 'btn-scenario-export', 'btn-editor-ask']) {
      el(id).disabled = !on;
    }
  }

  async function selectGame(gameID) {
    if (!confirmDiscard()) return;
    ed.gameID = gameID;
    resetLevelState();
    ed.tab = 'game';
    renderGameList();
    el('editor-levels-block').hidden = false;
    gameActionsEnabled(true);
    try {
      const data = await api(`/admin/games/${gameID}`);
      ed.original.game = data?.params ?? {};
    } catch (e) {
      ed.original.game = {};
      toast(`Игра ${gameID}: ${e.message || String(e)}`, true);
    }
    const g = ed.games.find((x) => x.id === gameID);
    el('editor-title').textContent = g?.name || `Игра ${gameID}`;
    el('editor-subtitle').textContent = `id ${gameID}`;
    setTab('game');
    await loadLevels();
  }

  function createGame() {
    if (!confirmDiscard()) return;
    ed.gameID = null;
    ed.gamesError = '';
    ed.original.game = {};
    resetLevelState();
    renderGameList();
    el('editor-levels-block').hidden = true;
    gameActionsEnabled(false);
    el('editor-title').textContent = 'Новая игра';
    el('editor-subtitle').textContent = 'Название, дата и время обязательны';
    setTab('game');
  }

  async function copyGame() {
    if (ed.gameID == null) return;
    const withLevels = window.confirm('Скопировать игру вместе с заданиями?');
    try {
      const data = await api(`/admin/games/${ed.gameID}/copy`, {
        method: 'POST',
        body: { with_levels: withLevels },
      });
      toast(`Создана копия: игра ${data.id}`);
      await loadGames();
    } catch (e) {
      toast(`Копирование: ${e.message || String(e)}`, true);
    }
  }

  async function deleteGame() {
    if (ed.gameID == null) return;
    const g = ed.games.find((x) => x.id === ed.gameID);
    if (!window.confirm(`Удалить игру «${g?.name || ed.gameID}»? Движок откажет, пока в ней есть задания.`)) return;
    try {
      await api(`/admin/games/${ed.gameID}`, { method: 'DELETE' });
      toast('Игра удалена');
      ed.gameID = null;
      ed.original.game = {};
      resetLevelState();
      el('editor-levels-block').hidden = true;
      gameActionsEnabled(false);
      el('editor-title').textContent = 'Редактор';
      el('editor-subtitle').textContent = 'Ручная заливка игр, уровней и кодов';
      setTab('game');
      await loadGames();
    } catch (e) {
      toast(`Удаление: ${e.message || String(e)}`, true);
    }
  }

  // ---------------------------------------------------------------- levels

  async function loadLevels() {
    if (ed.gameID == null) {
      ed.levels = [];
      ed.levelsError = '';
      renderLevelList();
      return;
    }
    try {
      const data = await api(`/admin/games/${ed.gameID}/levels`);
      ed.levels = Array.isArray(data?.levels) ? data.levels : [];
      ed.levelsError = '';
    } catch (e) {
      ed.levels = [];
      ed.levelsError = e.message || String(e);
      toast(`Уровни: ${ed.levelsError}`, true);
    }
    renderLevelList();
  }

  function renderLevelList() {
    const list = el('editor-level-list');
    list.innerHTML = '';
    if (ed.levelsError) {
      const li = document.createElement('li');
      li.className = 'editor-list-empty editor-list-error';
      li.textContent = ed.levelsError;
      list.appendChild(li);
      return;
    }
    for (const l of ed.levels) {
      const li = document.createElement('li');
      li.className = 'editor-level-row';

      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'editor-item';
      btn.classList.toggle('is-active', l.id === ed.levelID);
      const name = document.createElement('span');
      name.className = 'editor-item-name';
      name.textContent = `${l.order}. ${l.title || 'без названия'}`;
      const meta = document.createElement('span');
      meta.className = 'editor-item-meta';
      const codes = Array.isArray(l.codes) ? l.codes.length : 0;
      meta.textContent = [l.kind, codes ? `${codes} кодов` : null, l.published ? null : 'черновик']
        .filter(Boolean)
        .join(' · ');
      btn.append(name, meta);
      btn.addEventListener('click', () => void selectLevel(l.id));

      const up = document.createElement('button');
      up.type = 'button';
      up.className = 'btn btn-ghost btn-icon btn-xs';
      up.textContent = '↑';
      up.title = 'Поднять уровень';
      up.addEventListener('click', () => void moveLevel(l.id, true));

      const down = document.createElement('button');
      down.type = 'button';
      down.className = 'btn btn-ghost btn-icon btn-xs';
      down.textContent = '↓';
      down.title = 'Опустить уровень';
      down.addEventListener('click', () => void moveLevel(l.id, false));

      const del = document.createElement('button');
      del.type = 'button';
      del.className = 'btn btn-ghost btn-icon btn-xs';
      del.textContent = '✕';
      del.title = 'Удалить уровень';
      del.addEventListener('click', () => void deleteLevel(l.id));

      const actions = document.createElement('span');
      actions.className = 'editor-level-actions';
      actions.append(up, down, del);

      li.append(btn, actions);
      list.appendChild(li);
    }
    if (!ed.levels.length) {
      const li = document.createElement('li');
      li.className = 'editor-list-empty';
      li.textContent = ed.gameID == null ? 'Выберите игру' : 'Уровней нет';
      list.appendChild(li);
    }
  }

  async function selectLevel(levelID) {
    if (!confirmDiscard()) return;
    ed.levelID = levelID;
    try {
      const data = await api(`/admin/games/${ed.gameID}/levels/${levelID}`);
      ed.original.level = data?.params ?? {};
    } catch (e) {
      ed.original.level = {};
      toast(`Уровень ${levelID}: ${e.message || String(e)}`, true);
    }
    el('editor-tab-level').disabled = false;
    renderLevelList();
    setTab('level');
  }

  function createLevel() {
    if (ed.gameID == null) {
      toast('Сначала выберите игру', true);
      return;
    }
    if (!confirmDiscard()) return;
    ed.levelID = null;
    ed.original.level = {};
    el('editor-tab-level').disabled = false;
    renderLevelList();
    setTab('level');
  }

  async function moveLevel(levelID, up) {
    try {
      const data = await api(`/admin/games/${ed.gameID}/levels/${levelID}/move`, {
        method: 'POST',
        body: { up },
      });
      if (data && data.moved === false) {
        toast('Уровень уже с краю списка');
      }
      await loadLevels();
    } catch (e) {
      toast(`Перестановка: ${e.message || String(e)}`, true);
    }
  }

  async function deleteLevel(levelID) {
    const l = ed.levels.find((x) => x.id === levelID);
    if (!window.confirm(`Удалить уровень «${l?.title || levelID}»?`)) return;
    try {
      await api(`/admin/games/${ed.gameID}/levels/${levelID}`, { method: 'DELETE' });
      toast('Уровень удалён');
      if (ed.levelID === levelID) {
        ed.levelID = null;
        ed.original.level = {};
        el('editor-tab-level').disabled = true;
        setTab('game');
      }
      await loadLevels();
    } catch (e) {
      toast(`Удаление: ${e.message || String(e)}`, true);
    }
  }

  // ---------------------------------------------------------------- form

  function setTab(tab) {
    ed.tab = tab;
    for (const node of document.querySelectorAll('.editor-tab')) {
      const active = node.dataset.target === tab;
      node.classList.toggle('is-active', active);
      node.setAttribute('aria-selected', active ? 'true' : 'false');
    }
    renderForm();
    setIssues(el('editor-issues'), []);
    markDirty(false);
  }

  function currentSpec() {
    return ed.tab === 'level' ? ed.spec?.level : ed.spec?.game;
  }

  function currentValues() {
    return ed.tab === 'level' ? ed.original.level : ed.original.game;
  }

  function renderForm() {
    const form = el('editor-form');
    form.innerHTML = '';
    const spec = currentSpec();
    if (!spec) return;
    const values = currentValues() ?? {};
    for (const group of spec.form?.groups ?? []) {
      form.appendChild(renderGroup(group, values));
    }
  }

  function renderGroup(group, values) {
    const box = document.createElement('fieldset');
    box.className = 'editor-group';
    const legend = document.createElement('legend');
    legend.textContent = group.title;
    box.appendChild(legend);
    const grid = document.createElement('div');
    grid.className = 'editor-grid';
    for (const field of group.fields ?? []) {
      grid.appendChild(renderField(field, values[field.name]));
    }
    box.appendChild(grid);
    return box;
  }

  function renderField(field, value) {
    const wrap = document.createElement('div');
    wrap.className = `editor-field editor-field-${field.type}`;
    wrap.dataset.field = field.name;
    wrap.dataset.type = field.type;

    const label = document.createElement('label');
    label.className = 'editor-label';
    label.textContent = field.label || field.name;
    if (field.hint) label.title = field.hint;
    wrap.appendChild(label);

    switch (field.type) {
      case 'codes':
      case 'bonus_codes':
      case 'fake_codes':
      case 'spoilers':
        wrap.appendChild(renderRowTable(field, Array.isArray(value) ? value : []));
        break;
      case 'strings':
        wrap.appendChild(renderStringList(field, Array.isArray(value) ? value : []));
        break;
      case 'bool': {
        const input = document.createElement('input');
        input.type = 'checkbox';
        input.className = 'editor-input editor-check';
        input.checked = value === true;
        label.prepend(input);
        label.classList.add('editor-label-check');
        wrap.classList.add('editor-field-inline');
        break;
      }
      case 'int': {
        const input = document.createElement('input');
        input.type = 'number';
        input.className = 'editor-input';
        input.step = '1';
        input.placeholder = 'не задано';
        input.value = value == null ? '' : String(value);
        wrap.appendChild(input);
        break;
      }
      case 'html':
        wrap.appendChild(renderRichText(value == null ? '' : String(value)));
        break;
      case 'textarea': {
        const input = document.createElement('textarea');
        input.className = 'editor-input editor-textarea';
        input.rows = LONG_TEXT_ROWS;
        input.value = value == null ? '' : String(value);
        wrap.appendChild(input);
        break;
      }
      default: {
        const input = document.createElement('input');
        input.type = 'text';
        input.className = 'editor-input';
        input.value = value == null ? '' : String(value);
        wrap.appendChild(input);
      }
    }

    if (field.hint) {
      const hint = document.createElement('span');
      hint.className = 'editor-hint';
      hint.textContent = field.hint;
      wrap.appendChild(hint);
    }
    return wrap;
  }

  // ------------------------------------------------------------- rich text

  // The engine renders a level's задание, подсказки and a game's легенда as
  // HTML to the players, and stores whatever markup it is given. Authors were
  // typing tags by hand; these fields get a formatting toolbar instead.
  //
  // The textarea underneath is the value — the contenteditable box writes into
  // it and «HTML» reveals it — so the source stays editable and nothing is
  // rewritten behind the author's back. Markup that arrived from the engine and
  // was not touched leaves exactly as it came.

  // RICH_TAGS and RICH_ATTRS are what survives a paste. Word, Google Docs and
  // the engine's own pages carry a mass of styling that means nothing to the
  // players' page; keeping the structure and dropping the rest is what makes
  // pasting a written scenario usable at all.
  const RICH_TAGS = new Set([
    'P', 'BR', 'B', 'STRONG', 'I', 'EM', 'U', 'S', 'A', 'IMG',
    'UL', 'OL', 'LI', 'H3', 'H4', 'BLOCKQUOTE', 'HR', 'PRE', 'CODE',
  ]);
  const RICH_ATTRS = {
    // data-dzzzr-* carry the address as the engine wrote it while the visual
    // box shows a resolvable one. Stripping them on a paste would turn a
    // cut-and-pasted picture's relative address into an absolute one in the
    // stored markup, so they are part of the keep-list.
    A: ['href', 'target', 'rel', 'data-dzzzr-href'],
    IMG: ['src', 'alt', 'width', 'height', 'data-dzzzr-src'],
  };

  // sanitizeRichHTML keeps the allowed tags and unwraps everything else, so a
  // pasted <span style="…"><b>код</b></span> keeps the bold and loses the
  // style rather than losing the word.
  function sanitizeRichHTML(html) {
    const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html');
    const walk = (node) => {
      for (const child of [...node.childNodes]) {
        if (child.nodeType === Node.TEXT_NODE) continue;
        if (child.nodeType !== Node.ELEMENT_NODE) {
          child.remove();
          continue;
        }
        walk(child);
        if (!RICH_TAGS.has(child.tagName)) {
          child.replaceWith(...child.childNodes);
          continue;
        }
        const keep = RICH_ATTRS[child.tagName] ?? [];
        for (const attr of [...child.attributes]) {
          if (!keep.includes(attr.name.toLowerCase())) child.removeAttribute(attr.name);
        }
        // javascript: in a link would run in the author's own browser when
        // they click it here, and in a player's when the engine serves it.
        const href = child.getAttribute('href');
        if (href && /^\s*javascript:/i.test(href)) child.removeAttribute('href');
        const src = child.getAttribute('src');
        if (src && /^\s*javascript:/i.test(src)) child.removeAttribute('src');
      }
    };
    walk(doc.body);
    return doc.body.innerHTML;
  }

  // RICH_URL_ATTRS are the attributes a level's markup points at files with.
  // The engine writes them relative to its own administration page —
  // «../../uploaded/moscow/Night/dvor.jpg» — and they resolve to nothing at
  // all against a local 127.0.0.1:8788, so an author editing a real level
  // would see broken pictures where the players see the task.
  const RICH_URL_ATTRS = [
    ['img', 'src'],
    ['a', 'href'],
  ];

  // ABSOLUTE_URL matches what must be left alone: a scheme, a protocol-relative
  // address, an in-page anchor, or an already-absolute path.
  const ABSOLUTE_URL = /^\s*([a-z][a-z0-9+.-]*:|\/\/|#|\/)/i;

  // relativeToAdmin turns an absolute engine address back into the relative
  // form the engine itself writes, so a picture inserted here survives a change
  // of host. An address somewhere else is left alone.
  function relativeToAdmin(absolute) {
    const base = ed.admin?.admin_url;
    if (!base) return absolute;
    let from;
    let to;
    try {
      from = new URL(base);
      to = new URL(absolute, base);
    } catch {
      return absolute;
    }
    if (from.origin !== to.origin) return absolute;
    const fromDirs = from.pathname.split('/').slice(1, -1);
    const toParts = to.pathname.split('/').slice(1);
    let same = 0;
    while (same < fromDirs.length && same < toParts.length - 1 && fromDirs[same] === toParts[same]) same++;
    const up = '../'.repeat(fromDirs.length - same);
    return up + toParts.slice(same).join('/') + to.search + to.hash;
  }

  // richToDisplay prepares engine markup for a live, editable node: relative
  // links become resolvable, with the original kept beside each one, and
  // anything that would execute is removed.
  //
  // The removal matters because this markup goes into the organizer's own
  // browser with the organizer's own session: a level whose задание carries
  // «<img src=x onerror=…>» would run it here. Only the executing parts go —
  // the stored value is the textarea, which is never touched by this.
  //
  // It reports whether it removed anything, because a box that cannot show the
  // whole field must not be allowed to write it back: a задание embedding a
  // map in an <iframe> would lose the map the moment the author fixed a typo
  // in the prose beside it. renderRichText keeps such a field in source view.
  //
  // RICH_UNSAFE names what cannot be shown in an editable node without running
  // it. Narrowing this list is not an option: <iframe src=javascript:…> and
  // <object> execute just as a <script> does.
  const RICH_UNSAFE = 'script, style, iframe, object, embed';

  function richToDisplay(html) {
    const base = ed.admin?.admin_url;
    const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html');
    let stripped = false;
    for (const node of doc.body.querySelectorAll(RICH_UNSAFE)) {
      node.remove();
      stripped = true;
    }
    for (const node of doc.body.querySelectorAll('*')) {
      for (const attr of [...node.attributes]) {
        const name = attr.name.toLowerCase();
        if (name.startsWith('on')) {
          node.removeAttribute(attr.name);
          stripped = true;
          continue;
        }
        if ((name === 'href' || name === 'src') && /^\s*javascript:/i.test(attr.value)) {
          node.removeAttribute(attr.name);
          stripped = true;
        }
      }
    }
    if (!base) return { html: doc.body.innerHTML, stripped };
    for (const [tag, attr] of RICH_URL_ATTRS) {
      for (const node of doc.body.querySelectorAll(tag)) {
        const raw = node.getAttribute(attr);
        if (!raw || ABSOLUTE_URL.test(raw)) continue;
        try {
          node.setAttribute(`data-dzzzr-${attr}`, raw);
          node.setAttribute(attr, new URL(raw, base).href);
        } catch {
          // An address the browser cannot parse is left exactly as written.
        }
      }
    }
    return { html: doc.body.innerHTML, stripped };
  }

  // richFromDisplay reads the visual box back, putting every relative address
  // back the way the engine wrote it.
  function richFromDisplay(area) {
    const doc = new DOMParser().parseFromString(`<body>${area.innerHTML}</body>`, 'text/html');
    for (const [tag, attr] of RICH_URL_ATTRS) {
      for (const node of doc.body.querySelectorAll(tag)) {
        const original = node.getAttribute(`data-dzzzr-${attr}`);
        if (original == null) continue;
        node.setAttribute(attr, original);
        node.removeAttribute(`data-dzzzr-${attr}`);
      }
    }
    return doc.body.innerHTML;
  }

  // richButton builds one toolbar control.
  function richButton(label, title, onClick) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'btn btn-ghost btn-xs editor-rich-btn';
    b.textContent = label;
    b.title = title;
    // Clicking a button would otherwise take the caret out of the box and
    // execCommand would have nothing to act on.
    b.addEventListener('mousedown', (e) => e.preventDefault());
    b.addEventListener('click', onClick);
    return b;
  }

  function renderRichText(value) {
    const box = document.createElement('div');
    box.className = 'editor-rich';

    const source = document.createElement('textarea');
    source.className = 'editor-input editor-textarea editor-rich-source';
    source.rows = LONG_TEXT_ROWS;
    source.value = value;
    source.hidden = true;

    const area = document.createElement('div');
    area.className = 'editor-input editor-rich-area';
    area.contentEditable = 'true';
    area.spellcheck = true;
    const first = richToDisplay(value);
    area.innerHTML = first.html;

    // A field the visual box cannot show whole is edited as markup instead.
    // Showing it anyway would be worse than showing nothing: the box writes
    // what it holds back into the value, so fixing a typo beside an embedded
    // map would delete the map without saying so.
    const sourceOnly = first.stripped;
    const notice = document.createElement('span');
    notice.className = 'editor-hint editor-rich-notice';
    notice.hidden = !sourceOnly;
    notice.textContent =
      'В поле есть разметка, которую визуальный редактор показать не может ' +
      '(<iframe>, <style>, скрипты). Правится как исходник — так ничего не потеряется.';

    // touched separates «the author changed this» from «the author looked at
    // it». Merely opening the markup view must not rewrite a field: the
    // browser normalises whatever it parses, so copying the visual box back
    // over untouched markup would turn `<img … />` into `<img …>` in a level
    // nobody edited.
    let touched = false;
    const sync = () => {
      touched = true;
      source.value = richFromDisplay(area);
      markDirty(true);
    };
    area.addEventListener('input', sync);
    source.addEventListener('input', () => markDirty(true));

    area.addEventListener('paste', (e) => {
      e.preventDefault();
      const data = e.clipboardData;
      const html = data?.getData('text/html');
      const text = data?.getData('text/plain') ?? '';
      // execCommand is deprecated but it is the only editing API every browser
      // still implements, and it is what keeps undo working.
      if (html) {
        document.execCommand('insertHTML', false, sanitizeRichHTML(html));
      } else {
        document.execCommand('insertText', false, text);
      }
      sync();
    });

    const cmd = (name, arg) => () => {
      area.focus();
      document.execCommand(name, false, arg);
      sync();
    };

    const bar = document.createElement('div');
    bar.className = 'editor-rich-bar';
    bar.append(
      richButton('Ж', 'Полужирный', cmd('bold')),
      richButton('К', 'Курсив', cmd('italic')),
      richButton('Ч', 'Подчёркнутый', cmd('underline')),
      richButton('¶', 'Обычный абзац', cmd('formatBlock', 'p')),
      richButton('З', 'Заголовок', cmd('formatBlock', 'h3')),
      richButton('•', 'Маркированный список', cmd('insertUnorderedList')),
      richButton('1.', 'Нумерованный список', cmd('insertOrderedList')),
    );

    bar.appendChild(
      richButton('🔗', 'Ссылка', () => {
        const url = window.prompt('Адрес ссылки', 'https://');
        if (!url) return;
        area.focus();
        document.execCommand('createLink', false, url);
        sync();
      }),
    );

    // Pictures go into the game's own file area, which is where the engine
    // serves them from; pasting a link to somewhere else would break for the
    // players the moment that somewhere else changes.
    const picker = document.createElement('input');
    picker.type = 'file';
    picker.accept = 'image/*';
    picker.hidden = true;
    picker.addEventListener('change', async () => {
      const file = picker.files?.[0];
      picker.value = '';
      if (!file) return;
      if (ed.gameID == null) {
        toast('Сначала сохраните игру: картинка загружается в её файлы', true);
        return;
      }
      try {
        const form = new FormData();
        form.append('file', file, file.name);
        const res = await api(`/admin/games/${ed.gameID}/files`, { method: 'POST', body: form });
        area.focus();
        // The engine hands back an absolute address built from this run's own
        // -base-url. Storing that would pin the level to one host; the engine
        // writes its own pictures relative to the administration page, so the
        // markup gets the relative form and the box shows the absolute one.
        const stored = relativeToAdmin(res.url).replace(/"/g, '&quot;');
        document.execCommand('insertHTML', false, richToDisplay(`<img src="${stored}" alt="" />`).html);
        sync();
        toast(res.already_exists ? `Файл ${res.name} уже был в игре, вставлена ссылка на него` : `Загружено: ${res.name}`);
      } catch (e) {
        toast(`Загрузка картинки: ${e.message || String(e)}`, true);
      }
    });
    bar.append(
      richButton('🖼', 'Вставить картинку и загрузить её в файлы игры', () => picker.click()),
      richButton('✕', 'Убрать форматирование', cmd('removeFormat')),
    );

    const toggle = richButton('HTML', 'Показать разметку', () => {
      const toSource = source.hidden;
      if (toSource) {
        if (touched) source.value = richFromDisplay(area);
      } else {
        const back = richToDisplay(source.value);
        if (back.stripped) {
          // The field still carries markup the box cannot hold — either it
          // arrived that way or the author has just written some.
          toast('Визуальный вид недоступен: в поле есть <iframe>, <style> или скрипт', true);
          return;
        }
        area.innerHTML = back.html;
        touched = false;
        // Getting here proves the field is showable now, so the state that
        // said otherwise has to go: an author who removed the <iframe> would
        // otherwise face a dead toolbar under a line that is no longer true.
        notice.hidden = true;
        for (const button of bar.querySelectorAll('button')) button.disabled = false;
      }
      source.hidden = !toSource;
      area.hidden = toSource;
      toggle.classList.toggle('is-active', toSource);
    });
    toggle.classList.add('editor-rich-toggle');
    bar.appendChild(toggle);

    if (sourceOnly) {
      source.hidden = false;
      area.hidden = true;
      toggle.classList.add('is-active');
      for (const button of bar.querySelectorAll('button')) {
        if (button !== toggle) button.disabled = true;
      }
    }

    box.append(bar, notice, area, source, picker);
    return box;
  }

  // ---------------------------------------------------------------- tables

  function renderRowTable(field, rows) {
    const cols = ROW_COLUMNS[field.type] ?? [];
    const box = document.createElement('div');
    box.className = 'editor-rows';
    box.style.setProperty('--row-cols', cols.map((c) => c.width).join(' '));

    const head = document.createElement('div');
    head.className = 'editor-row editor-row-head';
    for (const c of cols) {
      const cell = document.createElement('span');
      cell.textContent = c.label;
      head.appendChild(cell);
    }
    head.appendChild(document.createElement('span'));
    box.appendChild(head);

    const body = document.createElement('div');
    body.className = 'editor-row-body';
    box.appendChild(body);

    const addRow = (values) => body.appendChild(buildRow(cols, values ?? {}));
    for (const row of rows) addRow(row);

    const tools = document.createElement('div');
    tools.className = 'editor-row-tools';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'btn btn-ghost btn-xs';
    add.textContent = '＋ строка';
    add.addEventListener('click', () => {
      addRow({});
      markDirty(true);
    });
    tools.appendChild(add);

    // Bulk entry is the reason an author would pick this screen over dictating
    // the level to the agent: a sheet of codes pastes in as one block.
    const bulk = document.createElement('details');
    bulk.className = 'editor-bulk';
    const summary = document.createElement('summary');
    summary.textContent = 'Вставить списком';
    bulk.appendChild(summary);

    const area = document.createElement('textarea');
    area.className = 'editor-input editor-bulk-area';
    area.rows = 5;
    area.placeholder = `Одна строка — один код:\n${BULK_HINT[field.type] ?? 'КОД'}`;
    bulk.appendChild(area);

    const report = document.createElement('div');
    report.className = 'editor-issues editor-bulk-report';
    report.hidden = true;

    // apply is shared by «добавить» and «заменить»: both read the same box and
    // both have to say what they could not read, rather than quietly writing a
    // zero or an empty difficulty into a hundred codes.
    const apply = (wipe) => {
      const { rows, problems } = parseBulk(area.value, cols);
      if (wipe) body.innerHTML = '';
      for (const row of rows) addRow(row);
      markDirty(true);
      if (problems.length) {
        setIssues(report, [`Разобрано строк: ${rows.length}. Не прочитано:`, ...problems]);
        toast(`Разобрано ${rows.length}, с замечаниями: ${problems.length}`, true);
        return;
      }
      setIssues(report, []);
      area.value = '';
      toast(wipe ? `Строк в списке: ${rows.length}` : `Добавлено строк: ${rows.length}`);
    };

    const bulkActions = document.createElement('div');
    bulkActions.className = 'editor-bulk-actions';

    const append = document.createElement('button');
    append.type = 'button';
    append.className = 'btn btn-secondary btn-xs';
    append.textContent = 'Добавить к списку';
    append.addEventListener('click', () => apply(false));

    const replace = document.createElement('button');
    replace.type = 'button';
    replace.className = 'btn btn-ghost btn-xs';
    replace.textContent = 'Заменить список';
    replace.addEventListener('click', () => apply(true));

    // Loading from a file is what the engine's own administration area used to
    // offer for codes. The file lands in the box above rather than in the table
    // directly: an author who opened the wrong file, or whose file turned out
    // to be in another layout, sees it before anything is written.
    const picker = document.createElement('input');
    picker.type = 'file';
    picker.accept = '.txt,.csv,.tsv,text/plain,text/csv';
    picker.hidden = true;
    picker.addEventListener('change', async () => {
      const file = picker.files?.[0];
      picker.value = '';
      if (!file) return;
      try {
        const text = await readCodeFile(file, cols);
        area.value = area.value.trim() ? `${area.value.replace(/\n*$/, '')}\n${text}` : text;
        bulk.open = true;
        const lines = text.split('\n').filter((l) => l.trim()).length;
        setIssues(report, [`Файл ${file.name}: строк ${lines}. Проверьте разбор и нажмите «Добавить» или «Заменить».`], 'ok');
        toast(`Загружено строк из файла: ${lines}`);
      } catch (e) {
        setIssues(report, [`Файл ${file.name}: ${e.message || String(e)}`]);
      }
    });

    const fromFile = document.createElement('button');
    fromFile.type = 'button';
    fromFile.className = 'btn btn-ghost btn-xs';
    fromFile.textContent = 'Из файла';
    fromFile.title = 'Загрузить коды из текстового файла или CSV';
    fromFile.addEventListener('click', () => picker.click());

    bulkActions.append(append, replace, fromFile, picker);
    bulk.append(bulkActions, report);
    tools.appendChild(bulk);

    box.appendChild(tools);
    return box;
  }

  function buildRow(cols, values) {
    const row = document.createElement('div');
    row.className = 'editor-row';
    for (const c of cols) {
      let input;
      if (c.type === 'danger') {
        input = document.createElement('select');
        for (const v of DANGER_VALUES) {
          const opt = document.createElement('option');
          opt.value = v;
          opt.textContent = v === '' ? 'не указана' : v;
          input.appendChild(opt);
        }
        const current = values[c.name] == null || values[c.name] === 'null' ? '' : String(values[c.name]);
        input.value = DANGER_VALUES.includes(current) ? current : '';
      } else if (c.type === 'int') {
        input = document.createElement('input');
        input.type = 'number';
        input.step = '1';
        input.value = values[c.name] == null ? '' : String(values[c.name]);
      } else {
        input = document.createElement('input');
        input.type = 'text';
        input.value = values[c.name] == null ? '' : String(values[c.name]);
      }
      input.className = 'editor-input';
      input.dataset.col = c.name;
      input.dataset.colType = c.type;
      row.appendChild(input);
    }
    const drop = document.createElement('button');
    drop.type = 'button';
    drop.className = 'btn btn-ghost btn-icon btn-xs';
    drop.textContent = '✕';
    drop.title = 'Убрать строку';
    drop.addEventListener('click', () => {
      row.remove();
      markDirty(true);
    });
    row.appendChild(drop);
    return row;
  }

  // BULK_SEPARATOR is what divides one cell from the next: «|», a tab or a
  // semicolon, whichever the author's spreadsheet produced.
  const BULK_SEPARATOR = /\s*[|\t;]\s*/;

  // MAX_CODE_FILE_BYTES is far above any real list of codes and well below
  // what would freeze the tab if somebody picked a video by mistake.
  const MAX_CODE_FILE_BYTES = 4 << 20;

  // readCodeFile turns an author's file into the lines the bulk box takes.
  //
  // Encoding is the part that has to be right without being asked: a list of
  // codes typed in Windows and saved as .txt is windows-1251, and reading it
  // as UTF-8 turns every Russian synonym into replacement characters. UTF-8 is
  // tried strictly first, so a file that really is UTF-8 is never mangled, and
  // only a file that fails that test is read as windows-1251 — which is also
  // the charset the engine's own pages use.
  async function readCodeFile(file, cols) {
    if (file.size > MAX_CODE_FILE_BYTES) {
      throw new Error(`файл больше ${Math.round(MAX_CODE_FILE_BYTES / (1 << 20))} МиБ`);
    }
    let bytes = new Uint8Array(await file.arrayBuffer());
    // The byte-order mark has to go before decoding, not after. A strict UTF-8
    // decoder eats it on its own, and a file that fails that test and is read
    // as windows-1251 turns those three bytes into «п»ї» rather than into a
    // character any later replace would recognise — the first code of the list
    // would have carried it.
    if (bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) bytes = bytes.subarray(3);
    let text;
    try {
      text = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
    } catch {
      text = new TextDecoder('windows-1251').decode(bytes);
    }
    text = text.replace(/\r\n?/g, '\n');
    // A comma is a separator only in a file that says it is a CSV, and only on
    // lines carrying none of the usual ones: a code list is full of commas that
    // belong to the text.
    if (/\.csv$/i.test(file.name)) {
      text = text
        .split('\n')
        .map((line) => (BULK_SEPARATOR.test(line) ? line : csvToBulk(line, cols)))
        .join('\n');
    }
    return text.trim();
  }

  // csvToBulk rewrites one comma-separated line into the «|» form the bulk box
  // reads. A spreadsheet quotes any cell containing a comma, so a comma inside
  // quotes belongs to the text — «КОД,"синоним, ещё",2» is three cells, not
  // four — and a doubled quote inside a quoted cell is one quote character.
  //
  // A sheet naturally has one column per column of the table, synonyms among
  // them, while the paste box expects them inside the code cell after a «#».
  // When the line carries exactly as many cells as the table has columns, the
  // second is therefore folded into the first. The result goes into the box,
  // not into the table, so the author sees what that produced before applying
  // it.
  function csvToBulk(line, cols) {
    const cells = [];
    let cell = '';
    let quoted = false;
    for (let i = 0; i < line.length; i++) {
      const ch = line[i];
      if (quoted) {
        if (ch !== '"') {
          cell += ch;
        } else if (line[i + 1] === '"') {
          cell += '"';
          i++;
        } else {
          quoted = false;
        }
        continue;
      }
      if (ch === '"') {
        quoted = true;
      } else if (ch === ',') {
        cells.push(cell.trim());
        cell = '';
      } else {
        cell += ch;
      }
    }
    cells.push(cell.trim());
    const hasSynonyms = Array.isArray(cols) && cols.some((c) => c.name === 'synonyms');
    if (hasSynonyms && cells.length === cols.length) {
      const [code, synonyms, ...rest] = cells;
      return [synonyms ? `${code}#${synonyms}` : code, ...rest].join(' | ');
    }
    return cells.join(' | ');
  }

  // parseBulk turns pasted or uploaded lines into rows. The first column is the
  // code and may carry its synonyms after a «#», the way the engine itself
  // stores them; the remaining columns follow in the order the table shows
  // them. A line with fewer cells leaves the rest empty instead of shifting
  // values into the wrong column.
  //
  // It returns the rows together with everything it could not read, because a
  // hundred pasted codes with three unreadable cells among them is the case
  // that matters, and silently writing a zero or an empty difficulty there is
  // how a level goes live wrong.
  function parseBulk(text, cols) {
    const rows = [];
    const problems = [];
    const lines = String(text ?? '').split('\n');

    // The last column of a spoiler row is prose, and prose contains
    // semicolons. Splitting the whole line would cut the text in half and drop
    // the tail, so the split stops once the last column is reached. Synonyms
    // ride inside the code cell after a «#» and consume no separator of their
    // own, so they do not count towards the cells a line is expected to carry.
    const limit = cols.filter((c) => c.name !== 'synonyms').length;

    lines.forEach((rawLine, index) => {
      const line = rawLine.trim();
      if (!line) return;
      const where = `строка ${index + 1}`;

      const parts = [];
      let rest = line;
      while (parts.length < limit - 1) {
        const m = BULK_SEPARATOR.exec(rest);
        if (!m) break;
        parts.push(rest.slice(0, m.index).trim());
        rest = rest.slice(m.index + m[0].length);
      }
      parts.push(rest.trim());
      while (parts.length && parts[parts.length - 1] === '') parts.pop();
      if (!parts.length) return;

      const row = {};
      let cursor = 0;
      for (const c of cols) {
        if (c.name === 'synonyms') continue; // filled from the code cell below
        const value = parts[cursor++] ?? '';
        if (value === '') continue;
        if (c.type === 'int') {
          const n = toInt(value);
          if (n == null) {
            problems.push(`${where}: «${c.label}» — «${value}» не число`);
            continue;
          }
          row[c.name] = n;
          continue;
        }
        if (c.type === 'danger' && !DANGER_VALUES.includes(value) && value !== 'null') {
          problems.push(`${where}: сложность «${value}» движку неизвестна, оставлена пустой`);
          continue;
        }
        row[c.name] = value;
      }
      // «КОД#син1#син2» is how the engine writes synonyms, so accept it inline.
      if (typeof row.code === 'string' && row.code.includes('#')) {
        const [code, ...syn] = row.code.split('#');
        row.code = code.trim();
        const joined = syn.map((s) => s.trim()).filter(Boolean).join('#');
        if (joined) row.synonyms = joined;
      }
      if (!row.code) {
        problems.push(`${where}: нет кода, строка пропущена`);
        return;
      }
      rows.push(row);
    });
    return { rows, problems };
  }

  // toInt returns null rather than zero for something that is not a number: a
  // zero nobody typed is an edit, and «0» is a meaningful value in its own
  // right for a sector and for the game a scenario is imported into.
  function toInt(value) {
    const text = String(value).trim();
    if (!/^-?\d+$/.test(text)) return null;
    const n = Number.parseInt(text, 10);
    return Number.isFinite(n) ? n : null;
  }

  function renderStringList(field, values) {
    const box = document.createElement('div');
    box.className = 'editor-rows editor-rows-strings';
    const body = document.createElement('div');
    body.className = 'editor-row-body';
    box.appendChild(body);

    const addRow = (value) => {
      const row = document.createElement('div');
      row.className = 'editor-row editor-row-single';
      const input = document.createElement('input');
      input.type = 'text';
      input.className = 'editor-input';
      input.dataset.col = 'value';
      input.value = value == null ? '' : String(value);
      const drop = document.createElement('button');
      drop.type = 'button';
      drop.className = 'btn btn-ghost btn-icon btn-xs';
      drop.textContent = '✕';
      drop.addEventListener('click', () => {
        row.remove();
        markDirty(true);
      });
      row.append(input, drop);
      body.appendChild(row);
    };
    for (const v of values) addRow(v);

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'btn btn-ghost btn-xs';
    add.textContent = '＋ строка';
    add.addEventListener('click', () => {
      addRow('');
      markDirty(true);
    });
    box.appendChild(add);
    return box;
  }

  // ---------------------------------------------------------------- payload

  // collectParams reads the form back into the object the API takes. Empty
  // number and text fields are omitted rather than sent as zero, because a zero
  // the author never typed is an edit: the engine would store it.
  function collectParams() {
    const spec = currentSpec();
    const original = currentValues() ?? {};
    const clearable = new Set(spec?.clearable ?? []);
    const params = {};
    const clear = [];

    for (const wrap of el('editor-form').querySelectorAll('.editor-field')) {
      const name = wrap.dataset.field;
      const type = wrap.dataset.type;
      switch (type) {
        case 'bool':
          params[name] = wrap.querySelector('input[type=checkbox]').checked;
          break;
        case 'int': {
          // The control is <input type=number>, so the browser has already
          // refused anything that is not one; a value it still cannot read is
          // left out rather than sent as a zero nobody typed.
          const raw = wrap.querySelector('input').value.trim();
          const n = raw === '' ? null : toInt(raw);
          if (n != null) params[name] = n;
          break;
        }
        case 'html': {
          // The source textarea is the value, in both modes. Reading the
          // visual box instead would rewrite markup nobody touched: the
          // browser normalises what it parses, so an untouched
          // `<img … />` would go back to the engine as `<img …>`. The box
          // writes into the textarea on every keystroke, so an edited field is
          // just as current. Read it by class, because the toolbar puts a file
          // picker in the same wrapper.
          const source = wrap.querySelector('.editor-rich-source');
          const value = source.value;
          if (value !== '') {
            params[name] = value;
          } else if (clearable.has(name) && String(original[name] ?? '') !== '') {
            clear.push(name);
          }
          break;
        }
        case 'codes':
        case 'bonus_codes':
        case 'fake_codes':
        case 'spoilers': {
          const rows = collectRows(wrap, ROW_COLUMNS[type] ?? []);
          // An empty array is meaningful — it clears the list — but only say so
          // when there was something to clear, so an untouched empty table does
          // not turn every update into a wipe.
          if (rows.length || (Array.isArray(original[name]) && original[name].length)) {
            params[name] = rows;
          }
          break;
        }
        case 'strings': {
          const values = [...wrap.querySelectorAll('input[data-col=value]')].map((i) => i.value.trim());
          if (name === 'clear') {
            clear.push(...values.filter(Boolean));
            break;
          }
          // Sector names are positional: the engine writes secName[i+1] and
          // reads them back by index, so a blank in the middle is sector 2
          // having no name, not a row to remove. Dropping the blanks would
          // rename sector 3 to whatever sector 4 was called. Only the empty
          // tail carries no information.
          while (values.length && values[values.length - 1] === '') values.pop();
          if (values.length || (Array.isArray(original[name]) && original[name].length)) {
            params[name] = values;
          }
          break;
        }
        default: {
          const input = wrap.querySelector('input, textarea');
          const value = input.value;
          if (value !== '') {
            params[name] = value;
          } else if (clearable.has(name) && String(original[name] ?? '') !== '') {
            // The engine treats an absent field as «не трогать», so emptying a
            // box has to be said out loud. This is the same rule the CLI applies
            // to «ключ=» with nothing after the equals sign.
            clear.push(name);
          }
        }
      }
    }
    if (clear.length) params.clear = [...new Set(clear)];
    return params;
  }

  function collectRows(wrap, cols) {
    const out = [];
    for (const row of wrap.querySelectorAll('.editor-row-body .editor-row')) {
      const value = {};
      for (const c of cols) {
        const input = row.querySelector(`[data-col="${c.name}"]`);
        if (!input) continue;
        const raw = input.value.trim();
        if (c.type === 'int') {
          if (raw !== '') {
            const n = toInt(raw);
            if (n != null) value[c.name] = n;
          }
        } else if (raw !== '') {
          value[c.name] = raw;
        }
      }
      if (Object.keys(value).length) out.push(value);
    }
    return out;
  }

  // ---------------------------------------------------------------- save

  async function validateCurrent() {
    const issues = el('editor-issues');
    try {
      const res = await api('/admin/validate', {
        method: 'POST',
        body: { kind: ed.tab, params: collectParams() },
      });
      if (res?.ok) {
        setIssues(issues, ['Проверка пройдена: движок примет эти поля.'], 'ok');
      } else {
        const lines = (res?.errors ?? []).map(humanizeIssue);
        setIssues(issues, lines.length ? lines : ['Проверка не пройдена']);
      }
      return !!res?.ok;
    } catch (e) {
      setIssues(issues, [e.message || String(e)]);
      return false;
    }
  }

  async function saveCurrent() {
    if (!(await validateCurrent())) {
      toast('Проверка не пройдена, на движок ничего не отправлено', true);
      return;
    }
    const params = collectParams();
    try {
      if (ed.tab === 'game') {
        if (ed.gameID == null) {
          const res = await api('/admin/games', { method: 'POST', body: { params } });
          toast(`Игра создана: ${res.id}`);
          await loadGames();
          await selectGame(res.id);
        } else {
          await api(`/admin/games/${ed.gameID}`, { method: 'PATCH', body: { params } });
          toast('Игра сохранена');
          await loadGames();
          await selectGame(ed.gameID);
        }
        return;
      }
      if (ed.levelID == null) {
        const res = await api(`/admin/games/${ed.gameID}/levels`, { method: 'POST', body: { params } });
        toast(`Уровень создан: ${res.id}`);
        await loadLevels();
        await selectLevel(res.id);
      } else {
        await api(`/admin/games/${ed.gameID}/levels/${ed.levelID}`, { method: 'PATCH', body: { params } });
        toast('Уровень сохранён');
        await loadLevels();
        await selectLevel(ed.levelID);
      }
    } catch (e) {
      setIssues(el('editor-issues'), [e.message || String(e)]);
      toast(`Сохранение: ${e.message || String(e)}`, true);
    }
  }

  function revertCurrent() {
    renderForm();
    setIssues(el('editor-issues'), []);
    markDirty(false);
    toast('Правки отменены');
  }

  // ---------------------------------------------------------------- scenario

  // exportScenario downloads the whole game as one file. It fetches rather than
  // navigating, because a navigation to a failed export replaces the editor
  // with a page of JSON and the author loses what they were doing; and because
  // the one failure worth handling — a file the engine will not hand over —
  // has an answer the author can take right there.
  async function exportScenario(linked) {
    if (ed.gameID == null) return;
    const query = linked ? '?linked=1' : '';
    try {
      const res = await fetch(`/api/v1/admin/games/${ed.gameID}/scenario${query}`);
      const text = await res.text();
      if (!res.ok) {
        let message = `HTTP ${res.status}`;
        try {
          message = JSON.parse(text).error ?? message;
        } catch {
          // A non-JSON body is shown as the status alone.
        }
        if (!linked && /asset|файл/i.test(message)) {
          const ask =
            `${message}\n\nВыгрузить со ссылками на файлы вместо встроенных? ` +
            'Сценарий будет меньше, но без самих картинок.';
          if (window.confirm(ask)) {
            await exportScenario(true);
            return;
          }
        }
        toast(`Экспорт сценария: ${message}`, true);
        return;
      }
      const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
      const link = document.createElement('a');
      link.href = url;
      link.download = `scenario-${ed.gameID}${linked ? '-linked' : ''}.json`;
      link.click();
      // The download has not necessarily started when click() returns, and
      // revoking the address out from under it is the documented way to lose
      // the file. One turn of the event loop is enough.
      setTimeout(() => URL.revokeObjectURL(url), 0);
      toast(linked ? 'Сценарий выгружен со ссылками на файлы' : 'Сценарий выгружен');
    } catch (e) {
      toast(`Экспорт сценария: ${e.message || String(e)}`, true);
    }
  }

  function openImportDialog() {
    ed.scenario = null;
    el('import-file').value = '';
    el('import-game-id').value = ed.gameID == null ? '' : String(ed.gameID);
    el('import-level-ids').value = '';
    el('btn-import-apply').disabled = true;
    setIssues(el('import-issues'), []);
    el('import-dialog').showModal();
  }

  // openScenarioFromChatFile is the bridge the chat calls: a scenario the agent
  // wrote during a conversation opens here instead of having to be downloaded
  // and picked out of a file dialog by hand.
  async function openScenarioFromChatFile(chatID, name) {
    await setMode('editor');
    openImportDialog();
    try {
      const res = await fetch(`/api/v1/chats/${encodeURIComponent(chatID)}/files/${encodeURIComponent(name)}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      ed.scenario = JSON.parse(await res.text());
      setIssues(el('import-issues'), [`Загружен файл агента: ${name}`], 'ok');
      await validateScenario();
    } catch (e) {
      ed.scenario = null;
      setIssues(el('import-issues'), [`Файл ${name}: ${e.message || String(e)}`]);
    }
  }

  async function onImportFile(event) {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      ed.scenario = JSON.parse(await file.text());
      setIssues(el('import-issues'), [`Файл прочитан: ${file.name}`], 'ok');
      await validateScenario();
    } catch (e) {
      ed.scenario = null;
      el('btn-import-apply').disabled = true;
      setIssues(el('import-issues'), [`Файл ${file.name}: ${e.message || String(e)}`]);
    }
  }

  async function validateScenario() {
    if (!ed.scenario) {
      setIssues(el('import-issues'), ['Сначала выберите файл сценария']);
      return;
    }
    try {
      const res = await api('/admin/scenario/validate', {
        method: 'POST',
        body: { scenario: ed.scenario },
      });
      if (res?.ok) {
        setIssues(
          el('import-issues'),
          [`Сценарий корректен: уровней ${res.levels ?? '?'}, файлов ${res.assets ?? 0}.`],
          'ok',
        );
        el('btn-import-apply').disabled = false;
      } else {
        setIssues(el('import-issues'), res?.errors ?? ['Сценарий не прошёл проверку']);
        el('btn-import-apply').disabled = true;
      }
    } catch (e) {
      setIssues(el('import-issues'), [e.message || String(e)]);
      el('btn-import-apply').disabled = true;
    }
  }

  async function applyScenario() {
    if (!ed.scenario) return;
    const gameRaw = el('import-game-id').value.trim();
    const mapRaw = el('import-level-ids').value.trim();
    const body = { scenario: ed.scenario };
    if (gameRaw !== '') {
      // An empty box means «создать новую игру», and the importer spells that
      // as game_id 0. A typo must not quietly become the same thing: it would
      // create a game instead of writing into the one the author named, and
      // the engine has no rollback.
      const gameID = toInt(gameRaw);
      if (gameID == null || gameID <= 0) {
        setIssues(el('import-issues'), [`Игра-получатель: «${gameRaw}» не похоже на номер игры. Оставьте поле пустым, чтобы создать новую.`]);
        return;
      }
      body.game_id = gameID;
    }
    if (mapRaw !== '') {
      try {
        body.level_ids = JSON.parse(mapRaw);
      } catch (e) {
        setIssues(el('import-issues'), [`Карта уровней: ${e.message || String(e)}`]);
        return;
      }
    }
    if (!window.confirm('Залить сценарий в движок? Отката у движка нет.')) return;
    try {
      const res = await api('/admin/scenario/import', { method: 'POST', body });
      setIssues(el('import-issues'), [`Готово: игра ${res.game_id}, уровней ${Object.keys(res.level_ids ?? {}).length}.`], 'ok');
      toast('Сценарий залит');
      await loadGames();
      if (res.game_id) await selectGame(res.game_id);
    } catch (e) {
      // A failed import is not a no-op: the answer carries what already landed,
      // spliced into the same object as the error rather than nested under a
      // key of its own.
      const partial = e.data;
      const lines = [e.message || String(e)];
      if (partial && partial.stage) {
        lines.push(`Остановилось на шаге «${partial.stage}» (${partial.failed_key || 'без ключа'}).`);
        const landed = Object.keys(partial.level_ids ?? {}).length;
        if (partial.game_id) lines.push(`Записано в игру ${partial.game_id}, уровней затронуто: ${landed}.`);
        lines.push('Отката у движка нет: проверьте список уровней до повторной заливки.');
      }
      setIssues(el('import-issues'), lines);
    }
  }

  // ---------------------------------------------------------------- handoff

  // askAgent hands the editor's context to the chat. The text is built on the
  // server so the two views cannot disagree about which game is meant.
  async function askAgent() {
    if (ed.gameID == null) return;
    const note = window.prompt('Что спросить у агента об этой игре?', '');
    if (note === null) return;
    const body = { game_id: ed.gameID, note };
    if (ed.levelID != null) body.level_id = ed.levelID;
    try {
      const chat = await api('/admin/handoff', { method: 'POST', body });
      await window.dzzzrChat.openChat(chat.id);
      toast('Чат с контекстом игры открыт');
    } catch (e) {
      toast(`Передача агенту: ${e.message || String(e)}`, true);
    }
  }

  // ---------------------------------------------------------------- boot

  async function bootEditor() {
    try {
      ed.spec = await api('/admin/schema');
    } catch (e) {
      toast(`Схема полей: ${e.message || String(e)}`, true);
      return;
    }
    try {
      await loadAdminStatus();
    } catch (e) {
      toast(`Статус организатора: ${e.message || String(e)}`, true);
    }
    if (ed.admin?.has_admin) await loadGames();
    setTab('game');
  }

  function bindEditor() {
    for (const tab of document.querySelectorAll('.mode-tab')) {
      tab.addEventListener('click', () => setMode(tab.dataset.mode));
    }
    for (const tab of document.querySelectorAll('.editor-tab')) {
      tab.addEventListener('click', () => {
        if (tab.disabled || tab.dataset.target === ed.tab) return;
        if (!confirmDiscard()) return;
        setTab(tab.dataset.target);
      });
    }
    el('admin-login-form').addEventListener('submit', onAdminLogin);
    el('btn-admin-logout').addEventListener('click', () => void onAdminLogout());
    el('btn-game-new').addEventListener('click', () => createGame());
    el('btn-game-copy').addEventListener('click', () => void copyGame());
    el('btn-game-delete').addEventListener('click', () => void deleteGame());
    el('btn-level-new').addEventListener('click', () => createLevel());
    el('btn-editor-validate').addEventListener('click', () => void validateCurrent());
    el('btn-editor-save').addEventListener('click', () => void saveCurrent());
    el('btn-editor-revert').addEventListener('click', () => revertCurrent());
    el('btn-scenario-export').addEventListener('click', () => void exportScenario(false));
    el('btn-scenario-import').addEventListener('click', () => openImportDialog());
    el('btn-editor-ask').addEventListener('click', () => void askAgent());
    el('btn-editor-theme').addEventListener('click', () => window.dzzzrChat.toggleTheme());
    el('import-file').addEventListener('change', (e) => void onImportFile(e));
    el('btn-import-validate').addEventListener('click', () => void validateScenario());
    el('btn-import-apply').addEventListener('click', () => void applyScenario());
    el('btn-import-close').addEventListener('click', () => el('import-dialog').close());
    el('editor-form').addEventListener('input', () => markDirty(true));
  }

  window.dzzzrEditor = { setMode, openScenarioFromChatFile, parseBulk, humanizeIssue };

  bindEditor();
  setMode('chat');
})();
