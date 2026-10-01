// Cloud : fournisseurs d'API compatibles Chat Completions et modèles proposés
// dans le sélecteur. La clé reste en mémoire côté serveur.
import { Logo } from '../../ui/logo.js';
import { html, useState, useStore, cls } from '../../core/lib.js';
import { Drawer, inspectTrigger } from '../../ui/drawer.js';
import { SelectionInfo } from '../inspector/selection.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Menu, Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { post } from '../../core/api.js';
import { app, refreshWorkspace } from '../../core/state.js';

import { GROUPS, ALL, entryFor } from './catalog.js';

export async function setVisible(id, enabled) {
  const r = await post('/api/workspace/models/visibility', { id, enabled });
  if (!r.ok) toast(r.error, 'err'); refreshWorkspace();
}

function Connect({ provider, entry, onClose }) {
  const editing = !!provider;
  const [step, setStep] = useState(editing || entry ? 1 : 0);
  const [preset, setPreset] = useState(entry || null);
  const [name, setName] = useState(provider ? provider.name : entry && !entry.custom ? entry.name : '');
  const [endpoint, setEndpoint] = useState(provider ? provider.endpoint : entry ? entry.endpoint : '');
  const [key, setKey] = useState('');
  const [usage, setUsage] = useState(provider ? provider.usage_mode !== 'none' : entry ? !!entry.usage : true);
  const nokey = !!(preset && preset.nokey);
  const [remember, setRemember] = useState(provider ? !!provider.remember : true);
  const [catalog, setCatalog] = useState(provider ? provider.models || [] : []);
  const [sel, setSel] = useState(new Set(provider ? provider.models || [] : []));
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState('');
  const pick = p => { setPreset(p); setName(p.custom ? '' : p.name); setEndpoint(p.endpoint); setUsage(!!p.usage); setStep(1); };
  const detect = async () => {
    if (!endpoint || (!key && !nokey && !(provider && provider.ready))) { setMsg('Renseigne la clé' + (!endpoint ? ' et l’URL' : '') + '.'); return; }
    setBusy(true); setMsg('Lecture du catalogue…');
    const r = await post('/api/providers/models', { id: provider ? provider.id : '', endpoint, key: key || (nokey ? 'local' : ''), consent: true });
    setBusy(false);
    if (!r.ok) { setMsg(r.error || 'Catalogue indisponible'); return; }
    setCatalog([...new Set([...r.models, ...sel])]); setStep(2); setMsg(r.models.length + ' modèles détectés');
  };
  const save = async () => {
    const models = [...sel];
    if (!models.length || models.length > 32) { setMsg('Choisis entre 1 et 32 modèles.'); return; }
    const r = await post('/api/providers/save', { id: provider ? provider.id : '', name: name || (preset && preset.name) || 'Fournisseur', endpoint, model: models[0], models, key: key || (nokey && !editing ? 'local' : ''), usage_mode: usage ? '' : 'none', remember: !nokey && remember });
    if (!r.ok) { setMsg(r.error); return; }
    toast(r.warning || 'Fournisseur enregistré', r.warning ? 'err' : undefined); refreshWorkspace(); onClose();
  };
  const list = catalog.filter(m => !q || m.toLowerCase().includes(q.toLowerCase()));
  return html`<${Modal} wide title=${editing ? 'Modèles de ' + provider.name : 'Connecter un fournisseur'} sub=${step === 0 ? 'Choisis un fournisseur compatible Chat Completions.' : step === 1 ? 'La clé reste en mémoire côté serveur ; rien n’est écrit sur le disque.' : 'Choisis les modèles proposés dans tes discussions.'} onClose=${onClose}
      foot=${step === 2 ? html`<span class="grow muted" style="font-size:12.5px">${sel.size} sélectionné${sel.size > 1 ? 's' : ''}</span><button class="btn ghost" onClick=${onClose}>Annuler</button><button class="btn primary" onClick=${save}>Enregistrer</button>`
        : step === 1 ? html`<button class="btn ghost" onClick=${() => editing ? onClose() : setStep(0)}>${editing ? 'Annuler' : 'Retour'}</button><button class="btn primary" disabled=${busy} onClick=${detect}>${busy ? html`<span class="spinner"></span>` : ''}Détecter les modèles</button>` : ''}>
    ${step === 0 && html`<div class="preset-grid">${ALL.map(p => html`<button class="preset-card" onClick=${() => pick(p)}><b>${p.name}</b><span>${p.hint}</span></button>`)}</div>`}
    ${step === 1 && html`
      ${!editing && html`<label class="field"><span>Nom</span><input class="input" value=${name} onInput=${e => setName(e.target.value)} placeholder="ex. OpenRouter" /></label>`}
      <label class="field"><span>URL de base</span><input class="input mono" value=${endpoint} readonly=${editing} onInput=${e => setEndpoint(e.target.value)} placeholder="https://api.exemple.com/v1" /></label>
      <label class="field"><span>Clé API</span><input class="input" type="password" autocomplete="off" value=${key} onInput=${e => setKey(e.target.value)} placeholder=${provider && provider.ready ? 'Déjà en mémoire · laisser vide pour la garder' : nokey ? 'Facultative pour un serveur local' : 'sk-…'} /></label>
      ${!nokey && html`<label class="check"><input type="checkbox" checked=${remember} onChange=${e => setRemember(e.target.checked)} /><span>Mémoriser la clé dans le trousseau du système<${Tip} text="Gardée par Windows, macOS ou le trousseau Linux, jamais dans les fichiers de Loom. Décoché : la clé est oubliée au redémarrage." /></span></label>`}
      <label class="check"><input type="checkbox" checked=${usage} onChange=${e => setUsage(e.target.checked)} /><span>Demander le décompte des tokens<${Tip} text="À décocher si le fournisseur refuse l’option stream_options." /></span></label>`}
    ${step === 2 && html`<label class="search"><${Icon} n="search" /><input placeholder="Filtrer le catalogue…" value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <div class="catalog">${list.map(m => html`<label class=${cls('choice', sel.has(m) && 'on')}><input type="checkbox" checked=${sel.has(m)} onChange=${e => { const n = new Set(sel); e.target.checked ? n.add(m) : n.delete(m); setSel(n); }} /><span class="grow"><b class="mono">${m}</b></span></label>`)}</div>`}
    ${msg && html`<p class="note">${msg}</p>`}
  </${Modal}>`;
}

