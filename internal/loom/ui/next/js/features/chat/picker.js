import { t, getLang } from '../../core/i18n.js';
// Sélecteur d'exécution : qui répond au prochain message. Trois onglets (Local,
// Cloud, Harness), une recherche, et la même discussion quel que soit le choix.
import { Logo } from '../../ui/logo.js';
import { html, useState, useRef, useStore, useMemo, cls, fmtBytes, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Popover } from '../../ui/controls.js';
import { app, go, refreshLibrary, refreshWorkspace, runtimeKind } from '../../core/state.js';
import { chat, chooseLocal, chooseRemote } from './engine.js';
import { executionKey } from './execution.js';

// Regroupe les variantes natives (-low/-medium/-high) sous un seul modèle.
export function groupVariants(models) {
  const map = new Map();
  for (const m of models) {
    const k = m.runtime_id + '|' + String(m.model).replace(/-(low|medium|high|xhigh)$/, '');
    if (!map.has(k)) map.set(k, { key: k, name: String(m.name).replace(/-(low|medium|high|xhigh)$/, ''), variants: [] });
    map.get(k).variants.push(m);
  }
  return [...map.values()];
}

export function currentExec() {
  const c = chat.get(), s = c.session;
  if (c.mode === 'thread' && s) {
    const kind = runtimeKind(s.runtime_id);
    // Un harness ACP démarre sur le modèle par défaut de son compte : on affiche le harness.
    const model = baseName(s.model).replace(/-(low|medium|high|xhigh)$/, '').replace(/^[\w.-]+:(?=.)/, '').replace(/\.gguf$/i, '');
    return model && model !== 'default' ? { kind, name: model, sub: s.provider_name } : { kind, name: s.provider_name || s.runtime_id, sub: t("chat.picker.modele_par_defaut") };
  }
  const st = app.get().status;
  if (st && st.model) return { kind: 'local', name: st.preset_name || baseName(st.model_name || st.model).replace(/\.gguf$/i, ''), sub: st.preset_name ? 'preset' : '' };
  return { kind: 'local', name: '', sub: '' };
}

// Éditeur du modèle, déduit de son identifiant (« anthropic/claude… », « gemini-3… »).
const VENDORS = () => ([[/^loom:/, t("chat.picker.modeles_loom_local")], [/claude|anthropic|^(opus|sonnet|haiku|fable)/, 'Anthropic'], [/gemini|gemma|google/, 'Google'], [/(^|\/)(gpt|o\d|codex|openai)/, 'OpenAI'], [/deepseek/, 'DeepSeek'],
  [/qwen|alibaba/, 'Qwen'], [/glm|z-ai|zhipu/, 'Zhipu · GLM'], [/kimi|moonshot/, 'Moonshot · Kimi'], [/minimax/, 'MiniMax'], [/grok|x-ai|xai/, 'xAI'],
  [/mistral|codestral|magistral|devstral/, 'Mistral'], [/llama|meta/, 'Meta'], [/nemotron|nvidia/, 'NVIDIA'], [/cohere|command/, 'Cohere']]);
export const vendorOf = id => { const s = String(id).toLowerCase(); const v = VENDORS().find(([re]) => re.test(s)); return v ? v[1] : t("chat.picker.autres"); };

// Regroupe des lignes par fournisseur (ou harness), puis par éditeur quand il y en a plusieurs.
function grouped(rows) {
  const groups = new Map();
  for (const r of rows) { if (!groups.has(r.group)) groups.set(r.group, []); groups.get(r.group).push(r); }
  return [...groups].map(([name, items]) => {
    const vendors = new Map();
    for (const r of items) { const v = r.via || vendorOf(r.vendorKey); if (!vendors.has(v)) vendors.set(v, []); vendors.get(v).push(r); }
    return { name, logo: items[0].logo, cli: items[0].cli, subs: vendors.size > 1 || (items[0].cli && vendors.size === 1 && !vendors.has(t("chat.picker.autres"))) ? [...vendors].sort((a, b) => a[0].localeCompare(b[0])) : [['', items]] };
  });
}

