import { modeOptions, configOptions, permissionOptions } from '../harnesses/options.js';
import { t, locale, tSource } from '../../core/i18n.js';
// Panneau de droite : s'adapte à l'exécution choisie. Local = paramètres
// llama.cpp ; Cloud = fournisseur, usage, coût ; Harness = session native.
// Un second onglet montre le contexte partagé de la discussion.
import { html, useState, useEffect, useRef, useStore, cls, fmtTok, fmtSecs, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Switch, Tip } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { Logo } from '../../ui/logo.js';
import { WorkspacePicker } from '../workspaces/folders.js';
import { Modal, toast, confirm, prompt } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app, refreshWorkspace } from '../../core/state.js';
import { chat, open } from '../chat/engine.js';
import { currentExec, EXEC_TAG } from '../chat/picker.js';
import { agentSessions, runChoice } from '../chat/slash.js';
import { openTerminalWith } from '../terminals/page.js';
import { LocalParams } from './params.js';
import { ContextPanel } from './context.js';

const KV = ({ k, children }) => html`<div class="kv"><span>${k}</span><span>${children}</span></div>`;

function lastTurn(s) { return s && (s.turns || []).at(-1); }

function CloudPanel() {
  const s = useStore(chat, c => c.session);
  const ws = useStore(app, a => a.workspace);
  if (!s) return null;
  const p = ((ws && ws.providers) || []).find(x => x.id === s.provider_id);
  const localT = lastTurn(s), u = localT && localT.usage;
  return html`<div class="insp-body">
    <div class="insp-model"><b>${baseName(s.model)}</b><span>${s.provider_name}</span></div>
    <p class="note insp-hint"><${Icon} n="globe" />${t('search.cloud_where')}<${Tip} text=${t('search.cloud_tip')} /></p>
    <div class="card pad-sm">
      <${KV} k=${t('cloud.page.fournisseur')}>${s.provider_name}</${KV}>
      <${KV} k=${t('settings.page.cle')}>${p && p.ready ? html`<span class="tag green">${t("inspector.inspector.en_memoire")}</span>` : html`<span class="tag amber">${t("inspector.inspector.a_reconnecter")}</span>`}</${KV}>
      <${KV} k=${t('inspector.inspector.destination')}><span class="mono trunc">${s.endpoint}</span></${KV}>
    </div>
    <div class="card pad-sm">
      <div class="lbl" style="margin-bottom:4px">${t("inspector.inspector.dernier_tour")}</div>
      ${u ? html`<${KV} k=${t('usage.page.entree')}>${fmtTok(u.prompt_tokens)} ${t("inspector.inspector.tok")}</${KV}><${KV} k=${t('bench.page.sortie')}>${fmtTok(u.completion_tokens)} ${t("inspector.inspector.tok")}</${KV}>`
        : html`<p class="note">${t("inspector.inspector.le_fournisseur_n_a_pas_communique_de_decompte")}</p>`}
      ${localT && localT.duration_seconds > 0 && html`<${KV} k=${t('bench.page.duree')}>${fmtSecs(localT.duration_seconds)}</${KV}>`}
    </div>
    <p class="note">${t("inspector.inspector.texte_seulement_les_fichiers_outils_et_la_memoire_privee_ne_sont")}</p>
    <a class="btn" href="#/cloud">${t("inspector.inspector.gerer_les_fournisseurs")}</a>
  </div>`;
}

// Session d'un harness : dossier de travail, niveau d'autorisation, mode de
// l'agent, réglages qu'il expose (modèle, réflexion), contexte et fichiers modifiés.
const LEVELS = () => ([{ value: 'ask', label: t("inspector.inspector.demander") }, { value: 'edits', label: t("inspector.inspector.modifs_auto") }, { value: 'full', label: t("inspector.inspector.tout") }]);
const LEVEL_TIP = () => (t("inspector.inspector.demander_chaque_action_attend_ton_accord_modifs_auto_lectures_et"));

