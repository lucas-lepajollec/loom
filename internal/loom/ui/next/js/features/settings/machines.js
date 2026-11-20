import { html, cls, useState, useRef, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { shortVersion } from '../../core/version.js';
import { Icon } from '../../ui/icons.js';
import { Tip } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { refreshWorkspace, refreshEngineNode } from '../../core/state.js';
import { copyText } from '../../ui/clipboard.js';

// Appairage : sur la machine, `loom node pair` affiche un code à usage unique ;
// ici on donne son adresse (ou on la choisit parmi celles trouvées) et le code.
const NODE_INSTALL = 'curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh -s -- --node --listen lan';
export function addressWithPort(host, port) {
  const bare = host.replace(/^\[|\]$/g, '');
  return (bare.includes(':') ? '[' + bare + ']' : bare) + ':' + port;
}

export const nodeAddress = m => m.host ? addressWithPort(m.host, 2511) : '';

export function PairDialog({ start, onClose }) {
  const [v, setV] = useState({ address: start?.address || '', code: '' });
  const [step, setStep] = useState(start?.address ? 2 : 1);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const fmt = c => { const x = c.toUpperCase().replace(/[^0-9A-Z]/g, '').slice(0, 8); return x.length > 4 ? x.slice(0, 4) + '-' + x.slice(4) : x; };
  const pair = async force => {
    setBusy(true); setError('');
    const r = await post('/api/machines/pair', { address: v.address.trim(), code: v.code.replace('-', ''), ...(start?.machine_id ? { machine_id: start.machine_id } : {}), ...(force ? { force: true } : {}) }, { timeout: 30000, retryAuth: false }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok && r.error_code === 'already_paired' && !force) {
      if (await confirm(t('machines.pair.already_title'), r.error + '. ' + t('machines.pair.already_text'), { ok: t('machines.pair.already_ok') })) return pair(true);
      return;
    }
    if (!r.ok) { setError(r.error || t('machines.pair.failed')); return; }
    setResult(r.machine); setStep(3); await refreshEngineNode(); refreshWorkspace();
  };
  const title = t(step === 1 ? 'machines.add.install' : step === 2 ? 'machines.add.code' : 'machines.add.done');
  return html`<${Modal} title=${t('machines.add.title')} sub=${step + ' / 3 · ' + title} onClose=${busy ? undefined : () => onClose(result)}
      foot=${step === 3 ? html`<button class="btn primary" onClick=${() => onClose(result)}>${t('machines.add.open')}</button>` : html`<button class="btn ghost" disabled=${busy} onClick=${() => onClose()}>${t('ui.dialog.annuler')}</button>
        ${step === 2 && html`<button class="btn ghost" disabled=${busy} onClick=${() => setStep(1)}>${t('machines.add.back')}</button>`}
        <button class="btn primary" disabled=${busy || (step === 2 && (!v.address.trim() || v.code.replace('-', '').length !== 8))} onClick=${() => step === 1 ? setStep(2) : pair(false)}>${busy ? t('settings.machines.verification') : t(step === 1 ? 'machines.add.next' : 'machines.pair.ok')}</button>`}>
    <div class="mx-steps"><div class="mx-step"><span class="mx-n">${step}</span><div class="mx-body"><b>${title}</b>
      ${step === 1 ? html`<p class="note">${t('machines.add.linux')}</p><div class="mx-code"><pre>${NODE_INSTALL}</pre><button class="btn sm" onClick=${async () => { const ok = await copyText(NODE_INSTALL); toast(t(ok ? 'machines.edit.bloc_copie' : 'machines.edit.copie_refusee_selectionne_le_texte'), ok ? '' : 'err'); }}><${Icon} n="copy" />${t('machines.edit.copier')}</button></div><p class="note">${t('machines.add.installs')}</p>`
      : step === 2 ? html`<${Discovered} onPair=${n => setV(old => ({ ...old, address: n.address }))} />
        <label class="field"><span>${t('machines.pair.address')}</span><input class="input mono" value=${v.address} placeholder="192.168.1.20:2511" onInput=${e => setV({ ...v, address: e.target.value })} /></label>
        <label class="field"><span>${t('machines.pair.code')}</span><input class="input mono pair-code" value=${v.code} placeholder="K7QM-4XPA" autocomplete="one-time-code" onInput=${e => setV({ ...v, code: fmt(e.target.value) })} /></label>
        <p class="note">${t('machines.pair.lan_note')}</p><p class="note">${t('machines.add.fresh')}</p>`
      : html`<p>${result?.name}</p><${NodeModules} modules=${result?.modules} />`}
      ${error && html`<p class="note err" role="alert">${error}</p>`}
    </div></div></div></${Modal}>`;
}

export function NodeModules({ modules }) {
  const labels = {
    engine: t('machines.module.engine'),
    voice: t('settings.machines.brick_voice'),
    terminal: t('machines.module.terminal'),
    files: t('settings.machines.brick_files'),
    observe: t('machines.module.observe'),
  };
  if (!Array.isArray(modules) || !modules.length) return html`<span class="muted">${t('settings.machines.no_modules')}</span>`;
  const ids = [...new Set([...Object.keys(labels), ...modules])];
  return html`<div class="machine-chips">${ids.map(id => {
    const announced = modules.includes(id);
    const state = announced ? t('machines.module.enabled') : t('machines.module.disabled');
    return html`<span class=${cls('tag', announced ? 'green' : 'muted')} title=${state} key=${id}>
      ${labels[id] || id}<span class="sr">${' · ' + state}</span>
    </span>`;
  })}</div>`;
}

export function MachineMigration({ m, onChange }) {
  const [busy, setBusy] = useState(false), [phase, setPhase] = useState(''), [error, setError] = useState(''), [pairing, setPairing] = useState(false);
  const alive = useRef(true), timer = useRef(null);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; clearInterval(timer.current); };
  }, []);
  const migrate = async () => {
    if (busy) return;
    setBusy(true); setPhase('installing'); setError('');
    const base = '/api/machines/' + encodeURIComponent(m.id) + '/node/migrate';
    timer.current = setInterval(() => get(base).then(r => { if (alive.current && r.phase) setPhase(r.phase); }).catch(() => {}), 1500);
    try {
      const r = await post(base, {}, { timeout: 16 * 60 * 1000 });
      if (!r.ok) throw new Error(r.error || t('machines.pair.failed'));
      await refreshEngineNode(); await refreshWorkspace(); onChange();
    } catch (e) { if (alive.current) setError(e.message); }
    finally { clearInterval(timer.current); if (alive.current) { setBusy(false); setPhase(''); } }
  };
  return html`<div class="machine-migration">
    <span class="muted">${t('machines.migrate.old')}</span>
    <button class="btn sm primary" disabled=${busy} onClick=${migrate}>${t('machines.migrate.install')}</button>
    <button class="btn sm ghost" disabled=${busy} onClick=${() => setPairing(true)}>${t('machines.migrate.manual')}</button>
    ${phase && html`<span role="status">${t(phase === 'pairing' ? 'machines.migrate.pairing' : 'machines.migrate.installing')}</span>`}${error && html`<p class="note err" role="alert">${error}</p>`}
    ${pairing && html`<${PairDialog} start=${{ address: nodeAddress(m), name: m.name, machine_id: m.id }} onClose=${ok => { setPairing(false); if (ok) onChange(); }} />`}
  </div>`;
}

