import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { french } from './i18n-fixture.mjs';

function fixture({ failPreview, problem, route = {}, failSend } = {}) {
  const calls = [];
  const session = { id: 'discussion', runtime_id: 'pi', model: 'deepseek-flash', project_id: 'project', status: 'idle' };
  const env = { t: french, Map, Set, Date, setTimeout, crypto: { randomUUID: () => 'request-fixture' },
    document: { addEventListener() {} }, toast() {},
    createStore: state => ({ get: () => state, set: patch => Object.assign(state, patch) }),
    app: { get: () => ({ workspace: { projects: [{ id: 'project', brain_budget: 0, brain_sources: [] }] } }) },
    post: async (path, body) => {
      calls.push({ path, body });
      if (path.endsWith('/preview')) {
        if (failPreview) throw new Error('preview offline');
        return { ok: true, ...session, ...route, preview: { problem, context: { revision: 'draft-context', project_id: 'project' } } };
      }
      return failSend ? { ok: false, error: 'context changed during preparation' } : { ok: true };
    },
  };
  const src = fs.readFileSync(new URL('../next/js/features/chat/engine.js', import.meta.url), 'utf8');
  vm.runInNewContext(src.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.store = chat; globalThis.send = send; globalThis.reset = () => { ++epoch; chat.set({session: {id: "other"}, busy: false}); };', env);
  env.store.set({ mode: 'thread', session, busy: false, context: { revision: 'empty-context' }, notice: 'previous error' });
  return { env, calls };
}

test('a project with implicit Brain sources prepares the actual draft before sending', async () => {
  const { env, calls } = fixture();
  assert.equal(await env.send('Retrieve the project context'), true);
  assert.deepEqual(calls.map(c => c.path), ['/api/runtime/sessions/preview', '/api/runtime/sessions/send']);
  assert.equal(calls[0].body.text, calls[1].body.text);
  assert.equal(calls[1].body.context_revision, 'draft-context');
  assert.equal(env.store.get().notice, '');
});

test('an actual executor/provider/project change never sends a paid turn to an unseen route', async () => {
  for (const route of [{ runtime_id: 'codex' }, { model: 'other' }, { endpoint: 'https://other.example' }, { provider_id: 'other' }, { reasoning_effort: 'high' }]) {
    const { env, calls } = fixture({ route });
    assert.equal(await env.send('keep this draft'), false);
    assert.equal(calls.length, 1);
    assert.equal(env.store.get().busy, false);
  }
});

test('preview failure or oversized context settles busy without contacting the executor', async () => {
  for (const options of [{ failPreview: true }, { problem: 'Context too long' }]) {
    const { env, calls } = fixture(options);
    assert.equal(await env.send('draft'), false);
    assert.equal(calls.length, 1);
    assert.equal(env.store.get().busy, false);
  }
});

test('a server-side context race is reported without automatically retrying generation', async () => {
  const { env, calls } = fixture({ failSend: true });
  assert.equal(await env.send('draft'), false);
  assert.equal(calls.length, 2);
  assert.equal(env.store.get().busy, false);
  assert.match(env.store.get().notice, /context changed/);
});

test('navigating away while preparation is pending cannot send the old draft', async () => {
  const { env, calls } = fixture();
  const post = env.post;
  env.post = async (...args) => { const r = await post(...args); env.reset(); return r; };
  assert.equal(await env.send('draft'), false);
  assert.equal(calls.length, 1);
  assert.equal(env.store.get().session.id, 'other');
  assert.equal(env.store.get().busy, false);
});