function Tile({ e, p, onOpen, onConnect }) {
  const state = !p ? 'off' : p.ready ? 'on' : 'warn';
  return html`<div class=${cls('cl-tile', state !== 'off' && 'on')}>
    ${e.custom ? html`<span class="mono-tile">+</span>` : html`<${Logo} id=${e.logo} name=${e.name} />`}
    <span class="grow"><b>${e.name}${state !== 'off' && html`<i class=${cls('dot', state === 'on' ? 'green' : 'hollow')}></i>`}</b><small>${e.hint}</small></span>
    ${state === 'off' ? html`<button class="btn sm" onClick=${onConnect}>Connecter</button>`
      : html`<button class="btn sm ghost" onClick=${onOpen}>${state === 'on' ? 'Gérer' : 'Reconnecter'}</button>`}
  </div>`;
}

function ProviderDetail({ p, models, onModels, onForget, onInspect }) {
  const list = models.filter(m => m.provider_id === p.id);
  return html`<div class="insp-body">
    <div class="insp-model"><${Logo} id=${(entryFor(p) || {}).logo} name=${p.name} size="lg" /><div><b>${p.name}</b><span class="mono trunc">${p.endpoint}</span></div></div>
    <div class="card pad-sm">
      <div class="kv"><span>Clé API</span><span class="state"><i class=${cls('dot', p.ready && 'green')}></i>${!p.ready ? 'À reconnecter' : p.remember ? 'Mémorisée dans le trousseau' : 'En mémoire jusqu’au redémarrage'}</span></div>
      <div class="kv"><span>Protocole</span><span>Chat Completions</span></div>
      <div class="kv"><span>Dans le sélecteur</span><span class="num">${list.filter(m => m.enabled).length} sur ${list.length}</span></div>
    </div>
    <div class="sec-h" style="margin:4px 0 -4px"><h2>Modèles</h2><button class="btn sm" onClick=${onModels}>${p.ready ? 'Choisir les modèles' : 'Reconnecter'}</button></div>
    <div class="card rows">${list.map(m => html`<div class="row"><span class="grow t mono" ...${inspectTrigger(() => onInspect(m.id), 'Inspecter ' + m.name)}>${m.name}</span>
      <${Switch} checked=${m.enabled} label=${'Afficher ' + m.name + ' dans le sélecteur'} onChange=${v => setVisible(m.id, v)} /></div>`)}</div>
    <button class="btn ghost" style="align-self:flex-start" onClick=${onForget}><${Icon} n="key" />Oublier la clé</button>
  </div>`;
}

