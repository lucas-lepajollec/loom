// Parcourt chaque page de l'interface sur un vrai binaire Loom, en format
// ordinateur et téléphone, avec les données d'une installation neuve puis avec
// une machine distante simulée (ancienne version, agents installés, moteur
// lié). Échoue si une page plante, lève une erreur ou laisse deux vues
// montées : les écrans noirs et dédoublés viennent d'un rendu interrompu.
// Usage : node tools/tests/ui-routes/crawl.mjs bin/loom
import { chromium, devices } from 'playwright';
import { spawn } from 'node:child_process';
import { mkdtempSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import net from 'node:net';

const binary = resolve(process.argv[2] || 'bin/loom');
const port = await new Promise(ok => { const s = net.createServer().listen(0, '127.0.0.1', () => { const p = s.address().port; s.close(() => ok(p)); }); });
const dir = mkdtempSync(join(tmpdir(), 'loom-ui-routes-'));
for (const d of ['home', 'data']) mkdirSync(join(dir, d));
const env = { ...process.env, HOME: join(dir, 'home'), XDG_CONFIG_HOME: join(dir, 'home/.config'), XDG_DATA_HOME: join(dir, 'home/.local/share'), LOOM_HOME: join(dir, 'data'), LOOM_WEB_HOST: '127.0.0.1' };
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
  await fetch(base + 'api/prefs', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{"onboarded":"1"}' });
  const ws = await (await fetch(base + 'api/workspace')).json();
  const runtimes = (ws.runtimes || []).map(r => r.id);
  const routes = ['chat', 'models', 'cloud', 'local', 'engine', 'harnesses', 'harnesses/history', ...runtimes.map(id => 'harnesses/' + id),
    'machines', 'machines/local', 'machines/' + remote.id, 'machines/workspaces', 'machines/terminals', 'machines/environment',
    'brain', 'brain/sources', 'brain/memory', 'brain/skills', 'brain/mcp', 'usage', 'bench',
    'settings', 'settings/general', 'settings/internet', 'settings/startup', 'settings/security', 'settings/about'];
  const browser = await chromium.launch();
  for (const [form, context] of [['desktop', { viewport: { width: 1360, height: 860 } }], ['phone', devices['iPhone 15']]]) {
    for (const mocked of [false, true]) {
      const ctx = await browser.newContext(context);
      const page = await ctx.newPage();
      const errors = [];
      page.on('pageerror', e => errors.push(e.message));
      if (mocked) await page.route('**/api/**', mockRemote);
      await page.goto(base); await page.waitForTimeout(800);
      for (const r of routes) {
        if (!mocked && r === 'machines/' + remote.id) continue;
        errors.length = 0;
        await page.goto(base + '#/' + r); await page.waitForTimeout(500);
        const st = await page.evaluate(() => ({
          crash: (document.querySelector('.page-crash pre') || {}).textContent || '',
          bar: (document.querySelector('.client-error') || {}).textContent || '',
          views: document.querySelectorAll('.view').length,
        }));
        const label = `${form}${mocked ? '+remote' : ''} #/${r}`;
        if (st.crash) failures.push(`${label}: page crashed: ${st.crash.split('\n')[0]}`);
        else if (st.bar) failures.push(`${label}: ${st.bar}`);
        else if (st.views !== 1) failures.push(`${label}: ${st.views} views mounted`);
        for (const e of errors) failures.push(`${label}: ${e}`);
        await page.evaluate(() => document.querySelector('.client-error')?.remove());
      }
      await ctx.close();
    }
  }
  await browser.close();
  console.log(`UI route crawl: ${routes.length} routes × desktop/phone × fresh/remote`);
} catch (e) {
  failures.push(String(e && e.stack || e));
} finally {
  stop();
}
if (failures.length) { console.error([...new Set(failures)].join('\n')); process.exit(1); }
console.log('UI route crawl passed');
