import test from 'node:test';
import assert from 'node:assert/strict';
import { currentWindow, dailyUse, H, DAY } from '../next/js/features/agents/quota-math.js';

const now = Date.UTC(2026, 10, 19, 15, 0);

test('current window projects exhaustion from the recent rate', () => {
  const reset = now + 2 * H; // fenêtre de 5 h ouverte il y a 3 h
  const samples = [
    { at: now - 2 * H, claude: { '5h': { used: 10, reset_at: reset } } },
    { at: now - H, claude: { '5h': { used: 30, reset_at: reset + 20e3 } } }, // décalage de quelques secondes : même fenêtre
    { at: now - 5 * H, claude: { '5h': { used: 90, reset_at: now - 10 } } }, // fenêtre précédente : ignorée
  ];
  const w = currentWindow(samples, 'claude', '5h', { window: '5h', used: 60, reset_at: reset }, now);
  assert.equal(w.pts.length, 3);
  assert.equal(w.start, reset - 5 * H);
  // 30 points en une heure : 100 % atteint dans 1 h 20, avant la remise à zéro.
  assert.ok(Math.abs(w.eta - (now + (40 / 30) * H)) < 1000);
  assert.equal(Math.round(w.expected), 60);
});

test('slow use reports the share reached at reset', () => {
  const reset = now + 3 * DAY;
  const samples = [{ at: now - DAY, codex: { '7d': { used: 20, reset_at: reset } } }];
  const w = currentWindow(samples, 'codex', '7d', { window: '7d', used: 30, reset_at: reset }, now);
  assert.equal(w.eta, null);
  assert.equal(Math.round(w.atReset), 60);
  assert.equal(currentWindow([], 'codex', '7d', { window: '7d', used: 0 }, now), null);
  assert.equal(currentWindow([], 'x', 'k', { window: 'other', used: 5, reset_at: reset }, now), null);
});

test('daily use sums increases and restarts after a reset', () => {
  const d0 = new Date(now); d0.setHours(12, 0, 0, 0);
  const t = d0.getTime(), r1 = t + DAY, r2 = t + 8 * DAY;
  const samples = [
    { at: t - DAY, agy: { w: { used: 5, reset_at: r1 } } },
    { at: t - DAY + H, agy: { w: { used: 12, reset_at: r1 } } }, // +7 hier
    { at: t, agy: { w: { used: 20, reset_at: r1 } } }, // +8 aujourd'hui
    { at: r1 + H, agy: { w: { used: 4, reset_at: r2 } } }, // remise à zéro : +4 demain
  ];
  const days = dailyUse(samples, 'agy', 'w', 3, r1 + 2 * H);
  assert.deepEqual(days.map(d => d.used), [7, 8, 4]);
});
