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

import { taskJob, planTasks, jobFile } from '../next/js/features/agents/folder-tasks.js';

const DIR = '/srv/caps';
const shellTask = { file: 'tasks/nightly.md', name: 'nightly', description: 'Collects the map.', schedule: '0 4 * * *', agent: 'shell', run: 'bash scripts/collect.sh', enabled: true };
const agentTask = { file: 'tasks/triage.md', name: 'triage', description: '', every_minutes: '90', agent: 'codex', enabled: false };

test('a shell task types its command, then exit, on Codeman daily schedule', () => {
  const { job } = taskJob(shellTask, DIR);
  assert.equal(job.agentType, 'shell');
  assert.equal(job.launchCommand, 'bash scripts/collect.sh');
  assert.equal(job.promptText, 'exit');
  assert.equal(job.inputMode, 'typed');
  assert.equal(job.workingDir, DIR);
  assert.equal(job.scheduleType, 'daily');
  assert.equal(job.dailyTime, '04:00');
  assert.equal(jobFile(job), 'tasks/nightly.md');
});

test('an agent task points the agent at its file and uses an interval', () => {
  const { job } = taskJob(agentTask, DIR);
  assert.equal(job.launchCommand, '');
  assert.match(job.promptText, /tasks\/triage\.md/);
  assert.ok(!job.promptText.includes('\n'));
  assert.equal(job.scheduleType, 'interval');
  assert.equal(job.intervalMinutes, 90);
  assert.equal(job.enabled, false);
});

test('tasks Codeman cannot schedule or that the server flagged are invalid', () => {
  assert.ok(taskJob({ ...shellTask, schedule: '*/5 * * * *' }, DIR).error);
  assert.ok(taskJob({ ...agentTask, every_minutes: '0' }, DIR).error);
  assert.equal(taskJob({ ...shellTask, error: 'missing agent' }, DIR).error, 'missing agent');
});

test('planning compares files with existing jobs and keeps orphans visible', () => {
  const synced = { id: 'a', ...taskJob(shellTask, DIR).job, nextRunAt: 1 };
  const drifted = { id: 'b', ...taskJob(agentTask, DIR).job, intervalMinutes: 30, name: 'renamed in Codeman' };
  const orphan = { id: 'c', name: 'old', notes: 'loom-task:tasks/old.md' };
  const manual = { id: 'd', name: 'manual', notes: 'mine' };
  const rows = planTasks([shellTask, agentTask, { file: 'tasks/new.md', name: 'new', schedule: '30 6 * * 1-5', agent: 'claude', enabled: true }],
    [synced, drifted, orphan, manual], DIR);
  assert.deepEqual(rows.map(r => r.state), ['synced', 'changed', 'new', 'orphan']);
  assert.deepEqual(rows[1].changes.sort(), ['intervalMinutes', 'name']);
  assert.equal(rows[1].existing.id, 'b');
  assert.equal(rows[2].job.scheduleType, 'weekly');
  assert.equal(rows[3].existing.id, 'c');
});
