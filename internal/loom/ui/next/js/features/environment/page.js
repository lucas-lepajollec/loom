// Environnement : ce qui tourne autour de Loom et qui peut le joindre.
// Services déclarés (avec une matrice « depuis quelle machine ça répond »),
// conteneurs Docker de chaque machine, ressources Proxmox.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Tabs, Switch, Tip, Empty } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { go } from '../../core/state.js';

const KINDS = [['web', 'Site web'], ['api', 'API'], ['db', 'Base de données'], ['other', 'Autre']];
const bytes = b => b == null ? '—' : b >= 1 << 30 ? (b / (1 << 30)).toFixed(1) + ' Go' : (b / (1 << 20)).toFixed(0) + ' Mo';
const uptime = s => !s ? '' : s >= 86400 ? Math.floor(s / 86400) + ' j' : s >= 3600 ? Math.floor(s / 3600) + ' h' : Math.floor(s / 60) + ' min';

function useMachines() {
  const [m, setM] = useState(null);
  useEffect(() => { get('/api/machines').then(r => setM(r.ok ? r.machines : [])).catch(() => setM([])); }, []);
  return m;
}

function ServiceDialog({ service, machines, onClose }) {
  const [v, setV] = useState(service || { name: '', url: '', machine: 'local', kind: 'web', notes: '' });
  const save = async () => {
    const r = await post('/api/env/services', v);
    if (!r.ok) return toast(r.error || 'Enregistrement impossible', 'err');
    onClose(true);
  };
  return html`<${Modal} title=${service ? 'Modifier ' + service.name : 'Déclarer un service'} sub="Une appli, une API ou une base qui tourne sur une de tes machines." onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>Annuler</button><button class="btn primary" disabled=${!v.name.trim() || !v.url.trim()} onClick=${save}>Enregistrer</button>`}>
    <label class="field"><span>Nom</span><input class="input" value=${v.name} placeholder="ex. Forgejo" onInput=${e => setV({ ...v, name: e.target.value })} /></label>
    <label class="field"><span>Adresse</span><input class="input mono" value=${v.url} placeholder="http://192.168.1.20:3000 ou 192.168.1.20:5432" onInput=${e => setV({ ...v, url: e.target.value })} /></label>
    <div class="mx-fields">
      <label class="field"><span>Tourne sur</span><select class="select" value=${v.machine} onChange=${e => setV({ ...v, machine: e.target.value })}>
        <option value="local">Cette machine</option>${(machines || []).map(m => html`<option value=${m.id}>${m.name}</option>`)}</select></label>
      <label class="field"><span>Type</span><select class="select" value=${v.kind} onChange=${e => setV({ ...v, kind: e.target.value })}>${KINDS.map(([k, l]) => html`<option value=${k}>${l}</option>`)}</select></label>
    </div>
    <label class="field"><span>Notes</span><input class="input" value=${v.notes || ''} placeholder="facultatif" onInput=${e => setV({ ...v, notes: e.target.value })} /></label>
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
  const del = async s => { if (!await confirm('Retirer ' + s.name, 'Le service n’est plus suivi par Loom. Rien n’est arrêté.', { ok: 'Retirer' })) return; await post('/api/env/services/delete', { id: s.id }); load(); };
  const froms = [{ id: 'local', name: 'Loom' }, ...(machines || [])];
  const cell = (s, from) => (matrix || []).find(x => x.service_id === s.id && x.from === from);
  const where = id => id === 'local' ? 'cette machine' : ((machines || []).find(m => m.id === id) || {}).name || id;
  if (!list) return html`<div class="skeleton" style="height:160px;margin-top:14px"></div>`;
  return html`<div>
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">Les services de tes machines, et depuis où ils répondent : Loom, et chaque machine connectée (là où tournent tes harnesses).</span>
      ${list.length > 0 && html`<button class="btn" disabled=${busy} onClick=${check}><${Icon} n="refresh" />${busy ? 'Vérification…' : 'Vérifier l’accès'}</button>`}
      <button class="btn primary" onClick=${() => setDlg({})}><${Icon} n="plus" />Déclarer un service</button></div>
    ${!list.length ? html`<div class="card" style="margin-top:14px"><${Empty} icon="globe" title="Aucun service" text="Déclare tes applis (Forgejo, une base, une API…) pour savoir d’un coup d’œil si elles répondent, et depuis quelle machine."><button class="btn primary" onClick=${() => setDlg({})}>Déclarer un service</button></${Empty}></div>`
      : html`<div class="card env-table" style="margin-top:14px"><table>
        <thead><tr><th>Service</th><th>Sur</th>${matrix && froms.map(f => html`<th class="c">depuis ${f.name}</th>`)}<th></th></tr></thead>
        <tbody>${list.map(s => html`<tr key=${s.id}>
          <td><b>${s.name}</b><div class="mono muted">${s.url}</div></td>
          <td class="muted">${where(s.machine)}</td>
          ${matrix && froms.map(f => { const c = cell(s, f.id); return html`<td class="c" title=${c && (c.error || (c.status ? 'HTTP ' + c.status : ''))}>${!c ? '—' : c.ok ? html`<span class="state"><i class="dot green"></i>${c.latency_ms} ms</span>` : html`<span class="state err"><i class="dot red"></i>${c.status ? c.status : 'non'}</span>`}</td>`; })}
          <td class="actions"><button class="btn sm ghost" onClick=${() => setDlg({ service: s })}>Modifier</button><button class="icon-btn" aria-label=${'Retirer ' + s.name} onClick=${() => del(s)}><${Icon} n="close" /></button></td>
        </tr>`)}</tbody></table></div>`}
    ${dlg && html`<${ServiceDialog} service=${dlg.service} machines=${machines} onClose=${ok => { setDlg(null); if (ok) { load(); setMatrix(null); } }} />`}
  </div>`;
}