async function configure(s, patch) {
  const r = await post('/api/runtime/sessions/configure', { id: s.id, ...patch });
  if (!r.ok) toast(r.error || t("inspector.inspector.reglage_impossible"), 'err');
  return r.ok;
}

// Changer de modèle passe par le sélecteur : nouvelle session native, même fil.
async function switchModel(s, value) {
  const r = await post('/api/runtime/sessions/select', { id: s.id, choice_id: s.runtime_id + ':' + value, consent: true });
  if (!r.ok) return toast(r.error || t("inspector.inspector.modele_indisponible"), 'err');
  await refreshWorkspace(); open(s.id, true);
}

// Some agents fold the reasoning level into the model name ("Gemini 3.8
// Flash (Medium)", "…-high"). Show one model choice and one level choice.
const EFFORT = /^(.*?)(?:-| \()(minimal|low|medium|high|xhigh|max)\)?$/i;
export function splitEffort(opts) {
  const groups = new Map();
  for (const x of opts) {
    const byValue = String(x.value).match(EFFORT), byName = String(x.name || '').match(EFFORT);
    const family = byValue ? byValue[1] : String(x.value), level = byValue ? byValue[2].toLowerCase() : '';
    if (!groups.has(family)) groups.set(family, { family, name: byName ? byName[1] : (byValue ? String(x.name || x.value).replace(/-(minimal|low|medium|high|xhigh|max)$/i, '') : x.name || x.value), levels: [] });
    groups.get(family).levels.push({ level, value: x.value, label: byName ? byName[2] : level });
  }
  return [...groups.values()];
}

// Un nom de modèle lisible : « loom-deepseek/deepseek-flash (via Loom) » devient
// « deepseek-flash » rangé sous « deepseek · via Loom ».
export function modelLabel(name) {
  const raw = String(name || '').replace(/ \(via Loom\)$/, '');
  const m = raw.match(/^([\w.-]+)\/(.+)$/);
  if (!m) return { group: '', label: raw };
  const group = m[1] === 'loom' ? t('inspector.models.local') : m[1].startsWith('loom-') ? m[1].slice(5) + ' · ' + t('inspector.models.via_loom') : m[1];
  return { group, label: m[2].replace(/\.gguf$/i, '') };
}

function ModelOption({ o, onChange }) {
  const opts = (o.options || []).flatMap(x => x.options ? x.options : [x]);
  const groups = splitEffort(opts);
  const current = groups.find(g => g.levels.some(l => l.value === o.currentValue)) || groups[0];
  const level = current.levels.find(l => l.value === o.currentValue) || current.levels[0];
  const pick = family => { const g = groups.find(x => x.family === family); const same = g.levels.find(l => l.level === level.level) || g.levels.find(l => l.level === 'medium') || g.levels[0]; onChange(same.value); };
  return html`<${ListPick} label=${o.name} value=${current.family} onChange=${pick} options=${groups.map(g => { const l = modelLabel(g.name); return { value: g.family, label: l.label, group: l.group }; })} />
    ${current.levels.length > 1 && html`<div class="hs-sub">${t('inspector.reasoning_level')}</div>
      <${Seg} value=${level.value} onChange=${onChange} label=${t('inspector.reasoning_level')} options=${current.levels.map(l => ({ value: l.value, label: l.label || l.level }))} />`}`;
}

function ConfigOption({ o, onChange }) {
  const opts = (o.options || []).flatMap(x => x.options ? x.options : [x]);
  if (o.type === 'boolean') return html`<div class="prow"><span class="prow-l"><span>${o.name}</span></span><${Switch} checked=${!!o.currentValue} label=${o.name} onChange=${v => onChange(v)} /></div>`;
  return html`<div class="prow"><span class="prow-l"><span>${o.name}</span>${o.description && html`<${Tip} text=${o.description} />`}</span>
    <div class="prow-c"><${ListPick} label=${o.name} value=${o.currentValue} onChange=${onChange} options=${opts.map(x => ({ value: x.value, label: x.name || x.value }))} /></div></div>`;
}

