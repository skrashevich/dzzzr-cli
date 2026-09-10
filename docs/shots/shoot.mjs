// Screenshots of the dzzzr web interface (`dzzzr web`).
//
// Expects a `dzzzr web` server already running and reachable at DZZR_WEB_URL,
// backed by dzzzr-mock and seeded with the chat fixtures under
// fixtures/chats/. docs/shots/render.sh wires all of that up; this file only
// drives the browser.
//
// Transient states (agent running, tool chips, the approval bar) are staged by
// calling the page's own top-level functions through page.evaluate, so the
// shots stay in sync with app.js instead of a hand-built mock of it.

import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const WEB_URL = (process.env.DZZR_WEB_URL || 'http://127.0.0.1:18191').replace(/\/$/, '');
const OUT = process.env.SHOTS_OUT || fileURLToPath(new URL('../screenshots', import.meta.url));
mkdirSync(OUT, { recursive: true });

const HERO_CHAT = '00000000000000a1';
const FILES_CHAT = '00000000000000a2';

const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  deviceScaleFactor: 2,
  colorScheme: 'light',
});
const page = await context.newPage();
page.setDefaultTimeout(15000);

const settle = (ms = 400) => page.waitForTimeout(ms);

async function open() {
  await page.goto(WEB_URL + '/', { waitUntil: 'load' });
  await page.waitForFunction(() => typeof state !== 'undefined' && document.body.classList.contains('is-ready'));
  // boot() auto-selects the most recent chat once the fixtures load.
  await page.waitForSelector('#chat-list .chat-item');
  await page.waitForFunction(() => !!state.activeId);
  await settle(700);
}

async function setTheme(theme) {
  await page.evaluate((t) => {
    document.documentElement.dataset.theme = t;
    try { localStorage.setItem('dzzzr-theme', t); } catch {}
  }, theme);
  await settle(250);
}

async function selectChat(id) {
  await page.evaluate((chatId) => switchChat(String(chatId)), id);
  await page.waitForFunction((chatId) => state.activeId === chatId, id);
  await settle(600);
}

async function shot(name) {
  await settle(300);
  await page.screenshot({ path: `${OUT}/${name}.png`, animations: 'disabled' });
  console.log('  ✓', `${name}.png`);
}

// 1 + 2: the main working view, both themes.
await open();
await selectChat(HERO_CHAT);
await setTheme('light');
await shot('overview');
await setTheme('dark');
await shot('overview-dark');
await setTheme('light');

// 3: the city's games, pulled live from the engine.
await page.click('#games-info > summary');
await page.waitForSelector('#games-list li');
await page.waitForFunction(
  () => !/Загрузка/.test(document.querySelector('#games-list')?.textContent || 'Загрузка'),
);
await shot('games');
await page.click('#games-info > summary');
await settle(200);

// 4: an agent turn in flight — status bar, running pill, tool chips, a
// streaming answer.
await page.evaluate(() => {
  state.agentRunning = true;
  refreshSendState();
  setAgentStatus('tool', 'Вызов инструмента: read_level');
  onToolStart({ name: 'read_level' });
  onToolDone({ name: 'read_level' });
  onToolStart({ name: 'send_code', args: 'code=(112)D46R93' });
  state.streamBuf =
    'Отправляю код `(112)D46R93` по сектору «Пустырь». Если движок ответит [9], уровень и вся игра закрыты — ';
  renderMessages();
});
await shot('agent-run');
await page.evaluate(() => {
  state.agentRunning = false;
  state.streamBuf = '';
  clearToolChips();
  clearAgentStatus();
  refreshSendState();
  renderMessages();
});

// 5: the approval gate under the "с согласованием" policy.
await page.evaluate(() => {
  state.agentRunning = true;
  refreshSendState();
  setAgentStatus('llm_wait', 'Агент ждёт согласования');
  showApprovalPrompt({
    tool: 'send_code',
    action: 'Отправить код на текущем уровне игры',
    args: 'code=(112)D46R93',
  });
});
await shot('approval');
await page.evaluate(() => {
  hideApprovalBar();
  state.agentRunning = false;
  clearAgentStatus();
  refreshSendState();
});

// 6: files the agent saved during a chat.
await selectChat(FILES_CHAT);
await page.waitForSelector('#session-files:not([hidden])');
await shot('session-files');

// 7: signed-out state — the login panel and the empty transcript hint.
await selectChat(HERO_CHAT);
await page.request.post(WEB_URL + '/api/v1/auth/logout', { data: {} });
await page.reload({ waitUntil: 'load' });
await page.waitForFunction(() => typeof state !== 'undefined' && document.body.classList.contains('is-ready'));
await page.waitForSelector('#login-form:not(.is-collapsed)');
await settle(600);
await shot('login');

await browser.close();
console.log('screenshots ->', OUT);
