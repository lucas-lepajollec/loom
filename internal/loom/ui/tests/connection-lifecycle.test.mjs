import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const source = fs.readFileSync(new URL('../next/js/core/poll.js', import.meta.url), 'utf8');
const module = {};
vm.runInNewContext(source.replace(/^export /gm, '') + '\nglobalThis.connection = typeof visibleConnection === "function" ? visibleConnection : undefined;', module);
const settle = () => new Promise(r => setImmediate(r));
function surface() {
  const listeners = new Map(), timers = new Map(); let seq = 0;
  return { listeners, timers, document: { hidden: false, addEventListener: (n, f) => listeners.set(n, f), removeEventListener: n => listeners.delete(n) },
    navigator: { onLine: true }, AbortController, addEventListener: (n, f) => listeners.set(n, f), removeEventListener: n => listeners.delete(n),
    setTimeout: f => { timers.set(++seq, f); return seq; }, clearTimeout: n => timers.delete(n) };
}
test('one observation connection survives suspend, bfcache, offline and reconnect; old callbacks cannot publish', async () => {
  assert.equal(typeof module.connection, 'function');
  const env = surface(), owners = []; let closed = 0;
  const stop = module.connection(owner => { owners.push(owner); return () => closed++; }, 4000, env);
  await settle(); assert.equal(owners.length, 1); assert.equal(owners[0].alive(), true);
  env.document.hidden = true; env.listeners.get('visibilitychange')();
  assert.equal(closed, 1); assert.equal(owners[0].signal.aborted, true); assert.equal(owners[0].alive(), false);
  env.document.hidden = false; env.listeners.get('pageshow')({ persisted: true }); await settle(); assert.equal(owners.length, 2);
  owners[0].retry(); assert.equal(env.timers.size, 0);
  owners[1].retry(); assert.equal(closed, 2); assert.equal(env.timers.size, 1);
  [...env.timers.values()][0](); await settle(); assert.equal(owners.length, 3);
  env.navigator.onLine = false; env.listeners.get('offline')(); assert.equal(closed, 3);
  env.navigator.onLine = true; env.listeners.get('online')(); await settle(); assert.equal(owners.length, 4);
  stop(); assert.equal(closed, 4); assert.equal(env.listeners.size, 0); assert.equal(env.timers.size, 0);
});
test('late async connection cleanup runs after suspension and cannot replace the resumed connection', async () => {
  assert.equal(typeof module.connection, 'function');
  const env = surface(), finish = []; let closed = 0;
  const stop = module.connection(() => new Promise(r => finish.push(r)), 1000, env);
  env.listeners.get('pagehide')(); env.listeners.get('pageshow')({ persisted: true });
  assert.equal(finish.length, 2);
  finish[0](() => closed++); await settle(); assert.equal(closed, 1);
  finish[1](() => closed++); await settle(); stop(); assert.equal(closed, 2);
});