// Terminal dans le dossier de travail du harness, sur sa machine.
async function openHarnessTerminal(s, dir, remote) {
  let target = 'local';
  if (remote) {
    const r = await get('/api/machines').catch(() => null);
    const m = r && r.ok && r.machines.find(x => (x.harnesses || []).includes(s.runtime_id));
    if (!m) return toast(t("inspector.inspector.machine_de_ce_harness_introuvable"), 'err');
    target = m.id;
  }
  openTerminalWith({ target, dir, title: s.provider_name });
}

// Reprendre la session native du harness dans sa propre CLI, dans un terminal.
async function resumeInTerminal(s) {
  const r = await get('/api/runtime/sessions/terminal?id=' + encodeURIComponent(s.id)).catch(e => ({ ok: false, error: e.message }));
  if (!r.ok) return toast(r.error, 'err');
  openTerminalWith({ target: r.target, dir: r.dir, command: r.command, title: r.title });
}

function HarnessPanel() {
  const { s, h } = useStore(chat, c => ({ s: c.session, h: c.harness || {} }));
  const runtimes = useStore(app, a => (a.workspace && a.workspace.runtimes) || []);
  const catalog = useStore(app, a => (a.workspace && a.workspace.models) || []);
  const [diff, setDiff] = useState(null);
  if (!s) return null;
  const caps = ((runtimes.find(r => r.id === s.runtime_id) || {}).capabilities) || [];
  const rt = runtimes.find(r => r.id === s.runtime_id) || {};
  const filesystem = s.filesystem_policy || 'native';
  const protections = rt.filesystem_policies || ['native'];
  const setFilesystem = async value => { if (value === 'full-access' && !await confirm(t('filesystem.full'), t('filesystem.full_note'), { danger: true })) return; if (await configure(s, { filesystem_policy: value, consent: value === 'full-access' })) open(s.id, true); };
  const canDir = caps.includes('workdir'), canAsk = (rt.features?.permissions || []).length > 0, remote = caps.includes('remote');
  const workdir = h.workdir || s.workdir || '';
  const level = h.permission || s.permission || 'ask';
  const modes = modeOptions(rt.features, h.modes || s.available_modes || []);
  const mode = h.mode || s.mode || '';
  const config = configOptions(rt.features, h.config || s.available_config_options || []);
  // The model choice always has a place, even before the agent's session
  // exists: then it comes from Loom's catalog for this agent.
  const nativeModel = config.find(o => o.category === 'model');
  const fromCatalog = catalog.filter(m => m.runtime_id === s.runtime_id && m.enabled !== false);
  const modelOpt = nativeModel || (fromCatalog.length > 1 ? { id: 'model', name: t('inspector.model'), category: 'model', currentValue: s.model, options: fromCatalog.map(m => ({ value: m.model, name: m.name })) } : null);
  const selectedModel = fromCatalog.find(m => m.model === s.model);
  const others = config.filter(o => o.category !== 'model' && !(filesystem !== 'native' && ['mode','sandbox','sandbox_mode'].includes(o.id)) && !(modes.length > 1 && (o.id === 'mode' || o.category === 'mode'))).map(o => rt.features?.protocol === 'app-server' && o.category === 'thought_level' ? { ...o, options: (o.options || []).filter(v => selectedModel?.reasoning_efforts?.includes(v.value)) } : o).filter(o => o.type !== 'select' || (o.options || []).length > 0);
  // Some agents (Pi) announce the same choice as modes and as an option: keep the option.
  const modeIds = new Set(modes.map(m => m.id));
  const modesDuplicated = others.some(o => { const vals = (o.options || []).flatMap(x => x.options ? x.options : [x]).map(x => x.value); return vals.length > 1 && vals.every(v => modeIds.has(v)); });
  const files = h.files || [];
  const usage = h.usage || null;
  const localT = lastTurn(s), u = localT && localT.usage;
  const setLevel = async v => {
    if (v === 'full' && !await confirm(t("inspector.inspector.tout_autoriser"), t("inspector.inspector.le_harness_pourra_lancer_des_commandes_et_modifier_ou_supprimer_d") + (workdir || t("inspector.inspector.le_dossier_de_travail")) + t("inspector.inspector.sans_te_demander"), { ok: t("inspector.inspector.tout_autoriser"), danger: true })) return;
    configure(s, v === 'full' ? { permission: v, consent: true } : { permission: v });
  };
  const showDiff = async f => {
    const r = await get('/api/runtime/sessions/diff?id=' + encodeURIComponent(s.id) + '&path=' + encodeURIComponent(f.path)).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || t("inspector.inspector.diff_indisponible"), 'err');
    setDiff({ path: f.path, text: r.diff || '' });
  };
  const ctxPct = usage && usage.context && usage.context.size ? Math.min(100, Math.round(usage.context.used * 100 / usage.context.size)) : null;
  return html`<div class="insp-body">
    <div class="insp-model"><${Logo} name=${s.runtime_id} /><div><b>${s.model && s.model !== 'default' ? baseName(s.model).replace(/^[\w.-]+:(?=.)/, '').replace(/\.gguf$/i, '') : s.provider_name}</b><span>${s.model && s.model !== 'default' ? s.provider_name + ' · ' : ''}${/^loom[:/]/.test(s.model || '') ? t("inspector.inspector.modele_local_servi_par_loom") : /^(provider:|loom-)/.test(s.model || '') ? t("inspector.inspector.fournisseur_cloud_via_loom") : t("inspector.inspector.compte_natif")}</span></div></div>

    ${modelOpt && html`<div class="hs-sec"><div class="hs-h">${t('inspector.model')}</div><${ModelOption} o=${{ ...modelOpt, name: t('inspector.model') }} onChange=${v => switchModel(s, v)} /></div>`}

    ${canDir && html`<div class="hs-sec"><div class="hs-h">${t("inspector.inspector.dossier_de_travail")}</div>
      <${WorkspacePicker} target=${remote ? ((runtimes.find(r => r.id === s.runtime_id) || {}).machine_id || s.workspace_target || '') : 'local'} current=${workdir} onPick=${async w => { const ok = await configure(s, w.id ? { workspace_id: w.id } : { workdir: w.path }); if (ok) open(s.id, true); return ok; }} />
      ${workdir && html`<button class="btn sm ghost hs-term" onClick=${() => openHarnessTerminal(s, workdir, remote)}><${Icon} n="prompt" />${t("inspector.inspector.ouvrir_un_terminal_ici")}</button>`}
      ${rt.features?.terminal && s.native_session_id && html`<button class="btn sm ghost hs-term" title=${t('inspector.resume.tip')} onClick=${() => resumeInTerminal(s)}><${Icon} n="terminal" />${t('inspector.resume.label')}</button>`}</div>`}

    ${canAsk && html`<div class="hs-sec"><div class="hs-h">${t("inspector.inspector.autorisations")}<${Tip} text=${LEVEL_TIP()} /></div>
      <${Seg} value=${level} onChange=${setLevel} label="${t("inspector.inspector.niveau_d_autorisation")}" options=${permissionOptions(rt.features, LEVELS())} /></div>`}

    ${modes.length > 1 && !modesDuplicated && html`<div class="hs-sec"><div class="hs-h">${t("inspector.inspector.mode_de_l_agent")}<${Tip} text="${t("inspector.inspector.modes_proposes_par_le_harness_lui_meme_par_exemple_planifier_avan")}" /></div>
      <${ListPick} label=${t("inspector.inspector.mode_de_l_agent")} disabled=${filesystem !== 'native'} value=${mode} onChange=${v => configure(s, { mode: v })} options=${modes.map(m => ({ value: m.id, label: tSource(m.name) }))} /></div>`}

    ${protections.length > 1 && html`<div class="hs-sec"><div class="hs-h">${t('filesystem.title')}<${Tip} text=${t('filesystem.note')} /></div>
      <${ListPick} label=${t('filesystem.title')} value=${filesystem} onChange=${setFilesystem} options=${protections.map(p => ({ value: p, label: p === 'native' ? t('filesystem.native') : p === 'workspace-write' ? t('filesystem.workspace_write') : t('filesystem.full') }))} /><p class="note">${filesystem === 'workspace-write' ? t('filesystem.workspace_write_note') : t('filesystem.native_note')}</p>
    </div>`}


    ${others.length > 0 && html`<div class="hs-sec"><div class="hs-h">${t("inspector.inspector.reglages_du_harness")}</div>
      <div class="prows">${others.map(o => html`<${ConfigOption} key=${o.id} o=${o} onChange=${v => configure(s, { config: { [o.id]: v } })} />`)}</div></div>`}

    <div class="hs-sec"><div class="hs-h">${t("inspector.inspector.contexte")}</div>
      ${ctxPct != null ? html`<div class="vram"><div class="vram-h"><span>${t("inspector.inspector.utilise")}</span><b>${fmtTok(usage.context.used)} <small>/ ${fmtTok(usage.context.size)}</small></b></div>
          <div class="gauge"><i style=${`width:${ctxPct}%`}></i></div>
          ${usage.cost && usage.cost.amount != null && html`<div class="vram-leg"><span>${t("inspector.inspector.cout_de_la_session")}</span><span class="end">${usage.cost.amount.toFixed(2)} ${usage.cost.currency || ''}</span></div>`}</div>`
        : u ? html`<div class="kv"><span>${t("inspector.inspector.dernier_tour")}</span><span class="num">${fmtTok(u.prompt_tokens)} → ${fmtTok(u.completion_tokens)} ${t("inspector.inspector.tok")}</span></div>`
        : html`<p class="note">${t("inspector.inspector.non_communique_par_le_harness_pour_l_instant")}</p>`}</div>

    ${canDir && html`<div class="hs-sec"><div class="hs-h">${t("inspector.inspector.fichiers_modifies")} <span class="count">${files.length}</span></div>
      ${files.length ? html`<div class="hs-files">${[...files].reverse().map(f => html`<button class="hs-file" key=${f.path} onClick=${() => showDiff(f)} title=${f.path}>
          <${Icon} n=${f.op === 'delete' ? 'trash' : 'file'} /><span class="trunc">${f.path.startsWith(workdir + '/') ? f.path.slice(workdir.length + 1) : f.path}</span>
          <span class="tc-n">${f.add > 0 ? html`<em class="plus">+${f.add}</em> ` : ''}${f.del > 0 ? html`<em class="minus">−${f.del}</em>` : ''}${f.op === 'create' ? html` <em>${t("inspector.inspector.nouveau")}</em>` : ''}</span></button>`)}</div>`
        : html`<p class="note">${t("inspector.inspector.aucune_modification_dans_cette_discussion")}</p>`}</div>`}
    ${!canDir && html`<p class="note">${t("inspector.inspector.ce_harness_discute_en_texte_pas_de_dossier_de_travail_ni_d_outils")}</p>`}

    <${OtherSessions} s=${s} />


    ${diff && html`<${Modal} wide title=${diff.path.split('/').pop()} sub=${diff.path} onClose=${() => setDiff(null)}><${UnifiedDiff} text=${diff.text} /></${Modal}>`}
  </div>`;
}

