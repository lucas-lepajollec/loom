import { t } from '../../core/i18n.js';
// Cloud : fournisseurs d'API compatibles Chat Completions et modèles proposés
// dans le sélecteur. La clé reste en mémoire côté serveur.
import { Logo } from '../../ui/logo.js';
import { SectionTabs } from '../../app/sections.js';
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
    if (!endpoint || (!key && !nokey && !(provider && provider.ready))) { setMsg(t("cloud.page.renseigne_la_cle") + (!endpoint ? t("cloud.page.et_l_url") : '') + '.'); return; }
    setBusy(true); setMsg(t("cloud.page.lecture_du_catalogue"));
    const r = await post('/api/providers/models', { id: provider ? provider.id : '', endpoint, key: key || (nokey ? 'local' : ''), consent: true });
    setBusy(false);
    if (!r.ok) { setMsg(r.error || t("cloud.page.catalogue_indisponible")); return; }
    setCatalog([...new Set([...r.models, ...sel])]); setStep(2); setMsg(r.models.length + t("cloud.page.modeles_detectes"));
  };
  const save = async () => {
    const models = [...sel];
    if (!models.length || models.length > 32) { setMsg(t("cloud.page.choisis_entre_1_et_32_modeles")); return; }
    const r = await post('/api/providers/save', { id: provider ? provider.id : '', name: name || (preset && preset.name) || t("cloud.page.fournisseur"), endpoint, model: models[0], models, key: key || (nokey && !editing ? 'local' : ''), usage_mode: usage ? '' : 'none', remember: !nokey && remember });
    if (!r.ok) { setMsg(r.error); return; }
    toast(r.warning || t("cloud.page.fournisseur_enregistre"), r.warning ? 'err' : undefined); refreshWorkspace(); onClose();
  };
  const list = catalog.filter(m => !q || m.toLowerCase().includes(q.toLowerCase()));
  return html`<${Modal} wide title=${editing ? t("cloud.page.modeles_de") + provider.name : t("cloud.page.connecter_un_fournisseur")} sub=${step === 0 ? t("cloud.page.choisis_un_fournisseur_compatible_chat_completions") : step === 1 ? t("cloud.page.la_cle_reste_en_memoire_cote_serveur_rien_n_est_ecrit_sur_le_disq") : t("cloud.page.choisis_les_modeles_proposes_dans_tes_discussions")} onClose=${onClose}
      foot=${step === 2 ? html`<span class="grow muted" style="font-size:12.5px">${sel.size} ${t("cloud.page.selectionne")}${sel.size > 1 ? 's' : ''}</span><button class="btn ghost" onClick=${onClose}>${t("cloud.page.annuler")}</button><button class="btn primary" onClick=${save}>${t("cloud.page.enregistrer")}</button>`
        : step === 1 ? html`<button class="btn ghost" onClick=${() => editing ? onClose() : setStep(0)}>${editing ? t("cloud.page.annuler") : t("chat.composer.retour")}</button><button class="btn primary" disabled=${busy} onClick=${detect}>${busy ? html`<span class="spinner"></span>` : ''}${t("cloud.page.detecter_les_modeles")}</button>` : ''}>
    ${step === 0 && html`<div class="preset-grid">${ALL.map(p => html`<button class="preset-card" onClick=${() => pick(p)}><b>${p.name}</b><span>${p.hint}</span></button>`)}</div>`}
    ${step === 1 && html`
      ${!editing && html`<label class="field"><span>${t("cloud.page.nom")}</span><input class="input" value=${name} onInput=${e => setName(e.target.value)} placeholder="${t("cloud.page.ex_openrouter")}" /></label>`}
      <label class="field"><span>${t("cloud.page.url_de_base")}</span><input class="input mono" value=${endpoint} readonly=${editing} onInput=${e => setEndpoint(e.target.value)} placeholder="https://api.exemple.com/v1" /></label>
      <label class="field"><span>${t("cloud.page.cle_api")}</span><input class="input" type="password" autocomplete="off" value=${key} onInput=${e => setKey(e.target.value)} placeholder=${provider && provider.ready ? t("cloud.page.deja_en_memoire_laisser_vide_pour_la_garder") : nokey ? t("cloud.page.facultative_pour_un_serveur_local") : 'sk-…'} /></label>
      ${!nokey && html`<label class="check"><input type="checkbox" checked=${remember} onChange=${e => setRemember(e.target.checked)} /><span>${t("cloud.page.memoriser_la_cle_dans_le_trousseau_du_systeme")}<${Tip} text="${t("cloud.page.gardee_par_windows_macos_ou_le_trousseau_linux_jamais_dans_les_fi")}" /></span></label>`}
      <label class="check"><input type="checkbox" checked=${usage} onChange=${e => setUsage(e.target.checked)} /><span>${t("cloud.page.demander_le_decompte_des_tokens")}<${Tip} text="${t("cloud.page.a_decocher_si_le_fournisseur_refuse_l_option_stream_options")}" /></span></label>`}
    ${step === 2 && html`<label class="search"><${Icon} n="search" /><input placeholder="${t("cloud.page.filtrer_le_catalogue")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <div class="catalog">${list.map(m => html`<label class=${cls('choice', sel.has(m) && 'on')}><input type="checkbox" checked=${sel.has(m)} onChange=${e => { const n = new Set(sel); e.target.checked ? n.add(m) : n.delete(m); setSel(n); }} /><span class="grow"><b class="mono">${m}</b></span></label>`)}</div>`}
    ${msg && html`<p class="note">${msg}</p>`}
  </${Modal}>`;
}

function Tile({ e, p, onOpen, onConnect }) {
  const state = !p ? 'off' : p.ready ? 'on' : 'warn';
  return html`<div class=${cls('cl-tile', state !== 'off' && 'on')}>
    ${e.custom ? html`<span class="mono-tile">+</span>` : html`<${Logo} id=${e.logo} name=${e.name} />`}
    <span class="grow"><b>${e.name}${state !== 'off' && html`<i class=${cls('dot', state === 'on' ? 'green' : 'hollow')}></i>`}</b><small>${e.hint}</small></span>
    ${state === 'off' ? html`<button class="btn sm" onClick=${onConnect}>${t("cloud.page.connecter")}</button>`
      : html`<button class="btn sm ghost" onClick=${onOpen}>${state === 'on' ? t("cloud.page.gerer") : t("cloud.page.reconnecter")}</button>`}
  </div>`;
}

function ProviderDetail({ p, models, onModels, onForget, onInspect }) {
  const list = models.filter(m => m.provider_id === p.id);
  return html`<div class="insp-body">
    <div class="insp-model"><${Logo} id=${(entryFor(p) || {}).logo} name=${p.name} size="lg" /><div><b>${p.name}</b><span class="mono trunc">${p.endpoint}</span></div></div>
    <div class="card pad-sm">
      <div class="kv"><span>${t("cloud.page.cle_api")}</span><span class="state"><i class=${cls('dot', p.ready && 'green')}></i>${!p.ready ? t("cloud.page.a_reconnecter") : p.remember ? t("cloud.page.memorisee_dans_le_trousseau") : t("cloud.page.en_memoire_jusqu_au_redemarrage")}</span></div>
      <div class="kv"><span>${t("cloud.page.protocole")}</span><span>${t("cloud.page.chat_completions")}</span></div>
      <div class="kv"><span>${t("cloud.page.dans_le_selecteur")}</span><span class="num">${list.filter(m => m.enabled).length} ${t("cloud.page.sur")} ${list.length}</span></div>
    </div>
    <div class="sec-h" style="margin:4px 0 -4px"><h2>${t("cloud.page.modeles")}</h2><button class="btn sm" onClick=${onModels}>${p.ready ? t("cloud.page.choisir_les_modeles") : t("cloud.page.reconnecter")}</button></div>
    <div class="card rows">${list.map(m => html`<div class="row"><span class="grow t mono" ...${inspectTrigger(() => onInspect(m.id), t("cloud.page.inspecter") + m.name)}>${m.name}</span>
      <${Switch} checked=${m.enabled} label=${t("cloud.page.afficher") + m.name + t("cloud.page.dans_le_selecteur_2")} onChange=${v => setVisible(m.id, v)} /></div>`)}</div>
    <button class="btn ghost" style="align-self:flex-start" onClick=${onForget}><${Icon} n="key" />${t("cloud.page.oublier_la_cle")}</button>
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
  const forget = async p => { if (!await confirm(t("cloud.page.oublier_la_cle"), t("cloud.page.la_cle_de") + p.name + t("cloud.page.est_retiree_de_la_memoire_les_modeles_restent_listes"), { ok: t("cloud.page.oublier") })) return; await post('/api/providers/disconnect', { id: p.id }); refreshWorkspace(); };
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
    <${SectionTabs} /><div class="page-head"><div><h1>${t("cloud.page.cloud")}</h1><p>${t("cloud.page.connecte_tes_fournisseurs_d_ia_les_cles_restent_en_memoire_ou_dan")}<${Tip} text="${t("cloud.page.une_discussion_n_envoie_que_du_texte_au_fournisseur_apres_ton_acc")}" /></p></div>
      <div class="acts"><span class="cl-sum"><i class=${cls('dot', ready && 'green')}></i>${ready} ${t("cloud.page.connecte")}${ready > 1 ? 's' : ''}${warn ? ' · ' + warn + t("cloud.page.a_reconnecter_2") : ''}</span></div></div>
    <div class="cl-bar">
      ${[['all', t("cloud.page.tous"), ALL.length], ['on', t("cloud.page.connectes"), ready], ['warn', t("cloud.page.a_reconnecter"), warn]].map(([v, l, n]) => html`<button class="chipf" aria-pressed=${String(filter === v)} onClick=${() => setFilter(v)}>${l}<span>${n}</span></button>`)}
      <span class="grow"></span>
      <label class="search"><${Icon} n="search" /><input placeholder="${t("cloud.page.chercher_un_fournisseur")}" aria-label="${t("cloud.page.chercher_un_fournisseur")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
    </div>
    ${!ws ? html`<div class="skeleton" style="height:160px;margin-top:20px"></div>` : html`
      ${connected.filter(([e, p]) => keep(e, p)).length > 0 && html`<div class="cl-h"><h2>${t("cloud.page.connectes")}</h2><span>${providers.length}</span></div>
        <div class="cl-grid stagger">${connected.filter(([e, p]) => keep(e, p)).map(([e, p]) => tile(e, p))}</div>`}
      ${filter === 'all' && GROUPS.map(g => { const items = g.items.filter(e => !byEntry.has(e) && match(e)); return items.length > 0 && html`<div class="cl-h"><h2>${g.name}</h2><span>${items.length}</span></div>
        <div class="cl-grid">${items.map(e => tile(e, null))}</div>`; })}`}
    ${opened && html`<${Drawer} title=${opened.name} onClose=${() => setOpen(null)}><${ProviderDetail} p=${opened} models=${models} onModels=${() => setDlg({ provider: opened })} onForget=${() => forget(opened)} onInspect=${setSelected} /></${Drawer}>`}
    ${selection && selectionProvider && html`<${Drawer} title=${selection.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selection} provider=${selectionProvider} /></${Drawer}>`}
    ${dlg && html`<${Connect} provider=${dlg.provider} entry=${dlg.entry} onClose=${() => setDlg(null)} />`}
  </div></div>`;
}
