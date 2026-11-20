// Tâches planifiées de Codeman (/api/v1/cron). Un déclenchement lance une
// session ; « prompt envoyé » ne prouve pas que la tâche a réussi.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Seg } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { api } from './client.js';
import { agents, cliOf, onDomain } from './store.js';
import { openSession } from './nav.js';

const fmt = ms => ms ? new Date(ms).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '—';
const DAYS = [1, 2, 3, 4, 5, 6, 0];
const dayName = d => new Date(2024, 0, 7 + d).toLocaleDateString(locale(), { weekday: 'short' });

export function schedule(j) {
  if (j.scheduleType === 'once') return t('agents.cron.once_at', { at: fmt(j.runAt) });
  if (j.scheduleType === 'interval') return t('agents.cron.every', { n: j.intervalMinutes });
  if (j.scheduleType === 'daily') return t('agents.cron.daily_at', { at: j.dailyTime });
  return t('agents.cron.weekly_at', { days: (j.weeklyDays || []).map(dayName).join(', '), at: j.weeklyTime });
}

const pad = n => String(n).padStart(2, '0');
const localInput = ms => { const d = new Date(ms); return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`; };

function JobForm({ job, runs, onClose, onSaved, onDelete }) {
  const clis = useStore(agents, s => s.clis.filter(c => c.enabled !== false && c.installed !== false));
  const unified = useStore(agents, s => s.unified);
  const defaultCli = clis.find(c => c.id === 'claude') || clis[0];
  const [v, setV] = useState(() => ({
    name: '', agentType: defaultCli?.id || '', workingDir: unified[0]?.workingDir || '', promptMode: 'inline_text', promptText: '', promptFilePath: '',
    inputMode: 'paste', scheduleType: 'daily', runAt: Date.now() + 3600e3, intervalMinutes: 60, dailyTime: '09:00', weeklyDays: [1], weeklyTime: '09:00',
    enabled: true, concurrencyPolicy: 'skip_if_same_agent_running', autoClosePreviousSession: true, notes: '', launchCommand: '',
    ...Object.fromEntries(Object.entries(job || {}).filter(([, x]) => x != null)),
  }));
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!job && defaultCli && !v.agentType) setV(draft => ({ ...draft, agentType: defaultCli.id }));
  }, [defaultCli?.id, v.agentType, job]);
  const set = patch => setV({ ...v, ...patch });
  const body = () => {
    const b = { name: v.name.trim(), agentType: v.agentType, workingDir: v.workingDir.trim(), promptMode: v.promptMode, inputMode: v.inputMode, scheduleType: v.scheduleType,
      concurrencyPolicy: v.concurrencyPolicy };
    if (!job) b.enabled = v.enabled;
    if (v.promptMode === 'inline_text') b.promptText = v.promptText.replace(/\r?\n+/g, ' ').trim(); else b.promptFilePath = v.promptFilePath.trim();
    if (v.scheduleType === 'once') b.runAt = v.runAt;
    if (v.scheduleType === 'interval') b.intervalMinutes = Number(v.intervalMinutes);
    if (v.scheduleType === 'daily') b.dailyTime = v.dailyTime;
    if (v.scheduleType === 'weekly') { b.weeklyDays = v.weeklyDays; b.weeklyTime = v.weeklyTime; }
    if (v.scheduleType !== 'once') b.autoClosePreviousSession = v.autoClosePreviousSession;
    if (v.notes.trim() || job?.notes) b.notes = v.notes.trim();
    if (v.agentType === 'shell' && (v.launchCommand.trim() || job?.launchCommand)) b.launchCommand = v.launchCommand.trim();
    return b;
  };
  const submit = async () => {
    setBusy(true);
    try { job ? await api.cronUpdate(job.id, body()) : await api.cronCreate(body()); onSaved(); }
    catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  const scheduleValid = {
    once: Number.isFinite(v.runAt) && v.runAt > 0,
    interval: Number.isInteger(Number(v.intervalMinutes)) && v.intervalMinutes >= 1 && v.intervalMinutes <= 525600,
    daily: !!v.dailyTime,
    weekly: !!v.weeklyDays.length && !!v.weeklyTime,
  };
  const valid = scheduleValid[v.scheduleType] && v.name.trim() && v.workingDir.trim() && (v.promptMode === 'inline_text' ? v.promptText.trim() : v.promptFilePath.trim());
  return html`<section class="cm-job-editor" aria-label=${job ? t('agents.cron.edit') : t('agents.cron.new')}>
    <div class="cm-section-head"><h2>${job ? job.name : t('agents.cron.new')}</h2>
      ${job && html`<button class="btn sm ghost danger" disabled=${busy} onClick=${onDelete}>${t('agents.cron.delete')}</button>`}</div>
      <label class="field"><span>${t('agents.cron.name')}</span><input class="input" maxlength="200" value=${v.name} onInput=${e => set({ name: e.target.value })} /></label>
      <label class="cm-editor-row"><span>${t('memory.scope.agent')}</span><select class="select" value=${v.agentType} onChange=${e => set({ agentType: e.target.value })}>${v.agentType && !clis.some(c => c.id === v.agentType) && html`<option value=${v.agentType}>${cliOf(v.agentType).label}</option>`}${clis.map(c => html`<option value=${c.id}>${c.label}</option>`)}</select></label>
    <label class="cm-editor-row"><span>${t('agents.new.folder')}</span><input class="input mono" value=${v.workingDir} onInput=${e => set({ workingDir: e.target.value })} list="cm-cron-dirs" />
      <datalist id="cm-cron-dirs">${[...new Set(unified.map(r => r.workingDir).filter(Boolean))].map(d => html`<option value=${d} />`)}</datalist></label>
    ${v.agentType === 'shell' && html`<label class="field"><span>${t('agents.cron.command')}</span><input class="input mono" value=${v.launchCommand} onInput=${e => set({ launchCommand: e.target.value })} /></label>`}
    <div class="field"><div class="cm-section-head"><label for="cm-cron-prompt">${t('agents.cron.prompt')}</label>
      <${Seg} size="sm" value=${v.promptMode} onChange=${m => set({ promptMode: m })} label=${t('agents.cron.prompt')} options=${[{ value: 'inline_text', label: t('agents.cron.inline') }, { value: 'prompt_file_path', label: t('agents.cron.file') }]} /></div>
      ${v.promptMode === 'inline_text' ? html`<textarea id="cm-cron-prompt" class="textarea" rows="3" value=${v.promptText} onInput=${e => set({ promptText: e.target.value })}></textarea><small>${t('agents.composer.single_line')}</small>`
        : html`<input id="cm-cron-prompt" class="input mono" placeholder="/…/prompt.md" value=${v.promptFilePath} onInput=${e => set({ promptFilePath: e.target.value })} /><small>${t('agents.cron.file_hint')}</small>`}</div>
    <div class="field"><span>${t('agents.cron.when')}</span>
      <${Seg} size="sm" value=${v.scheduleType} onChange=${m => set({ scheduleType: m })} label=${t('agents.cron.when')}
        options=${['once', 'interval', 'daily', 'weekly'].map(x => ({ value: x, label: x === 'weekly' ? t('agents.admin.weekly') : lt('cronType', x) }))} />
      ${v.scheduleType === 'once' && html`<input class="input" aria-label=${t('agents.cron.when')} type="datetime-local" value=${Number.isFinite(v.runAt) ? localInput(v.runAt) : ''} onInput=${e => set({ runAt: new Date(e.target.value).getTime() })} />`}
      ${v.scheduleType === 'interval' && html`<div class="cm-inline"><input class="input" aria-label=${t('agents.cron.minutes')} type="number" min="1" max="525600" value=${v.intervalMinutes} onInput=${e => set({ intervalMinutes: e.target.value })} /><span class="muted">${t('agents.cron.minutes')}</span></div>`}
      ${v.scheduleType === 'daily' && html`<input class="input" aria-label=${t('agents.admin.time')} type="time" value=${v.dailyTime} onInput=${e => set({ dailyTime: e.target.value })} />`}
      ${v.scheduleType === 'weekly' && html`<div class="chips">${DAYS.map(d => html`<button type="button" aria-pressed=${v.weeklyDays.includes(d)} class=${cls('chip-btn', v.weeklyDays.includes(d) && 'on')} onClick=${() => set({ weeklyDays: v.weeklyDays.includes(d) ? v.weeklyDays.filter(x => x !== d) : [...v.weeklyDays, d].sort() })}>${dayName(d)}</button>`)}</div>
        <input class="input" aria-label=${t('agents.admin.time')} type="time" value=${v.weeklyTime} onInput=${e => set({ weeklyTime: e.target.value })} />`}
      <small>${t('agents.cron.server_time')}</small></div>
      <label class="cm-editor-row"><span>${t('agents.cron.input')}</span><select class="select" value=${v.inputMode} onChange=${e => set({ inputMode: e.target.value })}>
        <option value="paste">${t('agents.cron.input_paste')}</option><option value="typed">${t('agents.cron.input_typed')}</option></select></label>
      <label class="cm-editor-row"><span>${t('agents.cron.concurrency')}</span><select class="select" value=${v.concurrencyPolicy} onChange=${e => set({ concurrencyPolicy: e.target.value })}>
        <option value="skip_if_same_agent_running">${t('agents.cron.skip')}</option><option value="warn_only">${t('agents.cron.warn')}</option></select></label>
    ${v.scheduleType !== 'once' && html`<label class="cm-switch"><span><b>${t('agents.cron.autoclose')}</b><small>${t('agents.cron.autoclose_hint')}</small></span>
      <${Switch} checked=${v.autoClosePreviousSession} onChange=${x => set({ autoClosePreviousSession: x })} label=${t('agents.cron.autoclose')} /></label>`}
    <label class="field"><span>${t('agents.cron.notes')}</span><input class="input" maxlength="2000" value=${v.notes} onInput=${e => set({ notes: e.target.value })} /></label>
    <div class="cm-editor-foot"><button class="btn ghost" disabled=${busy} onClick=${onClose}>${t('agents.cancel')}</button>
      <button class="btn primary" disabled=${busy || !valid || !v.agentType} onClick=${submit}>${t('agents.save')}</button></div>
    ${job && html`<details class="cm-job-history"><summary>${t('agents.cc.runs')}</summary><${Runs} jobs=${[job]} runs=${runs.filter(r => r.cronJobId === job.id)} /></details>`}
  </section>`;
}

function Runs({ jobs, runs, overview }) {
  const sessions = useStore(agents, s => s.sessions);
  const recent = [...runs].sort((a, b) => b.startedAt - a.startedAt).slice(0, 8);
  if (!recent.length) return html`<p class="cm-admin-empty">${t('agents.cc.no_runs')}</p>`;
  return html`<ul class="cm-runs">${recent.map(r => html`<li key=${r.id}>
    ${overview && html`<b>${jobs.find(j => j.id === r.cronJobId)?.name || r.cronJobId}</b>`}
    <span class=${cls('cm-run-info', r.status === 'failed' && 'cm-run-err')}>${fmt(r.startedAt)} · ${lt('runStatus', r.status)}${r.triggerType === 'manual_run_now' ? ' · ' + t('agents.cron.manual') : ''}</span>
    ${r.errorMessage && html`<span class="cm-run-err">${r.errorMessage}</span>`}
    ${r.sessionId && sessions[r.sessionId] && html`<button class="btn sm ghost" onClick=${() => openSession(r.sessionId)}>${t('agents.cc.open')}</button>`}</li>`)}</ul>`;
}

const NEW_JOB = Symbol('new job');

export function CronPage() {
  const [st, setSt] = useState({ jobs: null, runs: [], error: '' });
  const [selection, setSelection] = useState(undefined);
  const [pending, setPending] = useState({});
  const editor = useRef(null);
  const revision = useRef(0);
  const load = async () => {
    const request = ++revision.current;
    try {
      const [jobs, runs] = await Promise.all([api.cronJobs(), api.cronRuns()]);
      if (revision.current !== request) return;
      const list = jobs || [];
      setSt({ jobs: list, runs: runs || [], error: '' });
      setSelection(current => current === NEW_JOB || current === null || list.some(j => j.id === current) ? current : list[0]?.id);
    } catch (e) {
      if (revision.current === request) setSt(s => ({ ...s, error: e.message }));
    }
  };
  useEffect(() => {
    load();
    const off = onDomain('cron', load);
    return () => { revision.current++; off(); };
  }, []);
  const act = async (id, fn, ok) => {
    setPending(p => ({ ...p, [id]: true }));
    try { await fn(); if (ok) toast(ok); await load(); }
    catch (e) { toast(e.message, 'err'); } finally { setPending(p => ({ ...p, [id]: false })); }
  };
  const jobs = st.jobs || [];
  const id = selection;
  const job = jobs.find(j => j.id === id);
  const select = value => {
    setSelection(value);
    if (matchMedia('(max-width: 760px)').matches) requestAnimationFrame(() => editor.current?.scrollIntoView({ block: 'start' }));
  };
  const remove = async () => {
    if (await confirm(t('agents.cron.delete'), job.name, { ok: t('agents.cron.delete'), danger: true })) {
      await act(job.id, async () => { await api.cronDelete(job.id); setSelection(undefined); });
    }
  };
  return html`<div class="cm-scroll"><div class="cm-cron">
    <section class="cm-cron-list">
      <div class="cm-cron-h"><div><h1>${t('agents.cron.title')}</h1><p>${t('agents.admin.cron_lead')}</p></div>
        <button class="btn primary" onClick=${() => select(NEW_JOB)}>${t('agents.cron.new')}</button></div>
      ${st.error && html`<div class="msg-err"><${Icon} n="alert" /><span>${st.error}</span></div>`}
      ${st.jobs === null ? !st.error && html`<div class="skeleton cm-admin-loading"></div>`
        : !jobs.length ? html`<${Empty} icon="history" title=${t('agents.cron.empty')} text=${t('agents.cron.empty_text')} />`
        : html`<ul class="cm-jobs">${jobs.map(j => html`<li key=${j.id} class=${cls('cm-job', j.id === id && 'selected')}>
            <${Switch} checked=${j.enabled} disabled=${pending[j.id]} label=${j.name + ' · ' + t('agents.cron.enabled')} onChange=${x => act(j.id, () => api.cronEnable(j.id, x))} />
            <button class="cm-job-t" aria-current=${j.id === id ? 'true' : undefined} onClick=${() => select(j.id)}>
              <b>${j.name}</b><small>${cliOf(j.agentType).label} · ${schedule(j)} · ${!j.enabled ? t('agents.cfg.cli.off') : j.nextRunAt ? t('agents.cron.next', { at: fmt(j.nextRunAt) }) : j.lastStatus ? lt('runStatus', j.lastStatus) : j.workingDir}</small></button>
            <button class="btn sm" disabled=${pending[j.id]} onClick=${() => act(j.id, () => api.cronRun(j.id), t('agents.cron.launched'))}>${t('agents.cron.run_now')}</button>
          </li>`)}</ul>`}
      ${st.jobs && html`<section class="cm-cron-recent"><h2>${t('agents.cc.runs')}</h2><${Runs} jobs=${jobs} runs=${st.runs} overview /></section>`}
    </section>
    <div ref=${editor} class="cm-editor-pane">
      ${(job || id === NEW_JOB) && html`<${JobForm} key=${job ? job.id : 'create'} job=${job} runs=${st.runs}
        onClose=${() => setSelection(null)} onDelete=${remove} onSaved=${() => { load(); setSelection(current => current === id ? null : current); }} />`}
    </div>
  </div></div>`;
}
