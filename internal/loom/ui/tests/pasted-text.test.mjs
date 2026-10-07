import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';
import { french } from './i18n-fixture.mjs';
import { attachPaste, pastedMessage, splitPastedMessage, PASTE_THRESHOLD, MAX_MESSAGE_BYTES } from '../next/js/features/chat/pasted-text.js';

test('long text becomes a removable TXT without changing short paste behavior', () => {
  assert.equal(attachPaste('before after', 7, 7, 'x'.repeat(PASTE_THRESHOLD), 1), null);
  const content = '  é🙂\r\n'.repeat(500) + '\n\n--- End Loom text attachments ---\n';
  const result = attachPaste('before REPLACE after', 7, 14, content, 1);
  assert.equal(result.text, 'before  after');
  assert.equal(result.caret, 7);
  assert.equal(result.file.content, content);
  assert.equal(result.file.name, 'pasted-text-1.txt');
  const files = [result.file, { name: 'pasted-text-2.txt', content: ' <script>alert(1)</script>\n\t ' }];
  for (const prompt of ['Read these notes', '']) {
    const wire = pastedMessage(prompt, files);
    const restored = splitPastedMessage(wire.trim());
    assert.equal(restored.text, prompt);
    assert.deepEqual(restored.files, files);
    for (const file of files) assert.ok(wire.includes(file.content));
  }
});

test('ordinary messages or malformed attachment markers are displayed without hiding text', () => {
  for (const text of ['normal message', '--- Loom text attachments ---\npasted-text-1.txt (99999 UTF-16 units)\nshort\n--- End Loom text attachments ---']) {
    assert.deepEqual(splitPastedMessage(text), { text, files: [] });
  }
});

const flatten = x => Array.isArray(x) ? x.flatMap(flatten) : x && typeof x === 'object' ? [x, ...flatten(x.children)] : [];
function composer(mode = 'thread', accepted = false) {
  const states = [], refs = [], calls = [], notices = [];
  let stateIndex = 0, refIndex = 0, tree;
  const env = { t: french, html: htm.bind((type, props, ...children) => ({ type, props: props || {}, children })),
    TextEncoder, setTimeout: f => f(), attachPaste, pastedMessage, downloadPaste() {}, MAX_MESSAGE_BYTES,
    runtimeCaps: () => [], currentExec: () => ({ name: 'Fixture' }), slashEntries: () => [],
    chat: { get: () => ({ mode, busy: false, session: { runtime_id: 'pi', workdir: '/fixture' } }) },
    app: { get: () => ({ status: { health: true } }) },
    useStore: (store, select) => select(store.get()), useEffect() {},
    useState: init => { const i = stateIndex++; if (!(i in states)) states[i] = init; return [states[i], value => { states[i] = typeof value === 'function' ? value(states[i]) : value; }]; },
    useRef: init => { const i = refIndex++; return refs[i] ||= { current: init }; },
    Icon: 'Icon', Popover: 'Popover', cls: (...v) => v.filter(Boolean).join(' '),
    toast: text => notices.push(text), send: async (...args) => { calls.push(args); return accepted; },
  };
  const src = fs.readFileSync(new URL('../next/js/features/chat/composer.js', import.meta.url), 'utf8');
  vm.runInNewContext(src.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.render = Composer;', env);
  const render = () => { stateIndex = refIndex = 0; tree = env.render(); return flatten(tree); };
  const paste = content => {
    const ta = render().find(n => n.type === 'textarea');
    const event = { clipboardData: { getData: () => content }, currentTarget: { selectionStart: states[0].length, selectionEnd: states[0].length }, preventDefault() { this.prevented = true; } };
    ta.props.onPaste(event); return event;
  };
  const submit = () => render().find(n => n.type === 'button' && n.props.class === 'send').props.onClick();
  render();
  return { env, states, calls, notices, paste, submit };
}

test('local, cloud and harness sends include pasted text, and a refused send preserves the draft and TXT', async () => {
  for (const mode of ['native', 'thread']) {
    const f = composer(mode);
    f.states[0] = 'Please read';
    const content = 'exact pasted content\n'.repeat(300);
    assert.equal(f.paste(content).prevented, true);
    assert.equal(f.states[1].length, 1);
    await f.submit();
    assert.equal(f.calls.length, 1);
    assert.ok(f.calls[0][0].includes(content));
    assert.equal(f.states[0], 'Please read');
    assert.equal(f.states[1][0].content, content);
    f.env.send = async () => true;
    await f.submit();
    assert.equal(f.states[0], '');
    assert.equal(f.states[1].length, 0);
  }
});

test('oversized paste is refused explicitly without deleting existing draft or attachments', () => {
  const f = composer();
  f.states[0] = 'keep';
  assert.equal(f.paste('é'.repeat(MAX_MESSAGE_BYTES)).prevented, true);
  assert.equal(f.states[0], 'keep');
  assert.equal(f.states[1].length, 0);
  assert.equal(f.notices.length, 1);
});
