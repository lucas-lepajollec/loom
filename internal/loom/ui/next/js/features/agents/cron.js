// Codeman stores scheduled jobs and reports session startup and prompt delivery.
import { lt } from './labels.js';
import { ListPick } from '../../ui/listpick.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Seg } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { api } from './client.js';
import { agents, cliOf, onDomain, projectOf, upsertSession } from './store.js';
import { openSession } from './nav.js';
import { CliMark } from './new-session.js';
import { parseCron } from './schedule.js';
import { planTasks } from './folder-tasks.js';
import { loadCapabilities } from '../connections/page.js';

const fmt = ms => new Date(ms).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const DAYS = [1, 2, 3, 4, 5, 6, 0];
const dayName = (d, weekday) => new Date(2024, 0, 7 + d).toLocaleDateString(locale(), { weekday });

function clockTime(time) {
  if (!time) return '—';
  const [h, m] = time.split(':').map(Number);
  if (locale() === 'fr-FR') {
    return m ? t('agents.cron.hour_minutes', { h, m: pad(m) }) : t('agents.cron.hour', { h });
  }
  return new Intl.DateTimeFormat(locale(), { hour: 'numeric', minute: '2-digit', timeZone: 'UTC' })
    .format(Date.UTC(2024, 0, 1, h, m));
}

export function schedule(job) {
  switch (job.scheduleType) {
    case 'once': return job.runAt ? t('agents.cron.once_at', { at: fmt(job.runAt) }) : '—';
    case 'interval': return t('agents.cron.every', { n: job.intervalMinutes });
    case 'daily': return t('agents.cron.daily_at', { at: clockTime(job.dailyTime) });
    case 'weekly': {
      const days = DAYS.filter(d => job.weeklyDays?.includes(d)).map(d => dayName(d, 'long'));
      return t('agents.cron.weekly_at', { days: new Intl.ListFormat(locale()).format(days), at: clockTime(job.weeklyTime) });
    }
    default: return '—';
  }
}

const pad = n => String(n).padStart(2, '0');
const localInput = ms => {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
};
const SCHEDULE_TYPES = ['once', 'interval', 'daily', 'weekly'];

