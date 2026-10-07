import { t } from '../../core/i18n.js';
// Environnement : ce qui tourne autour de Loom et qui peut le joindre.
// Services déclarés (avec une matrice « depuis quelle machine ça répond »),
// conteneurs Docker de chaque machine, ressources Proxmox.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { SectionTabs } from '../../app/sections.js';
import { Icon } from '../../ui/icons.js';
import { Tabs, Switch, Tip, Empty } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { go } from '../../core/state.js';

const KINDS = () => ([['web', t("environment.page.site_web")], ['api', 'API'], ['db', t("environment.page.base_de_donnees")], ['other', t("environment.page.autre")]]);
const bytes = b => b == null ? '—' : b >= 1 << 30 ? (b / (1 << 30)).toFixed(1) + t("environment.page.go") : (b / (1 << 20)).toFixed(0) + t("environment.page.mo");
const uptime = s => !s ? '' : s >= 86400 ? Math.floor(s / 86400) + t("environment.page.j") : s >= 3600 ? Math.floor(s / 3600) + ' h' : Math.floor(s / 60) + ' min';

function useMachines() {
  const [m, setM] = useState(null);
  useEffect(() => { get('/api/machines').then(r => setM(r.ok ? r.machines : [])).catch(() => setM([])); }, []);
  return m;
}

function ServiceDialog({ service, machines, onClose }) {
  const [v, setV] = useState(service || { name: '', url: '', machine: 'local', kind: 'web', notes: '' });
  const save = async () => {
    const r = await post('/api/env/services', v);
    if (!r.ok) return toast(r.error || t("environment.page.enregistrement_impossible"), 'err');
    onClose(true);
  };
  return html`<${Modal} title=${service ? t("environment.page.modifier_prefix") + service.name : t("environment.page.declarer_un_service")} sub="${t("environment.page.une_appli_une_api_ou_une_base_qui_tourne_sur_une_de_tes_machines")}" onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>${t("environment.page.annuler")}</button><button class="btn primary" disabled=${!v.name.trim() || !v.url.trim()} onClick=${save}>${t("environment.page.enregistrer")}</button>`}>
    <label class="field"><span>${t("environment.page.nom")}</span><input class="input" value=${v.name} placeholder="${t("environment.page.ex_forgejo")}" onInput=${e => setV({ ...v, name: e.target.value })} /></label>
    <label class="field"><span>${t("environment.page.adresse")}</span><input class="input mono" value=${v.url} placeholder="http://192.168.1.20:3000 ou 192.168.1.20:5432" onInput=${e => setV({ ...v, url: e.target.value })} /></label>
    <div class="mx-fields">
      <label class="field"><span>${t("environment.page.tourne_sur")}</span><select class="select" value=${v.machine} onChange=${e => setV({ ...v, machine: e.target.value })}>
        <option value="local">${t("environment.page.cette_machine")}</option>${(machines || []).map(m => html`<option value=${m.id}>${m.name}</option>`)}</select></label>
      <label class="field"><span>${t("environment.page.type")}</span><select class="select" value=${v.kind} onChange=${e => setV({ ...v, kind: e.target.value })}>${KINDS().map(([k, l]) => html`<option value=${k}>${l}</option>`)}</select></label>
    </div>
    <label class="field"><span>${t("environment.page.notes")}</span><input class="input" value=${v.notes || ''} placeholder="${t("environment.page.facultatif")}" onInput=${e => setV({ ...v, notes: e.target.value })} /></label>
  </${Modal}>`;
}

function Services({ machines }) {
  const [list, setList] = useState(null);
  const [matrix, setMatrix] = useState(null);
  const [busy, setBusy] = useState(false);
  const [dlg, setDlg] = useState(null);
  const load = () => get('/api/env/services').then(r => setList(r.services || [])).catch(() => setList([]));
  useEffect(() => { load(); }, []);
  const check = async () => { setBusy(true); const r = await get('/api/env/matrix').catch(() => null); setBusy(false); setMatrix(r && r.ok ? r.results : []); };
  const del = async s => { if (!await confirm(t("environment.page.retirer") + s.name, t("environment.page.le_service_n_est_plus_suivi_par_loom_rien_n_est_arrete"), { ok: t("environment.page.retirer_2") })) return; await post('/api/env/services/delete', { id: s.id }); load(); };
  const froms = [{ id: 'local', name: 'Loom' }, ...(machines || [])];
  const cell = (s, from) => (matrix || []).find(x => x.service_id === s.id && x.from === from);
  const where = id => id === 'local' ? t("environment.page.cette_machine_2") : ((machines || []).find(m => m.id === id) || {}).name || id;
  if (!list) return html`<div class="skeleton" style="height:160px;margin-top:14px"></div>`;
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">${t("environment.page.les_services_de_tes_machines_et_depuis_ou_ils_repondent_loom_et_c")}</span>
      ${list.length > 0 && html`<button class="btn" disabled=${busy} onClick=${check}><${Icon} n="refresh" />${busy ? t("environment.page.verification") : t("environment.page.verifier_l_acces")}</button>`}
      <button class="btn primary" onClick=${() => setDlg({})}><${Icon} n="plus" />${t("environment.page.declarer_un_service")}</button></div>
    ${!list.length ? html`<div class="card" style="margin-top:14px"><${Empty} icon="globe" title="${t("environment.page.aucun_service")}" text="${t("environment.page.declare_tes_applis_forgejo_une_base_une_api_pour_savoir_d_un_coup")}"><button class="btn primary" onClick=${() => setDlg({})}>${t("environment.page.declarer_un_service")}</button></${Empty}></div>`
      : html`<div class="card env-table" style="margin-top:14px"><table>
        <thead><tr><th>${t("environment.page.service")}</th><th>${t("environment.page.sur")}</th>${matrix && froms.map(f => html`<th class="c">${t("environment.page.depuis")} ${f.name}</th>`)}<th></th></tr></thead>
        <tbody>${list.map(s => html`<tr key=${s.id}>
          <td><b>${s.name}</b><div class="mono muted">${s.url}</div></td>
          <td class="muted">${where(s.machine)}</td>
          ${matrix && froms.map(f => { const c = cell(s, f.id); return html`<td class="c" title=${c && (c.error || (c.status ? 'HTTP ' + c.status : ''))}>${!c ? '—' : c.ok ? html`<span class="state"><i class="dot green"></i>${c.latency_ms} ${t("environment.page.ms")}</span>` : html`<span class="state err"><i class="dot red"></i>${c.status ? c.status : t('common.no_lower')}</span>`}</td>`; })}
          <td class="actions"><button class="btn sm ghost" onClick=${() => setDlg({ service: s })}>${t("environment.page.modifier")}</button><button class="icon-btn" aria-label=${t("environment.page.retirer") + s.name} onClick=${() => del(s)}><${Icon} n="close" /></button></td>
        </tr>`)}</tbody></table></div>`}
    ${dlg && html`<${ServiceDialog} service=${dlg.service} machines=${machines} onClose=${ok => { setDlg(null); if (ok) { load(); setMatrix(null); } }} />`}
  </div>`;
}

