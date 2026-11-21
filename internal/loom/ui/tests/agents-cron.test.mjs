import test from 'node:test';
import assert from 'node:assert/strict';
import { parseCron } from '../next/js/features/agents/schedule.js';

test('custom cron uses Codeman daily and weekly schedule fields', () => {
  assert.deepEqual(parseCron('0 9 * * *'), { scheduleType: 'daily', dailyTime: '09:00' });
  assert.deepEqual(parseCron('  5 17 * * mon-fri,7  '), {
    scheduleType: 'weekly', weeklyDays: [0, 1, 2, 3, 4, 5], weeklyTime: '17:05',
  });
  assert.deepEqual(parseCron('15 10 * * 0,7,3'), {
    scheduleType: 'weekly', weeklyDays: [0, 3], weeklyTime: '10:15',
  });
  assert.deepEqual(parseCron('03 06 * * 1-7'), { scheduleType: 'daily', dailyTime: '06:03' });
});

test('custom cron rejects expressions the Codeman scheduler cannot represent', () => {
  for (const input of ['@monthly', '*/15 * * * *', '0 9 1 * *', '0 9 * 1 *', '0 9,11 * * *', '0 24 * * *', '60 8 * * *', '0 9 * * 5-2', '0 9 * * FOO', '0 9 * * -1']) {
    assert.equal(parseCron(input), null, input);
  }
});