test('the real terminal view reattaches after mobile resume without opening a process or replaying input', async () => {
  const terminalSource = fs.readFileSync(process.env.LOOM_TERMINAL_BASELINE || new URL('../next/js/features/terminals/page.js', import.meta.url), 'utf8');
  const sockets = [], posts = [], effects = [], env = surface(); let writes = 0, resets = 0;
  class Socket {
    constructor() { this.readyState = 1; sockets.push(this); }
    send() {} close() { this.readyState = 3; }
  }
  class Terminal {
    cols = 80; rows = 24;
    loadAddon() {} open() {} reset() { resets++; } focus() {} dispose() {} onData() {} write() { writes++; }
  }
  const context = { ...env, t: key => key, html: () => null, cls: () => '',
    document: { ...env.document, documentElement: {} }, location: { protocol: 'http:', host: 'fixture' },
    getComputedStyle: () => ({ getPropertyValue: () => '' }), WebSocket: Socket, ResizeObserver: class { observe() {} disconnect() {} },
    window: { Terminal, FitAddon: { FitAddon: class { fit() {} } } },
    useRef: () => ({ current: {} }), useState: v => [v, () => {}], useEffect: fn => effects.push(fn),
    visibleConnection: (connect, delay) => module.connection(connect, delay, env),
    post: async (...args) => { posts.push(args); return { ok: true, ticket: 'synthetic' }; },
  };
  vm.runInNewContext(terminalSource.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nxtermReady = Promise.resolve(window); globalThis.view = TermView;', context);
  context.view({ t: { id: 'existing', title: 'Shell', running: true } });
  const stop = effects[0](); await settle(); assert.equal(sockets.length, 1);
  const oldMessage = sockets[0].onmessage;
  env.listeners.get('pagehide')?.(); env.listeners.get('pageshow')?.({ persisted: true });
  await settle(); assert.equal(sockets.length, 2);
  oldMessage({ data: new ArrayBuffer(4) }); assert.equal(writes, 0);
  sockets[1].onopen(); assert.equal(resets, 1, 'server scrollback replay replaces prior terminal output');
  assert.deepEqual(posts.map(p => p[0]), ['/api/terminals/ticket', '/api/terminals/ticket']);
  assert.equal(posts[1][1].id, 'existing');
  stop(); assert.equal(sockets[1].readyState, 3);
});

test('Tasks rejects malformed SSE snapshots and reconnects the real page on bfcache resume', async () => {
  const source = fs.readFileSync(process.env.LOOM_TASKS_BASELINE || new URL('../next/js/features/tasks/page.js', import.meta.url), 'utf8');
  const env = surface(), streams = [], effects = [], slots = []; let cursor = 0;
  const { validData } = await import('../next/js/core/shape.js');
  const context = { ...env, validData, html: () => null, t: x => x, SectionTabs: 'Tabs', Icon: 'Icon', Empty: 'Empty', cls: () => '',
    EventSource: class { constructor() { streams.push(this); } close() {} },
    useState: initial => { const i = cursor++; if (!(i in slots)) slots[i] = initial; return [slots[i], v => { slots[i] = v; }]; },
    useEffect: f => effects.push(f), get: async () => ({ ok: true, tasks: [] }),
    visibleConnection: (connect, delay) => module.connection(connect, delay, env),
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.view = TasksPage;', context);
  context.view(); const stop = effects[0](); await settle();
  const previous = slots[0]; streams[0].onmessage({ data: '{"ok":true,"tasks":{}}' });
  assert.equal(slots[0], previous); cursor = 0; assert.doesNotThrow(() => context.view());
  env.listeners.get('pagehide')?.(); env.listeners.get('pageshow')?.({ persisted: true }); await settle();
  assert.equal(streams.length, 2); stop();
});
test('Voice rejects malformed frames and reconnects without replaying a turn or reacquiring the microphone', async () => {
  const source = fs.readFileSync(process.env.LOOM_VOICE_BASELINE || new URL('../next/js/features/voice/mode.js', import.meta.url), 'utf8');
  const env = surface(), sockets = [], effects = [], posts = [], states = []; let microphones = 0, buffers = 0, cursor = 0, turns = 0;
  const { validShape } = await import('../next/js/core/shape.js');
  class Audio {
    audioWorklet = { addModule: async () => {} }; destination = {}; currentTime = 0;
    createMediaStreamSource() { return { connect() {} }; }
    createAnalyser() { return { connect() {}, fftSize: 512 }; }
    createBuffer(_channels, length, rate) { assert.ok(length > 0 && rate >= 8000 && rate <= 96000); buffers++; return { copyToChannel() {}, duration: .01 }; }
    createBufferSource() { return { connect() {}, start() {}, stop() {} }; }
    resume() { return Promise.resolve(); } suspend() { return Promise.resolve(); } close() { return Promise.resolve(); }
  }
  const context = { ...env, validShape, ArrayBuffer, TextDecoder, html: () => null, t: x => x, Icon: 'Icon', cls: () => '', Blob,
    URL: { createObjectURL: () => 'synthetic-worklet', revokeObjectURL() {} },
    window: { isSecureContext: true, AudioContext: Audio },
    navigator: { mediaDevices: { getUserMedia: async () => { microphones++; return { getTracks: () => [{ stop() {} }] }; } } },
    location: { protocol: 'https:', host: 'fixture' },
    AudioWorkletNode: class { port = {}; }, WebSocket: class { constructor() { this.readyState = 1; sockets.push(this); } close() { this.readyState = 3; } send() {} },
    useState: v => { const i = cursor++; states[i] = v; return [v, next => { states[i] = typeof next === 'function' ? next(states[i]) : next; }]; }, useRef: v => ({ current: v }), useEffect: f => effects.push(f), setInterval: () => 1, clearInterval() {},
    get: async () => ({ engine: { installed: true }, config: { stt: 'fixture', tts: 'fixture' }, service: { running: true } }),
    post: async url => { posts.push(url); return { ok: true, session_id: 'fixture' }; },
    fetch: async () => { turns++; let read = false; return { ok: true, body: { getReader: () => ({ read: async () => {
      if (read) return { done: true }; read = true;
      return { done: false, value: new TextEncoder().encode('data: {"type":"delta","text":{}}\n\ndata: {"type":"delta","text":"Good."}\n\n') };
    } }) } }; },
    visibleConnection: (connect, delay) => module.connection(connect, delay, env),
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm, '').replace(/^export /gm, '') + '\nglobalThis.view = VoiceMode;', context);
  context.view({ discussionId: 'existing', onClose() {} }); const stop = effects[0](); await settle();
  assert.equal(sockets.length, 1); assert.doesNotThrow(() => sockets[0].onmessage({ data: '{broken' }));
  sockets[0].onmessage({ data: '{"type":"error","error":{}}' }); assert.equal(states[1], '', 'invalid error frames cannot poison rendered error text');
  sockets[0].onmessage({ data: '{"type":"audio_start","sample_rate":16000}' });
  assert.doesNotThrow(() => sockets[0].onmessage({ data: new ArrayBuffer(0) }), 'empty PCM cannot reach Web Audio');
  sockets[0].onmessage({ data: '{"type":"audio_start","sample_rate":1}' });
  assert.doesNotThrow(() => sockets[0].onmessage({ data: new ArrayBuffer(2) }), 'invalid rates cannot reach Web Audio');
  assert.equal(buffers, 0);
  sockets[0].onmessage({ data: '{"type":"audio_start","sample_rate":16000}' });
  sockets[0].onmessage({ data: new ArrayBuffer(2) }); assert.equal(buffers, 1, 'valid PCM still reaches playback');
  env.listeners.get('pagehide')?.(); env.listeners.get('pageshow')?.({ persisted: true }); await settle();
  assert.equal(sockets.length, 2); assert.equal(microphones, 1); assert.deepEqual(posts, ['/api/voice/jarvis/session']);
  sockets[1].onmessage({ data: '{"type":"final","text":"Synthetic input"}' }); await settle();
  assert.equal(states[2].at(-1).text, 'Good.', 'malformed turn deltas cannot corrupt retained text');
  env.listeners.get('pagehide')?.(); env.listeners.get('pageshow')?.({ persisted: true }); await settle();
  assert.equal(turns, 1, 'reconnection cannot replay a turn'); stop();
});
