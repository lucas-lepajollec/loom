import { t } from '../../core/i18n.js';
// Cloud : fournisseurs d'API compatibles Chat Completions et modèles proposés
// dans le sélecteur. La clé reste en mémoire côté serveur.
import { Logo } from '../../ui/logo.js';
import { SectionTabs } from '../../app/sections.js';
import { html, useState, useStore, cls } from '../../core/lib.js';
import { Drawer, inspectTrigger } from '../../ui/drawer.js';
import { SelectionInfo } from '../inspector/selection.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Empty, Seg, Tip } from '../../ui/controls.js';
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
    const detected = r.models || [];
    setCatalog([...new Set([...detected, ...sel])]); setStep(2); setMsg(t('models.cloud.detected', { n: detected.length }));
  };
  const save = async () => {
    const models = [...sel];
    if (!models.length || models.length > 32) { setMsg(t("cloud.page.choisis_entre_1_et_32_modeles")); return; }
    setBusy(true);
    const r = await post('/api/providers/save', { id: provider ? provider.id : '', name: name || (preset && preset.name) || t("cloud.page.fournisseur"), endpoint, model: models[0], models, key: key || (nokey && !editing ? 'local' : ''), usage_mode: usage ? '' : 'none', remember: !nokey && remember });
    setBusy(false);
    if (!r.ok) { setMsg(r.error); return; }
    toast(r.warning || t("cloud.page.fournisseur_enregistre"), r.warning ? 'err' : undefined); refreshWorkspace(); onClose();
  };
  const list = catalog.filter(m => !q || m.toLowerCase().includes(q.toLowerCase()));
  return html`<${Modal} wide title=${editing ? t("cloud.page.modeles_de") + provider.name : t("cloud.page.connecter_un_fournisseur")} sub=${step === 0 ? t("cloud.page.choisis_un_fournisseur_compatible_chat_completions") : step === 1 ? t("cloud.page.la_cle_reste_en_memoire_cote_serveur_rien_n_est_ecrit_sur_le_disq") : t("cloud.page.choisis_les_modeles_proposes_dans_tes_discussions")} onClose=${onClose}
      foot=${step === 2 ? html`<span class="grow muted">${t('models.cloud.selected', { n: sel.size })}</span><button class="btn ghost" onClick=${onClose}>${t("cloud.page.annuler")}</button><button class="btn primary" disabled=${busy} onClick=${save}>${t("cloud.page.enregistrer")}</button>`
        : step === 1 ? html`<button class="btn ghost" onClick=${() => editing ? onClose() : setStep(0)}>${editing ? t("cloud.page.annuler") : t("chat.composer.retour")}</button><button class="btn primary" disabled=${busy} onClick=${detect}>${busy ? html`<span class="spinner"></span>` : ''}${t("cloud.page.detecter_les_modeles")}</button>` : ''}>
    ${step === 0 && html`<div class="preset-grid">${ALL.map(p => html`<button class="preset-card" onClick=${() => pick(p)}><b>${p.name}</b><span>${p.hint}</span></button>`)}</div>`}
    ${step === 1 && html`
      ${!editing && html`<label class="field"><span>${t("cloud.page.nom")}</span><input class="input" value=${name} onInput=${e => setName(e.target.value)} placeholder="${t("cloud.page.ex_openrouter")}" /></label>`}
      <label class="field"><span>${t("cloud.page.url_de_base")}</span><input class="input mono" value=${endpoint} readonly=${editing} onInput=${e => setEndpoint(e.target.value)} placeholder="https://api.example.com/v1" /></label>
      <label class="field"><span>${t("cloud.page.cle_api")}</span><input class="input" type="password" autocomplete="off" value=${key} onInput=${e => setKey(e.target.value)} placeholder=${provider && provider.ready ? t("cloud.page.deja_en_memoire_laisser_vide_pour_la_garder") : nokey ? t("cloud.page.facultative_pour_un_serveur_local") : 'sk-…'} /></label>
      ${!nokey && html`<label class="check"><input type="checkbox" checked=${remember} onChange=${e => setRemember(e.target.checked)} /><span>${t("cloud.page.memoriser_la_cle_dans_le_trousseau_du_systeme")}<${Tip} text="${t("cloud.page.gardee_par_windows_macos_ou_le_trousseau_linux_jamais_dans_les_fi")}" /></span></label>`}
      <label class="check"><input type="checkbox" checked=${usage} onChange=${e => setUsage(e.target.checked)} /><span>${t("cloud.page.demander_le_decompte_des_tokens")}<${Tip} text="${t("cloud.page.a_decocher_si_le_fournisseur_refuse_l_option_stream_options")}" /></span></label>`}
    ${step === 2 && html`<label class="search"><${Icon} n="search" /><input aria-label=${t('cloud.page.filtrer_le_catalogue')} placeholder=${t('cloud.page.filtrer_le_catalogue')} value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <div class="catalog">${list.map(m => html`<label class=${cls('choice', sel.has(m) && 'on')}><input type="checkbox" checked=${sel.has(m)} onChange=${e => { const n = new Set(sel); e.target.checked ? n.add(m) : n.delete(m); setSel(n); }} /><span class="grow"><b class="mono">${m}</b></span></label>`)}</div>`}
    ${msg && html`<p class="note">${msg}</p>`}
  </${Modal}>`;
}

