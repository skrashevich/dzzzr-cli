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
    // newGame is set while «＋» composes a game that is not on the engine yet.
    // Without it a signed-in author who has picked nothing faced the same empty
    // form, and «Сохранить» there quietly created a game.
    newGame: false,
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
    // routing counts the route applications in flight: while one runs, the
    // steps it takes must not write their own intermediate addresses.
    routing: 0,
    // routeHash is the address the page last settled on, so an event about
    // an address the page wrote itself is not taken for a navigation.
    routeHash: null,
    // pendingRoute is an address that names a game before an organizer has
    // signed in; it is applied once the login lets the games be read.
    pendingRoute: null,
  };

  // MODE_KEY remembers the view last used, so an author who works in the
  // editor comes back to it after restarting the program.
  const MODE_KEY = 'dzzzr-mode';

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
      { name: 'synonyms', label: 'Синонимы к коду (через #)', type: 'text', width: '2fr' },
      { name: 'danger', label: 'КО', type: 'danger', width: '1fr' },
      { name: 'sector', label: 'Сектор', type: 'int', width: '1fr' },
    ],
    bonus_codes: [
      { name: 'code', label: 'Код', type: 'text', width: '2fr' },
      { name: 'synonyms', label: 'Синонимы к коду (через #)', type: 'text', width: '2fr' },
      { name: 'danger', label: 'КО', type: 'danger', width: '1fr' },
      { name: 'minutes', label: 'Бонус, мин', type: 'int', width: '1fr' },
    ],
    fake_codes: [
      { name: 'code', label: 'Код', type: 'text', width: '2fr' },
      { name: 'synonyms', label: 'Синонимы к коду (через #)', type: 'text', width: '2fr' },
      { name: 'penalty', label: 'Штраф за нахождение, мин', type: 'int', width: '1fr' },
    ],
    spoilers: [
      { name: 'code', label: 'Код спойлера', type: 'text', width: '1.5fr' },
      { name: 'synonyms', label: 'Синонимы к спойлеру (через #)', type: 'text', width: '1.5fr' },
      { name: 'penalty', label: 'Штраф за открытие, мин', type: 'int', width: '1fr' },
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

  let formValues = null;
  let documentForLevel = null;
  let rememberedDraft = null;
  const selectionKey = 'dzzzr-editor-selection';
  const drafts = window.createDraftSync({
    api,
    read: collectDraftParams,
    render(params) {
      const active = document.activeElement;
      const field = active?.closest('.editor-field');
      const index = field ? [...field.querySelectorAll('input,textarea,[contenteditable]')].indexOf(active) : -1;
      const range = [active?.selectionStart, active?.selectionEnd];
      const fieldName = field?.dataset.field;
      const sourceFields = [...el('editor-form').querySelectorAll('.editor-rich-source')].filter((node) => !node.hidden).map((node) => node.closest('.editor-field').dataset.field);
      formValues = params;
      renderForm();
      for (const name of sourceFields) {
        const wrap = el('editor-form').querySelector(`[data-field="${name}"]`);
        const source = wrap?.querySelector('.editor-rich-source');
        if (source?.hidden) [...wrap.querySelectorAll('button')].find((button) => button.textContent === 'HTML')?.click();
      }
      if (fieldName && index >= 0) {
        const next = el('editor-form').querySelector(`[data-field="${fieldName}"]`)?.querySelectorAll('input,textarea,[contenteditable]')[index];
        next?.focus({ preventScroll: true });
        if (range[0] != null && next?.setSelectionRange) next.setSelectionRange(...range);
      }
    },
    status(text) {
      draftStatus = text;
      el('editor-dirty').hidden = false;
      renderDirty();
    },
    changed(draft, doc) {
      ed.gameID = draft.game_id || null;
      ed.newGame = !draft.game_id;
      if (doc.kind === 'level') ed.levelID = doc.level_id || null;
      ed.original[doc.kind] = structuredClone(doc.base);
      localStorage.setItem(selectionKey, JSON.stringify({ draft: draft.id, document: doc.id }));
      el('btn-editor-ask').disabled = false;
      renderDraftLevels();
      renderHeading();
      scheduleStatus();
    },
  });

  function collectDraftParams() {
    const params = {};
    for (const wrap of el('editor-form').querySelectorAll('.editor-field')) {
      params[wrap.dataset.field] = readField(wrap);
    }
    return params;
  }

  // readField is one control's value as the draft stores it.
  function readField(wrap) {
    const type = wrap.dataset.type;
    if (type === 'bool') return wrap.querySelector('input').checked;
    if (ROW_COLUMNS[type]) return collectRows(wrap, ROW_COLUMNS[type]);
    if (type === 'strings') return [...wrap.querySelectorAll('input[data-col=value]')].map((i) => i.value);
    if (type === 'html') return wrap.querySelector('.editor-rich-source').value;
    const value = wrap.querySelector('input,textarea').value;
    return type === 'int' && value !== '' ? Number(value) : value;
  }

  async function openDocument() {
    if (!ed.newGame && ed.gameID == null) { formValues = null; return; }
    const existing = drafts.draft;
    const sameGame = existing && existing.game_id === (ed.gameID ?? 0);
    const data = await api('/admin/drafts/open', { method: 'POST', body: {
      draft_id: sameGame ? existing.id : rememberedDraft ?? '',
      game_id: ed.gameID ?? 0, kind: ed.tab, level_id: ed.tab === 'level' ? ed.levelID ?? 0 : 0,
      document_id: ed.tab === 'level' ? documentForLevel ?? '' : '',
      params: currentValues() ?? {},
    } });
    rememberedDraft = null;
    await drafts.attach(data);
    if (ed.tab === 'level') documentForLevel = drafts.doc.id;
  }

  async function openDraftSelection(draftID, documentID) {
    if (!(await confirmDiscard())) return;
    const data = await api(`/admin/drafts/${draftID}`);
    const doc = data.documents[documentID ?? data.active];
    if (!doc) throw new Error('Документ черновика не найден');
    await drafts.detach();
    ed.gameID = data.game_id || null;
    ed.newGame = !data.game_id;
    ed.levelID = doc.level_id || null;
    ed.original[doc.kind] = structuredClone(doc.base);
    documentForLevel = doc.kind === 'level' ? doc.id : null;
    rememberedDraft = data.id;
    ed.tab = doc.kind;
    el('editor-tab-level').disabled = doc.kind !== 'level';
    gameActionsEnabled(!!data.game_id);
    await setTab(doc.kind);
    await loadLevels();
  }

  function renderDraftLevels() {
    const old = el('editor-level-list').querySelectorAll('[data-draft-document]');
    for (const node of old) node.remove();
    if (!drafts.draft) return;
    for (const doc of Object.values(drafts.draft.documents)) {
      if (doc.kind !== 'level' || doc.level_id) continue;
      const li = document.createElement('li');
      li.dataset.draftDocument = doc.id;
      const btn = document.createElement('button');
      btn.type = 'button'; btn.className = 'editor-item editor-level-item';
      btn.classList.toggle('is-active', drafts.doc?.id === doc.id);
      btn.innerHTML = '<span class="level-num" aria-hidden="true">＋</span><span class="level-text"><span class="editor-item-name"></span><span class="level-chips"><span class="chip-sm is-warn">черновик</span></span></span>';
      btn.querySelector('.editor-item-name').textContent = doc.params.title || 'Новое задание';
      btn.addEventListener('click', () => void openDraftSelection(drafts.draft.id, doc.id).catch((e) => toast(e.message, true)));
      li.append(btn); el('editor-level-list').append(li);
    }
    el('editor-levels-block').hidden = false;
  }

  // ---------------------------------------------------------------- mode

  // setMode swaps the whole page between the chat and the editor. The two views
  // share the sidebar and the window, so the switch is a body attribute the
  // stylesheet reads rather than a re-render.
  async function setMode(mode) {
    const previousMode = ed.mode;
    if (mode !== ed.mode && !(await confirmDiscard())) return;
    ed.mode = mode === 'editor' ? 'editor' : 'chat';
    document.body.dataset.mode = ed.mode;
    localStorage.setItem(MODE_KEY, ed.mode);
    for (const tab of document.querySelectorAll('.mode-tab')) {
      const active = tab.dataset.mode === ed.mode;
      tab.classList.toggle('is-active', active);
      tab.setAttribute('aria-selected', active ? 'true' : 'false');
    }
    if (ed.mode === 'editor' && !ed.booted) {
      ed.booted = true;
      ed.booting = bootEditor();
    }
    syncRoute();
    // The caller may need the editor to be usable before it acts on it; a mode
    // switch that changes nothing resolves immediately.
    await (ed.booting ?? Promise.resolve());
    if (ed.mode === 'editor') {
      const linked = window.dzzzrChat.activeDraft?.();
      if (previousMode === 'chat' && linked && linked !== drafts.draft?.id) await openDraftSelection(linked);
      else await drafts.flush();
    }
  }

  // ---------------------------------------------------------------- route

  // The address names what is on screen: #/chat, #/editor,
  // #/editor/game/4242, #/editor/game/4242/level/7, with «new» in place of an
  // id for something being composed. A reload, a bookmark, the back button
  // and «dzzzr editor» all come back to the same place through it.
  const ROUTE = /^#\/(chat|editor)(?:\/game\/(\d+|new)(?:\/level\/(\d+|new))?)?\/?$/;

  // parseRoute reads an address, or returns null for one that is not a route.
  function parseRoute(hash) {
    const m = ROUTE.exec(hash || '');
    if (!m) return null;
    const id = (v) => (v == null ? null : v === 'new' ? 'new' : Number(v));
    return { hash, mode: m[1], game: id(m[2]), level: m[1] === 'editor' ? id(m[3]) : null };
  }

  // routeOf spells the current state as an address.
  function routeOf() {
    if (ed.mode !== 'editor') return '#/chat';
    // A link waiting for the login keeps its address, so a reload before
    // signing in still leads where the link pointed.
    if (ed.pendingRoute && !ed.admin?.has_admin) return ed.pendingRoute.hash;
    let hash = '#/editor';
    if (ed.gameID != null) hash += `/game/${ed.gameID}`;
    else if (ed.newGame) return `${hash}/game/new`;
    else return hash;
    if (ed.tab === 'level') hash += `/level/${ed.levelID ?? 'new'}`;
    return hash;
  }

  // syncRoute writes the current state into the address. Each settled step
  // gets its own history entry, so «назад» retraces what the author did;
  // replace overwrites the entry instead, for corrections nobody chose.
  function syncRoute(replace = false) {
    if (ed.routing > 0) return;
    const hash = routeOf();
    ed.routeHash = hash;
    if (hash === location.hash) return;
    if (replace) history.replaceState(null, '', hash);
    else history.pushState(null, '', hash);
  }

  // applyRoute brings the page to what an address names. Every step goes
  // through the same functions a click does, so the unsaved-edits question
  // is asked here too; a refused step leaves the page where it was and the
  // address is put back to match it.
  async function applyRoute(route) {
    ed.routing++;
    try {
      await setMode(route.mode);
      if (route.mode !== 'editor') return;
      if (route.game != null && !ed.admin?.has_admin) {
        // The games cannot be read yet; the login finishes the trip.
        ed.pendingRoute = route;
        return;
      }
      ed.pendingRoute = null;
      if (route.game === 'new') {
        if (!ed.newGame || ed.gameID != null) await createGame();
        return;
      }
      if (route.game == null) {
        if (ed.gameID != null || ed.newGame) await clearGame();
        return;
      }
      if (route.game !== ed.gameID) {
        // An address outlives what it names: a deleted game or one of another
        // city would otherwise open as an empty form that «Сохранить» writes
        // into nothing.
        if (!ed.gamesError && !ed.games.some((g) => g.id === route.game)) {
          toast(`Игры ${route.game} нет в списке организатора`, true);
          return;
        }
        if (!(await selectGame(route.game))) return;
      }
      if (typeof route.level === 'number' && !ed.levelsError && !ed.levels.some((l) => l.id === route.level)) {
        toast(`Уровня ${route.level} нет в игре ${route.game}`, true);
        if (ed.tab !== 'game' && await confirmDiscard()) await setTab('game');
        return;
      }
      if (route.level === 'new') {
        if (ed.levelID != null || ed.tab !== 'level') await createLevel();
      } else if (route.level != null) {
        if (route.level !== ed.levelID || ed.tab !== 'level') await selectLevel(route.level);
      } else if (ed.tab !== 'game' && await confirmDiscard()) {
        await setTab('game');
      }
    } finally {
      ed.routing--;
      syncRoute(true);
    }
  }

  // onRouteEvent follows the back and forward buttons and a hand-edited
  // address. The browser may report one navigation as both popstate and
  // hashchange, and the page's own writes as neither; routeHash sorts both
  // out.
  function onRouteEvent() {
    if (location.hash === ed.routeHash) return;
    ed.routeHash = location.hash;
    void applyRoute(parseRoute(location.hash) ?? { mode: 'chat', game: null, level: null });
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
    if (node) node.hidden = false;
    if (on) drafts.schedule();
    scheduleStatus();
  }

  // confirmDiscard guards every path that rebuilds the form from what the
  // engine last said. Without it the «Есть несохранённые правки» banner warned
  // about a loss and was then cleared as part of causing it: switching tabs or
  // picking another level threw the work away without a word.
  async function confirmDiscard() {
    try { await drafts.flush(); return true; }
    catch (e) { toast(e.message, true); return false; }
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
  // «codes[2].danger is required» reads as «Коды, строка 3 — «КО»:
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
    const row = el('organizer-session');
    const status = el('admin-auth-status');
    const signed = !!ed.admin?.has_admin;
    el('btn-admin-login-toggle').hidden = signed;
    el('btn-admin-logout').hidden = !signed;
    row.classList.toggle('is-online', signed && !ed.adminError);
    row.classList.toggle('is-error', !!ed.adminError);
    if (ed.adminError) {
      status.textContent = ed.adminError;
      status.title = ed.adminError;
    } else if (signed) {
      const games = ed.games.length ? ` · ${ed.games.length} ${plural(ed.games.length, 'игра', 'игры', 'игр')}` : '';
      status.textContent = `${ed.admin.login || 'организатор'}${games}`;
      status.title = `${ed.admin.city}: организатор ${ed.admin.login}`;
    } else {
      status.textContent = 'не выполнен вход';
      status.title = `${ed.admin?.city ?? '—'}: организатор не задан`;
    }
    if (signed) closeAuthPopovers();
    el('editor-games-title').textContent = ed.admin?.city ? `Игры · ${ed.admin.city}` : 'Игры';
    el('editor-levels-block').hidden = !signed || ed.gameID == null;
    renderBody();
  }

  // renderBody picks what the main area shows: the login invitation, the
  // «pick a game» hint, or the form. The form only appears once there is
  // something for «Сохранить» to mean.
  function renderBody() {
    const signed = !!ed.admin?.has_admin;
    const editing = signed && (ed.gameID != null || ed.newGame);
    el('editor-empty').hidden = signed;
    el('editor-pick').hidden = !signed || editing;
    el('editor-sheet').hidden = !editing;
    el('editor-actions').hidden = !editing;
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
      renderAdminAuth();
      if (ed.pendingRoute) await applyRoute(ed.pendingRoute);
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
      ed.newGame = false;
      ed.original.game = {};
      resetLevelState();
      gameActionsEnabled(false);
      renderGameList();
      el('editor-form').innerHTML = '';
      renderSections();
      resetTitle();
      markDirty(false);
      renderAdminAuth();
      syncRoute();
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
      btn.className = 'editor-item editor-game-item';
      btn.classList.toggle('is-active', g.id === ed.gameID);
      const name = document.createElement('span');
      name.className = 'editor-item-name';
      name.textContent = g.name || `Игра ${g.id}`;
      const meta = document.createElement('span');
      meta.className = 'editor-item-meta';
      meta.textContent = [`№${g.id}`, g.date, g.status].filter(Boolean).join(' · ');
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

  // selectGame reports whether the game was opened: the author may refuse to
  // drop unsaved edits.
  async function selectGame(gameID) {
    if (!(await confirmDiscard())) return false;
    await drafts.detach();
    documentForLevel = null;
    ed.gameID = gameID;
    ed.newGame = false;
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
    await setTab('game');
    await loadLevels();
    return true;
  }

  function resetTitle() {
    renderHeading();
  }

  // renderHeading spells where the author is: the game in the breadcrumb, the
  // game or the level as the title, and the level's number on its tab.
  function renderHeading() {
    const game = ed.games.find((x) => x.id === ed.gameID);
    const gameName = game?.name || (ed.gameID != null ? `Игра ${ed.gameID}` : ed.newGame ? 'Новая игра' : '');
    const level = ed.levelID != null ? ed.levels.find((l) => l.id === ed.levelID) : null;
    const levelLabel = level ? `Уровень ${level.order}` : ed.levelID != null ? `Уровень ${ed.levelID}` : 'Новый уровень';
    let crumb = 'Ручная заливка игр, уровней и кодов';
    let title = 'Редактор';
    if (gameName) {
      if (ed.tab === 'level') {
        crumb = levelLabel;
        title = level ? `${level.order}. ${level.title || 'без названия'}` : levelLabel;
      } else {
        crumb = ed.newGame ? 'Название, дата и время обязательны' : 'Параметры игры';
        title = ed.gameID != null ? `${gameName} · №${ed.gameID}` : gameName;
      }
    }
    el('editor-crumb-game').textContent = gameName || 'Редактор';
    el('editor-subtitle').textContent = crumb;
    el('editor-title').textContent = title;
    const tab = el('editor-tab-level');
    tab.textContent = tab.disabled ? 'Уровень' : levelLabel;
  }

  // clearGame closes the open game without opening another, which is where
  // #/editor leads back to.
  async function clearGame() {
    if (!(await confirmDiscard())) return;
    await drafts.detach();
    ed.gameID = null;
    ed.newGame = false;
    ed.original.game = {};
    resetLevelState();
    renderGameList();
    el('editor-levels-block').hidden = true;
    gameActionsEnabled(false);
    resetTitle();
    await setTab('game');
  }

  async function createGame() {
    if (!(await confirmDiscard())) return;
    ed.gameID = null;
    await drafts.detach();
    documentForLevel = null;
    ed.newGame = true;
    ed.gamesError = '';
    ed.original.game = {};
    resetLevelState();
    renderGameList();
    el('editor-levels-block').hidden = true;
    gameActionsEnabled(false);
    await setTab('game');
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
      ed.newGame = false;
      ed.original.game = {};
      resetLevelState();
      el('editor-levels-block').hidden = true;
      gameActionsEnabled(false);
      resetTitle();
      await setTab('game');
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
    renderDraftLevels();
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
      btn.className = 'editor-item editor-level-item';
      btn.classList.toggle('is-active', l.id === ed.levelID && ed.tab === 'level');
      const num = document.createElement('span');
      num.className = 'level-num';
      num.textContent = String(l.order ?? '·');
      const text = document.createElement('span');
      text.className = 'level-text';
      const name = document.createElement('span');
      name.className = 'editor-item-name';
      name.textContent = l.title || 'без названия';
      const chips = document.createElement('span');
      chips.className = 'level-chips';
      const chip = (label, kind) => {
        const c = document.createElement('span');
        c.className = `chip-sm${kind ? ` is-${kind}` : ''}`;
        c.textContent = label;
        chips.appendChild(c);
      };
      const codes = Array.isArray(l.codes) ? l.codes.length : 0;
      if (l.kind) chip(l.kind, /бонус|сквоз|bonus/i.test(l.kind) ? 'bonus' : 'kind');
      if (codes) chip(`${codes} ${plural(codes, 'код', 'кода', 'кодов')}`);
      if (!l.published) chip('черновик', 'warn');
      text.append(name, chips);
      btn.append(num, text);
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

      for (const b of [up, down, del]) b.className = 'icon-btn icon-btn-sm';
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
    el('editor-levels-title').textContent = ed.levels.length ? `Уровни · ${ed.levels.length}` : 'Уровни';
    renderHeading();
  }

  async function selectLevel(levelID) {
    if (!(await confirmDiscard())) return false;
    await drafts.flush();
    documentForLevel = null;
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
    await setTab('level');
    return true;
  }

  async function createLevel() {
    if (ed.gameID == null && !ed.newGame) {
      toast('Сначала выберите игру', true);
      return;
    }
    if (!(await confirmDiscard())) return;
    documentForLevel = null;
    ed.levelID = null;
    ed.original.level = {};
    el('editor-tab-level').disabled = false;
    renderLevelList();
    await setTab('level');
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
        await setTab('game');
      }
      await loadLevels();
    } catch (e) {
      toast(`Удаление: ${e.message || String(e)}`, true);
    }
  }

  // ---------------------------------------------------------------- form

  async function setTab(tab) {
    el('editor-form').inert = true;
    await drafts.pause();
    ed.tab = tab;
    formValues = null;
    for (const node of document.querySelectorAll('.editor-tab')) {
      const active = node.dataset.target === tab;
      node.classList.toggle('is-active', active);
      node.setAttribute('aria-selected', active ? 'true' : 'false');
    }
    renderForm();
    try {
      await openDocument();
    } catch (e) {
      if (drafts.doc) {
        ed.gameID = drafts.draft.game_id || null;
        ed.newGame = !drafts.draft.game_id;
        ed.tab = drafts.doc.kind;
        ed.levelID = drafts.doc.level_id || null;
        await drafts.attach(drafts.draft, drafts.doc.id);
      }
      toast(`Черновик не открыт: ${e.message}`, true);
    } finally { el('editor-form').inert = false; }
    renderBody();
    setEditorIssues([]);
    markDirty(false);
    renderLevelList();
    renderDraftLevels();
    syncRoute();
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
    if (spec) {
      const values = formValues ?? currentValues() ?? {};
      (spec.form?.groups ?? []).forEach((group, index) => form.appendChild(renderGroup(group, values, index)));
    }
    refreshStatus();
  }

  // collapsed remembers the sections an author folded away, per tab, so a
  // re-render from the shared draft does not unfold them again.
  const collapsed = new Set();

  function renderGroup(group, values, index) {
    const key = `${ed.tab}:${group.title}`;
    const box = document.createElement('section');
    box.className = 'editor-group';
    box.id = `editor-group-${index}`;
    box.dataset.title = group.title;
    const head = document.createElement('button');
    head.type = 'button';
    head.className = 'editor-group-head';
    head.innerHTML = '<span class="editor-group-title"></span><span class="editor-group-summary"></span><span class="editor-group-badge" hidden></span><span class="editor-group-chevron" aria-hidden="true"></span>';
    head.querySelector('.editor-group-title').textContent = group.title;
    const body = document.createElement('div');
    body.className = 'editor-group-body';
    body.id = `editor-group-${index}-body`;
    head.setAttribute('aria-controls', body.id);
    const setOpen = (open) => {
      box.classList.toggle('is-collapsed', !open);
      head.setAttribute('aria-expanded', open ? 'true' : 'false');
      head.querySelector('.editor-group-chevron').textContent = open ? '▾' : '▸';
      if (open) collapsed.delete(key);
      else collapsed.add(key);
    };
    head.addEventListener('click', () => setOpen(box.classList.contains('is-collapsed')));
    box.openGroup = () => setOpen(true);
    const grid = document.createElement('div');
    grid.className = 'editor-grid';
    for (const field of group.fields ?? []) {
      grid.appendChild(renderField(field, values[field.name]));
    }
    body.appendChild(grid);
    box.append(head, body);
    setOpen(!collapsed.has(key));
    return box;
  }

  // ------------------------------------------------------------ status

  // baseline is what the engine last said, read back through the same
  // controls the author edits, so «изменено» compares like with like: an
  // untouched field never shows as changed just because the engine spells a
  // number as a string.
  let baseline = null;
  let baselineSource = null;
  let baselineTab = null;
  // serverIssues are the validator's findings, kept until the next check.
  let serverIssues = [];
  let serverIssuesKind = 'err';
  let draftStatus = '';
  let statusFrame = 0;

  function baselineValues() {
    const source = currentValues() ?? {};
    if (baseline && baselineSource === source && baselineTab === ed.tab) return baseline;
    baseline = {};
    for (const group of currentSpec()?.form?.groups ?? []) {
      for (const field of group.fields ?? []) {
        baseline[field.name] = readField(renderField(field, source[field.name]));
      }
    }
    baselineSource = source;
    baselineTab = ed.tab;
    return baseline;
  }

  function plural(n, one, few, many) {
    const d = n % 10;
    const dd = n % 100;
    if (d === 1 && dd !== 11) return one;
    if (d >= 2 && d <= 4 && (dd < 12 || dd > 14)) return few;
    return many;
  }

  function isEmptyValue(v) {
    if (v == null || v === '' || v === false) return true;
    if (Array.isArray(v)) return v.every((x) => isEmptyValue(x) || (typeof x === 'object' && !Object.keys(x).length));
    return false;
  }

  function scheduleStatus() {
    if (statusFrame) return;
    statusFrame = requestAnimationFrame(() => {
      statusFrame = 0;
      refreshStatus();
    });
  }

  // DUP_CHECKED are the tables whose codes the engine refuses to repeat.
  const DUP_CHECKED = new Set(['codes', 'bonus_codes', 'fake_codes']);

  // checkDuplicates marks a code typed twice in one table. The engine would
  // refuse the level at «Сохранить»; saying so while the author types is
  // cheaper than a round trip.
  function checkDuplicates() {
    const found = [];
    for (const wrap of el('editor-form').querySelectorAll('.editor-field')) {
      if (!DUP_CHECKED.has(wrap.dataset.type)) continue;
      const seen = new Map();
      [...wrap.querySelectorAll('.editor-row-body > .editor-row')].forEach((row, i) => {
        const code = row.querySelector('[data-col="code"]')?.value.trim().toLowerCase() ?? '';
        const note = row.querySelector('.editor-row-note');
        const first = code ? seen.get(code) : undefined;
        row.classList.toggle('is-dup', first !== undefined);
        if (note) {
          note.hidden = first === undefined;
          note.textContent = first === undefined ? '' : `Код совпадает со строкой ${first + 1} — движок не примет дубликат`;
        }
        if (first !== undefined) {
          found.push({ field: wrap.dataset.field, text: `${fieldLabel(wrap.dataset.field) ?? wrap.dataset.field}, строка ${i + 1}: дубликат кода` });
        } else if (code) {
          seen.set(code, i);
        }
      });
    }
    return found;
  }

  // refreshStatus recomputes everything that describes the form rather than
  // being part of it: changed fields, section summaries and dots, the issue
  // chips and the draft line in the footer.
  function refreshStatus() {
    const form = el('editor-form');
    const base = baselineValues();
    const local = checkDuplicates();
    const issueCount = new Map();
    for (const issue of [...serverIssues, ...local]) {
      if (issue.field) issueCount.set(issue.field, (issueCount.get(issue.field) ?? 0) + 1);
    }
    let changedTotal = 0;
    const sections = [];
    for (const box of form.querySelectorAll('.editor-group')) {
      let total = 0;
      let filled = 0;
      let changed = 0;
      let errors = 0;
      let rows = 0;
      let hasRows = false;
      for (const wrap of box.querySelectorAll('.editor-field')) {
        const name = wrap.dataset.field;
        const value = readField(wrap);
        const isChanged = JSON.stringify(value) !== JSON.stringify(base[name] ?? null);
        wrap.classList.toggle('is-changed', isChanged);
        const errs = issueCount.get(name) ?? 0;
        wrap.classList.toggle('has-issue', errs > 0);
        total++;
        if (isChanged) changed++;
        if (!isEmptyValue(value)) filled++;
        if (ROW_COLUMNS[wrap.dataset.type]) {
          hasRows = true;
          rows += value.length;
        }
        errors += errs;
      }
      changedTotal += changed;
      const state = errors ? 'err' : changed ? 'edit' : filled ? 'ok' : 'empty';
      const parts = [hasRows ? `${rows} ${plural(rows, 'строка', 'строки', 'строк')}` : `заполнено ${filled} из ${total}`];
      if (changed) parts.push(`изменено ${changed}`);
      box.querySelector('.editor-group-summary').textContent = parts.join(' · ');
      const badge = box.querySelector('.editor-group-badge');
      badge.hidden = !errors;
      badge.textContent = `${errors} ${plural(errors, 'ошибка', 'ошибки', 'ошибок')}`;
      box.dataset.state = state;
      sections.push({ box, title: box.dataset.title, state, meta: errors ? String(errors) : hasRows ? String(rows) : `${filled}/${total}` });
    }
    renderSections(sections);
    renderIssueChips(local);
    renderDirty(changedTotal);
  }

  function renderSections(sections = []) {
    const list = el('editor-sections');
    list.innerHTML = '';
    for (const s of sections) {
      const li = document.createElement('li');
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = `editor-section-link is-${s.state}`;
      btn.innerHTML = `<span class="status-dot is-${s.state}" aria-hidden="true"></span><span class="editor-section-title"></span><span class="editor-section-meta"></span>`;
      btn.querySelector('.editor-section-title').textContent = s.title;
      btn.querySelector('.editor-section-meta').textContent = s.meta;
      btn.addEventListener('click', () => {
        s.box.openGroup?.();
        s.box.scrollIntoView({ block: 'start', behavior: 'smooth' });
      });
      li.appendChild(btn);
      list.appendChild(li);
    }
  }

  // lastChanged is the count refreshStatus last saw, so a draft status that
  // arrives on its own does not wipe «N правок» from the footer.
  let lastChanged = 0;

  function renderDirty(changed = lastChanged) {
    lastChanged = changed;
    const node = el('editor-dirty');
    if (!node) return;
    const failed = /не синхронизирован|Не удалось/.test(draftStatus);
    const dot = node.querySelector('.status-dot');
    dot.className = `status-dot ${failed ? 'is-err' : changed ? 'is-edit' : 'is-ok'}`;
    node.classList.toggle('is-err', failed);
    node.classList.toggle('is-edit', !failed && changed > 0);
    const count = changed ? `${changed} ${plural(changed, 'правка', 'правки', 'правок')}` : '';
    el('editor-dirty-text').textContent = failed
      ? draftStatus
      : count
        ? `Черновик · ${count}`
        : draftStatus || 'Совпадает с сохранённой игрой';
    node.title = draftStatus;
  }

  // setEditorIssues keeps what the validator (or a failed save) said. Each
  // line becomes a chip in the footer that leads to its field.
  function setEditorIssues(lines, kind = 'err') {
    serverIssuesKind = kind;
    serverIssues = (lines ?? []).map((line) => {
      const m = ISSUE_PATH.exec(line);
      // Some findings name the field mid-sentence — «duplicate code … in
      // codes[0]» — so the first known «поле[i]» anywhere in the line counts.
      const inside = [...line.matchAll(/\b([a-z_]+)\[\d+\]/g)].map((x) => x[1]).find((name) => fieldLabel(name));
      const field = m && fieldLabel(m[1]) ? m[1] : inside ?? null;
      return { text: kind === 'ok' ? line : humanizeIssue(line), field };
    });
    refreshStatus();
  }

  // MAX_CHIPS keeps the footer on one line; the rest fold behind «ещё N».
  const MAX_CHIPS = 3;

  function renderIssueChips(local) {
    const node = el('editor-issues');
    node.innerHTML = '';
    const ok = serverIssuesKind === 'ok' && serverIssues.length > 0;
    const issues = ok ? local : [...serverIssues, ...local];
    node.hidden = !ok && !issues.length;
    node.classList.toggle('is-ok', ok && !issues.length);
    node.classList.toggle('is-err', issues.length > 0);
    if (ok && !issues.length) {
      const chip = document.createElement('span');
      chip.className = 'issue-chip is-ok';
      chip.textContent = `✓ ${serverIssues[0].text}`;
      node.appendChild(chip);
      return;
    }
    const expanded = node.dataset.expanded === 'true';
    const shown = expanded ? issues : issues.slice(0, MAX_CHIPS);
    for (const issue of shown) {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'issue-chip';
      chip.textContent = issue.field ? `${issue.text} →` : issue.text;
      chip.title = issue.text;
      chip.disabled = !issue.field;
      chip.addEventListener('click', () => goToField(issue.field));
      node.appendChild(chip);
    }
    if (issues.length > MAX_CHIPS) {
      const more = document.createElement('button');
      more.type = 'button';
      more.className = 'issue-chip issue-chip-more';
      more.textContent = expanded ? 'свернуть' : `ещё ${issues.length - MAX_CHIPS}`;
      more.addEventListener('click', () => {
        node.dataset.expanded = expanded ? 'false' : 'true';
        refreshStatus();
      });
      node.appendChild(more);
    }
  }

  function goToField(name) {
    const wrap = el('editor-form').querySelector(`.editor-field[data-field="${name}"]`);
    if (!wrap) return;
    wrap.closest('.editor-group')?.openGroup?.();
    wrap.scrollIntoView({ block: 'center', behavior: 'smooth' });
    wrap.querySelector('input:not([type=file]),textarea:not([hidden]),[contenteditable="true"]:not([hidden]),select')?.focus({ preventScroll: true });
  }

  function renderField(field, value) {
    const wrap = document.createElement('div');
    wrap.className = `editor-field editor-field-${field.type}`;
    wrap.dataset.field = field.name;
    wrap.dataset.type = field.type;

    const label = document.createElement('label');
    label.className = 'editor-label';
    const labelText = document.createElement('span');
    labelText.className = 'editor-label-text';
    labelText.textContent = field.label || field.name;
    const badge = document.createElement('span');
    badge.className = 'field-badge';
    badge.textContent = 'изменено';
    label.append(labelText, badge);
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
        const box = document.createElement('span');
        box.className = 'editor-check-box';
        box.setAttribute('aria-hidden', 'true');
        label.prepend(input, box);
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

    // Many engine fields carry their label again as the hint; saying it twice
    // is noise.
    if (field.hint && field.hint !== field.label) {
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
    const hash = document.createElement('span');
    hash.textContent = '#';
    head.appendChild(hash);
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
    add.className = 'btn btn-xs btn-dashed';
    add.textContent = field.type === 'spoilers' ? '＋ Добавить спойлер' : '＋ Добавить код';
    add.addEventListener('click', () => {
      addRow({});
      markDirty(true);
      body.lastElementChild?.querySelector('input')?.focus();
    });
    tools.appendChild(add);

    // Bulk entry is the reason an author would pick this screen over dictating
    // the level to the agent: a sheet of codes pastes in as one block.
    const bulk = document.createElement('div');
    bulk.className = 'editor-bulk';
    bulk.hidden = true;
    const bulkToggle = document.createElement('button');
    bulkToggle.type = 'button';
    bulkToggle.className = 'btn btn-link btn-xs';
    bulkToggle.textContent = 'Вставить списком';
    bulkToggle.setAttribute('aria-expanded', 'false');
    const setBulkOpen = (open) => {
      bulk.hidden = !open;
      bulkToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
      bulkToggle.classList.toggle('is-active', open);
      if (open) area.focus();
    };
    bulkToggle.addEventListener('click', () => setBulkOpen(bulk.hidden));
    tools.appendChild(bulkToggle);

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
        setBulkOpen(true);
        const lines = text.split('\n').filter((l) => l.trim()).length;
        setIssues(report, [`Файл ${file.name}: строк ${lines}. Проверьте разбор и нажмите «Добавить» или «Заменить».`], 'ok');
        toast(`Загружено строк из файла: ${lines}`);
      } catch (e) {
        setIssues(report, [`Файл ${file.name}: ${e.message || String(e)}`]);
      }
    });

    const fromFile = document.createElement('button');
    fromFile.type = 'button';
    fromFile.className = 'btn btn-link btn-xs';
    fromFile.textContent = 'Из файла';
    fromFile.title = 'Загрузить коды из текстового файла или CSV';
    fromFile.addEventListener('click', () => picker.click());

    bulkActions.append(append, replace);
    bulk.append(bulkActions, report);
    tools.append(fromFile, picker);

    box.append(tools, bulk);
    return box;
  }

  function buildRow(cols, values) {
    const row = document.createElement('div');
    row.className = 'editor-row';
    const num = document.createElement('span');
    num.className = 'editor-row-num';
    num.setAttribute('aria-hidden', 'true');
    row.appendChild(num);
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
    drop.className = 'icon-btn icon-btn-sm editor-row-drop';
    const note = document.createElement('span');
    note.className = 'editor-row-note';
    note.hidden = true;
    row.append(drop, note);
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
      drop.className = 'icon-btn icon-btn-sm editor-row-drop';
      drop.textContent = '✕';
      drop.title = 'Убрать строку';
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
    add.className = 'btn btn-xs btn-dashed';
    add.textContent = '＋ строка';
    add.addEventListener('click', () => {
      addRow('');
      markDirty(true);
    });
    const tools = document.createElement('div');
    tools.className = 'editor-row-tools';
    tools.appendChild(add);
    box.appendChild(tools);
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
    el('editor-issues').dataset.expanded = 'false';
    try {
      const res = await api('/admin/validate', {
        method: 'POST',
        body: { kind: ed.tab, params: collectParams() },
      });
      if (res?.ok) {
        setEditorIssues(['Проверка пройдена: движок примет эти поля.'], 'ok');
      } else {
        const lines = res?.errors ?? [];
        setEditorIssues(lines.length ? lines : ['Проверка не пройдена']);
      }
      return !!res?.ok;
    } catch (e) {
      setEditorIssues([e.message || String(e)]);
      return false;
    }
  }

  async function saveCurrent() {
    el('btn-editor-save').disabled = true;
    for (const id of ['view-editor', 'editor-nav', 'mode-switch']) el(id).inert = true;
    try {
      const data = await drafts.publish();
      ed.gameID = data.game_id || null;
      ed.newGame = !data.game_id;
      ed.levelID = drafts.doc.level_id || null;
      gameActionsEnabled(!!data.game_id);
      syncRoute();
      await loadGames();
      await loadLevels();
      toast('Изменения сохранены в игру');
    } catch (e) {
      setEditorIssues([e.message]);
      toast(`Сохранение: ${e.message}`, true);
    } finally { el('btn-editor-save').disabled = false; for (const id of ['view-editor', 'editor-nav', 'mode-switch']) el(id).inert = false; }
  }

  function revertCurrent() {
    formValues = structuredClone(currentValues());
    renderForm();
    drafts.schedule();
    setEditorIssues([]);
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
    try {
      await drafts.flush();
      if (!drafts.draft) return;
      const chat = await api(`/admin/drafts/${drafts.draft.id}/chat`, { method: 'POST', body: {} });
      await window.dzzzrChat.openChat(chat.id);
    } catch (e) { toast(`Передача агенту: ${e.message}`, true); }
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
    await setTab('game');
  }

  function bindEditor() {
    for (const tab of document.querySelectorAll('.mode-tab')) {
      tab.addEventListener('click', () => {
        const action = tab.dataset.mode === 'chat' && drafts.draft ? askAgent() : setMode(tab.dataset.mode);
        void action.catch((e) => toast(e.message, true));
      });
    }
    for (const tab of document.querySelectorAll('.editor-tab')) {
      tab.addEventListener('click', async () => {
        if (tab.disabled || tab.dataset.target === ed.tab) return;
        if (!(await confirmDiscard())) return;
        await setTab(tab.dataset.target);
      });
    }
    el('admin-login-form').addEventListener('submit', onAdminLogin);
    el('editor-login-form').addEventListener('submit', onAdminLogin);
    const menu = el('editor-menu');
    const menuBtn = el('btn-editor-menu');
    const setMenu = (open) => {
      menu.hidden = !open;
      menuBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
      if (open) menu.querySelector('.menu-item:not(:disabled)')?.focus();
    };
    menuBtn.addEventListener('click', () => setMenu(menu.hidden));
    menu.addEventListener('click', (e) => {
      if (e.target.closest('.menu-item')) setMenu(false);
    });
    menu.addEventListener('keydown', (e) => {
      const items = [...menu.querySelectorAll('.menu-item:not(:disabled)')];
      const at = items.indexOf(document.activeElement);
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        const step = e.key === 'ArrowDown' ? 1 : -1;
        items[(at + step + items.length) % items.length]?.focus();
      } else if (e.key === 'Escape') {
        e.stopPropagation();
        setMenu(false);
        menuBtn.focus();
      }
    });
    document.addEventListener('click', (e) => {
      if (!menu.hidden && !e.target.closest('.menu-wrap')) setMenu(false);
    });
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
    el('import-file').addEventListener('change', (e) => void onImportFile(e));
    el('btn-import-validate').addEventListener('click', () => void validateScenario());
    el('btn-import-apply').addEventListener('click', () => void applyScenario());
    el('btn-import-close').addEventListener('click', () => el('import-dialog').close());
    el('editor-form').addEventListener('input', () => markDirty(true));
  }

  window.dzzzrEditor = { flushDraft: () => drafts.flush(), setMode, openScenarioFromChatFile, parseBulk, humanizeIssue, refreshAdmin: loadAdminStatus };

  setInterval(() => {
    if (ed.mode === 'editor' && drafts.doc && document.visibilityState === 'visible') {
      void drafts.flush().catch(() => {});
    }
  }, 2000);
  bindEditor();
  // The sidebar shows the organizer session in both views, so its status is
  // read on load rather than when the editor first opens.
  void loadAdminStatus().catch(() => {});
  // An address that names a view wins; a bare one returns to the view last
  // used, so an author who works in the editor is not sent to the chat by
  // every restart.
  const initial = parseRoute(location.hash) ?? {
    mode: localStorage.getItem(MODE_KEY) === 'editor' ? 'editor' : 'chat',
    game: null,
    level: null,
  };
  window.addEventListener('popstate', onRouteEvent);
  window.addEventListener('hashchange', onRouteEvent);
  void (async () => {
    const saved = localStorage.getItem(selectionKey);
    if (saved) {
      try {
        const selection = JSON.parse(saved);
        const data = await api(`/admin/drafts/${selection.draft}`);
        const doc = data.documents[selection.document];
        const sameLevel = initial.level === 'new' ? doc?.kind === 'level' && !doc.level_id : initial.level != null ? doc?.level_id === initial.level : doc?.kind === 'game';
        if (initial.game == null || initial.game === 'new' && !data.game_id || initial.game === data.game_id && sameLevel) {
          await setMode('editor');
          await openDraftSelection(selection.draft, selection.document);
          await setMode(initial.mode);
          return;
        }
      } catch (e) { toast(`Восстановление черновика: ${e.message}`, true); }
    }
    await applyRoute(initial);
  })();
})();