function DockerMachine({ machine }) {
  const [x, setX] = useState(null);
  const load = () => get('/api/env/docker?machine=' + encodeURIComponent(machine.id)).then(setX).catch(() => setX({ error: 'lecture impossible' }));
  useEffect(() => { load(); }, [machine.id]);
  const toggle = async on => { const r = await post('/api/env/docker', { machine: machine.id, enabled: on }); if (r.ok === false) return toast(r.error, 'err'); load(); };
  return html`<section class="sec"><div class="sec-h"><h2>${machine.name}</h2>
      <span class="row-inline">${x && x.enabled && html`<button class="btn sm ghost" onClick=${load}><${Icon} n="refresh" />Relire</button>`}
      ${x && html`<${Switch} checked=${!!x.enabled} label=${'Docker sur ' + machine.name} onChange=${toggle} />`}</span></div>
    ${!x ? html`<div class="skeleton" style="height:60px"></div>`
      : !x.enabled ? html`<p class="note">Lecture des conteneurs désactivée pour cette machine.</p>`
      : x.error ? html`<p class="note err">${x.error}</p>`
      : !(x.containers || []).length ? html`<p class="note">Aucun conteneur.</p>`
      : html`<div class="card env-table"><table><thead><tr><th>Conteneur</th><th>Image</th><th>État</th><th>Ports</th></tr></thead>
        <tbody>${x.containers.map(c => html`<tr key=${c.name}><td><b>${c.name}</b></td><td class="mono muted">${c.image}</td>
          <td><span class="state"><i class=${'dot ' + (c.state === 'running' ? 'green' : c.state === 'exited' ? '' : 'amber')}></i>${c.status || c.state}</span></td>
          <td class="mono muted">${c.ports || ''}</td></tr>`)}</tbody></table></div>`}
  </section>`;
}

function Docker({ machines }) {
  if (!machines) return html`<div class="skeleton" style="height:160px;margin-top:14px"></div>`;
  return html`<div><p class="muted" style="font-size:13.5px;margin:4px 0 0">Les conteneurs Docker de chaque machine, lus avec <code>docker ps</code> (sur les machines connectées, en SSH). Active la lecture machine par machine.</p>
    ${[{ id: 'local', name: 'Cette machine' }, ...machines].map(m => html`<${DockerMachine} key=${m.id} machine=${m} />`)}</div>`;
}

