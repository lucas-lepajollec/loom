import { t, getLang } from '../../core/i18n.js';
// Sélecteur de modèle d'une discussion : une recherche, un filtre (tous,
// local, cloud), les presets, les modèles locaux puis ceux de chaque
// fournisseur cloud. La discussion reste la même quel que soit le choix.
import { Logo } from '../../ui/logo.js';
import { html, useState, useRef, useStore, useMemo, cls, fmtBytes, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Popover } from '../../ui/controls.js';
import { app, go, refreshLibrary, refreshWorkspace, runtimeKind } from '../../core/state.js';
import { chat, chooseLocal, chooseRemote } from './engine.js';
import { executionKey } from './execution.js';

// Regroupe les variantes natives (-low/-medium/-high) sous un seul modèle.


export function currentExec() {
  const c = chat.get(), s = c.session;
  if (c.mode === 'thread' && s) {
    const kind = runtimeKind(s.runtime_id);
    // Retired runtime IDs remain display-only; live selection uses Local/Cloud.
    const model = baseName(s.model).replace(/-(low|medium|high|xhigh)$/, '').replace(/^[\w.-]+:(?=.)/, '').replace(/\.gguf$/i, '');
    return model && model !== 'default' ? { kind, name: model, sub: s.provider_name } : { kind, name: s.provider_name || s.runtime_id, sub: t("chat.picker.modele_par_defaut") };
  }
  if (c.localTarget) return { kind: 'local', name: c.localTarget.name || baseName(c.localTarget.model || '').replace(/\.gguf$/i, ''), sub: c.localTarget.presetIndex ? 'preset' : '' };
  const st = app.get().status;
  if (st && st.model) return { kind: 'local', name: st.preset_name || baseName(st.model_name || st.model).replace(/\.gguf$/i, ''), sub: st.preset_name ? 'preset' : '' };
  return { kind: 'local', name: '', sub: '' };
}

// Éditeur du modèle, déduit de son identifiant (« anthropic/claude… », « gemini-3… »).
const VENDORS = () => ([[/^loom:/, t("chat.picker.modeles_loom_local")], [/claude|anthropic|^(opus|sonnet|haiku|fable)/, 'Anthropic'], [/gemini|gemma|google/, 'Google'], [/(^|\/)(gpt|o\d|codex|openai)/, 'OpenAI'], [/deepseek/, 'DeepSeek'],
  [/qwen|alibaba/, 'Qwen'], [/glm|z-ai|zhipu/, 'Zhipu · GLM'], [/kimi|moonshot/, 'Moonshot · Kimi'], [/minimax/, 'MiniMax'], [/grok|x-ai|xai/, 'xAI'],
  [/mistral|codestral|magistral|devstral/, 'Mistral'], [/llama|meta/, 'Meta'], [/nemotron|nvidia/, 'NVIDIA'], [/cohere|command/, 'Cohere']]);
export const vendorOf = id => { const s = String(id).toLowerCase(); const v = VENDORS().find(([re]) => re.test(s)); return v ? v[1] : t("chat.picker.autres"); };

// Regroupe des lignes par fournisseur, puis par éditeur quand il y en a plusieurs.
function grouped(rows) {
  const groups = new Map();
  for (const r of rows) { if (!groups.has(r.group)) groups.set(r.group, []); groups.get(r.group).push(r); }
  return [...groups].map(([name, items]) => {
    const vendors = new Map();
    for (const r of items) { const v = r.via || vendorOf(r.vendorKey); if (!vendors.has(v)) vendors.set(v, []); vendors.get(v).push(r); }
    return { name, logo: items[0].logo, cli: items[0].cli, subs: vendors.size > 1 || (items[0].cli && vendors.size === 1 && !vendors.has(t("chat.picker.autres"))) ? [...vendors].sort((a, b) => a[0].localeCompare(b[0])) : [['', items]] };
  });
}

export const EXEC_TAG = { local: ['Local', 'blue'], cloud: ['Cloud', 'violet'], retired: ['Archive', 'amber'] };

