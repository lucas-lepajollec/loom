import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';
import { french } from './i18n-fixture.mjs';

test('last Usage section labels all metrics and distinguishes historical cloud prices from harness cost', async () => {
  const html = htm.bind((type, props, ...children) => ({ type, props: props || {}, children }));
  const flatten = x => Array.isArray(x) ? x.flatMap(flatten) : x && typeof x === 'object' ? [x, ...flatten(x.children)] : [];
  const text = x => Array.isArray(x) ? x.map(text).join('') : x && typeof x === 'object' ? text(x.children) : String(x ?? '');
  let cursor = 0; const slots = [], polls = [];
  const models = [
    { kind: 'cloud', runtime_id: 'openai-compatible', choice_id: 'cloud', name: 'API model', provider: 'API', turns: 2, reported_turns: 1, usage: { prompt_tokens: 123, completion_tokens: 45 }, estimated_cost: .25 },
    { kind: 'harness', runtime_id: 'pi', name: 'loom:gemma.gguf', provider: 'Pi', turns: 1, reported_turns: 0, usage: {} },
    { kind: 'harness', runtime_id: 'claude-code', name: 'Native', provider: 'Claude Code', turns: 1, reported_turns: 1, usage: { prompt_tokens: 1, completion_tokens: 2 }, reported_cost: .5, currency: 'USD' },
  ];
  const env = { html, t: french, locale: () => 'fr-FR', app: {}, Logo: 'Logo', Tip: 'Tip', Empty: 'Empty',
    fmtTok: String, useStore: () => ({ runtimes: [] }), useVisibleRefresh: task => polls.push(task),
    useState: initial => { const i = cursor++; if (!(i in slots)) slots[i] = initial; return [slots[i], v => slots[i] = v]; },
    get: async () => ({ ok: true, models, quotas: [] }) };
  const source = fs.readFileSync(new URL('../next/js/features/usage/page.js', import.meta.url), 'utf8');
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.page = UsagePage;', env);
  const render = () => { cursor = 0; polls.length = 0; return env.page(); };
  render(); await polls[0](() => true);
  const tree = render();
  assert.equal(flatten(tree).filter(n => n.type?.name === 'Price').length, 1, 'historical cloud route still has editable API pricing');
  assert.match(text(tree), /0,25/);
  assert.match(text(tree), /0,50/);
  assert.doesNotMatch(text(flatten(tree).find(n => n.props.class === 'card table usage-t')), /Abonnement/);
  const labels = flatten(tree).filter(n => n.props['data-label']).map(n => n.props['data-label']);
  assert.deepEqual(labels, Array(3).fill(['Entrée', 'Sortie', 'Prix / M tokens', 'Estimation']).flat());
  assert.match(text(tree), /Pi/);
  assert.match(text(tree), /—/);
});
