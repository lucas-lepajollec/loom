// Panneau de droite : s'adapte à l'exécution choisie. Local = paramètres
// llama.cpp ; Cloud = fournisseur, usage, coût ; Harness = session native.
// Un second onglet montre le contexte partagé de la discussion.
import { html, useState, useStore, cls, fmtTok, fmtSecs, baseName } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Seg, Switch, Tip } from '../../ui/controls.js';
import { Logo } from '../../ui/logo.js';
import { FolderPicker } from '../../ui/folder.js';
import { Modal, toast, confirm, prompt } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { app } from '../../core/state.js';
import { chat } from '../chat/engine.js';
import { currentExec, EXEC_TAG } from '../chat/picker.js';
import { LocalParams } from './params.js';
import { ContextPanel } from './context.js';

const KV = ({ k, children }) => html`<div class="kv"><span>${k}</span><span>${children}</span></div>`;

function lastTurn(s) { return s && (s.turns || []).at(-1); }

function CloudPanel() {
  const s = useStore(chat, c => c.session);
  const ws = useStore(app, a => a.workspace);
  if (!s) return null;
  const p = ((ws && ws.providers) || []).find(x => x.id === s.provider_id);
  const t = lastTurn(s), u = t && t.usage;
  return html`<div class="insp-body">
    <div class="insp-model"><b>${baseName(s.model)}</b><span>${s.provider_name}</span></div>
    <div class="card pad-sm">
      <${KV} k="Fournisseur">${s.provider_name}</${KV}>
      <${KV} k="Clé">${p && p.ready ? html`<span class="tag green">en mémoire</span>` : html`<span class="tag amber">à reconnecter</span>`}</${KV}>
      <${KV} k="Destination"><span class="mono trunc">${s.endpoint}</span></${KV}>
    </div>
    <div class="card pad-sm">
      <div class="lbl" style="margin-bottom:4px">Dernier tour</div>
      ${u ? html`<${KV} k="Entrée">${fmtTok(u.prompt_tokens)} tok</${KV}><${KV} k="Sortie">${fmtTok(u.completion_tokens)} tok</${KV}>`
        : html`<p class="note">Le fournisseur n’a pas communiqué de décompte.</p>`}
      ${t && t.duration_seconds > 0 && html`<${KV} k="Durée">${fmtSecs(t.duration_seconds)}</${KV}>`}
    </div>
    <p class="note">Texte seulement : les fichiers, outils et la mémoire privée ne sont pas envoyés au fournisseur.</p>
    <a class="btn" href="#/cloud">Gérer les fournisseurs</a>
  </div>`;
}

// Session d'un harness : dossier de travail, niveau d'autorisation, mode de
// l'agent, réglages qu'il expose (modèle, réflexion), contexte et fichiers modifiés.
const LEVELS = [{ value: 'ask', label: 'Demander' }, { value: 'edits', label: 'Modifs auto' }, { value: 'full', label: 'Tout' }];
const LEVEL_TIP = 'Demander : chaque action attend ton accord. Modifs auto : lectures et modifications de fichiers acceptées, commandes et suppressions demandées. Tout : aucune question. Les règles propres au harness restent actives.';

async function configure(s, patch) {
  const r = await post('/api/runtime/sessions/configure', { id: s.id, ...patch });
  if (!r.ok) toast(r.error || 'Réglage impossible', 'err');
  return r.ok;
}

function ConfigOption({ o, onChange }) {
  const opts = (o.options || []).flatMap(x => x.options ? x.options : [x]);
  if (o.type === 'boolean') return html`<div class="prow"><span class="prow-l"><span>${o.name}</span></span><${Switch} checked=${!!o.currentValue} label=${o.name} onChange=${v => onChange(v)} /></div>`;
  return html`<div class="prow"><span class="prow-l"><span>${o.name}</span>${o.description && html`<${Tip} text=${o.description} />`}</span>
    <select class="select sm" value=${o.currentValue} onChange=${e => onChange(e.target.value)}>${opts.map(x => html`<option value=${x.value} selected=${x.value === o.currentValue}>${x.name || x.value}</option>`)}</select></div>`;
}

