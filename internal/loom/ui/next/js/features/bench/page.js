import { t } from '../../core/i18n.js';
// One model-only test across engines, APIs and supported native accounts.
import { html, useState, useEffect, useRef, useStore, cls, fmtBytes } from '../../core/lib.js';
import { SectionTabs } from '../../app/sections.js';
import { Icon } from '../../ui/icons.js';
import { Empty } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { toast, confirm, Modal } from '../../ui/dialog.js';
import { get, post, request } from '../../core/api.js';
import { validData } from '../../core/shape.js';
import { app, refreshLibrary } from '../../core/state.js';
import { vendorOf } from '../chat/picker.js';
import { useVisibleRefresh } from '../usage/refresh.js';
import { benchChoices, benchMethod } from './choices.js';

const n1 = v => (v == null || isNaN(v)) ? '—' : Number(v).toFixed(1);
const secs = v => (v == null || isNaN(v)) ? '—' : Number(v).toFixed(v < 10 ? 2 : 1) + ' s';

function Pick({ i, on, onChange }) {
  return html`<label class=${cls('choice', on && 'on')}>
    <input type="checkbox" disabled=${!i.supported} checked=${on} onChange=${e => onChange(e.target.checked)} />
    <${Logo} name=${i.logo} size="sm" />
    <span class="grow"><b>${i.name}</b><small>${i.sub}</small></span></label>`;
}

