import test from 'node:test';
import fs from 'node:fs';
import assert from 'node:assert/strict';
import { phoneFixtures } from '../../../../tools/tests/ui-routes/phone-fixtures.mjs';
import { validData } from '../next/js/core/shape.js';

const ws = { runtimes: [{ id: 'codex', name: 'Codex', kind: 'harness', implemented: true, capabilities: ['workdir'], features: { config_options: [], model_sources: ['native', 'loom'] } }], models: [], projects: [], providers: [], capabilities: [] };
const remote = { id: 'synthetic-remote', name: 'Synthetic remote' };
async function read(path, method = 'GET') {
  let result, continued = false;
  await phoneFixtures(ws, remote)({ request: () => ({ url: () => 'http://fixture.test' + path, method: () => method }), fulfill: r => result = r, continue: () => continued = true });
  return { ...result, continued };
}
test('populated phone fixtures respect UI observation contracts', async () => {
  for (const path of ['/api/workspace', '/api/agents/installations', '/api/machines', '/api/models', '/api/chat/history', '/api/runtime/sessions', '/api/runtimes/codex/probe', '/api/runtimes/codex/inspect', '/api/runtimes/codex/sessions', '/api/harness/lifecycle', '/api/harness/model-source', '/api/agents/catalog', '/api/usage', '/api/usage/native', '/api/usage/providers', '/api/tasks', '/api/workspaces', '/api/env/services', '/api/env/docker', '/api/env/proxmox/resources']) {
    const response = await read(path); assert.ok(response.json, path); assert.ok(validData(response.json, path), path);
  }
  const workspace = (await read('/api/workspace')).json;
  assert.ok(workspace.runtimes[0].features.config_options.includes('model'));
  assert.ok(workspace.runtimes[0].features.history_import);
  assert.ok(workspace.projects.length);
});
test('phone fixtures refuse generation, login, imports, installs and writes', async () => {
  for (const path of ['/api/runtime/sessions/send', '/api/runtimes/codex/account', '/api/terminals', '/api/harness-history/transfer', '/api/agents/catalog/add', '/api/agents/installations', '/api/projects/context']) {
    const response = await read(path, 'POST'); assert.equal(response.status, 409, path); assert.equal(response.continued, false, path);
  }
  for (const path of ['/api/estimate', '/api/runtimes/codex/quota']) assert.equal((await read(path, 'POST')).json.ok, true);
});


test('phone crawl inventory includes every registered route and dynamic agent details', () => {
  const registry = fs.readFileSync(new URL('../next/js/app/routes.js', import.meta.url), 'utf8');
  const crawl = fs.readFileSync(new URL('../../../../tools/tests/ui-routes/crawl.mjs', import.meta.url), 'utf8');
  const inventory = crawl.slice(crawl.indexOf('let routes ='), crawl.indexOf('if (process.env.CRAWL_ROUTES)'));
  for (const [, id] of registry.matchAll(/\{ id: '([^']+)', page:/g)) assert.ok(inventory.includes("'" + id + "'"), id + ' missing from crawl');
  assert.ok(inventory.includes("runtimes.map(id => 'harnesses/' + id)"));
});
