// Tasks declared as Markdown files in the capabilities folder (tasks/*.md)
// become Codeman cron jobs. The file is the source of truth: applying it
// creates the job or brings it back in line. A job carries its file in its
// notes so Loom finds it again, even after a rename in Codeman.
import { parseCron } from './schedule.js';

export const TASK_MARK = 'loom-task:';
const SCHEDULE_FIELDS = ['dailyTime', 'weeklyDays', 'weeklyTime', 'intervalMinutes'];
const COMPARED = ['name', 'agentType', 'workingDir', 'launchCommand', 'promptMode', 'promptText', 'inputMode', 'scheduleType', 'enabled', 'notes', ...SCHEDULE_FIELDS];

export const taskMark = file => TASK_MARK + file;
export const jobFile = job => {
  const line = String(job?.notes || '').split('\n').find(l => l.startsWith(TASK_MARK));
  return line ? line.slice(TASK_MARK.length).trim() : '';
};

function scheduleOf(task) {
  if (task.every_minutes) {
    const n = Number(task.every_minutes);
    return Number.isInteger(n) && n >= 1 && n <= 525600 ? { scheduleType: 'interval', intervalMinutes: n } : null;
  }
  return parseCron(task.schedule);
}

// The Codeman job body for a task, or { error } when it cannot be scheduled.
export function taskJob(task, dir) {
  if (task.error) return { error: task.error };
  const when = scheduleOf(task);
  if (!when) return { error: 'schedule' };
  const shell = task.agent === 'shell';
  const job = {
    name: task.name,
    agentType: task.agent,
    workingDir: dir,
    promptMode: 'inline_text',
    // A shell job types its command first, then the prompt: exit ends the
    // session once the command is done. An agent reads the file itself, so a
    // change to the file needs no new apply.
    promptText: shell ? 'exit' : `Run the scheduled task described in ${task.file} in this folder and follow its instructions.`,
    inputMode: shell ? 'typed' : 'paste',
    launchCommand: shell ? task.run : '',
    enabled: !!task.enabled,
    concurrencyPolicy: 'skip_if_same_agent_running',
    autoClosePreviousSession: true,
    notes: [task.description, taskMark(task.file)].filter(Boolean).join('\n\n'),
    ...when,
  };
  return { job };
}

const same = (a, b) => Array.isArray(a) || Array.isArray(b)
  ? JSON.stringify([...(a || [])].sort()) === JSON.stringify([...(b || [])].sort())
  : (a ?? '') === (b ?? '');

// Fields the existing job must change to match the task.
export function jobChanges(existing, job) {
  return COMPARED.filter(k => k in job && !(SCHEDULE_FIELDS.includes(k) && job[k] === undefined) && !same(existing[k], job[k]));
}

// One row per task file plus the Codeman jobs whose file has disappeared.
// state: synced | changed | new | invalid | orphan
export function planTasks(tasks, jobs, dir) {
  const byFile = new Map((jobs || []).map(j => [jobFile(j), j]).filter(([f]) => f));
  const rows = (tasks || []).map(task => {
    const existing = byFile.get(task.file);
    byFile.delete(task.file);
    const { job, error } = taskJob(task, dir);
    if (error) return { task, existing, state: 'invalid', error };
    if (!existing) return { task, job, state: 'new' };
    const changes = jobChanges(existing, job);
    return { task, job, existing, changes, state: changes.length ? 'changed' : 'synced' };
  });
  for (const [file, existing] of byFile) rows.push({ task: { file, name: existing.name }, existing, state: 'orphan' });
  return rows;
}