function Row({ title, sub, right, active, loaded, nested, onPick }) {
  return html`<button type="button" class=${cls('pick-row', active && 'active', nested && 'nested')} onClick=${onPick}>
    ${!nested && html`<span class=${cls('dot', loaded ? 'green' : '')}></span>`}
    <span class="pick-txt"><b>${title}</b>${sub && html`<small>${sub}</small>`}</span>
    ${right && html`<span class="pick-right">${right}</span>`}
    ${active && html`<${Icon} n="check" class="pick-check" />`}
  </button>`;
}

export function Picker() {
  const [anchor, setAnchor] = useState(null);
  const { status, workspace, models, presets, unavailable } = useStore(app, s => ({ status: s.status, workspace: s.workspace, models: s.models, presets: s.presets, unavailable: s.unavailable }));
  const route = useStore(chat, executionKey);
  const cur = currentExec();
  const [tab, setTab] = useState('all');
  const [q, setQ] = useState('');
  const btn = useRef();

  const toggle = () => {
    if (anchor) { setAnchor(null); return; }
    refreshLibrary(); refreshWorkspace();
    setTab('all'); setQ(''); setAnchor(btn.current);
  };
  const close = () => setAnchor(null);
  const wsModels = (workspace && workspace.models) || [];
  const match = localT => !q || String(localT).toLowerCase().includes(q.toLowerCase());

  const lists = useMemo(() => {
    if (!anchor) return { local: [], cloud: [] };
    const visible = new Map(wsModels.filter(m => m.kind === 'local').flatMap(m => [[m.model, m.enabled], [m.engine_value || m.model, m.enabled]]));
    const weights = (models || []).filter(m => !/mmproj/i.test(m.name) && visible.get(m.path || m.value) !== false);
    const pending = chat.get().localTarget;
    const local = [
      ...(presets || []).map((p, i) => ({ id: 'p' + p.id, preset: true, title: p.name || p.id, sub: p.model ? baseName(p.model).replace(/\.gguf$/i, '') : '', active: cur.kind === 'local' && (pending ? pending.presetIndex === i + 1 : status && status.preset_id === p.id), loaded: status && status.preset_id === p.id && status.health, run: () => chooseLocal({ presetIndex: i + 1, name: p.name, model: p.model }) })),
      ...weights.map(m => ({ id: 'm' + m.path, title: m.name.replace(/\.gguf$/i, ''), sub: [quantOf(m.name), fmtBytes(m.size), m.shards > 1 ? m.shards + t("chat.picker.fichiers") : '', status && status.health && (status.model === m.path || status.model === (m.value || m.path)) ? t('chat.picker.loaded') : ''].filter(Boolean).join(' · '), active: cur.kind === 'local' && (pending ? !pending.presetIndex && (pending.model === (m.value || m.path) || pending.path === m.path) : status && (status.model === m.path || status.model === (m.value || m.path)) && !status.preset_id), loaded: status && status.health && (status.model === m.path || status.model === (m.value || m.path)), run: () => chooseLocal({ path: m.path, model: m.value || m.path, name: m.name }) })),
    ];
    const cloud = wsModels.filter(m => m.kind === 'cloud' && m.enabled).map(m => ({ id: m.id, title: m.name, sub: '', group: m.provider_name, vendorKey: m.model, active: cur.kind === 'cloud' && chat.get().session && chat.get().session.provider_id === m.provider_id && chat.get().session.model === m.model, run: () => chooseRemote(m) }));
    return { local, cloud };
  }, [anchor, status, workspace, models, presets, route, cur.kind, cur.name, getLang()]);

  const pick = list => list.filter(r => match(r.title + ' ' + r.sub + ' ' + (r.group || '')));
  const shownLocal = tab === 'cloud' ? [] : pick(lists.local), shownCloud = tab === 'local' ? [] : pick(lists.cloud);
  const shown = [...shownLocal, ...shownCloud];
  const machine = (app.get().serverInfo && app.get().serverInfo.hostname) || '';
  const [tag] = EXEC_TAG[cur.kind];
  const emptyText = tab === 'cloud' ? t("chat.picker.aucun_modele_cloud_ajoute_un_fournisseur_dans_cloud") : t("chat.picker.aucun_modele_local_telecharge_un_gguf_depuis_le_hub");
  const rowsOf = list => list.map(r => html`<${Row} key=${r.id} ...${r} onPick=${() => { close(); r.run(); }} />`);
  const presetsShown = shownLocal.filter(r => r.preset), weightsShown = shownLocal.filter(r => !r.preset);

  const extra = cur.kind === 'local' ? quantOf(status && (status.model_name || status.model)) : cur.sub;
  return html`<button type="button" class=${cls('exec-btn', anchor && 'open')} ref=${btn} onClick=${toggle} aria-haspopup="dialog" aria-expanded=${String(!!anchor)}>
      ${cur.name ? html`<i class=${cls('dot', cur.kind !== 'local' || (status && status.health) ? 'green' : '')}></i><b>${cur.name}</b><span class="exec-sub"><span class="exec-route">${tag}</span>${extra ? ' · ' + extra : ''}</span>` : html`<b>${t("chat.picker.choisir_un_modele")}</b>`}
      <${Icon} n="chevron" class="exec-caret" />
    </button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${close} width=${430} heightLimit=${560} heightRatio=${0.75} class="picker">
      <div class="pick-top">
        <label class="search pick-q"><${Icon} n="search" /><input autofocus placeholder="${t("chat.picker.rechercher_un_modele_un_preset_un_harness")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
        <${Seg} value=${tab} onChange=${setTab} label="${t("chat.picker.type_d_execution")}" options=${[
          { value: 'all', label: t('chat.picker.all') },
          { value: 'local', label: t("chat.picker.local"), count: lists.local.length },
          { value: 'cloud', label: t("chat.picker.cloud"), count: lists.cloud.length }]} />
      </div>
      <div class="pick-list" key=${tab}>
        ${(tab !== 'cloud' && (unavailable.models || unavailable.presets) || tab !== 'local' && unavailable.workspace) && html`<div class="alert amber" role="status">${t('access.unavailable')}</div>`}
        ${!shown.length ? html`<div class="pick-empty">${q ? t("chat.picker.aucun_resultat_pour") + q + ' ».' : emptyText}</div>` : html`
          ${presetsShown.length > 0 && html`<div class="pick-h">${t('chat.picker.presets')}</div>${rowsOf(presetsShown)}`}
          ${weightsShown.length > 0 && html`<div class="pick-h">${t('chat.picker.local_on', { machine: machine || t('app.shell.cette_machine') })}</div>${rowsOf(weightsShown)}`}
          ${grouped(shownCloud).map(g => html`<div class="pick-g" key=${g.name}>
              <div class="pick-h"><span>${t('chat.picker.cloud')} · ${g.name}</span></div>
              ${g.subs.map(([v, items]) => html`${v && html`<div class="pick-sub"><${Logo} name=${v} size="xs" />${v}</div>`}${items.map(r => html`<${Row} key=${r.id} ...${r} nested onPick=${() => { close(); r.run(); }} />`)}`)}
            </div>`)}`}
      </div>
      <div class="pick-foot">
        <button type="button" onClick=${() => { close(); app.set({ newPreset: true }); go('local'); }}>${t("chat.picker.nouveau_preset")}</button>
        <span class="grow"></span>
        <button type="button" onClick=${() => { close(); go(tab === 'cloud' ? 'cloud' : 'local'); }}>${t('chat.picker.manage_models')}</button></div>
    </${Popover}>`}`;
}

// Quantisation lisible d'un fichier GGUF (« Q4_K_M »).
function quantOf(name) { const m = /(?:^|[-_.])((?:I?Q\d[\w]*?)|F16|BF16|F32)(?:\.gguf)?$/i.exec(String(name || '').replace(/\.gguf$/i, '')); return m ? m[1].toUpperCase() : ''; }