export function BenchPage() {
  const { models, presets } = useStore(app, s => ({ models: s.models, presets: s.presets }));
  const [catalog, setCatalog] = useState([]);
  const [runs, setRuns] = useState([]);
  const [viewed, setViewed] = useState(null);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ name: '', prompt: '', max_tokens: 400 });
  const [sending, setSending] = useState(false);
  const finished = useRef('');
  const alive = useRef(true);
  const viewRequest = useRef(0);
  useEffect(() => () => { alive.current = false; viewRequest.current++; }, []);
  const [tests, setTests] = useState([]);
  const [test, setTest] = useState('perf');
  const [pick, setPick] = useState({});
  const [job, setJob] = useState(null);
  const load = async () => { const r = await get('/api/bench/tests'); setTests(r.tests || []); };
  const poll = async alive => {
    const r = await get('/api/bench/queue?compact=1', { retryAuth: false });
    if (!r.ok || !alive()) return;
    if (r.job && r.job.status !== 'running' && finished.current !== r.job.id + r.job.status + (r.job.finished || 0)) {
      const [full, history] = await Promise.all([get('/api/bench/queue', { retryAuth: false }), get('/api/bench/runs?compact=1', { retryAuth: false })]);
      if (!alive()) return;
      if (full.ok) { setJob(full.job || null); finished.current = full.job ? full.job.id + full.job.status + (full.job.finished || 0) : ''; }
      if (history.ok) setRuns(history.runs || []);
    } else setJob(old => old?.id === r.job?.id && old?.status === r.job?.status && old?.status !== 'running' ? old : r.job || null);
  };
  useEffect(() => { refreshLibrary(); load().catch(e => toast(e.message, 'err')); }, []);
  useVisibleRefresh(poll, 1500);
  useVisibleRefresh(async alive => { const r = await get('/api/bench/catalog', { retryAuth: false }); if (alive() && r.ok) setCatalog(r.models || []); }, 10000);
  const weights = (models || []).filter(m => !m.mmproj && !/mmproj/i.test(m.name));
  const local = [...(presets || []).map(p => ({ key: 'p:' + p.id, name: p.name, sub: t("bench.page.preset"), logo: vendorOf(p.model || p.name), supported: true, v: { preset: p.id, name: p.name, model: p.model || '' } })),
    ...weights.map(m => ({ key: 'm:' + m.path, name: m.name.replace(/\.gguf$/i, ''), sub: fmtBytes(m.size), logo: vendorOf(m.name), supported: true, v: { model: m.path, name: m.name } }))];
  const external = benchChoices(catalog, t);
  const cloud = external.filter(i => i.kind === 'cloud');
  const accounts = external.filter(i => i.kind === 'harness');
  const all = [...local, ...external];
  const chosen = all.filter(i => i.supported && pick[i.key]);
  const busy = job && (job.status === 'running' || job.status === 'cancel' && !job.finished);
  const display = viewed || job;
  const run = async () => {
    if (sending) return;
    setSending(true);
    try {
      const remote = chosen.filter(i => i.external);
      if (remote.length && !await confirm(t('bench.external_confirm'), t('bench.external_consent') + ' ' + [...new Set(remote.map(i => i.provider))].join(', '), { ok: t('bench.page.lancer') })) return;
      const r = await post('/api/bench/queue', { test_id: test, models: chosen.map(i => i.v), consent: remote.length > 0 });
      if (!r.ok) return toast(r.error, 'err'); setJob(r.job); setViewed(null);
    } catch (e) { toast(e.message, 'err'); }
    finally { setSending(false); }
  };
  const addTest = async e => {
    e.preventDefault();
    if (sending) return;
    setSending(true);
    try { const r = await post('/api/bench/tests', draft); if (!r.ok) return toast(r.error, 'err'); setTests(r.tests || []); setTest(r.test.id); setAdding(false); setDraft({ name: '', prompt: '', max_tokens: 400 }); }
    catch (e) { toast(e.message, 'err'); } finally { setSending(false); }
  };
  const viewRun = async id => {
    const requestID = ++viewRequest.current;
    if (!id) { setViewed(null); return; }
    try { const r = await get('/api/bench/runs?id=' + encodeURIComponent(id)); if (r.ok && alive.current && viewRequest.current === requestID) setViewed(r.runs?.[0] || null); }
    catch (e) { toast(e.message, 'err'); }
  };
  const delTest = async localT => { if (!await confirm(t("bench.page.supprimer_le_test"), '« ' + localT.name + t("bench.page.sera_supprime"), { ok: t("bench.page.supprimer"), danger: true })) return; const r = await post('/api/bench/tests/delete', { id: localT.id }); setTests(r.tests || []); if (test === localT.id) setTest('perf'); };
  const stop = async () => {
    try {
      const response = await request('/api/bench/queue/cancel', { method: 'POST', headers: { 'X-Loom-Bench-Job': job.id } });
      const r = await response.json();
      if (!r.ok) return toast(r.error, 'err');
      if (!response.ok || !validData(r, '/api/bench/queue')) throw new Error(t('access.unavailable'));
      setJob(r.job || null);
    } catch (e) { toast(e.message, 'err'); }
  };
  const setMany = (list, v) => setPick({ ...pick, ...Object.fromEntries(list.filter(i => i.supported).map(i => [i.key, v])) });
  const group = (title, list, empty) => html`<div class="bench-g">
    <div class="bench-gh"><span>${title}</span><span class="count">${list.filter(i => i.supported && pick[i.key]).length}/${list.length}</span><span class="grow"></span>
      ${list.some(i => i.supported) && html`<button class="btn sm ghost" onClick=${() => setMany(list, !list.filter(i => i.supported).every(i => pick[i.key]))}>${list.filter(i => i.supported).every(i => pick[i.key]) ? t("bench.page.aucun") : t("bench.page.tout")}</button>`}</div>
    ${list.length ? html`<div class="choice-list">${list.filter(i => i.supported).map(i => html`<${Pick} key=${i.key} i=${i} on=${!!pick[i.key]} onChange=${v => setPick(old => ({ ...old, [i.key]: v }))} />`)}
      ${Object.entries(list.filter(i => !i.supported).reduce((by, i) => ((by[i.provider || i.name] ||= []).push(i), by), {})).map(([provider, items]) => html`<div class="choice off" key=${'off-' + provider}>
        <${Logo} name=${items[0].logo} size="sm" /><span class="grow"><b>${provider}</b><small>${t('bench.unsupported_group', { n: items.length })} · ${items[0].reason_text || items[0].sub.split(' · ').slice(1).join(' · ')}</small></span></div>`)}</div>` : html`<p class="note">${empty}</p>`}</div>`;

  return html`<div class="view page"><div class="page-in">
    <${SectionTabs} /><div class="page-head"><div><h1>${t("bench.page.bench")}</h1><p>${t('bench.description')}</p></div>
      <div class="acts">${busy ? html`<button class="btn" onClick=${stop}>${t("bench.page.arreter_la_file")}</button>`
        : html`<button class="btn primary" disabled=${!chosen.length || chosen.length > 16 || sending} onClick=${run}><${Icon} n="play" />${t("bench.page.lancer")}${chosen.length ? ' (' + chosen.length + ')' : ''}</button>`}</div></div>
    <div class="bench">
      <div class="grid-bench">
        <div class="card pad">
          <div class="sec-h"><h2>${t("bench.page.test")}</h2><button class="btn sm ghost" onClick=${() => setAdding(true)}><${Icon} n="plus" />${t("bench.page.nouveau")}</button></div>
          <div class="choice-list">${tests.map(localT => html`<label class=${cls('choice', test === localT.id && 'on')}>
            <input type="radio" name="bt" checked=${test === localT.id} onChange=${() => setTest(localT.id)} />
            <span class="grow"><b>${localT.name}</b><small>${localT.kind === 'perf' ? t("bench.page.2000_tokens_de_prompt_puis_300_generes") : (localT.prompt || '').slice(0, 70)}</small></span>
            ${!localT.builtin && html`<button class="icon-btn" aria-label="${t("bench.page.supprimer")}" onClick=${e => { e.preventDefault(); delTest(localT); }}><${Icon} n="trash" /></button>`}</label>`)}</div>
        </div>
        <div class="card pad">
          <div class="sec-h"><h2>${t("bench.page.modeles")}</h2></div>
          ${group('Local', local, t("bench.page.aucun_modele_dans_la_bibliotheque"))}
          ${group('Cloud', cloud, t("bench.page.aucun_modele_cloud_visible_connecte_un_fournisseur_dans_cloud"))}
          ${group(t('bench.accounts'), accounts, t('bench.accounts_empty'))}
          <p class="note">${t('bench.account_boundary')}</p><p class="note">${t('bench.selection_limit')}</p>
        </div>
      </div>
      <div class="card">
        <div class="sec-h pad-h"><h2>${t("bench.page.resultats")}</h2><span class="grow"></span>
          <select class="input" aria-label=${t('bench.history')} disabled=${busy} value=${viewed?.id || ''} onChange=${e => viewRun(e.target.value)}><option value="">${t('bench.current')}</option>${runs.map(r => html`<option value=${r.id}>${r.test_name} · ${new Date(r.started * 1000).toLocaleString()}</option>`)}</select></div>
        <p class="note pad-h">${t('bench.metrics_note')}</p>
        ${display && html`<p class="note pad-h">${display.test_name}</p>`}
        ${display && (display.rows || []).length ? html`<div class="table bench-t">
          <div class="tr th"><span>${t("bench.page.modele")}</span><span>${t("bench.page.premier_token")}</span><span>${t("bench.page.prefill")}</span><span>${t('bench.output_rate')}</span><span>${t("bench.page.duree")}</span></div>
          ${display.rows.map((r, i) => {
            const s = r.status || 'pending', res = r.result || {};
            const lab = { pending: t("bench.page.en_attente"), loading: t("bench.page.chargement"), running: t('bench.page.measure'), err: r.error || t('common.error'), skip: r.error || t("bench.page.passe") }[s];
            const external = r.kind === 'cloud' || r.kind === 'account' || !!r.choice_id;
            const method = benchMethod(r, t);
            return html`<div class=${cls('tr', !viewed && busy && i === job.index && 'current')}>
              <span class="cell-id"><${Logo} name=${external ? r.provider : vendorOf(r.name || r.model)} size="sm" /><span class="cell-main"><b>${r.name || r.model}</b><small class=${s === 'err' ? 'err' : ''}>${lab || method}</small></span></span>
              <span class="num" data-label=${t("bench.page.premier_token")}>${s === 'ok' ? secs(res.ttft_sec) : '—'}</span>
              <span class="num" data-label=${t("bench.page.prefill")}>${s === 'ok' && res.prompt_per_second != null ? n1(res.prompt_per_second) + ' t/s' : '—'}</span>
              <span class="num strong" data-label=${t("bench.output_rate")}>${s === 'ok' && res.predicted_per_second != null ? n1(res.predicted_per_second) + ' t/s' : '—'}</span>
              <span class="num" data-label=${t("bench.page.duree")}>${s === 'ok' ? secs(res.elapsed_sec) : ''}</span>
              ${(r.output || r.preview) && html`<details class="bench-prev"><summary>${t("bench.page.sortie")}</summary><p>${r.output || r.preview}</p></details>`}</div>`;
          })}</div>` : html`<${Empty} icon="gauge" title="${t("bench.page.aucune_mesure")}" text="${t("bench.page.choisis_un_test_et_des_modeles_puis_lance_la_file_chaque_modele_e")}" />`}
      </div>
    </div>
    ${adding && html`<${Modal} title=${t('bench.page.nouveau_test')} onClose=${() => setAdding(false)}><form class="bench-form" onSubmit=${addTest}>
      <label class="field"><span>${t('bench.test_name')}</span><input class="input" required maxlength="100" value=${draft.name} onInput=${e => setDraft({ ...draft, name: e.target.value })} /></label>
      <label class="field"><span>${t('bench.test_prompt')}</span><textarea class="textarea" required maxlength="32000" rows="6" value=${draft.prompt} onInput=${e => setDraft({ ...draft, prompt: e.target.value })}></textarea></label>
      <label class="field"><span>${t('bench.output_budget')}</span><input class="input" type="number" min="1" max="4096" required value=${draft.max_tokens} onInput=${e => setDraft({ ...draft, max_tokens: Number(e.target.value) })} /></label>
      <p class="note">${t('bench.same_prompt')}</p><button class="btn primary" disabled=${sending}>${t('bench.page.creer')}</button>
    </form><//>`}
  </div></div>`;
}
