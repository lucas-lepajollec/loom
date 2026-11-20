import { shortVersion } from '../../core/version.js';
import { t, locale } from '../../core/i18n.js';
// Réglages › Machines : une carte par machine (locale détaillée, distantes
// SSH / nœud Loom, hors ligne), avec spécifications, moteur, harnesses,
// dossiers de travail et terminaux.
import { html, useState, useRef, useEffect, useStore, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Logo } from '../../ui/logo.js';
import { Empty, Tip, Menu } from '../../ui/controls.js';
import { Modal, confirm, prompt, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshWorkspace, refreshEngineNode, refreshStatus, refreshLibrary } from '../../core/state.js';
import { copyText } from '../../ui/clipboard.js';
import { MachineDialog } from '../machines/edit.js';
import { openTerminalWith } from '../terminals/page.js';
import { Line, Group } from './kit.js';
import { LoomUpdates } from './updates.js';
import { ModelDirs, DirectEngineForm } from './page.js';

const home = p => String(p || '').replace(/^\/home\/[^/]+/, '~');
const CLI_LOGOS = { claude: 'claude', codex: 'openai', gemini: 'gemini', antigravity: 'google', deepseek: 'deepseek', grok: 'xai' };

// Lien vers cette page depuis les endroits qui en ont besoin.
export const MachinesLink = ({ label }) => html`<a class="btn sm ghost" href="#/machines"><${Icon} n="server" />${label || t("settings.machines.gerer_les_machines")}</a>`;

// Appairage : sur la machine, `loom node pair` affiche un code à usage unique ;
// ici on donne son adresse (ou on la choisit parmi celles trouvées) et le code.
const NODE_INSTALL = 'curl -fsSL https://raw.githubusercontent.com/lucas-lepajollec/loom/main/install.sh | sh -s -- --node --listen lan';
const nodeAddress = m => (m.host.includes(':') ? '[' + m.host.replace(/^\[|\]$/g, '') + ']' : m.host) + ':2511';

