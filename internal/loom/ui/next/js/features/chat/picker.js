// Sélecteur d'exécution : qui répond au prochain message. Trois onglets (Local,
// Cloud, Harness), une recherche, et la même discussion quel que soit le choix.
import { Logo } from '../../ui/logo.js';
import { html, useState, useRef, useStore, useMemo, cls, fmtBytes, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Popover } from '../../ui/controls.js';
import { app, go, refreshLibrary, refreshWorkspace, runtimeKind } from '../../core/state.js';
import { chat, chooseLocal, chooseRemote } from './engine.js';

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
    const model = baseName(s.model).replace(/-(low|medium|high|xhigh)$/, '');
    return model && model !== 'default' ? { kind, name: model, sub: s.provider_name } : { kind, name: s.provider_name || s.runtime_id, sub: 'modèle par défaut' };
  }
  const st = app.get().status;
  if (st && st.model) return { kind: 'local', name: st.preset_name || baseName(st.model_name || st.model).replace(/\.gguf$/i, ''), sub: st.preset_name ? 'preset' : '' };
  return { kind: 'local', name: '', sub: '' };
}

// Éditeur du modèle, déduit de son identifiant (« anthropic/claude… », « gemini-3… »).
const VENDORS = [[/claude|anthropic|^(opus|sonnet|haiku|fable)/, 'Anthropic'], [/gemini|gemma|google/, 'Google'], [/(^|\/)(gpt|o\d|codex|openai)/, 'OpenAI'], [/deepseek/, 'DeepSeek'],
  [/qwen|alibaba/, 'Qwen'], [/glm|z-ai|zhipu/, 'Zhipu · GLM'], [/kimi|moonshot/, 'Moonshot · Kimi'], [/minimax/, 'MiniMax'], [/grok|x-ai|xai/, 'xAI'],
  [/mistral|codestral|magistral|devstral/, 'Mistral'], [/llama|meta/, 'Meta'], [/nemotron|nvidia/, 'NVIDIA'], [/cohere|command/, 'Cohere']];
export const vendorOf = id => { const s = String(id).toLowerCase(); const v = VENDORS.find(([re]) => re.test(s)); return v ? v[1] : 'Autres'; };

