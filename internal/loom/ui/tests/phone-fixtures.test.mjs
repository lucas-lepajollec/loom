import test from 'node:test';
import fs from 'node:fs';
import assert from 'node:assert/strict';
import { phoneFixtures } from '../../../../tools/tests/ui-routes/phone-fixtures.mjs';
import { validData } from '../next/js/core/shape.js';

const ws = { runtimes: [{ id: 'llama.cpp', kind: 'local' }, { id: 'openai-compatible', kind: 'cloud' }], models: [], projects: [], providers: [], capabilities: [] };
const remote = { id: 'synthetic-remote', name: 'Synthetic remote' };
async function read(path, method = 'GET') {
  let result, continued = false;
  await phoneFixtures(ws, remote)({ request: () => ({ url: () => 'http://fixture.test' + path, method: () => method }), fulfill: r => result = r, continue: () => continued = true });
  return { ...result, continued };
}
test('populated phone fixtures respect UI observation contracts', async () => {
  for (const path of ['/api/workspace', '/api/machines', '/api/models', '/api/chat/history', '/api/runtime/sessions', '/api/usage', '/api/usage/providers', '/api/tasks', '/api/workspaces', '/api/env/services', '/api/env/docker', '/api/env/proxmox/resources']) {
    const response = await read(path); assert.ok(response.json, path); assert.ok(validData(response.json, path), path);
  }
  const workspace = (await read('/api/workspace')).json;
  assert.deepEqual(workspace.runtimes.map(r => r.kind), ['local', 'cloud']);
  assert.ok(workspace.projects.length);
});
test('phone fixtures refuse generation, login, imports, installs and writes', async () => {
  for (const path of ['/api/runtime/sessions/send', '/api/runtimes/codex/account', '/api/terminals', '/api/harness-history/transfer', '/api/agents/catalog/add', '/api/agents/installations', '/api/projects/context']) {
    const response = await read(path, 'POST'); assert.equal(response.status, 409, path); assert.equal(response.continued, false, path);
  }
  for (const path of ['/api/estimate']) assert.equal((await read(path, 'POST')).json.ok, true);
});