export function CloudPage() {
  const ws = useStore(app, s => s.workspace);
  const [dlg, setDlg] = useState(null);
  const [open, setOpen] = useState(null);
  const [selected, setSelected] = useState(null);
  const [filter, setFilter] = useState('all');
  const [q, setQ] = useState('');
  const providers = (ws && ws.providers) || [], models = (ws && ws.models) || [];
  const selection = models.find(m => m.id === selected), selectionProvider = providers.find(p => p.id === selection?.provider_id);
  const forget = async p => { if (!await confirm('Oublier la clé', 'La clé de ' + p.name + ' est retirée de la mémoire. Les modèles restent listés.', { ok: 'Oublier' })) return; await post('/api/providers/disconnect', { id: p.id }); refreshWorkspace(); };
  // Chaque fournisseur enregistré est rattaché à son entrée de catalogue ; les autres restent « personnalisés ».
  const byEntry = new Map(), custom = [];
  for (const p of providers) { const e = entryFor(p); if (e && !byEntry.has(e)) byEntry.set(e, p); else custom.push(p); }
  const ready = providers.filter(p => p.ready).length, warn = providers.length - ready;
  const match = e => !q || (e.name + ' ' + e.hint).toLowerCase().includes(q.toLowerCase());
  const keep = (e, p) => match(e) && (filter === 'all' || (filter === 'on' ? p && p.ready : p && !p.ready));
  const connected = [...byEntry].map(([e, p]) => [e, p]).concat(custom.map(p => [{ name: p.name, hint: p.endpoint.replace(/^https?:\/\//, ''), custom: false }, p]));
  const opened = providers.find(p => p.id === open);
  const tile = (e, p) => html`<${Tile} key=${(p && p.id) || e.name} e=${e} p=${p} onOpen=${() => setOpen(p.id)} onConnect=${() => setDlg({ entry: e })} />`;
  return html`<div class="view page"><div class="page-in wide">
    <div class="page-head"><div><h1>Cloud</h1><p>Connecte tes fournisseurs d’IA. Les clés restent en mémoire ou dans le trousseau du système, jamais dans les fichiers de Loom.<${Tip} text="Une discussion n’envoie que du texte au fournisseur, après ton accord. Fichiers, outils locaux et mémoire privée restent sur ta machine." /></p></div>
      <div class="acts"><span class="cl-sum"><i class=${cls('dot', ready && 'green')}></i>${ready} connecté${ready > 1 ? 's' : ''}${warn ? ' · ' + warn + ' à reconnecter' : ''}</span></div></div>
    <div class="cl-bar">
      ${[['all', 'Tous', ALL.length], ['on', 'Connectés', ready], ['warn', 'À reconnecter', warn]].map(([v, l, n]) => html`<button class="chipf" aria-pressed=${String(filter === v)} onClick=${() => setFilter(v)}>${l}<span>${n}</span></button>`)}
      <span class="grow"></span>
      <label class="search"><${Icon} n="search" /><input placeholder="Chercher un fournisseur" aria-label="Chercher un fournisseur" value=${q} onInput=${e => setQ(e.target.value)} /></label>
    </div>
    ${!ws ? html`<div class="skeleton" style="height:160px;margin-top:20px"></div>` : html`
      ${connected.filter(([e, p]) => keep(e, p)).length > 0 && html`<div class="cl-h"><h2>Connectés</h2><span>${providers.length}</span></div>
        <div class="cl-grid stagger">${connected.filter(([e, p]) => keep(e, p)).map(([e, p]) => tile(e, p))}</div>`}
      ${filter === 'all' && GROUPS.map(g => { const items = g.items.filter(e => !byEntry.has(e) && match(e)); return items.length > 0 && html`<div class="cl-h"><h2>${g.name}</h2><span>${items.length}</span></div>
        <div class="cl-grid">${items.map(e => tile(e, null))}</div>`; })}`}
    ${opened && html`<${Drawer} title=${opened.name} onClose=${() => setOpen(null)}><${ProviderDetail} p=${opened} models=${models} onModels=${() => setDlg({ provider: opened })} onForget=${() => forget(opened)} onInspect=${setSelected} /></${Drawer}>`}
    ${selection && selectionProvider && html`<${Drawer} title=${selection.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selection} provider=${selectionProvider} /></${Drawer}>`}
    ${dlg && html`<${Connect} provider=${dlg.provider} entry=${dlg.entry} onClose=${() => setDlg(null)} />`}
  </div></div>`;
}