// Les nœuds Loom du réseau local qui répondent à la recherche (sans secret).
export function Discovered({ onPair }) {
  const [nodes, setNodes] = useState(null);
  const [busy, setBusy] = useState(false);
  const scan = async () => { if (scanning.current) return; scanning.current = true; setBusy(true); const r = await get('/api/machines/discover', { timeout: 8000 }).catch(() => null); scanning.current = false; if (alive.current) { setBusy(false); setNodes(r?.nodes || []); } };
  const scanning = useRef(false), alive = useRef(true);
  useEffect(() => { alive.current = true; scan(); const timer = setInterval(scan, 10000); return () => { alive.current = false; clearInterval(timer); }; }, []);
  if (nodes === null || (!nodes.length && !busy)) return html`<div class="disc-empty"><button class="btn sm ghost" disabled=${busy} onClick=${scan}><${Icon} n="search" />${busy ? t('machines.disc.scanning') : t('machines.disc.scan')}</button></div>`;
  return html`<section class="sec"><div class="sec-h"><h2>${t('machines.disc.title')}<${Tip} text=${t('machines.disc.tip')} /></h2><span class="grow"></span>
      <button class="icon-btn" disabled=${busy} aria-label=${t('machines.disc.scan')} onClick=${scan}><${Icon} n="refresh" /></button></div>
    <div class="card bs-list">${nodes.map(n => html`<div class="bs-row" key=${n.id}>
      <span class="mono-tile"><${Icon} n="server" /></span>
      <div class="grow"><div class="bs-name">${n.name}${n.paired && html`<span class="tag">${t('machines.disc.paired_elsewhere')}</span>`}</div><div class="bs-sub"><span class="mono">${n.address}</span><span title=${n.version}>${t('settings.page.loom')} ${shortVersion(n.version)}</span></div></div>
      <button class="btn sm" onClick=${() => onPair({ address: n.address, name: n.name })}>${t('machines.pair.ok')}</button></div>`)}</div>
  </section>`;
}