function DockerMachine({ machine }) {
  const [x, setX] = useState(null);
  const load = () => get('/api/env/docker?machine=' + encodeURIComponent(machine.id)).then(setX).catch(() => setX({ error: t("environment.page.lecture_impossible") }));
  useEffect(() => { load(); }, [machine.id]);
  const toggle = async on => { const r = await post('/api/env/docker', { machine: machine.id, enabled: on }); if (r.ok === false) return toast(r.error, 'err'); load(); };
  return html`<section class="sec"><div class="sec-h"><h2>${machine.name}</h2>
      <span class="row-inline">${x && x.enabled && html`<button class="btn sm ghost" onClick=${load}><${Icon} n="refresh" />${t("environment.page.relire")}</button>`}
      ${x && html`<${Switch} checked=${!!x.enabled} label=${t("environment.page.docker_sur") + machine.name} onChange=${toggle} />`}</span></div>
    ${!x ? html`<div class="skeleton" style="height:60px"></div>`
      : !x.enabled ? html`<p class="note">${t("environment.page.lecture_des_conteneurs_desactivee_pour_cette_machine")}</p>`
      : x.error ? html`<p class="note err">${x.error}</p>`
      : !(x.containers || []).length ? html`<p class="note">${t("environment.page.aucun_conteneur")}</p>`
      : html`<div class="card env-table"><table><thead><tr><th>${t("environment.page.conteneur")}</th><th>${t("environment.page.image")}</th><th>${t("environment.page.etat")}</th><th>${t("environment.page.ports")}</th></tr></thead>
        <tbody>${x.containers.map(c => html`<tr key=${c.name}><td><b>${c.name}</b></td><td class="mono muted">${c.image}</td>
          <td><span class="state"><i class=${'dot ' + (c.state === 'running' ? 'green' : c.state === 'exited' ? '' : 'amber')}></i>${c.status || c.state}</span></td>
          <td class="mono muted">${c.ports || ''}</td></tr>`)}</tbody></table></div>`}
  </section>`;
}

