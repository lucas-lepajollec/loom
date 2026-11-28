// Parcourt chaque page de l'interface sur un vrai binaire Loom, en format
// ordinateur et téléphone, avec les données d'une installation neuve puis avec
// une machine distante simulée (ancienne version, agents installés, moteur
// lié). Échoue si une page plante, lève une erreur ou laisse deux vues
// montées : les écrans noirs et dédoublés viennent d'un rendu interrompu.
// Usage : node tools/tests/ui-routes/crawl.mjs bin/loom
import { chromium, webkit, devices } from 'playwright';
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import net from 'node:net';
import { phoneLayoutIssues } from './layout.mjs';
import { phoneFixtures } from './phone-fixtures.mjs';
import { validData } from '../../../internal/loom/ui/next/js/core/shape.js';
import en from '../../../internal/loom/ui/next/js/i18n/en.js';
import fr from '../../../internal/loom/ui/next/js/i18n/fr.js';

const dictionary = process.env.CRAWL_LANG === 'fr' ? fr : en;

const binary = resolve(process.argv[2] || 'bin/loom');
const port = await new Promise((ok, reject) => { const s = net.createServer().on('error', reject).listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => ok(p)); }); }).catch(e => { console.error('UI route crawl cannot start the isolated server: ' + e.message); process.exit(1); });
const dir = mkdtempSync(join(tmpdir(), 'loom-ui-routes-'));
for (const d of ['home', 'data']) mkdirSync(join(dir, d));
const env = { ...process.env, HOME: join(dir, 'home'), XDG_CONFIG_HOME: join(dir, 'home/.config'), XDG_DATA_HOME: join(dir, 'home/.local/share'), LOOM_HOME: join(dir, 'data'), LOOM_WEB_HOST: '127.0.0.1' };
for (const key of ['PI_CODING_AGENT_DIR', 'HERMES_HOME', 'DSH_HOME', 'CODEX_HOME', 'CLAUDE_CONFIG_DIR', 'OPENCODE_CONFIG', 'OPENCODE_CONFIG_DIR', 'LOOM_NODE_HOME']) delete env[key];
const server = spawn(binary, ['web', String(port)], { env, stdio: ['ignore', 'ignore', 'pipe'] });
let serverLog = ''; server.stderr.on('data', d => { serverLog = (serverLog + d).slice(-4000); });
const base = `http://127.0.0.1:${port}/`;
const stop = () => { server.kill('SIGTERM'); rmSync(dir, { recursive: true, force: true }); };

const remote = { id: 'remote-box', name: 'remote-box', host: '192.0.2.10', os: 'linux', kind: 'node', version: '0.1.4', modules: ['engine', 'terminal', 'observe'] };
const mockRemote = async route => {
  const u = new URL(route.request().url());
  if (u.pathname === '/api/machines') return route.fulfill({ json: { ok: true, machines: [remote], offers: { [remote.id]: [] } } });
  if (u.pathname === '/api/engine/node') return route.fulfill({ json: { ok: true, remote: true, machine_id: remote.id, hostname: remote.name, url: 'http://192.0.2.10:2510', version: '0.1.4' } });
  if (u.pathname === '/api/machines/metrics') return route.fulfill({ json: { ok: true, metrics: { [remote.id]: { cpu: null } } } });
  return route.continue();
};

