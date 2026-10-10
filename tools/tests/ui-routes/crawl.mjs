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

const remote = { id: 'remote-box', name: 'remote-box', host: '192.0.2.10', os: 'linux', kind: 'node', version: '0.1.4', modules: ['engine', 'harness', 'terminal', 'observe'] };
const mockRemote = async route => {
  const u = new URL(route.request().url());
  if (u.pathname === '/api/machines') return route.fulfill({ json: { ok: true, machines: [remote], offers: { [remote.id]: [] } } });
  if (u.pathname === '/api/engine/node') return route.fulfill({ json: { ok: true, remote: true, machine_id: remote.id, hostname: remote.name, url: 'http://192.0.2.10:2510', version: '0.1.4' } });
  if (u.pathname === '/api/machines/metrics') return route.fulfill({ json: { ok: true, metrics: { [remote.id]: { cpu: null } } } });
  if (u.pathname === '/api/agents/installations') {
    const res = await route.fetch(); const json = await res.json();
    json.installations = [...(json.installations || []), { machine: remote.id, machine_name: remote.name, harness: 'codex', name: 'Codex', installed: true, managed: true, version: 'codex-cli 0.150.0' }, { machine: remote.id, machine_name: remote.name, harness: 'opencode', name: 'OpenCode', installed: true, version: '1.18.0' }];
    return route.fulfill({ json });
  }
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
  let routes = ['chat', 'models', 'cloud', 'local', 'engine', 'workspaces', 'project', 'project/fixture-project', 'resources', 'terminals', 'environment', 'environment/docker', 'environment/proxmox', 'local/hub', 'voice', 'jarvis', 'harnesses', 'harnesses/history', ...runtimes.map(id => 'harnesses/' + id),
    'machines', 'machines/local', 'machines/' + remote.id, 'machines/workspaces', 'machines/terminals', 'machines/environment',
    'tasks', 'brain', 'brain/sources', 'brain/memory', 'brain/skills', 'brain/mcp', 'usage', 'bench',
    'settings', 'settings/general', 'settings/internet', 'settings/startup', 'settings/notifications', 'settings/policy', 'settings/doctor', 'settings/security', 'settings/about'];
  if (process.env.CRAWL_ROUTES) routes = routes.filter(r => new RegExp(process.env.CRAWL_ROUTES).test(r));
  const fixturePhone = phoneFixtures(ws, remote);
  const fixtureSessions = ['cloud', 'agent'].map(kind => ({ id: 'fixture-' + kind, title: 'Synthetic ' + kind + ' discussion', runtime_id: kind === 'cloud' ? 'openai-compatible' : 'fixture-agent', provider_id: 'fixture', provider_name: kind === 'cloud' ? 'Fixture cloud' : 'Fixture agent', model: 'fixture-model', status: 'idle', messages: [], turns: [], message_count: 1 }));
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
  let checks = 0;
  for (const [name, engine, forms] of [
    ['chromium', chromium, [['desktop', { viewport: { width: 1360, height: 860 } }], ['phone', { ...devices['iPhone 15'], viewport: { width: 390, height: 844 } }], ['phone-small', { ...devices['iPhone 15'], viewport: { width: 320, height: 740 } }]]],
    ['webkit', webkit, [['phone', { ...devices['iPhone 15'], viewport: { width: 390, height: 844 } }], ['phone-small', { ...devices['iPhone 15'], viewport: { width: 320, height: 740 } }]]],
  ]) {
    if (process.env.CRAWL_LAYOUT && name !== 'chromium') continue;
    let browser;
    try { browser = await engine.launch(); }
    catch (e) {
      if (name !== 'webkit' || process.env.CI || !/Executable doesn't exist|Host system is missing dependencies|error while loading shared libraries|cannot open shared object file/.test(String(e))) throw e;
      console.log(`SKIP WebKit iPhone crawl: system cannot run WebKit: ${String(e).split('\n').slice(0, 4).join(' ')}`);
      continue;
    }
    try {
      for (const [form, context] of forms) {
        if (process.env.CRAWL_LAYOUT && (name !== 'chromium' || form === 'desktop')) continue;
        // Every degraded scenario runs on the Chromium phone (the reported
        // crashes); desktop and WebKit keep a representative sample so CI stays
        // bounded. CRAWL_FULL=1 runs the whole matrix everywhere.
        const sample = ['fresh', 'healthy', 'phone-layout', '502', 'engine-unreachable'];
        const planned = process.env.CRAWL_LAYOUT ? ['phone-layout'] : !process.env.CRAWL_FULL && form === 'phone-small' ? ['fresh', 'healthy', 'phone-layout'] : process.env.CRAWL_FULL || (name === 'chromium' && form === 'phone') ? scenarios : scenarios.filter(x => sample.includes(x));
        for (const scenario of planned) {
          for (const r of routes) {
            if (scenario === 'fresh' && r === 'machines/' + remote.id) continue;
            // Fresh browser state per route/scenario: guards, observations,
            // pending requests and service workers never leak between cases.
            const ctx = await browser.newContext(context);
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
            page.on('pageerror', e => errors.push(e.message));
            page.on('response', async response => {
              const url = new URL(response.url());
              if (!url.pathname.startsWith('/api/') || url.pathname.startsWith('/api/auth/') || url.pathname === '/api/prefs' || response.request().method() !== 'GET') return;
              try { const data = await response.json(); if (response.ok() && data?.ok !== false && validData(data, url.pathname)) successfulReads++; } catch (_) {}
            });
            await page.route('**/api/**', async route => {
              const u = new URL(route.request().url());
              if (/^\/api\/(chat\/send|runtime\/sessions\/send|load-model|start|restart)$/.test(u.pathname)) {
                failures.push('Crawl attempted generation or engine startup: ' + u.pathname);
                return route.abort();
              }
              // Let the access shell and onboarding preference identify this
              // isolated installation. Domain APIs are faulted independently.
              if (scenario === 'phone-layout' && !u.pathname.startsWith('/api/auth/') && u.pathname !== '/api/prefs') return fixturePhone(route);
              const boot = u.pathname.startsWith('/api/auth/') || u.pathname === '/api/prefs';
              const engineOnly = /^\/api\/(models|presets|preset|status|vram|ram|server|llamacpp|hub|backends|config|catalog|paths|naked|llama-flags|engines\/vllm|engine\/(params|service|keys|auto-update)|model-caps)(\/|$)/.test(u.pathname);
              if (degraded && !boot && (scenario !== 'engine-unreachable' || engineOnly)) {
                injected++;
                if (scenario === 'abort' || scenario === 'offline') return route.abort('internetdisconnected');
                if (scenario === 'malformed-json') return route.fulfill({ status: 200, contentType: 'application/json', body: '{broken' });
                if (scenario === 'wrong-shape') return route.fulfill({ json: { ok: true, models: {}, presets: {}, sessions: {}, runtimes: {}, projects: {}, machines: {}, sources: {}, tasks: {}, servers: {}, installations: {} } });
                return route.fulfill({ status: scenario === '401' ? 401 : 502, json: { ok: false, error: 'remote engine unreachable' } });
              }
              if (scenario === 'engine-unreachable') {
                if (u.pathname === '/api/workspace') return route.fulfill({ json: { ...ws,
                  runtimes: [...ws.runtimes, { id: 'fixture-agent', name: 'Fixture agent', kind: 'harness', implemented: true, available: true, connected: true, capabilities: ['chat', 'stream'] }],
                  providers: [{ id: 'fixture', name: 'Fixture cloud', ready: true, models: ['fixture-model'] }],
                  models: fixtureSessions.map(session => ({ id: session.id, name: session.model, model: session.model, kind: session.id.endsWith('cloud') ? 'cloud' : 'harness', runtime_id: session.runtime_id, provider_id: session.provider_id, provider_name: session.provider_name, enabled: true, ready: true })),
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
              await page.goto(base + '#/' + r);
              await page.waitForTimeout(600);
              // Exercise the collection consumer that triggered the iPhone
              // crash, and both other catalogs while the engine is unavailable.
              if (r === 'chat') {
                await openPicker(page);
                for (const tab of ['cloud', 'harness']) {
                  await page.locator('.picker .seg button').nth(tab === 'cloud' ? 1 : 2).click();
                }
              }
              if (r === 'chat' && scenario === 'engine-unreachable') {
                await page.locator('.picker .pick-search button').click();
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
              if (scenario === 'phone-layout' && r === 'harnesses/codex') {
                if (process.env.CRAWL_SHOTS && form === 'phone') {
                  const out = resolve('.project-local/mission-f'); mkdirSync(out, { recursive: true });
                  await page.screenshot({ path: join(out, process.env.CRAWL_SHOTS + '-agent-390.png'), fullPage: true });
                }
                await inspect(page, errors, label + ' agent');
                await page.locator('.more-info').evaluate(el => el.open = true);
                await inspect(page, errors, label + ' details');
                const resource = page.locator('.agent .set-c .btn');
                if (await resource.count()) { await resource.first().click(); await page.waitForTimeout(100); await inspect(page, errors, label + ' resources'); await page.locator('.dialog-head .icon-btn').click(); }
                const more = page.locator('.more-models');
                if (await more.count()) { await more.click(); await inspect(page, errors, label + ' all models'); }
                const tip = page.locator('.agent .sec-h .tip').first();
                if (await tip.count()) { await tip.click(); await inspect(page, errors, label + ' tooltip'); await tip.blur(); }
                const tabs = page.locator('.agent .sec-h .seg button');
                if (await tabs.count()) { await tabs.last().click(); await page.waitForTimeout(100); await inspect(page, errors, label + ' native discussions'); }
                // A known disconnected account with no catalog exposes the
                // native sign-in dialog. Inspect it without starting sign-in.
                await page.route('**/api/runtimes/codex/probe', route => route.fulfill({ json: { ok: true, probe: { config: [] } } }));
                await page.reload(); await page.waitForTimeout(600);
                const accountLabel = await page.evaluate(async () => (await import('/next/js/core/i18n.js')).t('agents.account.login'));
                const account = page.locator('.agent-h .acts .btn').filter({ hasText: accountLabel });
                if (!await account.count()) throw new Error('Account fixture exposed no sign-in action');
                await account.first().click(); await page.locator('.dialog').waitFor();
                await inspect(page, errors, label + ' account'); await page.locator('.dialog-head .icon-btn').click();
              }
              if (scenario === 'phone-layout' && r === 'harnesses') {
                await page.locator('.page-head .btn.primary').click(); await page.locator('.dialog').waitFor();
                await inspect(page, errors, label + ' add agent');
                await page.locator('.add-agents .set-c .btn').last().click();
                await inspect(page, errors, label + ' custom agent'); await page.locator('.dialog-head .icon-btn').click();
              }
              if (scenario === 'phone-layout' && r === 'cloud') {
                await page.locator('.cl-tile').first().click(); await page.locator('.dialog').waitFor();
                await inspect(page, errors, label + ' provider dialog'); await page.locator('.dialog-head .icon-btn').click();
              }
              if (scenario === 'phone-layout' && r === 'environment') {
                await page.locator('.toolbar .btn.primary').click(); await page.locator('.dialog').waitFor();
                await inspect(page, errors, label + ' service dialog'); await page.locator('.dialog-head .icon-btn').click();
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
          }
          console.log(`UI crawl ${name}/${form}/${scenario}: ${routes.length} routes checked`);
        }
      }
    } finally { await browser.close(); }
  }
  console.log(`UI route crawl: ${routes.length} routes, ${checks} isolated checks including degraded recovery`);
} catch (e) {
  failures.push(String(e && e.stack || e));
} finally {
  stop();
}
if (failures.length) { console.error([...new Set(failures)].join('\n')); process.exit(1); }
console.log('UI route crawl passed');
