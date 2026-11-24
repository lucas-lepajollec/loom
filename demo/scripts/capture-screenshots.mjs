#!/usr/bin/env node
/**
 * Reproducible screenshot and GitHub social-preview generator for Loom.
 *
 * Serves demo/ over a local ephemeral HTTP server (with the exact CSP from
 * demo/vercel.json), captures clean English screenshots across desktop and
 * mobile viewports using Playwright Chromium, writes docs/screenshots/manifest.json,
 * and renders docs/brand/social-preview.png (1280x640, < 1 MB).
 */
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { dirname, extname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const demoDir = resolve(__dirname, '..');
const repoRoot = resolve(demoDir, '..');
const shotsDir = join(repoRoot, 'docs', 'screenshots');
const brandDir = join(repoRoot, 'docs', 'brand');

const playwrightEntry = join(repoRoot, 'tools', 'tests', 'ui-routes', 'node_modules', 'playwright', 'index.mjs');
if (!existsSync(playwrightEntry)) {
  console.error('Playwright not found at', playwrightEntry);
  console.error('Run: npm ci --prefix tools/tests/ui-routes');
  process.exit(1);
}
const { chromium } = await import(pathToFileURL(playwrightEntry).href);

const vercelCfg = JSON.parse(readFileSync(join(demoDir, 'vercel.json'), 'utf8'));
const securityHeaders = Object.fromEntries(
  (vercelCfg.headers?.[0]?.headers || []).map(h => [h.key, h.value]),
);

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.mjs': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
  '.json': 'application/json; charset=utf-8',
  '.webmanifest': 'application/manifest+json; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
};

function startStaticServer() {
  return new Promise((resolveServer, reject) => {
    const server = createServer((req, res) => {
      const reqPath = new URL(req.url, 'http://127.0.0.1').pathname;
      const safeRel = reqPath === '/' ? 'index.html' : reqPath.replace(/^\/+/, '');
      const fullPath = resolve(demoDir, safeRel);
      if (!fullPath.startsWith(demoDir) || !existsSync(fullPath) || statSync(fullPath).isDirectory()) {
        res.writeHead(404);
        res.end('Not found');
        return;
      }
      const ext = extname(fullPath).toLowerCase();
      res.writeHead(200, {
        'Content-Type': MIME[ext] || 'application/octet-stream',
        ...securityHeaders,
      });
      res.end(readFileSync(fullPath));
    });
    server.on('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const addr = server.address();
      resolveServer({ server, baseUrl: `http://127.0.0.1:${addr.port}` });
    });
  });
}

function sha256File(filePath) {
  return createHash('sha256').update(readFileSync(filePath)).digest('hex');
}

function gitCommit() {
  try {
    return execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: repoRoot, encoding: 'utf8' }).trim();
  } catch {
    return 'unknown';
  }
}

async function waitStable(page, ms = 700) {
  await page.evaluate(() => document.fonts ? document.fonts.ready : Promise.resolve());
  await page.waitForTimeout(ms);
}

async function hideDemoChip(page) {
  await page.addStyleTag({ content: '.lh-demo-chip { display: none !important; }' });
}

const DESKTOP = { width: 1440, height: 900 };
const MOBILE = { width: 390, height: 844 };

