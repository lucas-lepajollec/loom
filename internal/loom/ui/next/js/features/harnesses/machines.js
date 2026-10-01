// Machines distantes : un autre ordinateur joignable en SSH où tournent des
// harnesses (ex. Hermes dans un conteneur). L'utilisateur colle un bloc sur la
// machine ; il autorise la clé de Loom et affiche une ligne LOOM-MACHINE que
// Loom lit pour remplir le formulaire. Loom vérifie ensuite la connexion et
// propose les harnesses trouvés là-bas.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { go, refreshWorkspace } from '../../core/state.js';

export async function copyText(text) {
  try { await navigator.clipboard.writeText(text); return true; } catch (_) {}
  // Adresse http sur le réseau local : l'API presse-papiers est refusée.
  const ta = document.createElement('textarea');
  ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
  document.body.appendChild(ta); ta.select();
  let ok = false;
  try { ok = document.execCommand('copy'); } catch (_) {}
  ta.remove();
  return ok;
}

// Ligne « LOOM-MACHINE {…} » collée depuis le terminal de la machine.
export function parseMachineLine(text) {
  const i = String(text || '').indexOf('LOOM-MACHINE ');
  if (i < 0) return null;
  const rest = text.slice(i + 13);
  const end = rest.lastIndexOf('}');
  try { return JSON.parse(rest.slice(0, end + 1)); } catch (_) { return null; }
}

export function MachineDialog({ machine, onClose }) {
  const [setup, setSetup] = useState('');
  const [paste, setPaste] = useState('');
  const [v, setV] = useState(machine ? { id: machine.id, name: machine.name, host: machine.host, user: machine.user, port: String(machine.port || 22) } : { name: '', host: '', user: '', port: '22' });
  const [check, setCheck] = useState(null);
  const [chosen, setChosen] = useState(machine ? machine.harnesses.map(id => id.slice(('custom-' + machine.id + '-').length)) : []);
  const [busy, setBusy] = useState('');
  useEffect(() => { get('/api/machines').then(r => r.ok && setSetup(r.setup)); }, []);
  const set = patch => { setV({ ...v, ...patch }); setCheck(null); };
  const onPaste = text => {
    setPaste(text);
    const info = parseMachineLine(text);
    if (!info) return;
    set({ host: info.host || v.host, user: info.user || v.user, name: v.name || info.hostname || info.host || '' });
  };
  const body = () => ({ machine: { id: v.id || '', name: v.name.trim(), host: v.host.trim(), user: v.user.trim(), port: Number(v.port) || 22 } });
  const test = async () => {
    setBusy('check');
    const r = await post('/api/machines', { ...body(), check_only: true }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) { setCheck({ error: r.error || 'Connexion impossible' }); return; }
    setCheck(r);
    if (!machine) setChosen(r.offers.filter(o => o.ready).map(o => o.id));
  };
  const save = async () => {
    setBusy('save');
    const r = await post('/api/machines', { ...body(), harnesses: chosen }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || 'Enregistrement impossible', 'err');
    toast(r.machine.name + ' connectée'); await refreshWorkspace(); onClose(r.machine);
  };
  const toggle = id => setChosen(chosen.includes(id) ? chosen.filter(x => x !== id) : [...chosen, id]);
  const ready = v.host.trim() && v.user.trim();
  const offers = check && check.offers ? check.offers.filter(o => o.installed) : [];
  return html`<${Modal} wide title=${machine ? 'Modifier ' + machine.name : 'Connecter une machine'} sub="Un autre ordinateur où tournent des harnesses, joint en SSH." onClose=${() => onClose()}
      foot=${html`<button class="btn ghost" onClick=${() => onClose()}>Annuler</button>
        ${check && check.ok ? html`<button class="btn primary" disabled=${!!busy || !chosen.length} onClick=${save}>${busy === 'save' ? 'Connexion…' : 'Ajouter ' + (chosen.length > 1 ? chosen.length + ' harnesses' : 'le harness')}</button>`
          : html`<button class="btn primary" disabled=${!!busy || !ready} onClick=${test}>${busy === 'check' ? 'Test…' : 'Tester la connexion'}</button>`}`}>
    <div class="mx-steps">
      <div class="mx-step"><span class="mx-n">1</span><div class="mx-body">
        <b>Sur la machine distante, colle ce bloc dans un terminal</b>
        <p class="note">Avec l’utilisateur qui fait tourner tes agents (SSH, console Proxmox, écran…). Il autorise la clé de Loom, n’installe rien d’autre, puis affiche une ligne <code>LOOM-MACHINE</code>.</p>
        <div class="mx-code"><pre>${setup || '…'}</pre><button class="btn sm" disabled=${!setup} onClick=${async () => { const ok = await copyText(setup); toast(ok ? 'Bloc copié' : 'Copie refusée : sélectionne le texte', ok ? '' : 'err'); }}><${Icon} n="copy" />Copier</button></div>
      </div></div>
      <div class="mx-step"><span class="mx-n">2</span><div class="mx-body">
        <b>Colle ici la ligne affichée</b>
        <textarea class="input mono mx-paste" rows="2" placeholder="LOOM-MACHINE {…}" value=${paste} onInput=${e => onPaste(e.target.value)}></textarea>
        ${paste && !parseMachineLine(paste) && html`<p class="note warn">Ligne LOOM-MACHINE introuvable : copie toute la dernière ligne.</p>`}
        <div class="mx-fields">
          <label class="field"><span>Nom</span><input class="input" value=${v.name} placeholder="ex. hermes-agent" onInput=${e => set({ name: e.target.value })} /></label>
          <label class="field"><span>Adresse<${Tip} text="IP ou nom de la machine sur ton réseau. La ligne collée donne son IP principale ; change-la si Loom la joint autrement (VPN, nom DNS)." /></span><input class="input mono" value=${v.host} placeholder="192.168.1.20" onInput=${e => set({ host: e.target.value })} /></label>
          <label class="field"><span>Utilisateur</span><input class="input mono" value=${v.user} placeholder="moi" onInput=${e => set({ user: e.target.value })} /></label>
          <label class="field mx-port"><span>Port SSH</span><input class="input mono" inputmode="numeric" value=${v.port} onInput=${e => set({ port: e.target.value.replace(/\D/g, '') })} /></label>
        </div>
      </div></div>
      ${check && html`<div class="mx-step"><span class="mx-n">3</span><div class="mx-body">
        ${check.error ? html`<b>Connexion impossible</b><p class="note err">${check.error}</p>`
          : html`<b>Connectée à ${check.machine.hostname || check.machine.host}<span class="muted"> · ${check.machine.os} · ${check.machine.home}</span></b>
            ${offers.length ? html`<div class="mx-offers">${offers.map(o => html`<label class=${cls('mx-offer', !o.ready && 'off')} key=${o.id}>
                <input type="checkbox" disabled=${!o.ready} checked=${chosen.includes(o.id)} onChange=${() => toggle(o.id)} />
                <${Logo} name=${o.logo} /><span class="grow"><b>${o.name}</b><small>${o.ready ? o.version || 'installé' : o.missing}</small></span></label>`)}</div>`
              : html`<p class="note">Aucun harness pris en charge trouvé sur cette machine (Hermes, Claude Code, Codex, Pi, Gemini, OpenCode). Installe-le là-bas puis teste à nouveau.</p>`}`}
      </div></div>`}
    </div>
  </${Modal}>`;
}