export const EXEC_TAG = { local: ['Local', 'blue'], cloud: ['Cloud', 'violet'], harness: ['Harness', 'amber'] };

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
  const { status, workspace, models, presets } = useStore(app, s => ({ status: s.status, workspace: s.workspace, models: s.models, presets: s.presets }));
  const route = useStore(chat, executionKey);
  const cur = currentExec();
  const [tab, setTab] = useState(cur.kind);
  const [q, setQ] = useState('');
  const btn = useRef();

  const toggle = () => {
    if (anchor) { setAnchor(null); return; }
    refreshLibrary(); refreshWorkspace();
    setTab(currentExec().kind); setQ(''); setAnchor(btn.current);
  };
  const close = () => setAnchor(null);
  const wsModels = (workspace && workspace.models) || [];
  const visible = new Map(wsModels.filter(m => m.kind === 'local').flatMap(m => [[m.model, m.enabled], [m.engine_value || m.model, m.enabled]]));
  const match = localT => !q || String(localT).toLowerCase().includes(q.toLowerCase());

  const lists = useMemo(() => {
    const weights = (models || []).filter(m => !/mmproj/i.test(m.name) && visible.get(m.path || m.value) !== false);
    const local = [
      ...(presets || []).map((p, i) => ({ id: 'p' + p.id, title: p.name || p.id, sub: 'preset' + (p.model ? ' · ' + baseName(p.model) : ''), active: cur.kind === 'local' && status && status.preset_id === p.id, loaded: status && status.preset_id === p.id && status.health, run: () => chooseLocal({ presetIndex: i + 1, name: p.name, model: p.model }) })),
      ...weights.map(m => ({ id: 'm' + m.path, title: m.name.replace(/\.gguf$/i, ''), sub: [fmtBytes(m.size), m.shards > 1 ? m.shards + t("chat.picker.fichiers") : ''].filter(Boolean).join(' · '), active: cur.kind === 'local' && status && status.health && (status.model === m.path || status.model === (m.value || m.path)) && !status.preset_id, loaded: status && status.health && (status.model === m.path || status.model === (m.value || m.path)), run: () => chooseLocal({ path: m.path, model: m.value || m.path, name: m.name }) })),
    ];
    const cloud = wsModels.filter(m => m.kind === 'cloud' && m.enabled).map(m => ({ id: m.id, title: m.name, sub: '', group: m.provider_name, vendorKey: m.model, active: cur.kind === 'cloud' && chat.get().session && chat.get().session.provider_id === m.provider_id && chat.get().session.model === m.model, run: () => chooseRemote(m) }));
    const harness = groupVariants(wsModels.filter(m => m.kind === 'harness' && m.enabled)).map(g => {
      const m = g.variants.find(v => /-medium$/.test(v.model)) || g.variants[0];
      const rt = ((workspace && workspace.runtimes) || []).find(r => r.id === m.runtime_id) || {};
      const acp = (rt.capabilities || []).includes('workdir');
      return { id: g.key, title: g.name, sub: g.variants.length > 1 ? t("chat.picker.reflexion_reglable") : m.model && m.model !== g.name && m.model !== 'default' ? m.model : '', group: rt.machine ? m.provider_name + t("chat.picker.sur") + rt.machine : m.provider_name, logo: m.runtime_id, via: m.via || '', cli: acp ? 'ACP' : rt.cli || '', vendorKey: m.model + ' ' + g.name, active: cur.kind === 'harness' && g.variants.some(v => chat.get().session && v.model === chat.get().session.model && chat.get().session.runtime_id === v.runtime_id), run: () => chooseRemote(m) };
    });
    return { local, cloud, harness };
  }, [status, workspace, models, presets, route, cur.kind, cur.name, getLang()]);

  const shown = lists[tab].filter(r => match(r.title + ' ' + r.sub));
  const [tag] = EXEC_TAG[cur.kind];
  const emptyText = { local: t("chat.picker.aucun_modele_local_telecharge_un_gguf_depuis_le_hub"), cloud: t("chat.picker.aucun_modele_cloud_ajoute_un_fournisseur_dans_cloud"), harness: t("chat.picker.aucun_harness_connecte_connecte_en_un_dans_harnesses") }[tab];

  return html`<button type="button" class=${cls('exec-btn', anchor && 'open')} ref=${btn} onClick=${toggle} aria-haspopup="dialog" aria-expanded=${String(!!anchor)}>
      ${cur.name ? html`<i class=${cls('dot', cur.kind !== 'local' || (status && status.health) ? 'green' : '')}></i><span class="exec-route">${cur.kind === 'harness' ? cur.sub || tag : tag}</span><b>${cur.name}</b>${cur.kind !== 'harness' && cur.sub && html`<span class="exec-sub">${cur.sub}</span>`}` : html`<b>${t("chat.picker.choisir_un_modele")}</b>`}
      <${Icon} n="chevron" class="exec-caret" />
    </button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${close} width=${420} class="picker">
      <div class="pick-top">
        <label class="search"><${Icon} n="search" /><input autofocus placeholder="${t("chat.picker.rechercher_un_modele_un_preset_un_harness")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
        <${Seg} value=${tab} onChange=${setTab} label="${t("chat.picker.type_d_execution")}" options=${[
          { value: 'local', label: t("chat.picker.local"), count: lists.local.length },
          { value: 'cloud', label: t("chat.picker.cloud"), count: lists.cloud.length },
          { value: 'harness', label: t("chat.picker.harness"), count: lists.harness.length }]} />
      </div>
      <div class="pick-list" key=${tab}>
        ${!shown.length ? html`<div class="pick-empty">${q ? t("chat.picker.aucun_resultat_pour") + q + ' ».' : emptyText}</div>`
          : tab === 'local' ? shown.map(r => html`<${Row} key=${r.id} ...${r} onPick=${() => { close(); r.run(); }} />`)
          : grouped(shown).map(g => html`<div class="pick-g" key=${g.name}>
              <div class="pick-group"><${Logo} name=${g.logo || g.name} size="sm" /><span>${g.name}</span><span class="grow"></span>${g.cli && html`<small>${g.cli}</small>`}</div>
              ${g.subs.map(([v, items]) => html`${v && html`<div class="pick-sub"><${Logo} name=${v} size="xs" />${v}</div>`}${items.map(r => html`<${Row} key=${r.id} ...${r} nested onPick=${() => { close(); r.run(); }} />`)}`)}
            </div>`)}
      </div>
      <div class="pick-foot"><span>${t("chat.picker.changer_d_execution_garde_la_meme_discussion")}</span>
        ${tab === 'local' && html`<button type="button" onClick=${() => { close(); app.set({ newPreset: true }); go('local'); }}>${t("chat.picker.nouveau_preset")}</button>`}
        <button type="button" onClick=${() => { close(); go(tab === 'local' ? 'local' : tab === 'cloud' ? 'cloud' : 'harnesses'); }}>${t("chat.picker.gerer")}</button></div>
    </${Popover}>`}`;
}