const SHOTS = [
  {
    file: 'chat.png',
    route: '#/chat',
    viewport: DESKTOP,
    waitFor: '.thread-in .msg-ai',
    description: 'Unified discussion view with sidebar folders, local model selector, and VRAM inspector',
  },
  {
    file: 'agents.png',
    route: '#/agents',
    viewport: DESKTOP,
    waitFor: '.cm-session-row',
    description: 'Codeman Agents control center with active sessions, pending approvals, harnesses, and schedules',
  },
  ...[DESKTOP, MOBILE].map(viewport => ({
    file: viewport === MOBILE ? 'session-review-mobile.png' : 'session-review.png',
    route: '#/agents/s/cm-homelab-audit',
    viewport,
    isMobile: viewport === MOBILE,
    waitFor: '.workspace-tools',
    description: 'Selected diff line in an agent workspace, ready to add to the reply',
    afterNav: async page => {
      await page.locator('.workspace-tools').getByRole('button', { name: 'Changes' }).click();
      await page.getByRole('button', { name: /compose\/monitoring\.yml/ }).click();
      await page.locator('.review-line.add').first().click();
    },
  })),
  {
    file: 'files.png',
    route: '#/files/n/homelab%2Fcluster-topology.md',
    viewport: DESKTOP,
    waitFor: '.fl-doc',
    description: 'Files / Second Brain Markdown workspace with tree navigation and rich editor',
  },
  {
    file: 'machines.png',
    route: '#/machines',
    viewport: DESKTOP,
    waitFor: '.machine-card',
    description: 'Machines fleet overview with capability bricks and live resource meters',
  },
  {
    file: 'models.png',
    route: '#/local',
    viewport: DESKTOP,
    waitFor: '.models-status',
    description: 'Local GGUF model library, presets, and GPU VRAM meter',
  },
  {
    file: 'local.png',
    route: '#/local',
    viewport: DESKTOP,
    waitFor: '.models-status',
    description: 'Local GGUF library, presets, and model parameters (README reference)',
  },
  {
    file: 'engines.png',
    route: '#/engine',
    viewport: DESKTOP,
    waitFor: '.models-engine-grid',
    description: 'Engine router runtime, parallel slots, idle VRAM unload, and OpenAI /v1 API',
  },
  {
    file: 'activity.png',
    route: '#/quotas',
    viewport: DESKTOP,
    waitFor: '.q-acc',
    description: 'Activity multi-harness subscription quotas, pace curve, and 14-day usage chart',
  },
  {
    file: 'usage.png',
    route: '#/usage',
    viewport: DESKTOP,
    waitFor: '.usage-t',
    description: 'Activity cloud provider balances and per-model token cost tracking',
  },
  {
    file: 'terminals.png',
    route: '#/terminals/term-workstation',
    viewport: DESKTOP,
    waitFor: '.xterm-screen',
    description: 'Local and remote SSH terminal sessions powered by xterm.js',
  },
  { file: 'settings.png', route: '#/settings/notifications', viewport: DESKTOP, waitFor: '.settings-detail',
    description: 'Searchable preferences with notification channels and delivery settings' },
  { file: 'mobile-more.png', route: '#/more', viewport: MOBILE, isMobile: true, waitFor: '.more-destination',
    description: 'Phone workspace launcher with local engine status and project tools' },
  {
    file: 'welcome.png',
    route: '#/settings/about',
    viewport: DESKTOP,
    waitFor: '.set-body',
    afterNav: async (page) => {
      // Open the first-run guide modal via Settings -> About
      const btn = page.locator('.set-body button', { hasText: /open|guide|welcome|ouvrir/i }).first();
      await btn.click();
      await page.waitForSelector('.dialog.onboard', { timeout: 5000 });
    },
    description: 'First-run guide with language, hardware, and engine setup',
  },
  {
    file: 'mobile.png',
    route: '#/sessions',
    viewport: MOBILE,
    isMobile: true,
    waitFor: '.phone-tabs',
    description: 'Mobile PWA bottom tab layout with unified discussions and Codeman sessions',
  },
];