function HarnessPanel() {
  const { s, h } = useStore(chat, c => ({ s: c.session, h: c.harness || {} }));
  const runtimes = useStore(app, a => (a.workspace && a.workspace.runtimes) || []);
  const [pick, setPick] = useState(false);
  const [diff, setDiff] = useState(null);
  if (!s) return null;
  const caps = ((runtimes.find(r => r.id === s.runtime_id) || {}).capabilities) || [];
  const canDir = caps.includes('workdir'), canAsk = caps.includes('approvals'), remote = caps.includes('remote');
  const chooseDir = async () => {
    if (!remote) return setPick(true);
    const p = await prompt('Dossier sur la machine distante', { value: workdir, placeholder: '/home/moi/projet', ok: 'Choisir' });
    if (p) configure(s, { workdir: p });
  };
  const workdir = h.workdir || s.workdir || '';
  const level = h.permission || s.permission || 'ask';
  const modes = h.modes || s.available_modes || [];
  const mode = h.mode || s.mode || '';
  const config = h.config || s.config_options || [];
  const files = h.files || [];
  const usage = h.usage || null;
  const t = lastTurn(s), u = t && t.usage;
  const setLevel = async v => {
    if (v === 'full' && !await confirm('Tout autoriser', 'Le harness pourra lancer des commandes et modifier ou supprimer des fichiers dans ' + (workdir || 'le dossier de travail') + ' sans te demander.', { ok: 'Tout autoriser', danger: true })) return;
    configure(s, v === 'full' ? { permission: v, consent: true } : { permission: v });
  };
  const showDiff = async f => {
    const r = await get('/api/runtime/sessions/diff?id=' + encodeURIComponent(s.id) + '&path=' + encodeURIComponent(f.path)).catch(e => ({ ok: false, error: e.message }));
    if (r.ok === false) return toast(r.error || 'Diff indisponible', 'err');
    setDiff({ path: f.path, text: r.diff || '' });
  };
  const ctxPct = usage && usage.context && usage.context.size ? Math.min(100, Math.round(usage.context.used * 100 / usage.context.size)) : null;
  return html`<div class="insp-body">
    <div class="insp-model"><${Logo} name=${s.runtime_id} /><div><b>${baseName(s.model) || s.provider_name}</b><span>${s.provider_name} · compte natif</span></div></div>

    ${canDir && html`<div class="hs-sec"><div class="hs-h">Dossier de travail</div>
      ${workdir ? html`<button class="hs-dir" onClick=${chooseDir} title=${workdir}><${Icon} n="folder" /><span class="mono trunc">${workdir.replace(/^\/home\/[^/]+/, '~')}</span><span class="muted">Changer</span></button>`
        : html`<button class="btn" onClick=${chooseDir}><${Icon} n="folder" />${remote ? 'Indiquer le dossier distant' : 'Choisir un dossier'}</button>`}</div>`}

    ${canAsk && html`<div class="hs-sec"><div class="hs-h">Autorisations<${Tip} text=${LEVEL_TIP} /></div>
      <${Seg} value=${level} onChange=${setLevel} label="Niveau d’autorisation" options=${LEVELS} /></div>`}

    ${modes.length > 1 && html`<div class="hs-sec"><div class="hs-h">Mode de l’agent<${Tip} text="Modes proposés par le harness lui-même (par exemple planifier avant d’agir)." /></div>
      <select class="select" value=${mode} onChange=${e => configure(s, { mode: e.target.value })}>${modes.map(m => html`<option value=${m.id} selected=${m.id === mode}>${m.name}</option>`)}</select></div>`}

    ${config.filter(o => !(modes.length > 1 && (o.id === 'mode' || o.category === 'mode'))).length > 0 && html`<div class="hs-sec"><div class="hs-h">Réglages du harness</div>
      <div class="prows">${config.filter(o => !(modes.length > 1 && (o.id === 'mode' || o.category === 'mode'))).map(o => html`<${ConfigOption} key=${o.id} o=${o} onChange=${v => configure(s, { config: { [o.id]: v } })} />`)}</div></div>`}

    <div class="hs-sec"><div class="hs-h">Contexte</div>
      ${ctxPct != null ? html`<div class="vram"><div class="vram-h"><span>Utilisé</span><b>${fmtTok(usage.context.used)} <small>/ ${fmtTok(usage.context.size)}</small></b></div>
          <div class="gauge"><i style=${`width:${ctxPct}%`}></i></div>
          ${usage.cost && usage.cost.amount != null && html`<div class="vram-leg"><span>Coût de la session</span><span class="end">${usage.cost.amount.toFixed(2)} ${usage.cost.currency || ''}</span></div>`}</div>`
        : u ? html`<div class="kv"><span>Dernier tour</span><span class="num">${fmtTok(u.prompt_tokens)} → ${fmtTok(u.completion_tokens)} tok</span></div>`
        : html`<p class="note">Non communiqué par le harness pour l’instant.</p>`}</div>

    ${canDir && html`<div class="hs-sec"><div class="hs-h">Fichiers modifiés <span class="count">${files.length}</span></div>
      ${files.length ? html`<div class="hs-files">${[...files].reverse().map(f => html`<button class="hs-file" key=${f.path} onClick=${() => showDiff(f)} title=${f.path}>
          <${Icon} n=${f.op === 'delete' ? 'trash' : 'file'} /><span class="trunc">${f.path.startsWith(workdir + '/') ? f.path.slice(workdir.length + 1) : f.path}</span>
          <span class="tc-n">${f.add > 0 ? html`<em class="plus">+${f.add}</em> ` : ''}${f.del > 0 ? html`<em class="minus">−${f.del}</em>` : ''}${f.op === 'create' ? html` <em>nouveau</em>` : ''}</span></button>`)}</div>`
        : html`<p class="note">Aucune modification dans cette discussion.</p>`}</div>`}
    ${!canDir && html`<p class="note">Ce harness discute en texte : pas de dossier de travail ni d’outils pilotés par Loom.</p>`}

    ${pick && html`<${FolderPicker} start=${workdir} onClose=${() => setPick(false)} onPick=${async p => { setPick(false); await configure(s, { workdir: p }); }} />`}
    ${diff && html`<${Modal} wide title=${diff.path.split('/').pop()} sub=${diff.path} onClose=${() => setDiff(null)}><${UnifiedDiff} text=${diff.text} /></${Modal}>`}
  </div>`;
}