const failures = [];
try {
  for (let i = 0; ; i++) {
    try { if ((await fetch(base + 'api/ping')).ok) break; } catch (_) {}
    if (i > 100) throw new Error('Loom did not start:\n' + serverLog);
    await new Promise(r => setTimeout(r, 200));
  }
  await fetch(base + 'api/prefs', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ onboarded: '1', lang: process.env.CRAWL_LANG || 'en' }) });
  const ws = await (await fetch(base + 'api/workspace')).json();
  const runtimes = (ws.runtimes || []).map(r => r.id);
  let routes = ['sessions', 'inbox', 'more', 'more/jarvis', 'chat', 'models', 'cloud', 'local', 'engine', 'workspaces', 'terminals', 'local/hub', 'voice', 'jarvis',
    'machines', 'machines/local', 'machines/' + remote.id, 'machines/workspaces', 'machines/terminals', 'machines/environment',
    'files', 'usage', 'quotas', 'market', 'agents', 'agents/s/fixture-session-0', 'agents/cron', 'agents/settings', 'connections',
    'settings', 'settings/general', 'settings/internet', 'settings/startup', 'settings/notifications', 'settings/policy', 'settings/doctor', 'settings/security', 'settings/about'];
  if (process.env.CRAWL_ROUTES) routes = routes.filter(r => new RegExp(process.env.CRAWL_ROUTES).test(r));
  const fixtureSessions = ['cloud'].map(kind => ({ id: 'fixture-' + kind, title: 'Synthetic ' + kind + ' discussion', runtime_id: kind === 'cloud' ? 'openai-compatible' : 'fixture-agent', provider_id: 'fixture', provider_name: kind === 'cloud' ? 'Fixture cloud' : 'Fixture agent', model: 'fixture-model', status: 'idle', messages: [], turns: [], message_count: 1 }));
  const scenarios = ['fresh', 'healthy', 'phone-layout', '401', '502', 'malformed-json', 'wrong-shape', 'abort', 'offline', 'engine-unreachable'];
  const inspect = async (page, errors, label) => {
    const st = await page.evaluate(() => ({
      crash: (document.querySelector('.page-crash pre') || {}).textContent || '',
      guarded: !!document.querySelector('.page-crash'),
      bar: (document.querySelector('.client-error') || {}).textContent || '',
      views: document.querySelectorAll('.view').length,
    }));
    if (st.guarded) failures.push(`${label}: PageGuard: ${st.crash.split('\n')[0]}`);
    if (st.bar) failures.push(`${label}: ${st.bar}`);
    if (st.views !== 1) failures.push(`${label}: ${st.views} views mounted`);
    if (page.viewportSize().width <= 390) for (const issue of await page.evaluate(phoneLayoutIssues)) failures.push(`${label}: layout: ${issue}`);
    for (const e of errors.splice(0)) failures.push(`${label}: ${e}`);
  };
  const resume = page => page.evaluate(async () => {
    window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }));
    document.dispatchEvent(new Event('visibilitychange'));
    window.dispatchEvent(new Event('online'));
    window.dispatchEvent(new CustomEvent('loom:changed'));
    const s = await import('/next/js/core/state.js');
    await Promise.allSettled([s.refreshStatus(), s.refreshWorkspace(), s.refreshLibrary(), s.refreshNav(), s.refreshEngineNode(), s.refreshHardware()]);
  });
  const openPicker = async page => {
    const picker = page.locator('.exec-btn');
    if (await picker.count() && await picker.getAttribute('aria-expanded') !== 'true') await picker.click();
  };
  const checkTrigger = async (trigger, popup) => {
    await trigger.click();
    await popup.waitFor({ state: 'visible', timeout: 5000 });
    const sheet = await popup.evaluate(el => {
      const dialog = el.closest('.dialog'), scrim = dialog?.parentElement;
      const bounds = scrim?.getBoundingClientRect();
      return bounds ? { x: bounds.left + bounds.width / 2, y: bounds.top + 2 } : null;
    });
    if (sheet) await popup.page().mouse.click(sheet.x, sheet.y);
    else await trigger.click();
    await popup.waitFor({ state: 'detached', timeout: 5000 });
  };
  const swipe = (locator, from, to) => locator.evaluate((el, [start, end]) => {
    const touch = (type, point) => {
      const event = new Event(type, { bubbles: true, cancelable: true });
      const p = { clientX: point[0], clientY: point[1] };
      Object.defineProperties(event, { touches: { value: type === 'touchend' ? [] : [p] }, changedTouches: { value: [p] } });
      el.dispatchEvent(event);
    };
    touch('touchstart', start);
    touch('touchmove', end);
    touch('touchend', end);
  }, [from, to]);
  let checks = 0;
  // Chaque cas (moteur, format, scénario, page) tourne dans un contexte de
  // navigateur neuf : les cas sont indépendants, donc exécutés en parallèle
  // (CRAWL_CONCURRENCY, 4 par défaut) et répartissables entre machines CI
  // (CRAWL_SHARD=i/n, CRAWL_ENGINE=chromium|webkit).
  const runCase = async (browser, { name, form, context, scenario, r }) => {
    // Fresh browser state per route/scenario: guards, observations,
    // pending requests and service workers never leak between cases.
    // Service workers are blocked: once one controls the page, WebKit sends
    // requests through it and Playwright's route fixtures no longer apply.
    const ctx = await browser.newContext({ ...context, serviceWorkers: 'block' });
    const fixturePhone = phoneFixtures(ws, remote);
    let degraded = !['fresh', 'healthy', 'phone-layout'].includes(scenario), successfulReads = 0, injected = 0;
    const page = await ctx.newPage(), errors = [];
    if (process.env.CRAWL_BASELINE_UI) await page.route('**/next/**', route => {
      const pathname = new URL(route.request().url()).pathname.slice('/next/'.length);
      const root = resolve(process.env.CRAWL_BASELINE_UI), file = resolve(root, pathname);
      if (!file.startsWith(root + '/')) return route.abort();
      const ext = file.split('.').pop(), types = { js: 'text/javascript', mjs: 'text/javascript', css: 'text/css', svg: 'image/svg+xml', woff2: 'font/woff2' };
      try { return route.fulfill({ body: readFileSync(file), contentType: types[ext] || 'application/octet-stream' }); }
      catch (_) { return route.abort(); }
    });
    // La première ligne de pile située dans Loom indique d'où vient l'erreur.
    page.on('pageerror', e => errors.push(e.message + ((e.stack || '').split('\n').slice(1).find(l => /\/next\//.test(l))?.trim().replace(/^at /, ' @ ') || '')));
    page.on('response', async response => {
      const url = new URL(response.url());
      if (!url.pathname.startsWith('/api/') || url.pathname.startsWith('/api/auth/') || url.pathname === '/api/prefs' || response.request().method() !== 'GET') return;
      try { const data = await response.json(); if (response.ok() && data?.ok !== false && validData(data, url.pathname)) successfulReads++; } catch (_) {}
    });
    await page.route('**/api/**', async route => {
      const u = new URL(route.request().url());
      if (/^\/api\/(agents|harness|harness-history|runtimes)(\/|$)|^\/api\/(brain\/agents|skills\/targets|usage\/native)(\/|$)/.test(u.pathname)) {
        failures.push(`Removed integration requested by #/${r}: ${u.pathname}`);
        return route.abort();
      }
      if (/^\/api\/(chat\/send|runtime\/sessions\/send|load-model|start|restart)$/.test(u.pathname)) {
        failures.push('Crawl attempted generation or engine startup: ' + u.pathname);
        return route.abort();
      }
      // Let the access shell and onboarding preference identify this
      // isolated installation. Domain APIs are faulted independently.
      if (scenario === 'phone-layout' && !u.pathname.startsWith('/api/auth/') && u.pathname !== '/api/prefs') return fixturePhone(route);
      const boot = u.pathname.startsWith('/api/auth/') || u.pathname === '/api/prefs';
      const engineOnly = /^\/api\/(models|presets|preset|status|vram|ram|server|llamacpp|hub|backends|config|catalog|paths|naked|llama-flags|engines|engine\/(params|service|keys|auto-update)|model-caps)(\/|$)/.test(u.pathname);
      if (degraded && !boot && (scenario !== 'engine-unreachable' || engineOnly)) {
        injected++;
        if (scenario === 'abort' || scenario === 'offline') return route.abort('internetdisconnected');
        if (scenario === 'malformed-json') return route.fulfill({ status: 200, contentType: 'application/json', body: '{broken' });
        if (scenario === 'wrong-shape') return route.fulfill({ json: { ok: true, models: {}, presets: {}, sessions: {}, runtimes: {}, projects: {}, machines: {}, sources: {}, tasks: {}, servers: {}, installations: {} } });
        return route.fulfill({ status: scenario === '401' ? 401 : 502, json: { ok: false, error: 'remote engine unreachable' } });
      }
      if (scenario === 'engine-unreachable') {
        if (u.pathname === '/api/workspace') return route.fulfill({ json: { ...ws,
          runtimes: ws.runtimes,
          providers: [{ id: 'fixture', name: 'Fixture cloud', ready: true, models: ['fixture-model'] }],
          models: fixtureSessions.map(session => ({ id: session.id, name: session.model, model: session.model, kind: 'cloud', runtime_id: session.runtime_id, provider_id: session.provider_id, provider_name: session.provider_name, enabled: true, ready: true })),
        } });
        if (u.pathname === '/api/runtime/sessions' && route.request().method() === 'GET') {
          const session = fixtureSessions.find(s => s.id === u.searchParams.get('id'));
          if (session) return route.fulfill({ json: { ok: true, session, context: {} } });
          if (!u.searchParams.has('id')) return route.fulfill({ json: { ok: true, sessions: fixtureSessions } });
        }
        if (u.pathname === '/api/discussion/events') {
          const session = fixtureSessions.find(s => s.id === route.request().postDataJSON()?.id);
          if (session) {
            const events = [{ reset: true, replay: true, session }, { type: 'text_delta', text: 'Synthetic retained history', seq: 1 }, { caught_up: true, session }];
            return route.fulfill({ contentType: 'text/event-stream', body: events.map(delta => 'data: ' + JSON.stringify({ choices: [{ delta }] }) + '\n\n').join('') });
          }
        }
      }
      return scenario === 'fresh' ? route.continue() : mockRemote(route);
    });
    const label = `${name}/${form}/${scenario} #/${r}`;
    try {
      const phone = form !== 'desktop';
      await page.goto(r === 'sessions' && phone && scenario === 'fresh' ? base : base + '#/' + r);
      await page.waitForTimeout(600);
      if (r === 'connections' && scenario === 'phone-layout') {
        const mcp = key => dictionary['connections.mcp.' + key];
        const snapshot = await page.evaluate(async () => (await fetch('/api/mcp')).json());
        await page.locator('.cx-mcp-add').click();
        const dialog = page.getByRole('dialog', { name: mcp('add') });
        await dialog.getByRole('button', { name: mcp('paste') }).click();
        await dialog.getByLabel('mcpServers').fill(JSON.stringify({ mcpServers: { 'paste-fixture': { command: 'fixture', args: ['a value'] } } }));
        await dialog.getByRole('button', { name: mcp('prefill') }).click();
        if (await dialog.getByLabel(mcp('name')).inputValue() !== 'paste-fixture') throw new Error('MCP JSON did not prefill the manual form');
        await dialog.getByRole('tab', { name: mcp('registry') }).click();
        const searched = page.waitForRequest(req => new URL(req.url()).searchParams.get('q') === 'fixture');
        await dialog.getByLabel(mcp('registry_search')).fill('fixture');
        await searched;
        await dialog.getByRole('button', { name: mcp('configure') }).nth(1).waitFor();
        await inspect(page, errors, label + ' MCP registry');
        await dialog.getByRole('button', { name: mcp('configure') }).nth(1).click();
        if (await dialog.getByLabel(mcp('command'), { exact: true }).inputValue() !== 'npx' ||
          await dialog.getByLabel(mcp('args')).inputValue() !== '["-y","@example/fixture@1.2.3"]' ||
          await dialog.getByLabel(mcp('env')).inputValue() !== 'API_KEY=') throw new Error('Registry preset lost its command, argv or environment placeholder');
        await inspect(page, errors, label + ' MCP manual');
        await dialog.getByRole('tab', { name: mcp('import') }).click();
        await dialog.getByText('Fixture from Claude').waitFor();
        await dialog.getByRole('button', { name: dictionary['ui.dialog.annuler'] }).click();
        await page.locator('.cx-card').filter({ hasText: 'Home-assistant-with-a-very-long-server-name-for-phone-layout' }).click();
        const drawer = page.getByRole('dialog', { name: 'Home-assistant-with-a-very-long-server-name-for-phone-layout' });
        const hidden = drawer.getByRole('switch', { name: 'list_rooms' });
        await hidden.waitFor();
        if (await hidden.getAttribute('aria-checked') !== 'false') throw new Error('MCP drawer lost the disabled tool setting');
        await page.route('**/api/mcp/test', route => route.fulfill({ json: { ...snapshot, latency_ms: 31 } }));
        const checked = page.waitForRequest(req => new URL(req.url()).pathname === '/api/mcp/test');
        await drawer.getByRole('button', { name: mcp('test'), exact: true }).click();
        if ((await checked).postDataJSON()?.name !== snapshot.servers[0].name) throw new Error('MCP connection test targeted the wrong server');
        await drawer.getByRole('status').filter({ hasText: '31 ms' }).waitFor();
        await inspect(page, errors, label + ' MCP drawer');
        await page.keyboard.press('Escape');
      }
      // Exercise the collection consumer that triggered the iPhone
      // crash, and both other catalogs while the engine is unavailable.
      if (r === 'chat') {
        if (scenario === 'phone-layout') await checkTrigger(page.locator('.exec-btn'), page.locator('.picker'));
        await openPicker(page);
        await page.locator('.picker .seg button').nth(2).click();
      }
      if (r === 'chat' && scenario === 'engine-unreachable') {
        await page.keyboard.press('Escape');
        for (const session of fixtureSessions) {
          const opened = await page.evaluate(async id => (await import('/next/js/features/chat/engine.js')).open(id), session.id);
          if (!opened) throw new Error('Failed to open synthetic ' + session.id);
          await page.waitForFunction(async () => !(await import('/next/js/features/chat/engine.js')).chat.get().loading);
          await page.locator('.composer textarea').fill('Retained mobile draft');
          await resume(page);
          if (await page.locator('.composer textarea').inputValue() !== 'Retained mobile draft') throw new Error('Resume lost discussion draft');
          if (await page.locator('.composer .send').isDisabled()) throw new Error('Engine failure disabled external discussion composer');
          await inspect(page, errors, label + ' ' + session.id);
        }
      }
      if (scenario === 'offline') {
        await ctx.setOffline(true);
        await resume(page);
        await page.waitForTimeout(100);
      }
      if (scenario === 'phone-layout' && r === 'cloud') {
        await page.locator('.models-row button.btn.sm', { hasText: /^(Connecter|Connect)$/ }).first().click(); await page.locator('.dialog').waitFor();
        await inspect(page, errors, label + ' provider dialog'); await page.locator('.dialog-head .icon-btn').click();
      }
      if (scenario === 'phone-layout' && r === 'engine') {
        const catalog = page.locator('.engine-catalog');
        await catalog.locator('.engine-catalog-row').nth(8).waitFor();
        await catalog.getByRole('button', { name: dictionary['engines.filter.installed'], exact: true }).click();
        if (await catalog.locator('.engine-catalog-row').count() !== 2) throw new Error('Catalog filter did not select installed engines');
        await catalog.getByRole('button', { name: dictionary['engines.filter.all'], exact: true }).click();
        const ollama = catalog.locator('.engine-catalog-row').filter({ has: page.getByText('Ollama', { exact: true }) });
        await ollama.getByRole('button', { name: dictionary['settings.page.lier'], exact: true }).click();
        const address = page.locator('.eng-link input').first();
        await address.waitFor();
        if (await address.inputValue() !== 'http://127.0.0.1:11434') throw new Error('Detected engine address was not prefilled');
        await inspect(page, errors, label + ' engine link');
        await page.getByRole('button', { name: dictionary['settings.page.annuler'], exact: true }).click();

        const basalt = catalog.locator('.engine-catalog-row').filter({ has: page.getByText('Basalt', { exact: true }) });
        await basalt.getByText(dictionary['engines.hardware.blackwell'], { exact: true }).waitFor();
        await basalt.getByRole('button', { name: dictionary['engines.install_guide'], exact: true }).click();
        const basaltDrawer = page.getByRole('dialog', { name: 'Basalt', exact: true });
        const basaltGuide = basaltDrawer.getByRole('link', { name: dictionary['engines.install_guide'], exact: true });
        if (await basaltGuide.getAttribute('href') !== 'https://github.com/jesdga95/basalt/blob/main/docs/platforms.md') throw new Error('Wrong Basalt guide for Blackwell');
        await inspect(page, errors, label + ' Basalt guide');
        await basaltDrawer.locator('.dialog-head .icon-btn, .insp-head .icon-btn').click();
        await basaltDrawer.waitFor({ state: 'hidden' });

        const exo = catalog.locator('.engine-catalog-row').filter({ has: page.getByText('exo', { exact: true }) });
        await exo.getByRole('button', { name: dictionary['settings.page.lier'], exact: true }).click();
        await address.waitFor();
        if (await address.inputValue() !== 'http://127.0.0.1:52415') throw new Error('Detected exo address was not prefilled');
        await page.getByRole('button', { name: dictionary['settings.page.annuler'], exact: true }).click();

        const wiring = page.locator('.engine-harness');
        await wiring.locator('summary').click();
        await wiring.getByRole('button', { name: 'Codex', exact: true }).click();
        if (!(await wiring.locator('pre').textContent()).includes('<LOOM_API_KEY>')) throw new Error('Coding client snippet missing key placeholder');
        await inspect(page, errors, label + ' coding client snippets');
      }
      if (phone && scenario === 'phone-layout' && r === 'local') {
        const chips = page.locator('.phone-section-tabs a');
        await chips.last().waitFor();
        if (await chips.count() !== 4 || await chips.first().getAttribute('aria-current') !== 'page') throw new Error('Missing model sections');
        await page.getByRole('tab', { name: 'SGLang', exact: true }).click();
        await page.getByText('Qwen/Qwen3-8B', { exact: false }).last().waitFor();
        await inspect(page, errors, label + ' SGLang library');
        await page.getByRole('tab', { name: 'llama.cpp', exact: true }).click();
        const rows = page.locator('.models-compact-row');
        await rows.nth(1).waitFor();
        await checkTrigger(rows.first().locator('[aria-haspopup="menu"]'), page.getByRole('menu'));
        await rows.last().locator('[aria-haspopup="menu"]').click();
        await page.getByRole('menuitem', { name: dictionary['local.page.regler'], exact: true }).waitFor();
        await inspect(page, errors, label + ' model actions');
        await page.keyboard.press('Escape');
        // The menu sheet animates out; a click during it lands on the sheet.
        await page.getByRole('menu').waitFor({ state: 'detached' });
        await page.locator('.models-phone-download').click();
        await page.locator('.phone-section-tabs a[href="#/local/hub"][aria-current=page]').waitFor();
        await page.locator('.phone-burger').click();
        await page.locator('.phone-drawer').waitFor();
        await page.locator('.phone-drawer-item[href="#/sessions"]').click();
        await page.locator('.phone-tabs').waitFor();
      }
      if (scenario === 'phone-layout' && r === 'agents') {
        if (phone) {
          await page.locator('.cm-harness-list li').first().waitFor();
          await page.locator('.cm-next-runs li').first().waitFor();
        }
        const rows = page.locator('.cm-session-row');
        await rows.first().waitFor();
        if (await rows.count() !== 8) throw new Error('Agents overview should show eight live sessions');
        await page.locator('[aria-controls="cm-live-sessions"]').click();
        await rows.nth(9).waitFor({ state: 'attached' });
        if (await rows.count() !== 10) throw new Error('Agents overview did not expand the live list');
        const past = page.locator('.cm-history-link');
        await past.first().waitFor();
        await page.locator('[aria-controls="cm-resume-sessions"]').click();
        await past.nth(9).waitFor({ state: 'attached' });
        if (await past.count() !== 10) throw new Error('Agents overview did not expand the history list');
        await checkTrigger(page.locator('.cm-server-actions button[aria-expanded]'), page.getByRole('menu'));
        await page.evaluate(async () => (await import('/next/js/features/agents/store.js')).agents.set({ creating: {} }));
        await page.locator('.dialog .cm-cli-grid').waitFor();
        await inspect(page, errors, label + ' new agent session dialog');
        await page.locator('.dialog-head .icon-btn').click();
      }
      if (scenario === 'phone-layout' && r === 'agents/cron') {
        await page.locator('.cm-job-t').first().click();
        const schedule = page.locator('#cm-cron-schedule');
        await checkTrigger(schedule, page.getByRole('listbox'));
        await schedule.click();
        await page.getByRole('option', { name: dictionary['agents.cron.custom'], exact: true }).click();
        await page.locator('.cm-job-editor input[aria-describedby="cm-cron-expression-hint"]').fill('0 9 * * MON-FRI');
        const submit = page.locator(phone ? '.cm-job-sheet .dialog-foot button[type=submit]' : '.cm-editor-foot button[type=submit]');
        if (await submit.isDisabled()) throw new Error('A supported cron expression could not be saved');
        if (phone) {
          await inspect(page, errors, label + ' schedule editor sheet');
          await page.keyboard.press('Escape');
          await page.locator('.cm-job-sheet').waitFor({ state: 'detached' });
        }
      }
      if (phone && scenario === 'phone-layout' && r === 'machines') {
        await page.locator('.machine-add').click();
        await page.getByRole('dialog', { name: dictionary['machines.add.title'], exact: true }).waitFor();
        await inspect(page, errors, label + ' machine pairing sheet');
        const pairing = page.getByRole('dialog', { name: dictionary['machines.add.title'], exact: true });
        await pairing.locator('.dialog-head .icon-btn').click();
        await pairing.waitFor({ state: 'detached' });
        await page.locator('.machine-card-link[href="#/machines/' + remote.id + '"]').click();
        await page.locator('.machine-detail .machine-meters [role=meter]').first().waitFor();
        await checkTrigger(page.locator('.machine-detail-controls button[aria-haspopup=menu]'), page.getByRole('menu'));
        await inspect(page, errors, label + ' machine detail');
        await page.locator('.phone-back-link[href="#/machines"]').click();
        await page.locator('.machine-card-link').first().waitFor();
      }
      if (phone && scenario === 'phone-layout' && r === 'settings') {
        await page.locator('.phone-settings').waitFor();
        await inspect(page, errors, label + ' quick settings');
        await checkTrigger(page.getByRole('button', { name: dictionary['settings.language.label'], exact: true }), page.getByRole('listbox'));
        await page.locator('.phone-setting-link[href="#/settings/notifications"]').click();
        await page.locator('.phone-back-link[href="#/settings"]').click();
        await page.locator('.phone-settings').waitFor();
        await page.locator('.phone-burger').click();
        await page.locator('.phone-drawer').waitFor();
        await page.locator('.phone-drawer a[href="#/sessions"]').click();
        await page.locator('.phone-tabs a[href="#/sessions"][aria-current=page]').waitFor();
      }
      if (scenario === 'phone-layout' && r === 'agents/settings') {
        await page.locator('.cm-cfg-body .set-group').first().waitFor();
        const summaries = page.locator('.cm-cfg-body .set-fold > summary');
        if (await summaries.count()) {
          await summaries.first().click();
          await inspect(page, errors, label + ' session defaults');
          await page.locator('.cm-cfg-picker .select').click();
          await page.getByRole('option', { name: dictionary['agents.cfg.g.notifications'], exact: true }).click();
          await summaries.last().click();
          await page.locator('.cm-setting-json input[type=password]').waitFor();
        }
      }
      if (phone && scenario === 'phone-layout' && r === 'sessions') {
        const tabBar = page.locator('.phone-tabs');
        const tabs = await tabBar.locator('a').evaluateAll(items => items.map(a => a.getAttribute('href')));
        if (tabs.join(',') !== '#/sessions,#/inbox,#/files,#/machines,#/quotas') throw new Error('Wrong phone tabs or order');
        await tabBar.locator('.phone-badge', { hasText: '2' }).waitFor();
        await page.getByRole('tab', { name: dictionary['side.filter_chats'], exact: true }).click();
        if (await page.locator('.phone-session-row .logo-mark').count()) throw new Error('Discussion filter retained an agent');
        await page.getByRole('tab', { name: dictionary['mobile.all'], exact: true }).click();
        const refreshed = page.waitForRequest(req => req.url().endsWith('/api/chat/history'));
        await swipe(page.locator('.phone-session-list'), [100, 180], [100, 280]);
        await refreshed;
        await page.getByRole('button', { name: dictionary['app.palette.rechercher'], exact: true }).click();
        const search = page.locator('.palette');
        await search.waitFor();
        if (await tabBar.isVisible()) throw new Error('Tab bar is visible behind the search sheet');
        await swipe(search.locator('.sheet-grab'), [100, 530], [100, 640]);
        await search.waitFor({ state: 'detached' });
        await page.getByRole('button', { name: dictionary['side.new_session'], exact: true }).click();
        const newSession = page.locator('.phone-new-session');
        await newSession.getByRole('tab', { name: dictionary['side.type_agent'], exact: true }).click();
        await newSession.locator('.cm-cli-grid').waitFor();
        await newSession.getByRole('textbox', { name: dictionary['mobile.first_message'] }).waitFor();
        await newSession.getByRole('button', { name: dictionary['mobile.session_folder'], exact: true }).click();
        const folders = page.getByRole('listbox', { name: dictionary['mobile.session_folder'], exact: true });
        await folders.getByRole('option').last().click();
        await folders.waitFor({ state: 'detached' });
        await inspect(page, errors, label + ' phone launch sheet');
        await newSession.getByRole('tab', { name: dictionary['side.type_chat'], exact: true }).click();
        await newSession.getByRole('button', { name: dictionary['agents.new.start'], exact: true }).click();
        await page.waitForFunction(() => document.activeElement === document.querySelector('.phone-composer textarea'));
        const aboveBottom = await page.locator('.phone-composer').evaluate(el => el.parentElement.getBoundingClientRect().bottom - el.getBoundingClientRect().bottom);
        if (aboveBottom > 2) throw new Error('An empty discussion should keep its composer at the bottom');
        if (await tabBar.isVisible()) throw new Error('Tab bar is visible in a discussion');
        await swipe(page.locator('.chat-view'), [10, 180], [105, 182]);
        await page.locator('.phone-session-list').waitFor();
      }
      if (phone && scenario === 'phone-layout' && r === 'inbox') {
        await page.evaluate(() => { location.hash = '#/inbox/fixture-approval'; });
        const selected = page.locator('.inbox-row[data-current]');
        await selected.waitFor();
        await page.waitForFunction(() => document.activeElement === document.querySelector('.inbox-row[data-current]'));
        await page.locator('.inbox-completed').waitFor();
        await page.locator('.cm-restore').waitFor();
        // The inbox only routes: the answer is given in the conversation.
        await selected.click();
        const tray = page.locator('.cm-conv .cv-tray');
        await tray.waitFor();
        const approval = page.waitForRequest(req => req.url().endsWith('/approvals/fixture-approval/answer'));
        await tray.getByRole('button', { name: dictionary['agents.approval.deny'], exact: true }).click();
        if ((await approval).postDataJSON().action !== 'deny') throw new Error('Phone approval used the wrong action');
        await tray.waitFor({ state: 'detached' });
        await page.evaluate(() => { location.hash = '#/inbox'; });
        await page.locator('.inbox-row').first().click();
        const composer = page.locator('.cm-conv .composer textarea');
        await composer.fill('Review the test report');
        const prompt = page.waitForRequest(req => req.url().endsWith('/sessions/fixture-session-4/input'));
        await page.locator('.cm-conv .cm-send').click();
        if ((await prompt).postDataJSON().input !== 'Review the test report\r') throw new Error('Reply from the conversation did not deliver the draft');
      }
      if (phone && scenario === 'phone-layout' && r === 'agents/s/fixture-session-0') {
        await page.locator('.cm-conv .cv-tray').waitFor();
        await page.getByRole('tab', { name: dictionary['agents.view.terminal'], exact: true }).click();
        await page.locator('.cm-line-keys').click();
        await page.locator('.cm-acc').getByRole('button', { name: dictionary['mobile.key_ctrl_c'], exact: true }).waitFor();
        await inspect(page, errors, label + ' terminal');
        await page.getByRole('tab', { name: dictionary['agents.view.chat'], exact: true }).click();
        await page.locator('.phone-session-head button').last().click();
        await page.locator('.drawer-sheet .cm-panel').waitFor();
        await inspect(page, errors, label + ' agent inspector');
      }
      if (phone && scenario === 'phone-layout' && r === 'more') {
        await page.locator('.more-links a[href="#/files"]').click();
        await page.locator('.phone-tabs a[href="#/files"][aria-current=page]').waitFor();
      }
      await inspect(page, errors, label + ' degraded');
      if (!['fresh', 'healthy', 'phone-layout'].includes(scenario) && !injected) failures.push(`${label}: scenario injected no API failures`);
      if (!['fresh', 'healthy', 'phone-layout'].includes(scenario)) {
        const identity = await page.evaluate(() => { window.__crawlDocument = crypto.randomUUID(); return window.__crawlDocument; });
        degraded = false; await ctx.setOffline(false); successfulReads = 0;
        await resume(page);
        // Pages with mount-only reads recover on navigation too. Hash
        // navigation uses the same document, without a browser reload.
        await page.evaluate(() => { location.hash = '#/' + (location.hash.startsWith('#/chat') ? 'cloud' : 'chat'); });
        await page.waitForTimeout(100);
        await page.evaluate(route => { location.hash = '#/' + route; }, r);
        await page.waitForTimeout(600);
        if (r === 'chat') await openPicker(page);
        await inspect(page, errors, label + ' recovery');
        if (await page.evaluate(() => window.__crawlDocument) !== identity) failures.push(`${label}: recovery reloaded the document`);
        if (!successfulReads) failures.push(`${label}: recovery made no valid API reads`);
        const unavailable = await page.evaluate(async () => (await import('/next/js/core/state.js')).app.get().unavailable);
        for (const key of ['models', 'presets', 'workspace', 'history', 'sessions']) if (unavailable[key]) failures.push(`${label}: ${key} did not recover`);
      }
      checks++;
    } catch (e) { failures.push(`${label}: ${e.stack || e}`); }
    finally { await ctx.close(); }
  };
  const [shardIndex, shardCount] = (process.env.CRAWL_SHARD || '1/1').split('/').map(Number);
  const concurrency = Math.max(1, +process.env.CRAWL_CONCURRENCY || 4);
  for (const [name, engine, forms] of [
    ['chromium', chromium, [['desktop', { viewport: { width: 1360, height: 860 } }], ['phone', { ...devices['iPhone 15'], viewport: { width: 390, height: 844 } }], ['phone-small', { ...devices['iPhone 15'], viewport: { width: 320, height: 740 } }]]],
    ['webkit', webkit, [['phone', { ...devices['iPhone 15'], viewport: { width: 390, height: 844 } }], ['phone-small', { ...devices['iPhone 15'], viewport: { width: 320, height: 740 } }]]],
  ]) {
    if (process.env.CRAWL_LAYOUT && name !== 'chromium') continue;
    if (process.env.CRAWL_ENGINE && process.env.CRAWL_ENGINE !== name) continue;
    const cases = [];
    for (const [form, context] of forms) {
      if (process.env.CRAWL_LAYOUT && (name !== 'chromium' || form === 'desktop')) continue;
      // Every degraded scenario runs on the Chromium phone (the reported
      // crashes); desktop and WebKit keep a representative sample so CI stays
      // bounded. CRAWL_FULL=1 runs the whole matrix everywhere.
      const sample = ['fresh', 'healthy', 'phone-layout', '502', 'engine-unreachable'];
      const planned = process.env.CRAWL_SCENARIOS ? process.env.CRAWL_SCENARIOS.split(',') : process.env.CRAWL_LAYOUT ? ['phone-layout'] : !process.env.CRAWL_FULL && form === 'phone-small' ? ['fresh', 'healthy', 'phone-layout'] : process.env.CRAWL_FULL || (name === 'chromium' && form === 'phone') ? scenarios : scenarios.filter(x => sample.includes(x));
      for (const scenario of planned) for (const r of routes) {
        if (scenario === 'fresh' && r === 'machines/' + remote.id) continue;
        cases.push({ name, form, context, scenario, r });
      }
    }
    const mine = cases.filter((_, i) => i % shardCount === shardIndex - 1);
    if (!mine.length) continue;
    let browser;
    try { browser = await engine.launch(); }
    catch (e) {
      if (name !== 'webkit' || process.env.CI || !/Executable doesn't exist|Host system is missing dependencies|error while loading shared libraries|cannot open shared object file/.test(String(e))) throw e;
      console.log(`SKIP WebKit iPhone crawl: system cannot run WebKit: ${String(e).split('\n').slice(0, 4).join(' ')}`);
      continue;
    }
    const started = Date.now();
    try {
      let next = 0;
      await Promise.all(Array.from({ length: concurrency }, async () => { while (next < mine.length) await runCase(browser, mine[next++]); }));
    } finally { await browser.close(); }
    console.log(`UI crawl ${name}: ${mine.length} cases (shard ${shardIndex}/${shardCount}) in ${Math.round((Date.now() - started) / 1000)} s`);
  }
  console.log(`UI route crawl: ${routes.length} routes, ${checks} isolated checks including degraded recovery`);
} catch (e) {
  failures.push(String(e && e.stack || e));
} finally {
  stop();
}
if (failures.length) { console.error([...new Set(failures)].join('\n')); process.exit(1); }
console.log('UI route crawl passed');
