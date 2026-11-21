// Codeman stores scheduled jobs and reports session startup and prompt delivery.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Seg } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { api } from './client.js';
import { agents, cliOf, onDomain, projectOf, upsertSession } from './store.js';
import { openSession } from './nav.js';
import { CliMark } from './new-session.js';
import { parseCron } from './schedule.js';

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

function JobForm({ job, onClose, onSaved, onDelete }) {
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
  return html`<aside class="cm-job-editor" aria-label=${title}>
    <form onSubmit=${submit}>
      <div class="cm-section-head"><h2>${job ? job.name : title}</h2>
        ${job && html`<button type="button" class="btn sm ghost danger" disabled=${busy} onClick=${remove}>${t('agents.cron.delete')}</button>`}</div>
      <label class="field"><span>${t('agents.cron.name')}</span><input class="input" maxlength="200" value=${v.name} onInput=${e => set({ name: e.target.value })} required /></label>
      <label class="cm-editor-row"><span>${t('agents.cron.harness')}</span><select class="select" value=${v.agentType} onChange=${e => set({ agentType: e.target.value })} required>
        ${!v.agentType && html`<option value="">${t('agents.cron.pick_harness')}</option>`}
        ${v.agentType && !clis.some(c => c.id === v.agentType) && html`<option value=${v.agentType}>${cliOf(v.agentType).label}</option>`}
        ${clis.map(c => html`<option key=${c.id} value=${c.id}>${c.label}</option>`)}
      </select></label>
      <label class="cm-editor-row"><span>${t('agents.new.folder')}</span><input class="input mono" value=${v.workingDir} onInput=${e => set({ workingDir: e.target.value })} list="cm-cron-dirs" required />
        <datalist id="cm-cron-dirs">${folders.map(dir => html`<option key=${dir} value=${dir} />`)}</datalist></label>
      ${v.agentType === 'shell' && html`<label class="field"><span>${t('agents.cron.command')}</span><input class="input mono" maxlength="2000" value=${v.launchCommand} onInput=${e => set({ launchCommand: e.target.value })} /></label>`}
      <div class="field"><div class="cm-section-head"><label for="cm-cron-prompt">${t('agents.cron.prompt')}</label>
        <${Seg} size="sm" value=${v.promptMode} onChange=${m => set({ promptMode: m })} label=${t('agents.cron.prompt')} options=${[{ value: 'inline_text', label: t('agents.cron.inline') }, { value: 'prompt_file_path', label: t('agents.cron.file') }]} /></div>
        ${v.promptMode === 'inline_text' ? html`<textarea id="cm-cron-prompt" class="textarea" rows="3" maxlength="100000" value=${v.promptText} onInput=${e => set({ promptText: e.target.value })} required></textarea><small>${t('agents.composer.single_line')}</small>`
          : html`<input id="cm-cron-prompt" class="input mono" placeholder="/…/prompt.md" value=${v.promptFilePath} onInput=${e => set({ promptFilePath: e.target.value })} required /><small>${t('agents.cron.file_hint')}</small>`}</div>
      <div class="field">
        <label for="cm-cron-schedule">${t('agents.cron.when')}</label>
        <select id="cm-cron-schedule" class="select" value=${v.scheduleType} onChange=${e => set({ scheduleType: e.target.value })}>
          ${SCHEDULE_TYPES.map(type => html`<option key=${type} value=${type}>${lt('cronType', type)}</option>`)}
          <option value="custom">${t('agents.cron.custom')}</option>
        </select>
        ${v.scheduleType === 'once' && html`<input class="input" aria-label=${t('agents.cron.when')} type="datetime-local" value=${Number.isFinite(v.runAt) ? localInput(v.runAt) : ''} onInput=${e => set({ runAt: new Date(e.target.value).getTime() })} required />`}
        ${v.scheduleType === 'interval' && html`<div class="cm-inline"><input class="input" aria-label=${t('agents.cron.minutes')} type="number" min="1" max="525600" value=${v.intervalMinutes} onInput=${e => set({ intervalMinutes: e.target.value })} required /><span class="muted">${t('agents.cron.minutes')}</span></div>`}
        ${v.scheduleType === 'weekly' && html`<div class="chips" role="group" aria-label=${t('agents.cron.days')}>${DAYS.map(d => html`<button key=${d} type="button" aria-pressed=${v.weeklyDays.includes(d)} class=${cls('chip-btn', v.weeklyDays.includes(d) && 'on')} onClick=${() => set({ weeklyDays: v.weeklyDays.includes(d) ? v.weeklyDays.filter(x => x !== d) : [...v.weeklyDays, d] })}>${dayName(d, 'short')}</button>`)}</div>`}
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
        <label class="cm-editor-row"><span>${t('agents.cron.input')}</span><select class="select" value=${v.inputMode} onChange=${e => set({ inputMode: e.target.value })}>
          <option value="paste">${t('agents.cron.input_paste')}</option><option value="typed">${t('agents.cron.input_typed')}</option></select></label>
        <label class="cm-editor-row"><span>${t('agents.cron.concurrency')}</span><select class="select" value=${v.concurrencyPolicy} onChange=${e => set({ concurrencyPolicy: e.target.value })}>
          <option value="skip_if_same_agent_running">${t('agents.cron.skip')}</option><option value="warn_only">${t('agents.cron.warn')}</option></select></label>
        ${!job && html`<label class="cm-switch"><span>${t('agents.cron.enabled')}</span><${Switch} checked=${v.enabled} onChange=${enabled => set({ enabled })} label=${t('agents.cron.enabled')} /></label>`}
        ${v.scheduleType !== 'once' && html`<label class="cm-switch"><span><b>${t('agents.cron.autoclose')}</b><small>${t('agents.cron.autoclose_hint')}</small></span>
          <${Switch} checked=${v.autoClosePreviousSession} onChange=${autoClosePreviousSession => set({ autoClosePreviousSession })} label=${t('agents.cron.autoclose')} /></label>`}
        <label class="field"><span>${t('agents.cron.notes')}</span><input class="input" maxlength="2000" value=${v.notes} onInput=${e => set({ notes: e.target.value })} /></label>
      </details>
      ${error && html`<p role="alert" class="msg-err">${error}</p>`}
      <div class="cm-editor-foot"><button type="button" class="btn ghost" disabled=${busy} onClick=${onClose}>${t('agents.cancel')}</button>
        <button type="submit" class="btn primary" disabled=${busy || !valid}>${t('agents.save')}</button></div>
    </form>
    ${job && html`<details class="cm-job-history" onToggle=${e => setHistoryOpen(e.currentTarget.open)}><summary>${t('agents.cc.runs')}</summary>
      ${runsError && html`<p role="alert" class="msg-err">${runsError}</p>`}
      ${runs ? html`<${Runs} runs=${runs} />` : !runsError && html`<span class="spinner"></span>`}</details>`}
  </aside>`;
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
    ${run.sessionId && html`<a class="btn sm ghost" href=${'#/agents/s/' + encodeURIComponent(run.sessionId)} onClick=${e => open(e, run.sessionId)}>${t('agents.cc.open')}</a>`}
  </li>`)}</ul>`;
}

const NEW_JOB = Symbol('new job');

export function CronPage() {
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
  return html`<div class="cm-scroll"><div class=${cls('cm-cron', editing && 'editing')}>
    <section class="cm-cron-list">
      <div class="cm-cron-h"><div><h1>${t('agents.cron.title')}</h1><p>${t('agents.admin.cron_lead')}</p></div>
        <button class="btn primary" onClick=${() => setSelection(NEW_JOB)}><${Icon} n="plus" />${t('agents.cron.new')}</button></div>
      ${jobsError && html`<p role="alert" class="msg-err">${jobsError}</p>`}
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
    ${editing && html`<div ref=${editor} class="cm-editor-pane"><${JobForm} key=${job?.id || 'new'} job=${job}
      onClose=${() => setSelection(null)} onDelete=${deleted} onSaved=${saved} /></div>`}
  </div></div>`;
}