function UnifiedDiff({ text }) {
  if (!text) return html`<p class="note">Aucune différence.</p>`;
  return html`<pre class="diff">${text.split('\n').map(l => html`<span class=${'dl ' + (l.startsWith('+') && !l.startsWith('+++') ? 'add' : l.startsWith('-') && !l.startsWith('---') ? 'del' : '')}><b>${l[0] === '+' || l[0] === '-' ? l[0] : ''}</b>${l[0] === '+' || l[0] === '-' || l[0] === ' ' ? l.slice(1) : l}</span>`)}</pre>`;
}

export function Inspector({ open, onClose }) {
  const mode = useStore(chat, c => c.mode);
  const hasCtx = useStore(chat, c => !!c.session);
  useStore(app, a => a.status && a.status.model);
  const [tab, setTab] = useState('params');
  const exec = currentExec();
  const t = hasCtx ? tab : 'params';
  const [tag, tone] = EXEC_TAG[exec.kind];
  return html`<aside class=${cls('insp', open && 'open')} aria-label="Panneau" aria-hidden=${String(!open)}>
    <div class="insp-in">
      <header class="insp-head">
        <span class=${'tag ' + tone}>${tag}</span>
        <${Seg} size="sm" value=${t} onChange=${setTab} label="Panneau" options=${[{ value: 'params', label: exec.kind === 'local' ? 'Paramètres' : exec.kind === 'cloud' ? 'Modèle' : 'Session' }, { value: 'context', label: 'Contexte', disabled: !hasCtx }]} />
        <button class="icon-btn" aria-label="Fermer le panneau" onClick=${onClose}><${Icon} n="close" /></button>
      </header>
      <div class="insp-scroll" key=${t + exec.kind}>
        ${t === 'context' ? html`<${ContextPanel} />` : exec.kind === 'local' ? html`<${LocalParams} />` : exec.kind === 'cloud' ? html`<${CloudPanel} />` : html`<${HarnessPanel} />`}
      </div>
    </div>
  </aside>`;
}
