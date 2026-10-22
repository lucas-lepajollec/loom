// Tâches planifiées de Codeman (/api/v1/cron). Un déclenchement lance une
// session ; « prompt envoyé » ne prouve pas que la tâche a réussi.
import { lt } from './labels.js';
import { t, locale } from '../../core/i18n.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Seg } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { api } from './client.js';
import { agents, onDomain } from './store.js';
import { CliMark } from './new-session.js';
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
const localInput = ms => { const d = new Date(ms || Date.now() + 3600e3); return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`; };

function JobForm({ job, onClose, onSaved }) {
  const clis = useStore(agents, s => s.clis.filter(c => c.enabled !== false && c.installed !== false));
  const unified = useStore(agents, s => s.unified);
  const [v, setV] = useState(() => ({
    name: '', agentType: 'claude', workingDir: unified[0]?.workingDir || '', promptMode: 'inline_text', promptText: '', promptFilePath: '',
    inputMode: 'paste', scheduleType: 'daily', runAt: Date.now() + 3600e3, intervalMinutes: 60, dailyTime: '09:00', weeklyDays: [1], weeklyTime: '09:00',
    enabled: true, concurrencyPolicy: 'skip_if_same_agent_running', autoClosePreviousSession: true, notes: '', launchCommand: '',
    ...Object.fromEntries(Object.entries(job || {}).filter(([, x]) => x != null)),
  }));
  const [busy, setBusy] = useState(false);
  const set = patch => setV({ ...v, ...patch });
  const body = () => {
    const b = { name: v.name.trim(), agentType: v.agentType, workingDir: v.workingDir.trim(), promptMode: v.promptMode, inputMode: v.inputMode, scheduleType: v.scheduleType,
      enabled: v.enabled, concurrencyPolicy: v.concurrencyPolicy };
    if (v.promptMode === 'inline_text') b.promptText = v.promptText.replace(/\r?\n+/g, ' ').trim(); else b.promptFilePath = v.promptFilePath.trim();
    if (v.scheduleType === 'once') b.runAt = v.runAt;
    if (v.scheduleType === 'interval') b.intervalMinutes = Number(v.intervalMinutes);
    if (v.scheduleType === 'daily') b.dailyTime = v.dailyTime;
    if (v.scheduleType === 'weekly') { b.weeklyDays = v.weeklyDays; b.weeklyTime = v.weeklyTime; }
    if (v.scheduleType !== 'once') b.autoClosePreviousSession = v.autoClosePreviousSession;
    if (v.notes.trim()) b.notes = v.notes.trim();
    if (v.agentType === 'shell' && v.launchCommand.trim()) b.launchCommand = v.launchCommand.trim();
    return b;
  };
  const submit = async () => {
    setBusy(true);
    try { job ? await api.cronUpdate(job.id, body()) : await api.cronCreate(body()); onSaved(); onClose(); }
    catch (e) { toast(e.message, 'err'); } finally { setBusy(false); }
  };
  const valid = v.name.trim() && v.workingDir.trim() && (v.promptMode === 'inline_text' ? v.promptText.trim() : v.promptFilePath.trim()) && (v.scheduleType !== 'weekly' || v.weeklyDays.length);
  return html`<${Modal} wide title=${job ? t('agents.cron.edit') : t('agents.cron.new')} sub=${t('agents.cron.form_sub')} onClose=${onClose}
      foot=${html`<button class="btn ghost" onClick=${onClose}>${t('agents.cancel')}</button><button class="btn primary" disabled=${busy || !valid} onClick=${submit}>${t('agents.save')}</button>`}>
    <div class="cm-row2">
      <label class="field"><span>${t('agents.cron.name')}</span><input class="input" maxlength="200" value=${v.name} onInput=${e => set({ name: e.target.value })} /></label>
      <label class="field"><span>${t('agents.new.cli')}</span><select class="select" value=${v.agentType} onChange=${e => set({ agentType: e.target.value })}>${clis.map(c => html`<option value=${c.id}>${c.label}</option>`)}</select></label>
    </div>
    <label class="field"><span>${t('agents.new.folder')}</span><input class="input mono" value=${v.workingDir} onInput=${e => set({ workingDir: e.target.value })} list="cm-cron-dirs" />
      <datalist id="cm-cron-dirs">${[...new Set(unified.map(r => r.workingDir).filter(Boolean))].map(d => html`<option value=${d} />`)}</datalist></label>
    ${v.agentType === 'shell' && html`<label class="field"><span>${t('agents.cron.command')}</span><input class="input mono" value=${v.launchCommand} onInput=${e => set({ launchCommand: e.target.value })} /></label>`}
    <div class="field"><span>${t('agents.cron.prompt')}</span>
      <${Seg} size="sm" value=${v.promptMode} onChange=${m => set({ promptMode: m })} label=${t('agents.cron.prompt')} options=${[{ value: 'inline_text', label: t('agents.cron.inline') }, { value: 'prompt_file_path', label: t('agents.cron.file') }]} />
      ${v.promptMode === 'inline_text' ? html`<textarea class="textarea" rows="3" value=${v.promptText} onInput=${e => set({ promptText: e.target.value })}></textarea><small>${t('agents.composer.single_line')}</small>`
        : html`<input class="input mono" placeholder="/…/prompt.md" value=${v.promptFilePath} onInput=${e => set({ promptFilePath: e.target.value })} /><small>${t('agents.cron.file_hint')}</small>`}</div>
    <div class="field"><span>${t('agents.cron.when')}</span>
      <${Seg} size="sm" value=${v.scheduleType} onChange=${m => set({ scheduleType: m })} label=${t('agents.cron.when')}
        options=${['once', 'interval', 'daily', 'weekly'].map(x => ({ value: x, label: lt('cronType', x) }))} />
      ${v.scheduleType === 'once' && html`<input class="input" type="datetime-local" value=${localInput(v.runAt)} onInput=${e => set({ runAt: new Date(e.target.value).getTime() })} />`}
      ${v.scheduleType === 'interval' && html`<div class="cm-inline"><input class="input" type="number" min="1" max="525600" value=${v.intervalMinutes} onInput=${e => set({ intervalMinutes: e.target.value })} /><span class="muted">${t('agents.cron.minutes')}</span></div>`}
      ${v.scheduleType === 'daily' && html`<input class="input" type="time" value=${v.dailyTime} onInput=${e => set({ dailyTime: e.target.value })} />`}
      ${v.scheduleType === 'weekly' && html`<div class="chips">${DAYS.map(d => html`<button type="button" class=${cls('chip-btn', v.weeklyDays.includes(d) && 'on')} onClick=${() => set({ weeklyDays: v.weeklyDays.includes(d) ? v.weeklyDays.filter(x => x !== d) : [...v.weeklyDays, d].sort() })}>${dayName(d)}</button>`)}</div>
        <input class="input" type="time" value=${v.weeklyTime} onInput=${e => set({ weeklyTime: e.target.value })} />`}
      <small>${t('agents.cron.server_time')}</small></div>
    <div class="cm-row2">
      <label class="field"><span>${t('agents.cron.input')}</span><select class="select" value=${v.inputMode} onChange=${e => set({ inputMode: e.target.value })}>
        <option value="paste">${t('agents.cron.input_paste')}</option><option value="typed">${t('agents.cron.input_typed')}</option></select></label>
      <label class="field"><span>${t('agents.cron.concurrency')}</span><select class="select" value=${v.concurrencyPolicy} onChange=${e => set({ concurrencyPolicy: e.target.value })}>
        <option value="skip_if_same_agent_running">${t('agents.cron.skip')}</option><option value="warn_only">${t('agents.cron.warn')}</option></select></label>
    </div>
    ${v.scheduleType !== 'once' && html`<label class="cm-switch"><span><b>${t('agents.cron.autoclose')}</b><small>${t('agents.cron.autoclose_hint')}</small></span>
      <${Switch} checked=${v.autoClosePreviousSession} onChange=${x => set({ autoClosePreviousSession: x })} label=${t('agents.cron.autoclose')} /></label>`}
    <label class="field"><span>${t('agents.cron.notes')}</span><input class="input" maxlength="2000" value=${v.notes} onInput=${e => set({ notes: e.target.value })} /></label>
  </${Modal}>`;
}

function Runs({ job, runs }) {
  const mine = runs.filter(r => r.cronJobId === job.id).sort((a, b) => b.startedAt - a.startedAt).slice(0, 8);
  if (!mine.length) return html`<p class="muted">${t('agents.cc.no_runs')}</p>`;
  return html`<ul class="cm-runs">${mine.map(r => html`<li key=${r.id}><i class=${cls('dot', r.status === 'failed' ? 'red' : r.status === 'skipped' ? '' : 'green')}></i>
    <span>${fmt(r.startedAt)}</span><span class="muted">${lt('runStatus', r.status)}${r.triggerType === 'manual_run_now' ? ' · ' + t('agents.cron.manual') : ''}</span>
    ${r.errorMessage && html`<span class="cm-run-err trunc" title=${r.errorMessage}>${r.errorMessage}</span>`}<span class="grow"></span>
    ${r.sessionId && agents.get().sessions[r.sessionId] && html`<button class="btn sm ghost" onClick=${() => openSession(r.sessionId)}>${t('agents.cc.open')}</button>`}</li>`)}</ul>`;
}

export function CronPage() {
  const [st, setSt] = useState({ jobs: null, runs: [], error: '' });
  const [form, setForm] = useState(null);
  const [open, setOpen] = useState('');
  const load = () => Promise.all([api.cronJobs(), api.cronRuns()]).then(([jobs, runs]) => setSt({ jobs: jobs || [], runs: runs || [], error: '' })).catch(e => setSt(s => ({ ...s, error: e.message, jobs: s.jobs || [] })));
  useEffect(() => { load(); return onDomain('cron', load); }, []);
  const act = async (fn, ok) => { try { await fn(); if (ok) toast(ok); load(); } catch (e) { toast(e.message, 'err'); } };
  return html`<div class="cm-scroll"><div class="cm-cron">
    <div class="cm-cron-h"><div><h1>${t('agents.cron.title')}</h1><p class="muted">${t('agents.cron.lead')}</p></div>
      <button class="btn primary" onClick=${() => setForm({})}><${Icon} n="plus" />${t('agents.cron.new')}</button></div>
    ${st.error && html`<div class="msg-err"><${Icon} n="alert" /><span>${st.error}</span></div>`}
    ${st.jobs === null ? html`<div class="skeleton" style="height:120px"></div>`
      : !st.jobs.length ? html`<${Empty} icon="history" title=${t('agents.cron.empty')} text=${t('agents.cron.empty_text')} />`
      : html`<ul class="cm-jobs">${st.jobs.map(j => html`<li key=${j.id} class=${cls('cm-job', !j.enabled && 'off')}>
          <div class="cm-job-main">
            <${CliMark} mode=${j.agentType} size="sm" />
            <button class="cm-job-t" onClick=${() => setOpen(open === j.id ? '' : j.id)} aria-expanded=${String(open === j.id)}>
              <b class="trunc">${j.name}</b><small class="muted trunc">${schedule(j)} · ${j.workingDir}</small></button>
            <span class="cm-job-next muted hide-narrow">${j.enabled && j.nextRunAt ? t('agents.cron.next', { at: fmt(j.nextRunAt) }) : j.lastStatus ? lt('runStatus', j.lastStatus) : ''}</span>
            <${Switch} checked=${j.enabled} label=${t('agents.cron.enabled')} onChange=${x => act(() => api.cronEnable(j.id, x))} />
            <button class="icon-btn" title=${t('agents.cron.run_now')} aria-label=${t('agents.cron.run_now')} onClick=${() => act(() => api.cronRun(j.id), t('agents.cron.launched'))}><${Icon} n="play" /></button>
            <button class="icon-btn" title=${t('agents.cron.edit')} aria-label=${t('agents.cron.edit')} onClick=${() => setForm(j)}><${Icon} n="edit" /></button>
            <button class="icon-btn" title=${t('agents.cron.delete')} aria-label=${t('agents.cron.delete')} onClick=${async () => { if (await confirm(t('agents.cron.delete'), j.name, { ok: t('agents.cron.delete'), danger: true })) act(() => api.cronDelete(j.id)); }}><${Icon} n="trash" /></button>
          </div>
          ${open === j.id && html`<div class="cm-job-more">${j.notes && html`<p>${j.notes}</p>`}<${Runs} job=${j} runs=${st.runs} /></div>`}
        </li>`)}</ul>`}
    ${form && html`<${JobForm} job=${form.id ? form : null} onClose=${() => setForm(null)} onSaved=${load} />`}
  </div></div>`;
}