function ProxmoxForm({ cfg, onSaved }) {
  const [v, setV] = useState({ url: cfg.url || '', token_id: cfg.token_id || '', token_secret: '', fingerprint: cfg.fingerprint || '' });
  const [probe, setProbe] = useState(null);
  const [busy, setBusy] = useState(false);
  const detect = async () => {
    const r = await post('/api/env/proxmox/fingerprint', { url: v.url });
    if (!r.ok) return toast(r.error || 'Proxmox injoignable', 'err');
    setProbe(r);
    if (r.trusted) setV({ ...v, fingerprint: '' });
  };
  const save = async () => {
    const pin = probe && !probe.trusted ? probe.fingerprint : v.fingerprint;
    if (pin && pin !== cfg.fingerprint && !await confirm('Faire confiance à ce certificat', 'Vérifie que cette empreinte est celle affichée par Proxmox (Datacenter › nœud › Système › Certificats) : ' + pin, { ok: 'Elle correspond' })) return;
    setBusy(true);
    const r = await post('/api/env/proxmox', { enabled: true, url: v.url, token_id: v.token_id, token_secret: v.token_secret, fingerprint: pin, confirm_fingerprint: !!pin });
    setBusy(false);
    if (r.ok === false) return toast(r.error || 'Enregistrement impossible', 'err');
    toast('Proxmox connecté'); onSaved();
  };
  return html`<div class="card pad env-form">
    <label class="field"><span>Adresse de Proxmox</span><div class="row-inline"><input class="input mono" style="flex:1" value=${v.url} placeholder="https://192.168.1.10:8006" onInput=${e => { setV({ ...v, url: e.target.value }); setProbe(null); }} /><button class="btn sm" disabled=${!v.url.startsWith('https://')} onClick=${detect}>Vérifier le certificat</button></div></label>
    ${probe && html`<p class=${'note' + (probe.trusted ? '' : ' warn')}>${probe.trusted ? 'Certificat reconnu par le système.' : html`Certificat auto-signé (${probe.subject}). Empreinte : <code class="mono">${probe.fingerprint}</code>`}</p>`}
    <div class="mx-fields">
      <label class="field"><span>Identifiant du jeton<${Tip} text="Dans Proxmox : Datacenter › Permissions › Jetons API. Format utilisateur@realm!nom. Un rôle en lecture seule (PVEAuditor) suffit." /></span><input class="input mono" value=${v.token_id} placeholder="loom@pve!lecture" onInput=${e => setV({ ...v, token_id: e.target.value })} /></label>
      <label class="field"><span>Secret du jeton<${Tip} text="Gardé dans le trousseau du système, jamais dans les fichiers de Loom. Vide : garde le secret actuel." /></span><input class="input mono" type="password" value=${v.token_secret} placeholder=${cfg.token_id ? '(inchangé)' : ''} onInput=${e => setV({ ...v, token_secret: e.target.value })} /></label>
    </div>
    <div class="form-foot"><span class="grow"></span><button class="btn primary" disabled=${busy || !v.url || !v.token_id || (!v.token_secret && !cfg.token_id)} onClick=${save}>${busy ? 'Connexion…' : 'Enregistrer'}</button></div>
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
    <div class="toolbar"><span class="grow muted" style="font-size:13.5px">Les nœuds, VM et conteneurs de ton Proxmox, lus avec un jeton API en lecture seule.</span>
      ${on && html`<button class="btn" onClick=${loadRes}><${Icon} n="refresh" />Relire</button><button class="btn ghost" onClick=${() => setEdit(true)}>Modifier la connexion</button>`}</div>
    ${!on ? html`<${ProxmoxForm} cfg=${cfg} onSaved=${() => { setEdit(false); load(); }} />`
      : !res ? html`<div class="skeleton" style="height:120px;margin-top:14px"></div>`
      : res.error || res.ok === false ? html`<p class="note err">${res.error}</p>`
      : html`<div class="env-nodes">${nodes.map(n => html`<div class="card pad env-node" key=${n.name}><span class="state"><i class=${'dot ' + (n.status === 'online' ? 'green' : 'red')}></i><b>${n.name}</b></span>
            <small class="muted">${n.cpu != null ? Math.round(n.cpu * 100) + ' % CPU · ' : ''}${bytes(n.mem)} / ${bytes(n.maxmem)}${n.uptime ? ' · ' + uptime(n.uptime) : ''}</small></div>`)}</div>
        <div class="card env-table" style="margin-top:12px"><table><thead><tr><th>ID</th><th>Nom</th><th>Type</th><th>Nœud</th><th>État</th><th>Mémoire</th></tr></thead>
          <tbody>${guests.map(g => html`<tr key=${g.vmid}><td class="mono">${g.vmid}</td><td><b>${g.name}</b></td><td class="muted">${g.type === 'lxc' ? 'Conteneur' : 'VM'}</td><td class="muted">${g.node}</td>
            <td><span class="state"><i class=${'dot ' + (g.status === 'running' ? 'green' : '')}></i>${g.status === 'running' ? 'allumé' : g.status === 'stopped' ? 'éteint' : g.status}</span></td>
            <td class="muted">${g.status === 'running' ? bytes(g.mem) + ' / ' : ''}${bytes(g.maxmem)}</td></tr>`)}</tbody></table></div>`}
  </div>`;
}

export function EnvironmentPage({ route }) {
  const tab = ['docker', 'proxmox'].includes(route.sub) ? route.sub : 'services';
  const machines = useMachines();
  return html`<div class="view page"><div class="page-in wide">
    <div class="page-head"><div><h1>Environnement</h1><p>Ce qui tourne autour de Loom, sur tes machines, et qui peut le joindre.</p></div>
      <div class="acts"><a class="btn" href="#/settings/machines"><${Icon} n="server" />Machines</a></div></div>
    <${Tabs} value=${tab} onChange=${t => go('environment', t)} label="Environnement" options=${[{ value: 'services', label: 'Services' }, { value: 'docker', label: 'Docker' }, { value: 'proxmox', label: 'Proxmox' }]} />
    <div class="tab-body" key=${tab}>${tab === 'services' ? html`<${Services} machines=${machines} />` : tab === 'docker' ? html`<${Docker} machines=${machines} />` : html`<${Proxmox} />`}</div>
  </div></div>`;
}