function JobForm({ job, onClose, onSaved, onDelete, phone }) {
  const registry = useStore(agents, s => s.clis);
  const sessions = useStore(agents, s => s.sessions);
  const unified = useStore(agents, s => s.unified);
  const cases = useStore(agents, s => s.cases);
  const defaults = useStore(agents, s => s.settings);
  // Codeman's admin registry can be unavailable to regular users. Their
  // existing sessions and configured default still identify known modes.
  const modes = [defaults.runMode, ...Object.values(sessions).map(s => s.mode), job?.agentType].filter(Boolean);
  const clis = registry.length ? registry.filter(c => c.enabled !== false && c.installed !== false) : [...new Set(modes)].map(cliOf);
  const defaultCli = clis.find(c => c.id === defaults.runMode) || clis.find(c => c.id === 'claude') || clis[0];
  const [v, setV] = useState(() => ({
    name: '', agentType: defaultCli?.id || '', workingDir: defaults.defaultWorkingDir || unified.find(r => r.workingDir && !r.remote)?.workingDir || '', promptMode: 'inline_text', promptText: '', promptFilePath: '',
    inputMode: 'paste', scheduleType: 'daily', runAt: Date.now() + 3600e3, intervalMinutes: 60, dailyTime: '09:00', weeklyDays: [1], weeklyTime: '09:00',
    enabled: true, concurrencyPolicy: 'skip_if_same_agent_running', autoClosePreviousSession: true, notes: '', launchCommand: '',
    ...Object.fromEntries(Object.entries(job || {}).filter(([, x]) => x != null)),
  }));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [runs, setRuns] = useState(null);
  const [runsError, setRunsError] = useState('');
  const [historyOpen, setHistoryOpen] = useState(false);
  const title = job ? t('agents.cron.edit') : t('agents.cron.new');
  const custom = v.scheduleType === 'custom';
  const parsed = custom && parseCron(v.expression);
  const scheduleValid = {
    once: Number.isFinite(v.runAt) && v.runAt > 0,
    interval: Number.isInteger(Number(v.intervalMinutes)) && v.intervalMinutes >= 1 && v.intervalMinutes <= 525600,
    daily: !!v.dailyTime,
    weekly: !!v.weeklyDays.length && !!v.weeklyTime,
    custom: !!parsed,
  };
  const prompt = v.promptMode === 'inline_text' ? v.promptText.replace(/[\r\n]+/g, ' ').trim() : v.promptFilePath.trim();
  const valid = scheduleValid[v.scheduleType] && v.name.trim() && v.agentType && v.workingDir.trim() && prompt;

  useEffect(() => {
    if (!job && defaultCli) setV(draft => draft.agentType ? draft : { ...draft, agentType: defaultCli.id });
  }, [defaultCli?.id, job?.id]);
  useEffect(() => {
    if (!job || !historyOpen) return;
    let active = true;
    let revision = 0;
    const load = async () => {
      const request = ++revision;
      try {
        const data = await api.cronJobRuns(job.id);
        if (active && request === revision) { setRuns(data || []); setRunsError(''); }
      } catch (e) {
        if (active && request === revision) setRunsError(e.message);
      }
    };
    load();
    const off = onDomain('cron', load);
    return () => { active = false; off(); };
  }, [job?.id, historyOpen]);

  const set = patch => setV(draft => ({ ...draft, ...patch }));
  const body = () => {
    const b = {
      name: v.name.trim(), agentType: v.agentType, workingDir: v.workingDir.trim(), promptMode: v.promptMode,
      inputMode: v.inputMode, scheduleType: v.scheduleType, concurrencyPolicy: v.concurrencyPolicy,
    };
    if (!job) b.enabled = v.enabled;
    if (v.promptMode === 'inline_text') b.promptText = prompt;
    else b.promptFilePath = prompt;
    switch (v.scheduleType) {
      case 'once': b.runAt = v.runAt; break;
      case 'interval': b.intervalMinutes = Number(v.intervalMinutes); break;
      case 'daily': b.dailyTime = v.dailyTime; break;
      case 'weekly': Object.assign(b, { weeklyDays: v.weeklyDays, weeklyTime: v.weeklyTime }); break;
      case 'custom': Object.assign(b, parsed); break;
    }
    if (b.scheduleType !== 'once') b.autoClosePreviousSession = v.autoClosePreviousSession;
    if (v.notes.trim() || job?.notes) b.notes = v.notes.trim();
    if (v.agentType === 'shell' || job?.launchCommand) b.launchCommand = v.agentType === 'shell' ? v.launchCommand.trim() : '';
    return b;
  };
  const submit = async e => {
    e.preventDefault();
    if (busy || !valid) return;
    setBusy(true);
    setError('');
    try {
      const result = job ? await api.cronUpdate(job.id, body()) : await api.cronCreate(body());
      onSaved(result?.job);
    } catch (e) { setError(e.message); }
    finally { setBusy(false); }
  };
  const remove = async () => {
    if (busy || !await confirm(t('agents.cron.delete'), job.name, { ok: t('agents.cron.delete'), danger: true })) return;
    setBusy(true);
    setError('');
    try { await api.cronDelete(job.id); onDelete(job.id); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  };
  const folders = [...new Set([...cases.filter(c => c.location !== 'remote').map(c => c.path), ...unified.filter(r => !r.remote).map(r => r.workingDir)].filter(Boolean))];
  const harnesses = clis.map(c => ({ value: c.id, label: c.label }));
  if (v.agentType && !clis.some(c => c.id === v.agentType)) {
    harnesses.unshift({ value: v.agentType, label: cliOf(v.agentType).label });
  }
  const formId = 'cm-job-form';
  const actions = html`<button type="button" class="btn" disabled=${busy} onClick=${onClose}>${t('agents.cancel')}</button>
    <button type="submit" form=${formId} class="btn primary" disabled=${busy || !valid}>${t('agents.save')}</button>`;
  const bodyForm = html`
    <form id=${formId} onSubmit=${submit}>
      ${(!phone || job) && html`<div class="cm-section-head">
        ${!phone && html`<h2>${job ? job.name : title}</h2>`}
        ${job && html`<button type="button" class="btn sm danger" disabled=${busy} onClick=${remove}>${t('agents.cron.delete')}</button>`}</div>`}
      <label class="field"><span>${t('agents.cron.name')}</span><input class="input" maxlength="200" value=${v.name} onInput=${e => set({ name: e.target.value })} required /></label>
      <label class="cm-editor-row"><span>${t('agents.cron.harness')}</span>
        <${ListPick} value=${v.agentType} placeholder=${t('agents.cron.pick_harness')} options=${harnesses} onChange=${agentType => set({ agentType })} />
      </label>
      <label class="cm-editor-row"><span>${t('agents.new.folder')}</span><input class="input mono" value=${v.workingDir} onInput=${e => set({ workingDir: e.target.value })} list="cm-cron-dirs" required />
        <datalist id="cm-cron-dirs">${folders.map(dir => html`<option key=${dir} value=${dir} />`)}</datalist></label>
      ${v.agentType === 'shell' && html`<label class="field"><span>${t('agents.cron.command')}</span><input class="input mono" maxlength="2000" value=${v.launchCommand} onInput=${e => set({ launchCommand: e.target.value })} /></label>`}
      <div class="field"><div class="cm-section-head"><label for="cm-cron-prompt">${t('agents.cron.prompt')}</label>
        <${Seg} size="sm" value=${v.promptMode} onChange=${m => set({ promptMode: m })} label=${t('agents.cron.prompt')} options=${[{ value: 'inline_text', label: t('agents.cron.inline') }, { value: 'prompt_file_path', label: t('agents.cron.file') }]} /></div>
        ${v.promptMode === 'inline_text' ? html`<textarea id="cm-cron-prompt" class="textarea" rows="3" maxlength="100000" value=${v.promptText} onInput=${e => set({ promptText: e.target.value })} required></textarea><small>${t('agents.composer.single_line')}</small>`
          : html`<input id="cm-cron-prompt" class="input mono" placeholder="/…/prompt.md" value=${v.promptFilePath} onInput=${e => set({ promptFilePath: e.target.value })} required /><small>${t('agents.cron.file_hint')}</small>`}</div>
      <div class="field">
        <label for="cm-cron-schedule">${t('agents.cron.when')}</label>
        <${ListPick} id="cm-cron-schedule" value=${v.scheduleType} onChange=${scheduleType => set({ scheduleType })}
          options=${[...SCHEDULE_TYPES.map(type => ({ value: type, label: lt('cronType', type) })),
            { value: 'custom', label: t('agents.cron.custom') }]} />
        ${v.scheduleType === 'once' && html`<input class="input" aria-label=${t('agents.cron.when')} type="datetime-local" value=${Number.isFinite(v.runAt) ? localInput(v.runAt) : ''} onInput=${e => set({ runAt: new Date(e.target.value).getTime() })} required />`}
        ${v.scheduleType === 'interval' && html`<div class="cm-inline"><input class="input" aria-label=${t('agents.cron.minutes')} type="number" min="1" max="525600" value=${v.intervalMinutes} onInput=${e => set({ intervalMinutes: e.target.value })} required /><span class="muted">${t('agents.cron.minutes')}</span></div>`}
        ${v.scheduleType === 'weekly' && html`<div class="chips" role="group" aria-label=${t('agents.cron.days')}>${DAYS.map(d => html`<button key=${d} type="button" aria-pressed=${v.weeklyDays.includes(d)} class=${cls('btn sm', v.weeklyDays.includes(d) && 'on')} onClick=${() => set({ weeklyDays: v.weeklyDays.includes(d) ? v.weeklyDays.filter(x => x !== d) : [...v.weeklyDays, d] })}>${dayName(d, 'short')}</button>`)}</div>`}
        ${['daily', 'weekly'].includes(v.scheduleType) && html`<input class="input" aria-label=${t('agents.admin.time')} type="time"
          value=${v[v.scheduleType + 'Time'].padStart(5, '0')} onInput=${e => set({ [v.scheduleType + 'Time']: e.target.value })} required />`}
        ${custom && html`<input class="input mono" aria-label=${t('agents.cron.custom')} aria-describedby="cm-cron-expression-hint"
          placeholder="0 9 * * MON-FRI" value=${v.expression || ''} onInput=${e => set({ expression: e.target.value })} required
          aria-invalid=${!!v.expression && !parsed} />
          <small id="cm-cron-expression-hint">${t('agents.cron.custom_hint')}</small>
          ${v.expression && html`<small role="status" class=${parsed ? '' : 'cm-run-err'}>${parsed ? schedule(parsed) : t('agents.cron.custom_invalid')}</small>`}`}
        ${!['once', 'interval'].includes(v.scheduleType) && html`<small>${t('agents.cron.server_time')}</small>`}
      </div>
      <details class="cm-job-history"><summary>${t('agents.cron.options')}</summary>
        <label class="cm-editor-row"><span>${t('agents.cron.input')}</span>
          <${ListPick} value=${v.inputMode} onChange=${inputMode => set({ inputMode })}
            options=${[{ value: 'paste', label: t('agents.cron.input_paste') }, { value: 'typed', label: t('agents.cron.input_typed') }]} />
        </label>
        <label class="cm-editor-row"><span>${t('agents.cron.concurrency')}</span>
          <${ListPick} value=${v.concurrencyPolicy} onChange=${concurrencyPolicy => set({ concurrencyPolicy })}
            options=${[{ value: 'skip_if_same_agent_running', label: t('agents.cron.skip') }, { value: 'warn_only', label: t('agents.cron.warn') }]} />
        </label>
        ${!job && html`<label class="cm-switch"><span>${t('agents.cron.enabled')}</span><${Switch} checked=${v.enabled} onChange=${enabled => set({ enabled })} label=${t('agents.cron.enabled')} /></label>`}
        ${v.scheduleType !== 'once' && html`<label class="cm-switch"><span><b>${t('agents.cron.autoclose')}</b><small>${t('agents.cron.autoclose_hint')}</small></span>
          <${Switch} checked=${v.autoClosePreviousSession} onChange=${autoClosePreviousSession => set({ autoClosePreviousSession })} label=${t('agents.cron.autoclose')} /></label>`}
        <label class="field"><span>${t('agents.cron.notes')}</span><input class="input" maxlength="2000" value=${v.notes} onInput=${e => set({ notes: e.target.value })} /></label>
      </details>
      ${error && html`<p role="alert" class="msg-err">${error}</p>`}
      ${!phone && html`<div class="cm-editor-foot">${actions}</div>`}
    </form>
    ${job && html`<details class="cm-job-history" onToggle=${e => setHistoryOpen(e.currentTarget.open)}><summary>${t('agents.cc.runs')}</summary>
      ${runsError && html`<p role="alert" class="msg-err">${runsError}</p>`}
      ${runs ? html`<${Runs} runs=${runs} />` : !runsError && html`<span class="spinner"></span>`}</details>`}`;
  if (phone) return html`<${Modal} title=${job ? job.name : title} onClose=${() => { if (!busy) onClose(); }}
    class="cm-job-sheet" foot=${actions} focusField=${false}>
    <div class="cm-job-editor">${bodyForm}</div>
  </${Modal}>`;
  return html`<aside class="cm-job-editor" aria-label=${title}>${bodyForm}</aside>`;
}

function Runs({ jobs, runs }) {
  const [opening, setOpening] = useState('');
  const recent = [...runs].sort((a, b) => b.startedAt - a.startedAt).slice(0, 8);
  const open = async (e, id) => {
    if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    if (opening) return;
    if (!agents.get().sessions[id]) {
      setOpening(id);
      try { upsertSession(await api.session(id)); }
      catch (error) {
        toast(error.code === 'NOT_FOUND' || error.status === 404 ? t('agents.session.missing') : error.message, 'err');
        return;
      } finally { setOpening(''); }
    }
    openSession(id);
  };
  if (!recent.length) return html`<p class="cm-admin-empty">${t('agents.cc.no_runs')}</p>`;
  const names = new Map(jobs?.map(j => [j.id, j.name]));
  return html`<ul class="cm-runs">${recent.map(run => html`<li key=${run.id}>
    <div class="cm-run-main">
      ${jobs && html`<b>${names.get(run.cronJobId) || run.sessionName || run.cronJobId}</b>`}
      <span class=${cls('cm-run-info', run.status === 'failed' && 'cm-run-err')}>
        <time datetime=${new Date(run.startedAt).toISOString()}>${fmt(run.startedAt)}</time> · ${lt('runStatus', run.status)}${run.triggerType === 'manual_run_now' ? ' · ' + t('agents.cron.manual') : ''}
      </span>
      ${run.errorMessage && html`<small class="cm-run-err">${run.errorMessage}</small>`}
    </div>
    ${run.sessionId && html`<a class="btn sm" href=${'#/agents/s/' + encodeURIComponent(run.sessionId)} onClick=${e => open(e, run.sessionId)}>${t('agents.cc.open')}</a>`}
  </li>`)}</ul>`;
}

// Tâches du dossier des capacités (tasks/*.md). Le fichier fait foi :
// « Appliquer » crée la tâche Codeman ou la réaligne sur son fichier.
const TASK_STATE = { synced: 'agents.tasks.synced', changed: 'agents.tasks.changed', new: 'agents.tasks.new', invalid: 'agents.tasks.invalid', orphan: 'agents.tasks.orphan' };

function FolderTasks({ jobs, onApplied }) {
  const [folder, setFolder] = useState(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => { loadCapabilities().then(setFolder); }, []);
  if (!folder?.dir || folder.error || !folder.tasks?.length || jobs === null) return null;
  const rows = planTasks(folder.tasks, jobs, folder.dir);
  const pending = rows.filter(r => r.state === 'new' || r.state === 'changed');
  const apply = async () => {
    setBusy(true);
    let failed = 0;
    for (const r of pending) {
      try { r.existing ? await api.cronUpdate(r.existing.id, r.job) : await api.cronCreate(r.job); }
      catch (e) { failed++; toast(r.task.name + ' · ' + e.message, 'err'); }
    }
    setBusy(false);
    if (!failed) toast(t('agents.tasks.applied', { n: pending.length }));
    onApplied();
  };
  const remove = async r => {
    if (!await confirm(t('agents.tasks.remove_title'), t('agents.tasks.remove_text', { name: r.existing.name }), { ok: t('agents.tasks.remove'), danger: true })) return;
    try { await api.cronDelete(r.existing.id); } catch (e) { toast(e.message, 'err'); }
    onApplied();
  };
  return html`<section class="cm-cron-folder">
    <div class="cm-cron-h"><div><h2>${t('agents.tasks.folder')}</h2><p>${t('agents.tasks.folder_lead')}</p></div>
      ${pending.length > 0 && html`<button class="btn primary" disabled=${busy} onClick=${apply}>${t('agents.tasks.apply', { n: pending.length })}</button>`}</div>
    <ul class="cm-jobs">${rows.map(r => html`<li key=${r.task.file} class="cm-job">
      <span class="cm-job-t"><${CliMark} mode=${r.job?.agentType || r.existing?.agentType || r.task.agent || 'shell'} size="sm" />
        <span><b>${r.task.name}</b>
          <small>${r.task.file}</small>
          ${r.job && html`<small>${schedule(r.job)}${r.job.enabled ? '' : ' · ' + t('agents.cron.disabled')}</small>`}
          <small class=${r.state === 'invalid' ? 'cm-run-err' : ''}>${t(TASK_STATE[r.state])}${r.error ? ' · ' + (r.error === 'schedule' ? t('agents.tasks.bad_schedule') : r.error) : ''}</small>
        </span></span>
      ${r.state === 'orphan' && html`<button class="btn sm" onClick=${() => remove(r)}>${t('agents.tasks.remove')}</button>`}
    </li>`)}</ul>
  </section>`;
}

const NEW_JOB = Symbol('new job');

export function CronPage({ phone }) {
  const live = useStore(agents, s => s.live);
  const [jobs, setJobs] = useState(null);
  const [runs, setRuns] = useState(null);
  const [jobsError, setJobsError] = useState('');
  const [runsError, setRunsError] = useState('');
  const [selection, setSelection] = useState(null);
  const [pending, setPending] = useState({});
  const editor = useRef(null);
  const revision = useRef(0);
  const load = async () => {
    const request = ++revision.current;
    await Promise.all([
      api.cronJobs().then(data => {
        if (revision.current === request) { setJobs(data || []); setJobsError(''); }
      }).catch(e => { if (revision.current === request) setJobsError(e.message); }),
      api.cronRuns().then(data => {
        if (revision.current === request) { setRuns(data || []); setRunsError(''); }
      }).catch(e => { if (revision.current === request) setRunsError(e.message); }),
    ]);
  };
  useEffect(() => {
    load();
    const off = onDomain('cron', load);
    return () => { revision.current++; off(); };
  }, [live]);
  useEffect(() => {
    if (selection === null || !editor.current) return;
    editor.current.querySelector('input')?.focus();
  }, [selection]);
  const act = async (id, fn) => {
    setPending(p => ({ ...p, [id]: true }));
    try { await fn(); }
    catch (e) { toast(e.message, 'err'); }
    finally { await load(); setPending(p => ({ ...p, [id]: false })); }
  };
  const runNow = j => act(j.id, async () => {
    const { run } = await api.cronRun(j.id);
    if (run?.status === 'failed' || run?.status === 'skipped') {
      toast(run.errorMessage || lt('runStatus', run.status), 'err');
    } else toast(t('agents.cron.launched'));
  });
  const job = jobs?.find(j => j.id === selection);
  const editing = !!job || selection === NEW_JOB;
  const saved = updated => {
    revision.current++;
    if (updated) setJobs(prev => prev?.some(j => j.id === updated.id)
      ? prev.map(j => j.id === updated.id ? updated : j)
      : [...(prev || []), updated]);
    setSelection(current => current === selection ? null : current);
    load();
  };
  const deleted = id => {
    revision.current++;
    setJobs(prev => prev?.filter(j => j.id !== id));
    setRuns(prev => prev?.filter(run => run.cronJobId !== id));
    setSelection(current => current === selection ? null : current);
    load();
  };
  const jobForm = editing && html`<${JobForm} key=${job?.id || 'new'} job=${job} phone=${phone}
    onClose=${() => setSelection(null)} onDelete=${deleted} onSaved=${saved} />`;
  const Heading = phone ? 'h2' : 'h1';
  return html`<div class="cm-scroll"><div class=${cls('cm-cron', editing && !phone && 'editing')}>
    <section class="cm-cron-list">
      <div class="cm-cron-h"><div><${Heading}>${t('agents.cron.title')}</${Heading}><p>${t('agents.admin.cron_lead')}</p></div>
        <button class="btn primary" onClick=${() => setSelection(NEW_JOB)}><${Icon} n="plus" />${t('agents.cron.new')}</button></div>
      ${jobsError && html`<p role="alert" class="msg-err">${jobsError}</p>`}
      <${FolderTasks} jobs=${jobs} onApplied=${load} />
      ${jobs === null ? !jobsError && html`<div class="skeleton cm-admin-loading"></div>`
        : !jobs.length ? html`<${Empty} icon="history" title=${t('agents.cron.empty')} text=${t('agents.cron.empty_text')} />`
        : html`<ul class="cm-jobs">${jobs.map(j => html`<li key=${j.id} class=${cls('cm-job', j.id === selection && 'selected')}>
            <${Switch} checked=${j.enabled} disabled=${pending[j.id]} label=${j.name + ' · ' + t('agents.cron.enabled')} onChange=${enabled => act(j.id, async () => {
              const result = await api.cronEnable(j.id, enabled);
              if (result?.job) setJobs(prev => prev.map(x => x.id === j.id ? result.job : x));
            })} />
            <button class="cm-job-t" aria-current=${j.id === selection ? 'true' : undefined} onClick=${() => setSelection(j.id)} title=${t('agents.cron.edit')}>
              <${CliMark} mode=${j.agentType} size="sm" />
              <span><b>${j.name}</b>
                <small title=${j.workingDir}>${cliOf(j.agentType).label} · ${projectOf(j.workingDir)}</small>
                <small>${schedule(j)}</small>
                <small>${j.enabled && j.nextRunAt ? t('agents.cron.next', { at: fmt(j.nextRunAt) }) : j.completedOnce ? t('agents.cron.completed') : j.enabled ? t('agents.cc.no_next') : t('agents.cron.disabled')}</small>
                <small class=${j.lastStatus === 'failed' ? 'cm-run-err' : ''}>${j.lastStatus ? t('agents.cron.last', { status: lt('runStatus', j.lastStatus) }) : t('agents.cc.no_runs')}</small>
              </span>
            </button>
            <button class="btn sm cm-job-run" disabled=${pending[j.id]} onClick=${() => runNow(j)}>${t('agents.cron.run_now')}</button>
          </li>`)}</ul>`}
      <section class="cm-cron-recent"><h2>${t('agents.cc.runs')}</h2>
        ${runsError && html`<p role="alert" class="msg-err">${runsError}</p>`}
        ${runs ? html`<${Runs} jobs=${jobs || []} runs=${runs} />` : !runsError && html`<div class="skeleton cm-admin-loading"></div>`}
      </section>
    </section>
    ${editing && (phone ? jobForm : html`<div ref=${editor} class="cm-editor-pane">${jobForm}</div>`)}
  </div></div>`;
}
