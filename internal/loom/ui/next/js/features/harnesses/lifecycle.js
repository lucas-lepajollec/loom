// Version et mises à jour d'un harness, sur cette machine ou une machine
// connectée : version installée et dernière publiée, installer, mettre à
// jour, mise à jour automatique (jamais pendant une discussion de ce harness).
import { html, useState, useEffect } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { refreshWorkspace } from '../../core/state.js';

const when = t => t ? new Date(t).toLocaleString('fr-FR', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '';
// « codex-cli 0.159.2 », « 2.1.287 (Claude Code) », « Hermes Agent v0.21.3 (…) » → le numéro.
const clean = v => { const m = String(v || '').match(/v?\d+(?:\.\d+)+[\w.+-]*/); return m ? m[0] : String(v || '').trim(); };

export function Lifecycle({ target, id, name, where, onChange, compact }) {
  const [x, setX] = useState(null);
  const [busy, setBusy] = useState('');
  const [log, setLog] = useState(null);
  const load = () => get('/api/harness/lifecycle?target=' + encodeURIComponent(target) + '&id=' + encodeURIComponent(id))
    .then(r => setX(r.state || false)).catch(() => setX(false));
  useEffect(() => { setX(null); load(); }, [target, id]);
  const run = async action => {
    const verb = action === 'install' ? 'Installer' : 'Mettre à jour';
    if (!await confirm(verb + ' ' + name, (action === 'install' ? 'Loom lance l’installation officielle de ' : 'Loom lance la mise à jour de ') + name + ' ' + where + '. Cela peut prendre quelques minutes.' + (action === 'update' ? ' Les discussions en cours avec ce harness peuvent être interrompues.' : ''), { ok: verb })) return;
    setBusy(action);
    const r = await post('/api/harness/lifecycle', { target, id, action }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    setLog({ ok: r.ok, text: r.log || r.error || '' });
    if (r.state) setX(r.state); else load();
    if (r.ok) { toast(name + (action === 'install' ? ' installé' : ' mis à jour')); refreshWorkspace(); onChange && onChange(); }
    else toast(r.error || 'Échec', 'err');
  };
  const auto = async on => {
    const r = await post('/api/harness/lifecycle/auto', { target, id, auto: on });
    if (!r.ok) return toast(r.error || 'Réglage impossible', 'err');
    setX({ ...x, auto: r.auto, last_auto: r.last_auto });
  };
  if (x === false) return null;
  if (!x) return compact ? html`<span class="spinner"></span>` : html`<div class="kv"><span>Version</span><span class="state"><span class="spinner"></span>Vérification…</span></div>`;
  const missing = (x.requires_missing || []).length > 0;
  // Ligne compacte d'un harness absent : juste de quoi l'installer.
  if (compact && !x.installed) return missing ? html`<span class="state err">Manque ${x.requires_missing.join(', ')}</span>`
    : html`<span class="muted">${x.latest ? 'v' + String(x.latest).replace(/^v/, '') : ''}</span><button class="btn sm" disabled=${!!busy} onClick=${() => run('install')}>${busy ? html`<span class="spinner"></span>Installation…` : html`<${Icon} n="download" />Installer`}</button>`;
  const la = x.last_auto;
  return html`<div class="lc">
    <div class="kv"><span>Version</span><span class="num">${x.installed ? clean(x.version) || 'inconnue' : 'non installé'}${x.latest && html`<span class="muted"> · dernière ${x.latest}</span>`}</span></div>
    ${x.installed ? html`<div class="kv"><span>Mise à jour</span><span>${x.update_available
        ? html`<button class="btn sm" disabled=${!!busy} onClick=${() => run('update')}>${busy ? html`<span class="spinner"></span>Mise à jour…` : html`<${Icon} n="download" />Mettre à jour vers ${x.latest}`}</button>`
        : html`<span class="state"><i class="dot green"></i>à jour</span><button class="btn sm ghost" disabled=${!!busy} onClick=${() => run('update')}>${busy ? 'Mise à jour…' : 'Forcer'}</button>`}</span></div>
      <div class="kv"><span>Mise à jour automatique<${Tip} text=${'Vérifiée toutes les 6 h ' + where + '. Jamais pendant une discussion avec ' + name + '.'} /></span><${Switch} checked=${x.auto} label=${'Mise à jour automatique de ' + name} onChange=${auto} /></div>
      ${la && html`<div class="kv"><span>Dernière auto</span><span class=${'state' + (la.ok ? '' : ' err')}>${when(la.at)} · ${la.ok ? (la.from ? clean(la.from) + ' → ' : '') + (la.to || 'à jour') : 'échec'}</span></div>`}`
    : html`<div class="kv"><span>Installation</span><span>${missing
        ? html`<span class="state err">Manque ${x.requires_missing.join(', ')} ${where}</span>`
        : html`<button class="btn sm" disabled=${!!busy} onClick=${() => run('install')}>${busy ? html`<span class="spinner"></span>Installation…` : html`<${Icon} n="download" />Installer ${name}`}</button>`}</span></div>`}
    ${x.unverified && html`<p class="note">Commande d’installation non vérifiée pour ce harness : relis le journal après l’action.</p>`}
    ${log && html`<details class="lc-log" open=${!log.ok}><summary>${log.ok ? 'Journal de l’action' : 'Journal de l’échec'}</summary><pre>${log.text || '(vide)'}</pre></details>`}
  </div>`;
}

// Machine connectée sur laquelle tourne un harness distant (custom-<machine>-<harness>).
export async function remoteHarnessTarget(runtimeId) {
  const r = await get('/api/machines').catch(() => null);
  const m = r && r.ok && r.machines.find(x => (x.harnesses || []).includes(runtimeId));
  return m ? { target: m.id, id: runtimeId.slice(('custom-' + m.id + '-').length), machine: m } : null;
}