const NAMES = { hermes: 'Hermes', 'claude-code': 'Claude Code', codex: 'Codex', pi: 'Pi', gemini: 'Gemini', opencode: 'OpenCode' };

export function MachinesSection({ onEdit }) {
  const [data, setData] = useState(null);
  const load = () => get('/api/machines').then(r => setData(r.ok ? r : { machines: [] }), () => setData({ machines: [] }));
  useEffect(() => { load(); }, []);
  MachinesSection.reload = load;
  const remove = async m => {
    if (!await confirm('Retirer ' + m.name, 'Ses harnesses disparaissent de Loom. Les discussions déjà faites restent ; rien n’est modifié sur la machine (la clé de Loom y reste autorisée tant que tu ne la retires pas).', { ok: 'Retirer', danger: true })) return;
    const r = await post('/api/machines/delete', { id: m.id });
    if (!r.ok) return toast(r.error || 'Suppression impossible', 'err');
    await refreshWorkspace(); load();
  };
  if (!data || !data.machines.length) return null;
  return html`<section class="sec"><div class="sec-h"><h2>Machines distantes <span class="count">${data.machines.length}</span><${Tip} text="Loom s’y connecte en SSH avec sa propre clé et lance les harnesses là-bas. Les fichiers restent sur la machine distante." /></h2></div>
    <div class="card">${data.machines.map(m => html`<div class="mx-row" key=${m.id}>
      <span class="mx-ico"><${Icon} n="server" /></span>
      <span class="grow"><b>${m.name}</b><small class="mono">${m.user}@${m.host}${m.port !== 22 ? ':' + m.port : ''}</small></span>
      <span class="mx-hs">${(m.harnesses || []).map(id => { const h = id.slice(('custom-' + m.id + '-').length); return html`<button class="chip-btn" key=${id} onClick=${() => go('harnesses', id)}><${Logo} name=${h} />${NAMES[h] || h}</button>`; })}</span>
      <button class="btn sm ghost" onClick=${() => onEdit(m)}>Modifier</button>
      <button class="icon-btn" aria-label=${'Retirer ' + m.name} onClick=${() => remove(m)}><${Icon} n="trash" /></button>
    </div>`)}</div></section>`;
}