// Regroupe des lignes par fournisseur (ou harness), puis par éditeur quand il y en a plusieurs.
function grouped(rows) {
  const groups = new Map();
  for (const r of rows) { if (!groups.has(r.group)) groups.set(r.group, []); groups.get(r.group).push(r); }
  return [...groups].map(([name, items]) => {
    const vendors = new Map();
    for (const r of items) { const v = vendorOf(r.vendorKey); if (!vendors.has(v)) vendors.set(v, []); vendors.get(v).push(r); }
    return { name, cli: items[0].cli, subs: vendors.size > 1 || (items[0].cli && vendors.size === 1 && !vendors.has('Autres')) ? [...vendors].sort((a, b) => a[0].localeCompare(b[0])) : [['', items]] };
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
  useStore(chat, s => s.session && s.session.model + s.mode);
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
  const visible = new Map(wsModels.filter(m => m.kind === 'local').map(m => [baseName(m.model), m.enabled]));
  const match = t => !q || String(t).toLowerCase().includes(q.toLowerCase());

  const lists = useMemo(() => {
    const live = status && status.health ? baseName(status.model) : '';
    const weights = (models || []).filter(m => !/mmproj/i.test(m.name) && visible.get(m.name) !== false);
    const local = [
      ...(presets || []).map((p, i) => ({ id: 'p' + p.id, title: p.name || p.id, sub: 'preset' + (p.model ? ' · ' + baseName(p.model) : ''), active: status && status.preset_id === p.id, loaded: status && status.preset_id === p.id && status.health, run: () => chooseLocal({ presetIndex: i + 1, name: p.name, model: p.model }) })),
      ...weights.map(m => ({ id: 'm' + m.path, title: m.name.replace(/\.gguf$/i, ''), sub: [fmtBytes(m.size), m.shards > 1 ? m.shards + ' fichiers' : ''].filter(Boolean).join(' · '), active: cur.kind === 'local' && live === m.name && !status.preset_id, loaded: live === m.name, run: () => chooseLocal({ model: m.value || m.path, name: m.name }) })),
    ];
    const cloud = wsModels.filter(m => m.kind === 'cloud' && m.enabled).map(m => ({ id: m.id, title: m.name, sub: '', group: m.provider_name, vendorKey: m.model, active: cur.kind === 'cloud' && chat.get().session && chat.get().session.model === m.model, run: () => chooseRemote(m) }));
    const harness = groupVariants(wsModels.filter(m => m.kind === 'harness' && m.enabled)).map(g => {
      const m = g.variants.find(v => /-medium$/.test(v.model)) || g.variants[0];
      const rt = ((workspace && workspace.runtimes) || []).find(r => r.id === m.runtime_id) || {};
      const acp = (rt.capabilities || []).includes('workdir');
      return { id: g.key, title: g.name, sub: g.variants.length > 1 ? 'réflexion réglable' : m.model && m.model !== g.name && m.model !== 'default' ? m.model : '', group: m.provider_name, cli: acp ? 'ACP' : rt.cli || '', vendorKey: m.model + ' ' + g.name, active: cur.kind === 'harness' && g.variants.some(v => chat.get().session && v.model === chat.get().session.model), run: () => chooseRemote(m) };
    });
    return { local, cloud, harness };
  }, [status, workspace, models, presets, cur.kind, cur.name]);

  const shown = lists[tab].filter(r => match(r.title + ' ' + r.sub));
  const [tag] = EXEC_TAG[cur.kind];
  const emptyText = { local: 'Aucun modèle local. Télécharge un GGUF depuis le Hub.', cloud: 'Aucun modèle cloud. Ajoute un fournisseur dans Cloud.', harness: 'Aucun harness connecté. Connecte-en un dans Harnesses.' }[tab];

  return html`<button type="button" class=${cls('exec-btn', anchor && 'open')} ref=${btn} onClick=${toggle} aria-haspopup="dialog" aria-expanded=${String(!!anchor)}>
      ${cur.name ? html`<i class=${cls('dot', cur.kind !== 'local' || (status && status.health) ? 'green' : '')}></i><b>${cur.name}</b><span class="exec-sub">${[tag, cur.sub].filter(Boolean).join(' · ')}</span>` : html`<b>Choisir un modèle</b>`}
      <${Icon} n="chevron" class="exec-caret" />
    </button>
    ${anchor && html`<${Popover} anchor=${anchor} onClose=${close} width=${420} class="picker">
      <div class="pick-top">
        <label class="search"><${Icon} n="search" /><input autofocus placeholder="Rechercher un modèle, un preset, un harness…" value=${q} onInput=${e => setQ(e.target.value)} /></label>
        <${Seg} value=${tab} onChange=${setTab} label="Type d’exécution" options=${[
          { value: 'local', label: 'Local', count: lists.local.length },
          { value: 'cloud', label: 'Cloud', count: lists.cloud.length },
          { value: 'harness', label: 'Harness', count: lists.harness.length }]} />
      </div>
      <div class="pick-list" key=${tab}>
        ${!shown.length ? html`<div class="pick-empty">${q ? 'Aucun résultat pour « ' + q + ' ».' : emptyText}</div>`
          : tab === 'local' ? shown.map(r => html`<${Row} key=${r.id} ...${r} onPick=${() => { close(); r.run(); }} />`)
          : grouped(shown).map(g => html`<div class="pick-g" key=${g.name}>
              <div class="pick-group"><${Logo} name=${g.name} size="sm" /><span>${g.name}</span><span class="grow"></span>${g.cli && html`<small>${g.cli}</small>`}</div>
              ${g.subs.map(([v, items]) => html`${v && html`<div class="pick-sub"><${Logo} name=${v} size="xs" />${v}</div>`}${items.map(r => html`<${Row} key=${r.id} ...${r} nested onPick=${() => { close(); r.run(); }} />`)}`)}
            </div>`)}
      </div>
      <div class="pick-foot"><span>Changer d’exécution garde la même discussion.</span>
        <button type="button" onClick=${() => { close(); go(tab === 'local' ? 'local' : tab === 'cloud' ? 'cloud' : 'harnesses'); }}>Gérer</button></div>
    </${Popover}>`}`;
}