const ago = localT => {
  if (!localT) return '';
  const m = Math.round((Date.now() - localT) / 60000);
  return m < 1 ? t("inspector.inspector.a_l_instant") : m < 60 ? m + ' min' : m < 1440 ? Math.round(m / 60) + ' h' : m < 43200 ? Math.round(m / 1440) + t("environment.page.j") : new Date(localT).toLocaleDateString(locale(), { day: 'numeric', month: 'short' });
};

// Autres discussions avec ce harness : celles de Loom s'ouvrent, celles faites
// dans le harness sont importées avec tout leur historique puis ouvertes.
function OtherSessions({ s }) {
  const [list, setList] = useState(null);
  const [all, setAll] = useState(false);
  const [busy, setBusy] = useState('');
  useEffect(() => { setList(null); agentSessions(s.runtime_id, s.id).then(setList, () => setList([])); }, [s.runtime_id, s.id]);
  const pick = async (x, i) => { setBusy(String(i)); await runChoice(x.run, { session: s, rtId: s.runtime_id }); setBusy(''); };
  const shown = list ? (all ? list : list.slice(0, 6)) : [];
  return html`<div class="hs-sec"><div class="hs-h">${t("inspector.inspector.autres_discussions")}${list && html` <span class="count">${list.length}</span>`}<${Tip} text="${t("inspector.inspector.tes_discussions_avec_ce_harness_dans_loom_et_directement_dans_le")}" /></div>
    ${!list ? html`<p class="note">${t("inspector.inspector.chargement")}</p>` : !list.length ? html`<p class="note">${t("inspector.inspector.aucune_autre_discussion_avec_ce_harness")}</p>`
      : html`<div class="hs-files">${shown.map((x, i) => html`<button class="hs-file hs-sess" key=${i} disabled=${!!busy} onClick=${() => pick(x, i)} title=${x.label}>
          <${Icon} n=${x.where === 'loom' ? 'chat' : 'download'} /><span class="trunc">${busy === String(i) ? t("harnesses.page.import") : x.label}</span><span class="tc-n">${ago(x.at)}</span></button>`)}</div>
        ${list.length > 6 && html`<button class="btn ghost sm hs-more" onClick=${() => setAll(!all)}>${all ? t("inspector.inspector.reduire") : t("inspector.inspector.tout_afficher") + list.length + ')'}</button>`}`}</div>`;
}