function Docker({ machines }) {
  if (!machines) return html`<div class="skeleton" style="height:160px;margin-top:14px"></div>`;
  return html`<div><p class="muted" style="font-size:13.5px;margin:4px 0 0">${t("environment.page.les_conteneurs_docker_de_chaque_machine_lus_avec")} <code>${t("environment.page.docker_ps")}</code> ${t("environment.page.sur_les_machines_connectees_en_ssh_active_la_lecture_machine_par")}</p>
    ${[{ id: 'local', name: t("environment.page.cette_machine") }, ...machines].map(m => html`<${DockerMachine} key=${m.id} machine=${m} />`)}</div>`;
}

function ProxmoxForm({ cfg, onSaved }) {
  const [v, setV] = useState({ url: cfg.url || '', token_id: cfg.token_id || '', token_secret: '', fingerprint: cfg.fingerprint || '' });
  const [probe, setProbe] = useState(null);
  const [busy, setBusy] = useState(false);
  const detect = async () => {
    const r = await post('/api/env/proxmox/fingerprint', { url: v.url });
    if (!r.ok) return toast(r.error || t("environment.page.proxmox_injoignable"), 'err');
    setProbe(r);
    if (r.trusted) setV({ ...v, fingerprint: '' });
  };
  const save = async () => {
    const pin = probe && !probe.trusted ? probe.fingerprint : v.fingerprint;
    if (pin && pin !== cfg.fingerprint && !await confirm(t("environment.page.faire_confiance_a_ce_certificat"), t("environment.page.verifie_que_cette_empreinte_est_celle_affichee_par_proxmox_datace") + pin, { ok: t("environment.page.elle_correspond") })) return;
    setBusy(true);
    const r = await post('/api/env/proxmox', { enabled: true, url: v.url, token_id: v.token_id, token_secret: v.token_secret, fingerprint: pin, confirm_fingerprint: !!pin });
    setBusy(false);
    if (r.ok === false) return toast(r.error || t("environment.page.enregistrement_impossible"), 'err');
    toast(t("environment.page.proxmox_connecte")); onSaved();
  };
  return html`<div class="card pad env-form">
    <label class="field"><span>${t("environment.page.adresse_de_proxmox")}</span><div class="row-inline"><input class="input mono" style="flex:1" value=${v.url} placeholder="https://192.168.1.10:8006" onInput=${e => { setV({ ...v, url: e.target.value }); setProbe(null); }} /><button class="btn sm" disabled=${!v.url.startsWith('https://')} onClick=${detect}>${t("environment.page.verifier_le_certificat")}</button></div></label>
    ${probe && html`<p class=${'note' + (probe.trusted ? '' : ' warn')}>${probe.trusted ? t("environment.page.certificat_reconnu_par_le_systeme") : html`${t("environment.page.certificat_auto_signe")}${probe.subject}${t("environment.page.empreinte")} <code class="mono">${probe.fingerprint}</code>`}</p>`}
    <div class="mx-fields">
      <label class="field"><span>${t("environment.page.identifiant_du_jeton")}<${Tip} text="${t("environment.page.dans_proxmox_datacenter_permissions_jetons_api_format_utilisateur")}" /></span><input class="input mono" value=${v.token_id} placeholder="${t("environment.page.loom_pve_lecture")}" onInput=${e => setV({ ...v, token_id: e.target.value })} /></label>
      <label class="field"><span>${t("environment.page.secret_du_jeton")}<${Tip} text="${t("environment.page.garde_dans_le_trousseau_du_systeme_jamais_dans_les_fichiers_de_lo")}" /></span><input class="input mono" type="password" value=${v.token_secret} placeholder=${cfg.token_id ? t("environment.page.inchange") : ''} onInput=${e => setV({ ...v, token_secret: e.target.value })} /></label>
    </div>
    <div class="form-foot"><span class="grow"></span><button class="btn primary" disabled=${busy || !v.url || !v.token_id || (!v.token_secret && !cfg.token_id)} onClick=${save}>${busy ? t("environment.page.connexion") : t("environment.page.enregistrer")}</button></div>
  </div>`;
}

