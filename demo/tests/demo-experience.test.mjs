import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';

function storage(initial) {
  const values = { ...initial };
  Object.defineProperties(values, {
    getItem: { value: (key) => values[key] ?? null },
    setItem: { value: (key, value) => { values[key] = String(value); } },
    removeItem: { value: (key) => { delete values[key]; } },
  });
  return values;
}

test('demo reset removes Loom state without clearing unrelated origin data', () => {
  const callbacks = new Map();
  const element = () => ({
    classList: { add() {}, remove() {} },
    addEventListener(name, callback) { this[name] = callback; },
    showModal() {},
    close() {},
  });
  const sessionStorage = storage({
    'lh-demo-intro-seen': '1',
    'loom_demo_models': '[1]',
    other_session: 'keep',
  });
  const localStorage = storage({
    'loom-theme': 'dark',
    'loom.chat': '[1]',
    other_local: 'keep',
  });
  let reloads = 0;
  const document = {
    readyState: 'complete',
    createElement: element,
    body: { appendChild() {} },
    getElementById(id) {
      return { addEventListener(name, callback) { callbacks.set(`${id}:${name}`, callback); } };
    },
  };

  runInNewContext(readFileSync(new URL('../DemoExperience.js', import.meta.url), 'utf8'), {
    document,
    sessionStorage,
    localStorage,
    window: { location: { reload() { reloads += 1; } } },
  });
  callbacks.get('lh-demo-chip-reset:click')();

  assert.equal(reloads, 1);
  assert.equal(sessionStorage.getItem('loom_demo_models'), null);
  assert.equal(sessionStorage.getItem('lh-demo-intro-seen'), null);
  assert.equal(localStorage.getItem('loom-theme'), null);
  assert.equal(localStorage.getItem('loom.chat'), null);
  assert.equal(sessionStorage.getItem('other_session'), 'keep');
  assert.equal(localStorage.getItem('other_local'), 'keep');
});