async function renderSocialPreview(browser) {
  const svgMark = readFileSync(join(brandDir, 'loom-mark-light.svg'), 'utf8');
  const geistB64 = readFileSync(join(demoDir, 'fonts', 'geist.woff2')).toString('base64');
  const geistMonoB64 = readFileSync(join(demoDir, 'fonts', 'geist-mono.woff2')).toString('base64');

  const html = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<style>
  @font-face { font-family: "Geist"; src: url("data:font/woff2;base64,${geistB64}") format("woff2"); font-weight: 100 900; }
  @font-face { font-family: "Geist Mono"; src: url("data:font/woff2;base64,${geistMonoB64}") format("woff2"); font-weight: 100 900; }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    width: 1280px; height: 640px; overflow: hidden;
    background: #09090a; color: #ededef;
    font-family: "Geist", system-ui, sans-serif;
    position: relative; display: flex; align-items: center;
    -webkit-font-smoothing: antialiased;
  }
  .grid {
    position: absolute; inset: 0;
    background-image:
      linear-gradient(rgba(255,255,255,0.035) 1px, transparent 1px),
      linear-gradient(90deg, rgba(255,255,255,0.035) 1px, transparent 1px);
    background-size: 48px 48px;
    mask-image: radial-gradient(ellipse 85% 85% at 50% 45%, black 35%, transparent 100%);
  }
  .glow {
    position: absolute; top: -180px; right: -120px; width: 720px; height: 720px;
    background: radial-gradient(circle, rgba(74, 222, 128, 0.085) 0%, rgba(255,255,255,0.03) 38%, transparent 70%);
    pointer-events: none;
  }
  .glow-2 {
    position: absolute; bottom: -220px; left: -120px; width: 640px; height: 640px;
    background: radial-gradient(circle, rgba(255, 255, 255, 0.045) 0%, transparent 65%);
    pointer-events: none;
  }
  .frame {
    position: relative; z-index: 2; width: 100%; height: 100%;
    padding: 72px 84px; display: flex; flex-direction: column; justify-content: space-between;
  }
  .top { display: flex; align-items: center; justify-content: space-between; }
  .brand { display: flex; align-items: center; gap: 20px; }
  .mark {
    width: 72px; height: 72px; border-radius: 18px;
    background: #141416; border: 1px solid rgba(255,255,255,0.12);
    display: grid; place-items: center;
    box-shadow: 0 18px 40px rgba(0,0,0,0.45);
  }
  .mark svg { width: 44px; height: 44px; }
  .badge {
    font-family: "Geist Mono", monospace; font-size: 14px; font-weight: 500;
    color: #a1a1aa; letter-spacing: 0.06em; text-transform: uppercase;
    padding: 8px 16px; border-radius: 999px;
    border: 1px solid rgba(255,255,255,0.12); background: rgba(255,255,255,0.03);
    display: inline-flex; align-items: center; gap: 10px;
  }
  .badge i { width: 8px; height: 8px; border-radius: 50%; background: #4ade80; box-shadow: 0 0 12px rgba(74,222,128,0.7); }
  .hero { max-width: 960px; }
  h1 {
    font-size: 84px; font-weight: 650; letter-spacing: -0.04em; line-height: 1.02;
    color: #ffffff; margin-bottom: 20px;
  }
  .tagline {
    font-size: 30px; font-weight: 400; line-height: 1.35; letter-spacing: -0.02em;
    color: #a1a1aa; max-width: 860px;
  }
  .tagline strong { color: #ededef; font-weight: 500; }
  .bottom { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; }
  .pills { display: flex; flex-wrap: wrap; gap: 10px; }
  .pill {
    font-family: "Geist Mono", monospace; font-size: 14px; color: #d4d4d8;
    background: #141417; border: 1px solid rgba(255,255,255,0.1);
    padding: 9px 15px; border-radius: 10px;
  }
  .meta {
    font-family: "Geist Mono", monospace; font-size: 15px; color: #71717a;
    white-space: nowrap;
  }
</style>
</head>
<body>
  <div class="grid"></div>
  <div class="glow"></div>
  <div class="glow-2"></div>
  <div class="frame">
    <div class="top">
      <div class="brand">
        <div class="mark">${svgMark}</div>
        <div class="badge"><i></i>Single Go Binary · Local-First</div>
      </div>
      <div class="meta">github.com/lucas-lepajollec/loom</div>
    </div>
    <div class="hero">
      <h1>Loom</h1>
      <p class="tagline"><strong>Local AI control station</strong> for local &amp; cloud models, Codeman agents, Second Brain notes, and multi-node homelab machines.</p>
    </div>
    <div class="bottom">
      <div class="pills">
        <span class="pill">llama.cpp &amp; vLLM</span>
        <span class="pill">Cloud Providers</span>
        <span class="pill">Codeman Agents</span>
        <span class="pill">Second Brain</span>
        <span class="pill">Machines &amp; PTY</span>
        <span class="pill">MCP Gateway</span>
      </div>
      <div class="meta">loom.lucas-homelab.fr</div>
    </div>
  </div>
</body>
</html>`;

  const ctx = await browser.newContext({ viewport: { width: 1280, height: 640 }, deviceScaleFactor: 1 });
  const page = await ctx.newPage();
  await page.setContent(html, { waitUntil: 'load' });
  await waitStable(page, 200);
  const outPath = join(brandDir, 'social-preview.png');
  await page.screenshot({ path: outPath, type: 'png' });
  await ctx.close();

  const size = statSync(outPath).size;
  if (size > 1024 * 1024) {
    throw new Error(`social-preview.png exceeds 1 MB (${size} bytes)`);
  }
  console.log(`Captured docs/brand/social-preview.png (1280x640, ${(size / 1024).toFixed(1)} KB)`);
}

async function main() {
  mkdirSync(shotsDir, { recursive: true });
  mkdirSync(brandDir, { recursive: true });

  const { server, baseUrl } = await startStaticServer();
  const browser = await chromium.launch({ headless: true });
  const errors = [];
  const manifestEntries = [];

  try {
    // 1. Verify first-visit intro dialog, dismiss it, and verify interactive chat streaming + ?lang=fr
    {
      const ctx = await browser.newContext({ viewport: DESKTOP, deviceScaleFactor: 1 });
      const page = await ctx.newPage();
      page.on('pageerror', e => errors.push(`intro: pageerror: ${e.message}`));
      page.on('console', m => { if (m.type() === 'error') errors.push(`intro: console.error: ${m.text()}`); });
      await page.goto(`${baseUrl}/?lang=en#/chat`, { waitUntil: 'networkidle' });
      await page.waitForSelector('#lh-demo-modal[open]', { timeout: 8000 });
      await page.click('#lh-demo-modal-continue');
      await page.waitForSelector('.thread-in .msg-ai', { timeout: 8000 });
      await ctx.close();
    }

    // 2. Verify ?lang=fr switches both UI and demo chrome cleanly with zero errors
    {
      const ctx = await browser.newContext({ viewport: DESKTOP, deviceScaleFactor: 1 });
      const page = await ctx.newPage();
      page.on('pageerror', e => errors.push(`fr: pageerror: ${e.message}`));
      page.on('console', m => { if (m.type() === 'error') errors.push(`fr: console.error: ${m.text()}`); });
      await page.goto(`${baseUrl}/?lang=fr#/chat`, { waitUntil: 'networkidle' });
      await page.waitForSelector('#lh-demo-modal[open]', { timeout: 8000 });
      const htmlLang = await page.evaluate(() => document.documentElement.lang);
      if (htmlLang !== 'fr') errors.push(`fr: expected html lang="fr", got "${htmlLang}"`);
      await ctx.close();
    }

    // 3. Capture all clean English screenshots
    for (const shot of SHOTS) {
      const ctx = await browser.newContext({
        viewport: shot.viewport,
        deviceScaleFactor: 1,
        isMobile: !!shot.isMobile,
        hasTouch: !!shot.isMobile,
      });
      await ctx.addInitScript(() => {
        sessionStorage.setItem('lh-demo-intro-seen', '1');
      });
      const page = await ctx.newPage();
      page.on('pageerror', e => errors.push(`${shot.file}: pageerror: ${e.message}`));
      page.on('console', m => { if (m.type() === 'error') errors.push(`${shot.file}: console.error: ${m.text()}`); });

      await page.goto(`${baseUrl}/?lang=en${shot.route}`, { waitUntil: 'networkidle' });
      await hideDemoChip(page);
      if (shot.waitFor) {
        await page.waitForSelector(shot.waitFor, { timeout: 10000 });
      }
      if (shot.afterNav) {
        await shot.afterNav(page);
      }
      await waitStable(page, 650);

      const outPath = join(shotsDir, shot.file);
      await page.screenshot({ path: outPath, type: 'png' });
      const bytes = statSync(outPath).size;
      const sha256 = sha256File(outPath);
      manifestEntries.push({
        file: shot.file,
        path: `docs/screenshots/${shot.file}`,
        route: shot.route,
        viewport: `${shot.viewport.width}x${shot.viewport.height}`,
        bytes,
        sha256,
        description: shot.description,
      });
      console.log(`Captured docs/screenshots/${shot.file} (${shot.viewport.width}x${shot.viewport.height}, ${(bytes / 1024).toFixed(1)} KB)`);
      await ctx.close();
    }

    // 4. Write docs/screenshots/manifest.json
    const manifest = {
      sourceCommit: gitCommit(),
      locale: 'en',
      theme: 'dark',
      generator: 'demo/scripts/capture-screenshots.mjs',
      screenshots: manifestEntries,
    };
    writeFileSync(join(shotsDir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n', 'utf8');
    console.log(`Wrote docs/screenshots/manifest.json (${manifestEntries.length} screenshots)`);

    // 5. Generate GitHub social preview (1280x640)
    await renderSocialPreview(browser);
  } finally {
    await browser.close();
    server.close();
  }

  if (errors.length) {
    console.error('\nErrors observed during screenshot capture:');
    for (const err of errors) console.error(' -', err);
    process.exit(1);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