function Proxmox() {
  const [cfg, setCfg] = useState(null);
  const [res, setRes] = useState(null);
  const [edit, setEdit] = useState(false);
  const loadRes = () => get('/api/env/proxmox/resources').then(r => setRes(r)).catch(e => setRes({ error: e.message }));
  const load = () => get('/api/env/proxmox').then(r => { setCfg(r.config || {}); if (r.config && r.config.enabled) loadRes(); }).catch(() => setCfg({}));
  useEffect(() => { load(); }, []);
  if (!cfg) return html`<div class="skeleton" style="height:160px;margin-top:14px"></div>`;
  const on = cfg.enabled && !edit;
  const items = (res && res.resources) || [];
  const nodes = items.filter(x => x.type === 'node'), guests = items.filter(x => x.type !== 'node').sort((a, b) => (a.vmid || 0) - (b.vmid || 0));
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">${t("environment.page.les_n_uds_vm_et_conteneurs_de_ton_proxmox_lus_avec_un_jeton_api_e")}</span>
      ${on && html`<button class="btn" onClick=${loadRes}><${Icon} n="refresh" />${t("environment.page.relire")}</button><button class="btn ghost" onClick=${() => setEdit(true)}>${t("environment.page.modifier_la_connexion")}</button>`}</div>
    ${!on ? html`<${ProxmoxForm} cfg=${cfg} onSaved=${() => { setEdit(false); load(); }} />`
      : !res ? html`<div class="skeleton" style="height:120px;margin-top:14px"></div>`
      : res.error || res.ok === false ? html`<p class="note err">${res.error}</p>`
      : html`<div class="env-nodes">${nodes.map(n => html`<div class="card pad env-node" key=${n.name}><span class="state"><i class=${'dot ' + (n.status === 'online' ? 'green' : 'red')}></i><b>${n.name}</b></span>
            <small class="muted">${n.cpu != null ? Math.round(n.cpu * 100) + ' % CPU · ' : ''}${bytes(n.mem)} / ${bytes(n.maxmem)}${n.uptime ? ' · ' + uptime(n.uptime) : ''}</small></div>`)}</div>
        <div class="card env-table" style="margin-top:12px"><table><thead><tr><th>ID</th><th>${t("environment.page.nom")}</th><th>${t("environment.page.type")}</th><th>${t("environment.page.n_ud")}</th><th>${t("environment.page.etat")}</th><th>${t("environment.page.memoire")}</th></tr></thead>
          <tbody>${guests.map(g => html`<tr key=${g.vmid}><td class="mono">${g.vmid}</td><td><b>${g.name}</b></td><td class="muted">${g.type === 'lxc' ? t("environment.page.conteneur") : 'VM'}</td><td class="muted">${g.node}</td>
            <td><span class="state"><i class=${'dot ' + (g.status === 'running' ? 'green' : '')}></i>${g.status === 'running' ? t("environment.page.allume") : g.status === 'stopped' ? t("environment.page.eteint") : g.status}</span></td>
            <td class="muted">${g.status === 'running' ? bytes(g.mem) + ' / ' : ''}${bytes(g.maxmem)}</td></tr>`)}</tbody></table></div>`}
  </div>`;
}

export function EnvironmentPage({ route }) {
  const tab = ['docker', 'proxmox'].includes(route.sub) ? route.sub : 'services';
  const machines = useMachines();
  return html`<div class="view page"><div class="page-in wide">
    <${SectionTabs} /><div class="page-head"><div><h1>${t("environment.page.environnement")}</h1><p>${t("environment.page.ce_qui_tourne_autour_de_loom_sur_tes_machines_et_qui_peut_le_join")}</p></div>
      <div class="acts"><a class="btn" href="#/machines"><${Icon} n="server" />${t("environment.page.machines")}</a></div></div>
    <${Tabs} value=${tab} onChange=${localT => go('environment', localT)} label="${t("environment.page.environnement")}" options=${[{ value: 'services', label: t("environment.page.services") }, { value: 'docker', label: t("environment.page.docker") }, { value: 'proxmox', label: t("environment.page.proxmox") }]} />
    <div class="tab-body" key=${tab}>${tab === 'services' ? html`<${Services} machines=${machines} />` : tab === 'docker' ? html`<${Docker} machines=${machines} />` : html`<${Proxmox} />`}</div>
  </div></div>`;
}
