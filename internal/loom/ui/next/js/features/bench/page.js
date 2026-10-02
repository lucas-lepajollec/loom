import { t } from '../../core/i18n.js';
// Bench : une file de modèles locaux et cloud mesurés un par un avec le même
// test (intégré ou prompt personnalisé). Un vrai système de bench viendra plus tard.
import { html, useState, useEffect, useStore, cls, fmtBytes } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { toast, prompt, confirm } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshLibrary, refreshWorkspace } from '../../core/state.js';
import { vendorOf } from '../chat/picker.js';

const n1 = v => (v == null || isNaN(v)) ? '—' : Number(v).toFixed(1);
const secs = v => (v == null || isNaN(v)) ? '—' : Number(v).toFixed(v < 10 ? 2 : 1) + ' s';

function Pick({ i, on, onChange }) {
  return html`<label class=${cls('choice', on && 'on')}>
    <input type="checkbox" checked=${on} onChange=${e => onChange(e.target.checked)} />
    <${Logo} name=${i.logo} size="sm" />
    <span class="grow"><b>${i.name}</b><small>${i.sub}</small></span></label>`;
}

export function BenchPage() {
  const { models, presets, workspace } = useStore(app, s => ({ models: s.models, presets: s.presets, workspace: s.workspace }));
  const [tests, setTests] = useState([]);
  const [test, setTest] = useState('perf');
  const [pick, setPick] = useState({});
  const [job, setJob] = useState(null);
  const load = async () => { const r = await get('/api/bench/tests'); setTests(r.tests || []); };
  const poll = async () => { try { const r = await get('/api/bench/queue'); setJob(r.job || null); } catch (_) {} };
  useEffect(() => { refreshLibrary(); refreshWorkspace(); load(); poll(); const localT = setInterval(poll, 1500); return () => clearInterval(localT); }, []);
  const weights = (models || []).filter(m => !m.mmproj && !/mmproj/i.test(m.name));
  const local = [...(presets || []).map(p => ({ key: 'p:' + p.id, name: p.name, sub: t("bench.page.preset"), logo: vendorOf(p.model || p.name), v: { preset: p.id, name: p.name, model: p.model || '' } })),
    ...weights.map(m => ({ key: 'm:' + m.path, name: m.name.replace(/\.gguf$/i, ''), sub: fmtBytes(m.size), logo: vendorOf(m.name), v: { model: m.path, name: m.name } }))];
  const cloud = ((workspace && workspace.models) || []).filter(m => m.kind === 'cloud' && m.enabled)
    .map(m => ({ key: 'c:' + m.id, name: m.name, sub: m.provider_name, logo: m.provider_name, cloud: true, v: { choice_id: m.id, name: m.name } }));
  const all = [...local, ...cloud];
  const chosen = all.filter(i => pick[i.key]);
  const busy = job && job.status === 'running';
  const run = async () => {
    const remote = chosen.filter(i => i.cloud);
    if (remote.length && !await confirm(t("bench.page.envoyer_le_test_au_cloud"), t("bench.page.le_prompt_du_test_sera_envoye_a") + [...new Set(remote.map(i => i.sub))].join(', ') + t("bench.page.ces_requetes_peuvent_etre_facturees_par_le_fournisseur"), { ok: t("bench.page.lancer") })) return;
    const r = await post('/api/bench/queue', { test_id: test, models: chosen.map(i => i.v), consent: remote.length > 0 });
    if (!r.ok) return toast(r.error, 'err'); setJob(r.job);
  };
  const addTest = async () => {
    const text = await prompt(t("bench.page.nouveau_test"), { message: t("bench.page.le_prompt_envoye_a_chaque_modele_les_sorties_sont_comparees_cote"), placeholder: t("bench.page.ex_explique_le_theoreme_de_pythagore"), ok: t("bench.page.creer") });
    if (!text) return;
    const r = await post('/api/bench/tests', { name: text.slice(0, 40), prompt: text, max_tokens: 400 }); if (!r.ok) return toast(r.error, 'err'); setTests(r.tests || []);
  };
  const delTest = async localT => { if (!await confirm(t("bench.page.supprimer_le_test"), '« ' + localT.name + t("bench.page.sera_supprime"), { ok: t("bench.page.supprimer"), danger: true })) return; const r = await post('/api/bench/tests/delete', { id: localT.id }); setTests(r.tests || []); if (test === localT.id) setTest('perf'); };
  const setMany = (list, v) => setPick({ ...pick, ...Object.fromEntries(list.map(i => [i.key, v])) });
  const group = (title, list, empty) => html`<div class="bench-g">
    <div class="bench-gh"><span>${title}</span><span class="count">${list.filter(i => pick[i.key]).length}/${list.length}</span><span class="grow"></span>
      ${list.length > 0 && html`<button class="btn sm ghost" onClick=${() => setMany(list, !list.every(i => pick[i.key]))}>${list.every(i => pick[i.key]) ? t("bench.page.aucun") : t("bench.page.tout")}</button>`}</div>
    ${list.length ? html`<div class="choice-list">${list.map(i => html`<${Pick} key=${i.key} i=${i} on=${!!pick[i.key]} onChange=${v => setPick({ ...pick, [i.key]: v })} />`)}</div>` : html`<p class="note">${empty}</p>`}</div>`;

  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>${t("bench.page.bench")}</h1><p>${t("bench.page.compare_la_vitesse_de_tes_modeles_locaux_et_cloud_sur_le_meme_tes")}</p></div>
      <div class="acts">${busy ? html`<button class="btn" onClick=${async () => { const r = await post('/api/bench/queue/cancel', {}); setJob(r.job || null); }}>${t("bench.page.arreter_la_file")}</button>`
        : html`<button class="btn primary" disabled=${!chosen.length} onClick=${run}><${Icon} n="play" />${t("bench.page.lancer")}${chosen.length ? ' (' + chosen.length + ')' : ''}</button>`}</div></div>
    <div class="bench">
      <div class="grid-bench">
        <div class="card pad">
          <div class="sec-h"><h2>${t("bench.page.test")}</h2><button class="btn sm ghost" onClick=${addTest}><${Icon} n="plus" />${t("bench.page.nouveau")}</button></div>
          <div class="choice-list">${tests.map(localT => html`<label class=${cls('choice', test === localT.id && 'on')}>
            <input type="radio" name="bt" checked=${test === localT.id} onChange=${() => setTest(localT.id)} />
            <span class="grow"><b>${localT.name}</b><small>${localT.kind === 'perf' ? t("bench.page.2000_tokens_de_prompt_puis_300_generes") : (localT.prompt || '').slice(0, 70)}</small></span>
            ${!localT.builtin && html`<button class="icon-btn" aria-label="${t("bench.page.supprimer")}" onClick=${e => { e.preventDefault(); delTest(localT); }}><${Icon} n="trash" /></button>`}</label>`)}</div>
        </div>
        <div class="card pad">
          <div class="sec-h"><h2>${t("bench.page.modeles")}</h2></div>
          ${group('Local', local, t("bench.page.aucun_modele_dans_la_bibliotheque"))}
          ${group('Cloud', cloud, t("bench.page.aucun_modele_cloud_visible_connecte_un_fournisseur_dans_cloud"))}
        </div>
      </div>
      <div class="card">
        <div class="sec-h pad-h"><h2>${t("bench.page.resultats")}</h2></div>
        ${job && (job.rows || []).length ? html`<div class="table bench-t">
          <div class="tr th"><span>${t("bench.page.modele")}</span><span>${t("bench.page.premier_token")}</span><span>${t("bench.page.prefill")}</span><span>${t("bench.page.decode")}</span><span>${t("bench.page.duree")}</span></div>
          ${job.rows.map((r, i) => {
            const s = r.status || 'pending', res = r.result || {};
            const lab = { pending: t("bench.page.en_attente"), loading: t("bench.page.chargement"), running: t('bench.page.measure'), err: r.error || t('common.error'), skip: t("bench.page.passe") }[s];
            const isCloud = r.kind === 'cloud' || !!r.choice_id;
            return html`<div class=${cls('tr', busy && i === job.index && 'current')}>
              <span class="cell-id"><${Logo} name=${isCloud ? r.provider : vendorOf(r.name || r.model)} size="sm" /><span class="cell-main"><b>${r.name || r.model}</b><small class=${s === 'err' ? 'err' : ''}>${lab || (isCloud ? r.provider || 'cloud' : 'local')}</small></span></span>
              <span class="num">${s === 'ok' ? secs(res.ttft_sec) : '—'}</span>
              <span class="num">${s === 'ok' && res.prompt_per_second != null ? n1(res.prompt_per_second) + ' t/s' : '—'}</span>
              <span class="num strong">${s === 'ok' && res.predicted_per_second != null ? n1(res.predicted_per_second) + ' t/s' : '—'}</span>
              <span class="num">${s === 'ok' ? secs(res.elapsed_sec) : ''}</span>
              ${r.preview && html`<details class="bench-prev"><summary>${t("bench.page.sortie")}</summary><p>${r.preview}</p></details>`}</div>`;
          })}</div>` : html`<${Empty} icon="gauge" title="${t("bench.page.aucune_mesure")}" text="${t("bench.page.choisis_un_test_et_des_modeles_puis_lance_la_file_chaque_modele_e")}" />`}
      </div>
    </div>
  </div></div>`;
}