function PairDialog({ start, onClose }) {
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

function NodeModules({ modules = [] }) {
  return html`<div class="mx-offers">${['engine', 'terminal', 'observe'].map(id => html`<div class="mx-offer" key=${id}><span class="grow">${t(({ engine: 'machines.module.engine', terminal: 'machines.module.terminal', observe: 'machines.module.observe' })[id])}</span><span class="state">${t(modules.includes(id) ? 'machines.module.enabled' : 'machines.module.disabled')}</span></div>`)}</div>`;
}

function MachineMigration({ m, onChange, compact = false }) {
  const [busy, setBusy] = useState(false), [phase, setPhase] = useState(''), [error, setError] = useState(''), [pairing, setPairing] = useState(false);
  const alive = useRef(true);
  useEffect(() => () => { alive.current = false; }, []);
  const migrate = async e => {
    e?.preventDefault(); e?.stopPropagation();
    if (busy) return;
    setBusy(true); setPhase('installing'); setError('');
    const base = '/api/machines/' + encodeURIComponent(m.id) + '/node/migrate';
    const timer = setInterval(() => get(base).then(r => { if (alive.current && r.phase) setPhase(r.phase); }).catch(() => {}), 1500);
    try {
      const r = await post(base, {}, { timeout: 16 * 60 * 1000 });
      if (!r.ok) throw new Error(r.error || t('machines.pair.failed'));
      await refreshEngineNode(); await refreshWorkspace(); onChange();
    } catch (e) { if (alive.current) setError(e.message); }
    finally { clearInterval(timer); if (alive.current) { setBusy(false); setPhase(''); } }
  };
  return html`<div class=${compact ? 'mc-mig' : 'card pad mig-banner'}>
    <span class="state">${t('machines.migrate.old')}</span>
    <button class="btn sm primary" disabled=${busy} onClick=${migrate}>${t('machines.migrate.install')}</button>
    <button class="btn sm ghost" disabled=${busy} onClick=${e => { e.preventDefault(); e.stopPropagation(); setPairing(true); }}>${t('machines.migrate.manual')}</button>
    ${phase && html`<span role="status">${t(phase === 'pairing' ? 'machines.migrate.pairing' : 'machines.migrate.installing')}</span>`}${error && html`<p class="note err" role="alert">${error}</p>`}
    ${pairing && html`<${PairDialog} start=${{ address: nodeAddress(m), name: m.name, machine_id: m.id }} onClose=${ok => { setPairing(false); if (ok) onChange(); }} />`}
  </div>`;
}

function NodeUpdateBadge({ m }) {
  const [info, setInfo] = useState(null);
  useEffect(() => {
    let alive = true;
    get('/api/machines/' + encodeURIComponent(m.id) + '/node/update').then(r => { if (alive && !r.error) setInfo(r); }).catch(() => {});
    return () => { alive = false; };
  }, [m.id]);
  return info?.available ? html`<span class="mcard-up" title=${t('machines.node.update_available', { version: info.latest })}><i></i>${t('machines.node.update_short')}</span>` : null;
}

// Les nœuds Loom du réseau local qui répondent à la recherche (sans secret).
function Discovered({ onPair }) {
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
      <div class="grow"><div class="bs-name">${n.name}${n.paired && html`<span class="tag">${t('machines.disc.paired_elsewhere')}</span>`}</div><div class="bs-sub"><span class="mono">${n.address}</span><span title=${n.version}>Loom ${shortVersion(n.version)}</span></div></div>
      <button class="btn sm" onClick=${() => onPair({ address: n.address, name: n.name })}>${t('machines.pair.ok')}</button></div>`)}</div>
  </section>`;
}

// SSH machines show user@host; machines paired by code only have their node.
const where = m => (m.user ? 'SSH · ' + m.user + '@' + m.host + (m.port && m.port !== 22 ? ':' + m.port : ':' + (m.port || 22)) : m.host + ' · ' + t('machines.node_label')) + (m.os ? ' · ' + m.os : '');

// Jauges d'une machine : processeur, mémoire, carte graphique, disque.
const pct = (u, total) => total ? Math.min(100, Math.round(u * 100 / total)) : 0;
const gb = n => (n / 1073741824).toFixed(n >= 107374182400 ? 0 : 1);
function Meters({ m }) {
  if (!m) return html`<div class="mmeters muted">…</div>`;
  if (m.error || m.supported === false) return html`<div class="mmeters"><span class="muted mmeter-none" title=${m.error || ''}>${t('machines.metrics.none')}</span></div>`;
  const gpu = (m.gpus || [])[0];
  const row = (label, value, sub, title) => html`<div class="mmeter" title=${title || ''}><span>${label}</span><i class="mmeter-bar"><b style=${`width:${value}%`} class=${value >= 90 ? 'hot' : value >= 70 ? 'warm' : ''}></b></i><em>${sub}</em></div>`;
  return html`<div class="mmeters" style="padding:0">
    ${row(t('machines.metrics.cpu'), Math.round(m.cpu || 0), Math.round(m.cpu || 0) + ' %', m.cores ? t('machines.metrics.cores', { n: m.cores }) : '')}
    ${row(t('machines.metrics.ram'), pct(m.ram_used, m.ram_total), gb(m.ram_used) + ' / ' + gb(m.ram_total) + t('common.units.gb'))}
    ${gpu && row(t('machines.metrics.gpu'), pct(gpu.vram_used, gpu.vram_total), gb(gpu.vram_used) + ' / ' + gb(gpu.vram_total) + t('common.units.gb'), gpu.name + ' · ' + Math.round(gpu.util || 0) + ' %')}
    ${m.disk && m.disk.total ? row(t('machines.metrics.disk'), pct(m.disk.used, m.disk.total), gb(m.disk.used) + ' / ' + gb(m.disk.total) + t('common.units.gb'), m.disk.path) : ''}
  </div>`;
}

function sameHost(n, m) {
  try { return n && m && (n.hostname === m.host || new URL(n.url).hostname === m.host); } catch (_) { return false; }
}

function LocalMachineCard({ local, metrics, serverInfo, node, codeman, terminals, onAddFolder, onRemoveFolder, onOpenSsh }) {
  const [menuAnchor, setMenuAnchor] = useState(null);
  const [busyCli, setBusyCli] = useState('');
  const name = (local && local.hostname) || t("settings.machines.cette_machine_2");
  const gpu = (metrics?.gpus || [])[0];
  const gpuSpec = gpu ? `${gpu.name.replace(/^NVIDIA (GeForce )?/, '')}${gpu.vram_total ? ' ' + Math.round(gpu.vram_total / 1073741824) + ' ' + t('common.units.gb') : ''}` : '';
  const ramSpec = metrics?.ram_total ? t('settings.machines.ram_suffix', { n: Math.round(metrics.ram_total / 1073741824) }) : '';
  const specs = [local?.os, gpuSpec, ramSpec].filter(Boolean).join(' · ');
  const folders = local?.folders || [];
  const openTerms = terminals.filter(x => x.target === 'local');
  const clis = (codeman.clis || []).filter(c => c.kind !== 'shell');
  const installedClis = clis.filter(c => c.installed !== false);
  const installableClis = clis.filter(c => c.installed === false && c.stock !== false && c.installCommand);
  const memUsed = gpu?.vram_total ? gpu.vram_used : metrics?.ram_used;
  const memTotal = gpu?.vram_total ? gpu.vram_total : metrics?.ram_total;
  const memPct = pct(memUsed, memTotal);

  const installCli = async c => {
    if (!await confirm(t('agents.cfg.cli.install_title', { cli: c.label }), t('agents.cfg.cli.install_text', { cmd: c.installCommand || '' }), { ok: t('agents.cfg.cli.install') })) return;
    setBusyCli(c.id);
    const r = await post('/api/codeman/v1/clis/' + encodeURIComponent(c.id) + '/install', {}, { timeout: 600000 }).catch(e => ({ success: false, error: e.message }));
    setBusyCli('');
    if (r?.success === false || r?.ok === false) return toast(r.error || t('voice.failed'), 'err');
    toast(t('agents.cfg.cli.installed', { cli: c.label }));
    codeman.reload();
  };

  return html`<section aria-label=${name} class="mc-card">
    <div class="mc-card-h">
      <i class="mc-dot green"></i>
      <div class="mc-card-title">
        <b><a href="#/machines/local">${name}</a> <span class="mc-tag-here">· ${t("settings.machines.cette_machine")}</span></b>
        ${specs && html`<span class="mc-sub">${specs}</span>`}
      </div>
      <div class="mc-card-acts">
        <button type="button" class="btn" onClick=${() => openTerminalWith({ target: 'local', dir: '', title: name })}><${Icon} n="terminal" />${t("settings.machines.nouveau_terminal")}</button>
        <button type="button" class="icon-btn" aria-label=${t('settings.machines.more_actions')} onClick=${e => setMenuAnchor(menuAnchor ? null : e.currentTarget)}><${Icon} n="more" /></button>
        ${menuAnchor && html`<${Menu} anchor=${menuAnchor} onClose=${() => setMenuAnchor(null)} items=${[
          { icon: 'server', label: t('settings.machines.details'), run: () => go('machines', 'local') },
          { icon: 'plus', label: t('settings.machines.ssh_add'), run: onOpenSsh },
          { icon: 'power', label: t('startup.title'), run: () => go('settings', 'startup') },
          { icon: 'info', label: t('settings.page.mises_a_jour'), run: () => go('settings', 'about') },
        ]} />`}
      </div>
    </div>

    <div class="mc-kv4">
      <div class="mc-kv">
        <span>${t('settings.page.loom')}</span>
        <span>${shortVersion(serverInfo?.version) || '—'}</span>
      </div>
      <div class="mc-kv">
        <span>Codeman</span>
        ${codeman.status?.auth === 'ok'
          ? html`<a href="#/agents">${[shortVersion(codeman.status.version), t('settings.machines.codeman_sessions', { n: codeman.sessions.length })].filter(Boolean).join(' · ')}</a>`
          : html`<a href="#/agents/settings" class="muted">${codeman.status?.configured ? t('agents.conn.unreachable') : t('settings.machines.codeman_not_connected')}</a>`}
      </div>
      <div class="mc-kv">
        <span>${t('settings.machines.moteur')}</span>
        <a href="#/engine">${!node ? `llama.cpp · ${t('settings.machines.used_by_loom')}` : `${t('settings.machines.celui_de')} ${node.hostname}`}</a>
      </div>
      <div class="mc-kv">
        <span>${gpu?.vram_total ? t('settings.machines.gpu_memory') : t('settings.machines.ram_memory')}</span>
        ${memTotal ? html`<div class="mc-vram"><div class="mc-bar"><i style=${`width:${memPct}%`} class=${memPct >= 90 ? 'hot' : memPct >= 70 ? 'warm' : ''}></i></div><span>${gb(memUsed)} / ${gb(memTotal)} ${t('common.units.gb')}</span></div>`
          : html`<span class="muted">—</span>`}
      </div>
    </div>

    ${(installedClis.length > 0 || installableClis.length > 0) && html`<div class="mc-sec">
      <span class="mc-lbl">${t('settings.machines.harnesses')}</span>
      <div class="mc-chips">
        ${installedClis.map(c => {
          const logo = CLI_LOGOS[c.id];
          const ver = c.version || '';
          const nextVer = c.latestVersion && c.latestVersion !== ver ? c.latestVersion : '';
          return html`<span class="mc-chip" key=${c.id}>
            ${logo && html`<${Logo} id=${logo} size="xs" />`}
            <span>${c.label}</span>
            ${ver && html`<span class="mc-chip-ver">${ver}</span>`}
            ${(c.updateAvailable || nextVer) && c.installCommand && html`<button type="button" class="mc-chip-up" disabled=${busyCli === c.id} onClick=${() => installCli(c)}>${t('settings.machines.harness_update', { v: nextVer || t('machines.node.update_short') })}</button>`}
          </span>`;
        })}
        ${installableClis.map(c => html`<button type="button" class="mc-chip dashed" key=${c.id} disabled=${busyCli === c.id} onClick=${() => installCli(c)}>
          ${c.label} · ${t('settings.machines.harness_install')}
        </button>`)}
      </div>
    </div>`}

    <div class="mc-cols2">
      <div class="mc-sec">
        <div class="mc-sec-h">
          <span class="mc-lbl">${t('settings.machines.dossiers_de_travail')}</span>
          <button type="button" class="mc-link-btn" onClick=${onAddFolder}>${t('settings.machines.ajouter')}</button>
        </div>
        <div class="mc-chips">
          ${folders.length ? folders.map(f => html`<span class="mc-chip mono" key=${f}>
            <button type="button" onClick=${() => openTerminalWith({ target: 'local', dir: f, title: name })}>${home(f)}</button>
            <button type="button" class="mc-chip-del" aria-label=${t('settings.machines.retirer_2')} onClick=${() => onRemoveFolder(f)}><${Icon} n="close" /></button>
          </span>`) : html`<span class="muted">${t('settings.machines.no_folders')}</span>`}
        </div>
      </div>
      <div class="mc-sec">
        <span class="mc-lbl">${t('settings.machines.open_terminals')}</span>
        <div class="mc-chips">
          ${openTerms.length ? openTerms.map(term => html`<a key=${term.id} href=${'#/terminals/' + term.id} class="mc-chip mono">${term.title}${term.dir ? ' · ' + home(term.dir) : ''}</a>`)
            : html`<span class="muted">${t('settings.machines.no_open_terminals')}</span>`}
        </div>
      </div>
    </div>
  </section>`;
}

function RemoteMachineCard({ m, metric, testState, node, terminals, onTest, onEdit, onRemove, onChange, onDirectEngine, children }) {
  const [menuAnchor, setMenuAnchor] = useState(null);
  const [pairing, setPairing] = useState(false);
  const [busy, setBusy] = useState(false);
  const owns = sameHost(node, m);
  const paired = !m.user;
  const rmErr = metric?.error && !String(metric.error).includes('observe module') && metric.supported !== false;
  const isOffline = testState?.ok === false || (rmErr && testState?.ok !== true);

  const useEngine = async () => {
    setBusy(true);
    const r = await post('/api/engine/node', { machine: m.id }, { timeout: 30000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) return toast(r.error || t("settings.machines.liaison_impossible"), 'err');
    toast(t("settings.machines.moteur_de") + r.hostname + t("settings.machines.utilise")); await refreshEngineNode(); refreshStatus(); refreshLibrary();
  };
  const unlink = async () => { await post('/api/engine/node', { unlink: true }); await refreshEngineNode(); refreshStatus(); refreshLibrary(); };

  const menuItems = [
    { icon: 'refresh', label: t('settings.machines.test_connection'), run: onTest },
    { icon: 'server', label: t('settings.machines.details'), run: () => go('machines', m.id) },
    ...(m.user ? [{ icon: 'edit', label: t('settings.machines.modifier'), run: onEdit }] : []),
    '-',
    { icon: 'trash', label: t('settings.machines.retirer_2'), danger: true, run: onRemove },
  ];

  if (isOffline) {
    const when = m.checked_at ? new Date(m.checked_at).toLocaleString(locale(), { weekday: 'short', hour: '2-digit', minute: '2-digit' }) : '';
    return html`<section aria-label=${m.name} class="mc-card offline">
      <i class="mc-dot"></i>
      <div class="mc-card-title">
        <b><a href=${'#/machines/' + encodeURIComponent(m.id)}>${m.name}</a></b>
        <span class="mc-sub">${testState?.error || (when ? t('settings.machines.offline_since', { when }) : t('settings.machines.offline'))}</span>
      </div>
      <div class="mc-card-acts">
        <button type="button" class="btn" disabled=${!!testState?.busy} onClick=${onTest}>${testState?.busy ? t('settings.machines.verification') : t('settings.machines.test_connection')}</button>
        <button type="button" class="icon-btn" aria-label=${t('settings.machines.more_actions')} onClick=${e => setMenuAnchor(menuAnchor ? null : e.currentTarget)}><${Icon} n="more" /></button>
        ${menuAnchor && html`<${Menu} anchor=${menuAnchor} onClose=${() => setMenuAnchor(null)} items=${menuItems} />`}
      </div>
    </section>`;
  }

  const gpu = (metric?.gpus || [])[0];
  const specBits = [where(m), gpu?.name ? gpu.name.replace(/^NVIDIA (GeForce )?/, '') : '', metric?.ram_total ? Math.round(metric.ram_total / 1073741824) + ' ' + t('common.units.gb') : ''].filter(Boolean).join(' · ');
  const remoteTerms = terminals.filter(x => x.target === m.id);
  const canTerm = m.user || (m.modules || []).includes('terminal');

  return html`<section aria-label=${m.name} class="mc-card">
    <div class="mc-card-h">
      <i class="mc-dot green"></i>
      <div class="mc-card-title">
        <b><a href=${'#/machines/' + encodeURIComponent(m.id)}>${m.name}</a>${m.node_id && html`<${NodeUpdateBadge} m=${m} />`}</b>
        <span class="mc-sub mono">${specBits}</span>
      </div>
      <div class="mc-card-acts">
        ${canTerm && html`<button type="button" class="btn" onClick=${() => openTerminalWith({ target: m.id, dir: m.home || '', title: m.name })}>${t("settings.machines.terminal")}</button>`}
        <button type="button" class="icon-btn" aria-label=${t('settings.machines.more_actions')} onClick=${e => setMenuAnchor(menuAnchor ? null : e.currentTarget)}><${Icon} n="more" /></button>
        ${menuAnchor && html`<${Menu} anchor=${menuAnchor} onClose=${() => setMenuAnchor(null)} items=${menuItems} />`}
      </div>
    </div>

    <div class="mc-kv3">
      <div class="mc-kv">
        <span>${t('settings.machines.harnesses')}</span>
        <span>${(m.harnesses || []).length ? m.harnesses.join(' · ') : '—'}</span>
      </div>
      <div class="mc-kv">
        <span>${t('settings.machines.moteur')}</span>
        ${owns ? html`<div class="mc-engine-acts"><span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.direct ? `${node.kind} ${t('settings.machines.used_by_loom')}` : t('settings.machines.moteur_utilise_par_loom')}</span>
            <button type="button" class="btn sm ghost" onClick=${unlink}>${t("settings.machines.ne_plus_l_utiliser")}</button></div>`
          : html`<div class="mc-engine-acts">
              ${paired ? html`<button type="button" class="btn sm" disabled=${busy} onClick=${useEngine}>${t('machines.node.use_engine')}</button>`
                : html`<button type="button" class="btn sm" onClick=${() => setPairing(true)}>${t('settings.machines.connect_node')}</button>`}
              <button type="button" class="btn sm ghost" onClick=${onDirectEngine}>${t('settings.machines.link_server')}</button>
            </div>`}
      </div>
      <div class="mc-kv">
        <span>${t('machines.module.observe')}</span>
        ${metric && !metric.error && metric.supported !== false ? html`<${Meters} m=${metric} />`
          : html`<span class="muted">${paired ? t('machines.link.paired') : t('machines.link.ssh')}</span>`}
      </div>
    </div>

    ${remoteTerms.length > 0 && html`<div class="mc-sec">
      <span class="mc-lbl">${t('settings.machines.open_terminals')}</span>
      <div class="mc-chips">${remoteTerms.map(term => html`<a key=${term.id} href=${'#/terminals/' + term.id} class="mc-chip mono">${term.title}${term.dir ? ' · ' + home(term.dir) : ''}</a>`)}</div>
    </div>`}

    ${children}
    ${pairing && html`<${PairDialog} start=${{ address: nodeAddress(m), name: m.name, machine_id: m.id }} onClose=${ok => { setPairing(false); if (ok) onChange?.(); }} />`}
  </section>`;
}

export function MachinesSettings({ route }) {
  const [data, setData] = useState(null);
  const [local, setLocal] = useState(null);
  const [dlg, setDlg] = useState(null);
  const [pair, setPair] = useState(null);
  const [directFor, setDirectFor] = useState(null);
  const [pickLocalFolder, setPickLocalFolder] = useState(false);
  const [metrics, setMetrics] = useState({});
  const [tests, setTests] = useState({});
  const [terminals, setTerminals] = useState([]);
  const [cm, setCm] = useState({ status: null, clis: [], sessions: [] });

  const loadCodeman = () => {
    get('/api/codeman/status', { retryAuth: false }).then(st => {
      if (!st || st.auth !== 'ok') { setCm({ status: st || null, clis: [], sessions: [] }); return; }
      Promise.all([
        get('/api/codeman/v1/clis', { retryAuth: false }).catch(() => null),
        get('/api/codeman/v1/sessions', { retryAuth: false }).catch(() => null),
      ]).then(([clisRes, sessRes]) => {
        const clis = Array.isArray(clisRes?.data) ? clisRes.data : Array.isArray(clisRes) ? clisRes : [];
        const sessions = Array.isArray(sessRes?.data) ? sessRes.data : Array.isArray(sessRes) ? sessRes : [];
        setCm({ status: st, clis, sessions });
      });
    }).catch(() => {});
  };

  useEffect(() => {
    let alive = true;
    const tick = () => get('/api/machines/metrics', { timeout: 9000 }).then(r => { if (alive && r && r.metrics) setMetrics(r.metrics); }).catch(() => {});
    tick(); const id = setInterval(tick, 10000);
    return () => { alive = false; clearInterval(id); };
  }, []);

  const load = () => Promise.all([
    get('/api/machines').then(r => setData(r.ok ? r : { machines: [], offers: {} })).catch(() => setData({ machines: [], offers: {} })),
    get('/api/machines/local').then(setLocal).catch(() => setLocal(null)),
  ]);

  const node = useStore(app, x => x.engineNode);
  const serverInfo = useStore(app, x => x.serverInfo);

  useEffect(() => {
    load();
    loadCodeman();
    get('/api/terminals').then(r => setTerminals(r.terminals || [])).catch(() => {});
  }, []);

  const id = route && route.id;
  if (!data) return html`<div class="skeleton" style="height:240px"></div>`;
  if (id) {
    const m = id === 'local' ? null : data.machines.find(x => x.id === id);
    if (id !== 'local' && !m) return html`<${Empty} icon="server" title="${t("settings.machines.machine_introuvable")}" />`;
    return html`<${MachineDetail} m=${m} local=${local} offers=${m ? (data.offers || {})[m.id] || [] : null} onChange=${load} onEdit=${() => setDlg({ machine: m })} />
      ${dlg && html`<${MachineDialog} machine=${dlg.machine} onClose=${x => { setDlg(null); if (x) load(); }} />`}`;
  }

  const saveLocalFolders = async next => {
    const r = await post('/api/machines/folders', { machine: 'local', folders: next });
    if (!r.ok) return toast(r.error || t("settings.machines.enregistrement_impossible"), 'err');
    setLocal(old => ({ ...(old || {}), folders: r.folders }));
  };

  const testMachine = async m => {
    setTests(old => ({ ...old, [m.id]: { busy: true } }));
    const r = await post('/api/machines', { machine: m, check_only: true }).catch(e => ({ ok: false, error: e.message }));
    if (r?.ok) {
      setTests(old => ({ ...old, [m.id]: { ok: true } }));
      toast(`${m.name} · ${t('settings.machines.connection_ok')}`);
      load();
    } else {
      const err = r?.error || t('machines.edit.connexion_impossible');
      setTests(old => ({ ...old, [m.id]: { ok: false, error: err } }));
      toast(err, 'err');
    }
  };

  const removeMachine = async m => {
    if (!await confirm(t("settings.machines.retirer") + m.name, t("settings.machines.ses_harnesses_disparaissent_de_loom_les_discussions_deja_faites_r"), { ok: t("settings.machines.retirer_2"), danger: true })) return;
    const r = await post('/api/machines/delete', { id: m.id });
    if (!r.ok) return toast(r.error || t("settings.machines.suppression_impossible"), 'err');
    await refreshWorkspace(); load();
  };

  return html`
    <div class="page-head">
      <div>
        <h1>${t('settings.page.machines')}</h1>
        <p>${t("settings.machines.loom_travaille_sur_cette_machine_et_sur_les_machines_que_tu_conne")}</p>
      </div>
      <div class="acts">
        <button type="button" class="mcard add" onClick=${() => setPair({})}>${t('machines.add.title')}</button>
      </div>
    </div>

    <div class="mc-list">
      <${LocalMachineCard}
        local=${local}
        metrics=${metrics.local}
        serverInfo=${serverInfo}
        node=${node}
        codeman=${{ ...cm, reload: loadCodeman }}
        terminals=${terminals}
        onAddFolder=${() => setPickLocalFolder(true)}
        onRemoveFolder=${f => saveLocalFolders((local?.folders || []).filter(x => x !== f))}
        onOpenSsh=${() => setDlg({ machine: null })}
      />
      ${data.machines.map(m => html`<${RemoteMachineCard}
        key=${m.id}
        m=${m}
        metric=${metrics[m.id]}
        testState=${tests[m.id]}
        node=${node}
        terminals=${terminals}
        onTest=${() => testMachine(m)}
        onEdit=${() => setDlg({ machine: m })}
        onRemove=${() => removeMachine(m)}
        onChange=${load}
        onDirectEngine=${() => setDirectFor(m)}
      >${m.user && html`<${MachineMigration} m=${m} onChange=${load} compact=${true} />`}</${RemoteMachineCard}>`)}
    </div>

    ${!pair && html`<${Discovered} onPair=${setPair} />`}
    ${pair && html`<${PairDialog} start=${pair} onClose=${x => { setPair(null); if (x) { load(); if (x.id) go('machines', x.id); } }} />`}
    ${dlg && html`<${MachineDialog} machine=${dlg.machine} onClose=${x => { setDlg(null); if (x) load(); }} />`}
    ${directFor && html`<${Modal} title=${t('settings.machines.link_server')} sub=${directFor.name} onClose=${() => setDirectFor(null)}>
      <${DirectEngineForm} start=${'http://' + directFor.host + ':8080'} onDone=${() => setDirectFor(null)} />
    </${Modal}>`}
    ${pickLocalFolder && typeof FolderPicker === 'function' && html`<${FolderPicker} start="" onClose=${() => setPickLocalFolder(false)} onPick=${d => { setPickLocalFolder(false); saveLocalFolders([...(local?.folders || []), d]); }} />`}
  `;
}

function MachineDetail({ m, local, offers, onChange, onEdit }) {
  const isLocal = !m;
  const target = isLocal ? 'local' : m.id;
  const name = isLocal ? (local && local.hostname) || t("settings.machines.cette_machine_2") : m.name;
  const remove = async () => {
    if (!await confirm(t("settings.machines.retirer") + m.name, t("settings.machines.ses_harnesses_disparaissent_de_loom_les_discussions_deja_faites_r"), { ok: t("settings.machines.retirer_2"), danger: true })) return;
    const r = await post('/api/machines/delete', { id: m.id });
    if (!r.ok) return toast(r.error || t("settings.machines.suppression_impossible"), 'err');
    await refreshWorkspace(); go('machines');
  };
  return html`
    <div class="mc-head anim-rise"><a class="btn sm ghost" href="#/machines"><${Icon} n="left" />${t("settings.machines.machines")}</a>
      <div class="mc-title"><span class="mx-ico"><${Icon} n=${isLocal ? 'chip' : 'server'} /></span><div><h2>${name}${!isLocal && html` <span class=${'pill ' + (m.user ? '' : 'green')} title=${m.user ? t('machines.ssh.card_note') : t('machines.pair.card_note')}>${m.user ? t('machines.link.ssh') : t('machines.link.paired')}</span>`}</h2>
        <p class="mono">${isLocal ? (local ? local.user + ' · ' + home(local.home) + ' · ' + local.os : '') : where(m) + (m.home ? ' · ' + m.home : '')}</p></div>
        <span class="grow"></span>
        ${m?.node_id && html`<${LoomUpdates} key=${m.node_id} node=${true} compact=${true} endpoint=${'/api/machines/' + encodeURIComponent(m.id) + '/node/update'} />`}
        ${(isLocal || m.user || (m.modules || []).includes('terminal')) && html`<button class="btn sm" onClick=${() => openTerminalWith({ target, dir: isLocal ? '' : m.home || '', title: name })}><${Icon} n="prompt" />${t("settings.machines.terminal")}</button>`}
        ${!isLocal && html`${m.user && html`<button class="btn sm ghost" onClick=${onEdit}>${t("settings.machines.modifier")}</button>`}<button class="icon-btn" aria-label=${t("settings.machines.retirer") + m.name} onClick=${remove}><${Icon} n="trash" /></button>`}</div></div>
    ${m?.user && html`<${MachineMigration} m=${m} onChange=${onChange} />`}
    ${m?.node_id && html`<${NodeModules} modules=${m.modules} />`}
    <${EngineSection} m=${m} onChange=${onChange} />
    <${FoldersSection} target=${target} m=${m} />
    <${TerminalsSection} target=${target} name=${name} m=${m} />`;
}

// Moteur : sur cette machine, ou le Loom d'une machine distante lié comme moteur.
function EngineSection({ m, onChange }) {
  const node = useStore(app, a => a.engineNode);
  const [direct, setDirect] = useState(false);
  const [pairing, setPairing] = useState(false);
  const [busy, setBusy] = useState(false);
  const isLocal = !m;
  const owns = isLocal ? !node : sameHost(node, m);
  const paired = m && !m.user;
  // A paired machine's credential is already known: no key to copy.
  const useEngine = async () => {
    setBusy(true);
    const r = await post('/api/engine/node', { machine: m.id }, { timeout: 30000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (!r.ok) return toast(r.error || t("settings.machines.liaison_impossible"), 'err');
    toast(t("settings.machines.moteur_de") + r.hostname + t("settings.machines.utilise")); await refreshEngineNode(); refreshStatus(); refreshLibrary();
  };
  const unlink = async () => { await post('/api/engine/node', { unlink: true }); await refreshEngineNode(); refreshStatus(); refreshLibrary(); };
  return html`<${Group} title="${t("settings.machines.moteur")}">
    ${isLocal ? html`<${Line} label="${t("settings.machines.moteur_de_loom")}" tip="${t("settings.machines.le_moteur_llama_cpp_qui_sert_les_modeles_locaux_dans_tes_discussi")}">
        ${owns ? html`<span class="state"><i class="dot green"></i>${t("settings.machines.sur_cette_machine")}</span><a class="btn sm ghost" href="#/engine">${t("settings.machines.reglages_du_moteur")}</a>`
          : html`<span class="state">${t("settings.machines.celui_de")} ${node.hostname}</span><button class="btn sm ghost" onClick=${unlink}>${t("settings.machines.utiliser_celui_de_cette_machine")}</button>`}</${Line}>`
      : owns ? html`<${Line} label="${t("settings.machines.moteur_de_loom")}"><span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.direct ? t("settings.machines.serveur") + node.kind + t("settings.machines.utilise_par_loom") : t("settings.machines.moteur_utilise_par_loom")}</span><a class="btn sm ghost" href="#/engine">${t("settings.machines.reglages_du_moteur")}</a><button class="btn sm ghost" onClick=${unlink}>${t("settings.machines.ne_plus_l_utiliser")}</button></${Line}>`
      : html`<${Line} label="${t("settings.machines.moteur_de_loom")}" tip="${t("settings.machines.si_cette_machine_a_une_carte_graphique_et_loom_installe_loom_peut")}">
          ${!direct && html`<button class="btn sm" onClick=${() => setDirect(true)}>${t("settings.machines.lier_son_serveur_llama_cpp_vllm")}</button>${paired ? html`<button class="btn sm ghost" disabled=${busy} onClick=${useEngine}>${t('machines.node.use_engine')}</button>` : html`<button class="btn sm ghost" onClick=${() => setPairing(true)}>${t("settings.machines.utiliser_le_loom_de_cette_machine")}</button>`}`}</${Line}>
        ${pairing && html`<${PairDialog} start=${{ address: nodeAddress(m), name: m.name, machine_id: m.id }} onClose=${ok => { setPairing(false); if (ok) onChange?.(); }} />`}
        ${direct && html`<${DirectEngineForm} start=${'http://' + m.host + ':8080'} onDone=${() => setDirect(false)} />`}
`}
  </${Group}>
  ${owns && !(node && node.direct) && html`<${ModelDirs} />`}
  ${m && html`<${MachineNodeMaintenance} key=${m.node_id || m.id} m=${m} onChange=${onChange} />`}`;
}

function MachineNodeMaintenance({ m, onChange }) {
  const base = '/api/machines/' + encodeURIComponent(m.id) + '/node';
  const [info, setInfo] = useState(null), [pairing, setPairing] = useState(false);
  const load = () => get(base).then(r => { if (r.ok) setInfo(r); }).catch(() => {});
  useEffect(() => { load(); }, [m.id]);
  return html`<${Group} title=${t('node.maintenance')}>
    <p class="set-note">${t('node.maintenance_note')}</p>
    <${Line} label=${t('node.address')}>
      ${info?.linked ? html`<span class="mono">${info.url}</span>` : html`<span class="muted">${t('machines.node.not_paired')}</span>`}
      <button class="btn sm ghost" onClick=${() => setPairing(true)}>${info?.linked ? t('machines.node.repair') : t('machines.pair.ok')}</button>
    </${Line}>
    ${pairing && html`<${PairDialog} start=${{ address: (info?.url || nodeAddress(m)).replace(/^https?:\/\//, ''), name: m.name, machine_id: m.id }} onClose=${ok => { setPairing(false); if (ok) { load(); onChange?.(); } }} />`}
  </${Group}>`;
}

// Dossiers de travail favoris : proposés pour un harness, un terminal…
function FoldersSection({ target, m }) {
  const [folders, setFolders] = useState(null);
  const [pick, setPick] = useState(false);
  useEffect(() => { get('/api/machines/folders?machine=' + encodeURIComponent(target)).then(r => setFolders(r.folders || [])).catch(() => setFolders([])); }, [target]);
  const save = async next => {
    const r = await post('/api/machines/folders', { machine: target, folders: next });
    if (!r.ok) return toast(r.error || t("settings.machines.enregistrement_impossible"), 'err');
    setFolders(r.folders);
  };
  const add = async () => {
    if (!m) return setPick(true);
    const p = await prompt(t("settings.machines.dossier_sur") + m.name, { placeholder: (m.home || '/home/moi') + t("settings.machines.projet"), ok: t("settings.machines.ajouter") });
    if (p) save([...(folders || []), p]);
  };
  return html`<${Group} title="${t("settings.machines.dossiers_de_travail")}">
    <div class="set-note">${t("settings.machines.proposes_quand_tu_choisis_ou_travaille_un_harness_ou_ou_s_ouvre_u")}${m ? t("settings.machines.sur_3") + m.name : ''}.</div>
    ${(folders || []).map(f => html`<div class="set-line" key=${f}><div class="set-l"><${Icon} n="folder" /><span class="mono path">${home(f)}</span></div>
      <div class="set-c"><button class="btn sm ghost" onClick=${() => openTerminalWith({ target, dir: f })}><${Icon} n="prompt" />${t("settings.machines.terminal")}</button>
        <button class="icon-btn" aria-label="${t("settings.machines.retirer_2")}" onClick=${() => save(folders.filter(x => x !== f))}><${Icon} n="close" /></button></div></div>`)}
    <div class="set-actions"><button class="btn ghost" onClick=${add}><${Icon} n="plus" />${t("settings.machines.ajouter_un_dossier")}</button></div>
    ${pick && typeof FolderPicker === 'function' && html`<${FolderPicker} start="" onClose=${() => setPick(false)} onPick=${d => { setPick(false); save([...(folders || []), d]); }} />`}
  </${Group}>`;
}

function TerminalsSection({ target, name, m }) {
  const [list, setList] = useState(null);
  const paired = m && !m.user && !(m.modules || []).includes('terminal');
  useEffect(() => { get('/api/terminals').then(r => setList((r.terminals || []).filter(localT => localT.target === target))).catch(() => setList([])); }, [target]);
  return html`<${Group} title="${t("settings.machines.terminaux")}">
    ${list && list.length ? list.map(localT => html`<a class="set-line link-line" key=${localT.id} href=${'#/terminals/' + localT.id}><div class="set-l"><i class=${'dot ' + (localT.running ? 'green' : '')}></i><span>${localT.title}</span><span class="muted mono">${home(localT.dir)}</span></div><div class="set-c"><${Icon} n="right" /></div></a>`)
      : html`<div class="set-note">${t("settings.machines.aucun_terminal_ouvert")} ${m ? t("settings.machines.sur") + name : t("settings.machines.sur_cette_machine")}.</div>`}
    ${paired ? html`<div class="set-note">${t('machines.node.terminals_ssh')}</div>`
      : html`<div class="set-actions"><button class="btn ghost" onClick=${() => openTerminalWith({ target, dir: m ? m.home || '' : '', title: name })}><${Icon} n="prompt" />${t("settings.machines.nouveau_terminal")}</button></div>`}
  </${Group}>`;
}