function ProviderRow({ e, p, onOpen, onConnect }) {
  const name = p?.name || e.name;
  const state = p ? p.ready ? t('models.cloud.connected') : t('cloud.page.a_reconnecter') : '';
  return html`<div class="models-row">
    ${e.custom ? html`<span class="mono-tile">+</span>` : html`<${Logo} id=${e.logo} name=${e.name} />`}
    <div class="models-row-main">
      <b>${name}</b><small>${[e.hint, state].filter(Boolean).join(' · ')}</small>
    </div>
    ${p && html`<i class=${cls('dot', p.ready ? 'green' : 'amber')} aria-hidden="true"></i>`}
    ${p ? html`<button class="btn sm ghost" onClick=${onOpen}>${p.ready ? t('cloud.page.gerer') : t('cloud.page.reconnecter')}</button>`
      : html`<button class="btn sm" onClick=${onConnect}>${t('cloud.page.connecter')}</button>`}
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
  const forget = async p => {
    if (!await confirm(t('cloud.page.oublier_la_cle'), t('cloud.page.la_cle_de') + p.name + t('cloud.page.est_retiree_de_la_memoire_les_modeles_restent_listes'), { ok: t('cloud.page.oublier') })) return;
    const result = await post('/api/providers/disconnect', { id: p.id });
    if (!result.ok) return toast(result.error, 'err');
    refreshWorkspace();
  };
  // Chaque fournisseur enregistré est rattaché à son entrée de catalogue ; les autres restent « personnalisés ».
  const byEntry = new Map(), custom = [];
  for (const p of providers) { const e = entryFor(p); if (e && !byEntry.has(e)) byEntry.set(e, p); else custom.push(p); }
  const ready = providers.filter(p => p.ready).length, warn = providers.length - ready;
  const query = q.trim().toLowerCase();
  const match = (e, p) => [e.name, e.hint, p?.name, p?.endpoint].some(text => text?.toLowerCase().includes(query));
  const connected = [...byEntry, ...custom.map(p => [{ name: p.name, hint: String(p.endpoint || '').replace(/^https?:\/\//, '') }, p])]
    .filter(([e, p]) => match(e, p) && (filter === 'all' || (filter === 'on' ? p.ready : !p.ready)));
  const groups = filter === 'all' ? GROUPS.map(g => ({ name: g.name, items: g.items.filter(e => !byEntry.has(e) && match(e)) }))
    .filter(g => g.items.length) : [];
  const filters = [
    { value: 'all', label: t('cloud.page.tous') },
    { value: 'on', label: t('cloud.page.connectes'), count: ready },
    { value: 'warn', label: t('cloud.page.a_reconnecter'), count: warn },
  ];
  const opened = providers.find(p => p.id === open);
  const row = (e, p) => html`<${ProviderRow} key=${p?.id || e.name} e=${e} p=${p} onOpen=${() => setOpen(p.id)} onConnect=${() => setDlg({ entry: e })} />`;
  const group = (name, rows) => html`<section class="models-group" key=${name}>
    <div class="models-section-head"><h2>${name}</h2></div><div class="models-card">${rows}</div>
  </section>`;
  const searchLabel = t('cloud.page.chercher_un_fournisseur');
  return html`<div class="view page models-page"><div class="page-in models-layout">
    <${SectionTabs} />
    <h1 class="sr">${t('cloud.page.cloud')}</h1>
    <div class="models-toolbar">
      <label class="search"><${Icon} n="search" /><input placeholder=${searchLabel} aria-label=${searchLabel} value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <${Seg} size="sm" value=${filter} onChange=${setFilter} label=${t('models.cloud.filter')} options=${filters} />
      <${Tip} text=${t('cloud.page.une_discussion_n_envoie_que_du_texte_au_fournisseur_apres_ton_acc')} />
    </div>
    ${!ws ? html`<div class="skeleton models-loading"></div>` : html`
      ${connected.length > 0 && group(t('models.cloud.saved'), connected.map(([e, p]) => row(e, p)))}
      ${groups.map(g => group(g.name, g.items.map(e => row(e, null))))}
      ${!connected.length && !groups.length && html`<div class="models-card"><${Empty} icon="search" title=${t('local.page.aucun_resultat')} text=${t('local.page.essaie_un_autre_filtre')} /></div>`}`}
    ${opened && html`<${Drawer} title=${opened.name} onClose=${() => setOpen(null)}><${ProviderDetail} p=${opened} models=${models} onModels=${() => setDlg({ provider: opened })} onForget=${() => forget(opened)} onInspect=${setSelected} /></${Drawer}>`}
    ${selection && selectionProvider && html`<${Drawer} title=${selection.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selection} provider=${selectionProvider} /></${Drawer}>`}
    ${dlg && html`<${Connect} provider=${dlg.provider} entry=${dlg.entry} onClose=${() => setDlg(null)} />`}
  </div></div>`;
}