function UnifiedDiff({ text }) {
  if (!text) return html`<p class="note">${t("inspector.inspector.aucune_difference")}</p>`;
  return html`<pre class="diff">${text.split('\n').map(l => html`<span class=${'dl ' + (l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : '')}><b>${l[0] === '+' || l[0] === '-' ? l[0] : ''}</b>${l[0] === '+' || l[0] === '-' || l[0] === ' ' ? l.slice(1) : l}</span>`)}</pre>`;
}

export function Inspector({ open, onClose }) {
  const mode = useStore(chat, c => c.mode);
  const hasCtx = useStore(chat, c => !!c.session);
  useStore(app, a => a.status && a.status.model);
  // currentExec() dépend de la discussion et du registre des runtimes (chargé après).
  useStore(chat, c => (c.mode || '') + ':' + ((c.session && c.session.runtime_id) || ''));
  useStore(app, a => a.workspace && a.workspace.runtimes);
  const [tab, setTab] = useState('params');
  const exec = currentExec();
  const localT = hasCtx ? tab : 'params';
  const [tag, tone] = EXEC_TAG[exec.kind];
  return html`<aside class=${cls('insp', open && 'open')} aria-label="${t("inspector.inspector.panneau")}" aria-hidden=${String(!open)}>
    <div class="insp-in">
      <header class="insp-head">
        <span class=${'tag ' + tone}>${tag}</span>
        <${Seg} size="sm" value=${localT} onChange=${setTab} label="${t("inspector.inspector.panneau")}" options=${[{ value: 'params', label: exec.kind === 'local' ? t("inspector.inspector.parametres") : exec.kind === 'cloud' ? t("inspector.inspector.modele") : t("inspector.inspector.session") }, { value: 'context', label: t("inspector.inspector.contexte"), disabled: !hasCtx }]} />
        <button class="icon-btn" aria-label="${t("inspector.inspector.fermer_le_panneau")}" onClick=${onClose}><${Icon} n="close" /></button>
      </header>
      <div class="insp-scroll" key=${localT + exec.kind}>
        ${localT === 'context' ? html`<${ContextPanel} />` : exec.kind === 'local' ? html`<${LocalParams} />` : exec.kind === 'cloud' ? html`<${CloudPanel} />` : html`<${HarnessPanel} />`}
      </div>
    </div>
  </aside>`;
}
