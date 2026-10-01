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
    setAttribute() {},
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

test('intro opens at its heading instead of jumping to off-screen actions', () => {
  let focusedWith;
  const heading = { focus(options) { focusedWith = options; } };
  const dialog = {
    classList: { add() {}, remove() {} },
    scrollTop: 0,
    setAttribute(name, value) { this[name] = value; },
    addEventListener() {},
    querySelector(selector) {
      assert.equal(selector, '#lh-demo-title');
      return heading;
    },
    showModal() { this.scrollTop = 200; },
    close() {},
  };
  const chip = { classList: { add() {}, remove() {} } };
  let created = 0;
  const document = {
    readyState: 'complete',
    createElement() { return created++ === 0 ? dialog : chip; },
    body: { appendChild() {} },
    getElementById() { return { addEventListener() {} }; },
  };

  runInNewContext(readFileSync(new URL('../DemoExperience.js', import.meta.url), 'utf8'), {
    document,
    sessionStorage: storage({}),
    localStorage: storage({}),
    window: { location: { reload() {} } },
  });

  assert.equal(dialog['aria-labelledby'], 'lh-demo-title');
  assert.equal(dialog.scrollTop, 0);
  assert.deepEqual({ ...focusedWith }, { preventScroll: true });
  assert.match(dialog.innerHTML, /id="lh-demo-title" tabindex="-1"/);
  assert.doesNotMatch(dialog.innerHTML, /autofocus/);
});
