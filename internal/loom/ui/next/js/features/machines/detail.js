import { html, cls, useState, useRef, useEffect, useStore } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { app, go, refreshEngineNode, refreshLibrary, refreshStatus, refreshWorkspace } from '../../core/state.js';
import { Icon } from '../../ui/icons.js';
import { OverflowMenu, Switch } from '../../ui/controls.js';
import { Modal, confirm, prompt, toast } from '../../ui/dialog.js';
import { FolderPicker } from '../../ui/folder.js';
import { api as codemanAPI } from '../agents/client.js';
import { ConnectionPanel } from '../agents/connection.js';
import { canInstall } from '../agents/settings.js';
import { addressWithPort, MachineMigration, NodeModules, nodeAddress, PairDialog } from '../settings/machines.js';
import { DirectEngineForm, ModelDirs } from '../settings/page.js';
import {
  AgentName, home, machineAgents, machineAPI, machineBricks, MachineStatus,
  MachineTerminalButton, MachineTerminals, MachineVersion, ResourceMeters, useMachineLoom,
} from './cards.js';

export const MachinesBack = () => html`<a class="btn machine-back" href="#/machines"><${Icon} n="left" />${t('settings.machines.machines')}</a>`;

function MachineSection({ title, children }) {
  return html`<section class="machine-section"><h2>${title}</h2><div class="card">${children}</div></section>`;
}

function FilesRoot({ machine, available, revision }) {
  const [folder, setFolder] = useState(null);
  useEffect(() => {
    if (!available) return;
    const controller = new AbortController();
    get('/api/files/root?machine=' + encodeURIComponent(machine), { signal: controller.signal, timeout: 10000 })
      .then(setFolder).catch(() => { if (!controller.signal.aborted) setFolder(null); });
    return () => controller.abort();
  }, [machine, available, revision]);
  return html`<div class="machine-file-root">
    <small>${t('files.root')}</small>
    ${available && folder?.root
      ? html`<code class="mono">${folder.root}</code>${folder.exists === false && html`<small class="machine-update">${t('settings.machines.files_root_missing')}</small>`}`
      : html`<small>${t('settings.machines.files_root_unavailable')}</small>`}
  </div>`;
}

function Bricks({ machine, roles, revision, onChange, codemanStatus, phone }) {
  const isLocal = machine.id === 'local';
  const [busy, setBusy] = useState('');
  const [direct, setDirect] = useState(false);
  const [codemanURL, setCodemanURL] = useState('');
  const pending = useRef(false);
  const target = machine.id;
  const openCodeman = () => setCodemanURL('http://' + addressWithPort(isLocal ? 'localhost' : machine.host, 3000));
  const assign = async (role, unlink = false) => {
    if (pending.current) return;
    if (role === 'codeman') return openCodeman();
    pending.current = true;
    setBusy(role);
    try {
      const [path, body] = role === 'engine'
        ? ['/api/engine/node', isLocal || unlink ? { unlink: true } : { machine: target }]
        : role === 'voice' ? ['/api/voice/node', { machine: target }]
          : ['/api/machines/roles', { role: 'files', machine: target }];
      const result = await post(path, body);
      if (!result.ok) return toast(result.error || t('settings.machines.liaison_impossible'), 'err');
      if (role === 'engine') {
        await refreshEngineNode();
        refreshStatus();
        refreshLibrary();
      }
      onChange();
    } finally {
      pending.current = false;
      setBusy('');
    }
  };
  return html`<${MachineSection} title=${t('settings.machines.bricks')}>
    ${machineBricks(machine, roles).map(brick => {
      const actions = [];
      if (brick.id === 'files') actions.push({ href: '#/files', label: t('settings.machines.open_files') });
      if (brick.id === 'codeman' && brick.provided) actions.push({ href: '#/agents', label: t('agents.setup.open_agents') });
      if (brick.id === 'engine' && brick.provided) {
        actions.push({ href: '#/engine', label: t('settings.machines.reglages_du_moteur') });
        if (!isLocal) actions.push({ label: t('settings.machines.ne_plus_l_utiliser'), disabled: !!busy, run: () => assign('engine', true) });
      }
      if (brick.id === 'engine' && !brick.provided && !isLocal && machine.host) {
        actions.push({ label: t('settings.machines.link_server'), run: () => setDirect(true) });
      }
      const use = brick.provided ? !phone && html`<span class="state"><i class="dot green"></i>${t('settings.machines.machine_used')}</span>`
        : html`<button class="btn sm" disabled=${!!brick.reason || !!busy} onClick=${() => assign(brick.id)}>
          ${busy === brick.id ? t('settings.machines.verification') : phone ? t('resources.sem.use') : t('settings.machines.use_machine')}
        </button>`;
      return html`<div class="machine-row machine-brick-row" key=${brick.id}>
        ${!phone && html`<span class="logo-mark sm machine-icon" aria-hidden="true"><${Icon} n=${brick.icon} /></span>`}
        <div class="machine-row-text">
          <b>${brick.label}</b>
          <small class=${cls(brick.provided && 'machine-provides')}>${brick.provided
            ? phone ? t('settings.machines.machine_used') : t('settings.machines.brick_provides')
            : brick.reason ? t('settings.machines.brick_unavailable') : t('settings.machines.brick_available')}</small>
          ${brick.reason && html`<small>${brick.reason}</small>`}
          ${brick.id === 'files' && html`<${FilesRoot} machine=${target} available=${!brick.reason} revision=${revision} />`}
          ${brick.id === 'codeman' && brick.provided && codemanStatus?.auth !== 'ok' && codemanStatus
            && html`<small>${t('agents.conn.unreachable')}</small>`}
        </div>
        <div class="machine-actions">
          ${phone ? html`${use}${actions.length > 0 && html`<${OverflowMenu} label=${t('models.row.actions', { name: brick.label })} items=${actions} />`}`
            : html`${actions.map(action => action.href ? html`<a class="btn sm" href=${action.href}>${action.label}</a>`
              : html`<button class="btn sm" disabled=${action.disabled} onClick=${action.run}>${action.label}</button>`)}${use}`}
        </div>
      </div>`;
    })}
  </${MachineSection}>
  ${direct && html`<${DirectEngineForm} start=${'http://' + addressWithPort(machine.host, 8080)}
    dialog=${{ title: t('settings.machines.lier_son_serveur_llama_cpp_vllm'), sub: machine.name, onClose: () => setDirect(false) }}
    onDone=${() => { setDirect(false); onChange(); }} />`}
  ${codemanURL && html`<${Modal} title=${t('agents.conn.title')} sub=${machine.name} onClose=${() => { setCodemanURL(''); onChange(); }}>
    <div class="machine-dialog"><${ConnectionPanel} embedded initialUrl=${codemanURL} /></div>
  </${Modal}>`}`;
}

// Les modules du nœud se règlent ici, depuis Loom : jamais sur la machine.
function NodeModuleSwitches({ machine, modules, onChange }) {
  const [busy, setBusy] = useState('');
  const [local, setLocal] = useState(null);
  const current = local || modules;
  const rows = [
    ['voice', t('settings.machines.brick_voice'), t('machines.module.voice_hint')],
    ['terminal', t('machines.module.terminal_files'), t('machines.module.terminal_hint')],
    ['observe', t('machines.module.observe'), t('machines.module.observe_hint')],
  ];
  const toggle = async (id, enabled) => {
    setBusy(id);
    const r = await post('/api/machines/' + encodeURIComponent(machine.id) + '/node/modules', { module: id, enabled }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (!r.ok) return toast(r.error || t('common.error'), 'err');
    setLocal(r.modules);
    onChange();
  };
  return rows.map(([id, label, hint]) => html`<div class="machine-row" key=${id}>
    <div class="machine-row-text"><b>${label}</b><small>${hint}</small></div>
    <${Switch} label=${label} checked=${current.includes(id)} disabled=${!!busy} onChange=${value => toggle(id, value)} />
  </div>`);
}

function MachineAgents({ machine, clis, onChange }) {
  const installed = machineAgents(machine, clis);
  const installable = clis.filter(cli => canInstall(cli) && !installed.some(installedCli => installedCli.id === cli.id));
  const [busy, setBusy] = useState('');
  const pending = useRef(false);
  const install = async cli => {
    if (pending.current) return;
    pending.current = true;
    try {
      if (!await confirm(t('agents.cfg.cli.install_title', { cli: cli.label }), t('agents.cfg.cli.install_text', { cmd: cli.installCommand }), { ok: t('agents.cfg.cli.install') })) return;
      setBusy(cli.id);
      const { roles } = await get('/api/machines/roles');
      if (roles?.codeman !== machine.id) throw new Error(t('settings.machines.codeman_moved'));
      const result = await codemanAPI.installCli(cli.id);
      if (result?.ok === false) throw new Error(result.error || t('voice.failed'));
      toast(t('agents.cfg.cli.installed', { cli: cli.label }));
    } catch (error) {
      toast(error.message, 'err');
    } finally {
      pending.current = false;
      setBusy('');
      onChange();
    }
  };
  return html`<${MachineSection} title=${t('agents.title')}>
    ${installed.map(cli => {
      const next = cli.latestVersion && cli.latestVersion !== cli.version ? cli.latestVersion : '';
      return html`<div class="machine-row" key=${cli.id}>
        <${AgentName} cli=${cli} />
        ${cli.installCommand && (cli.updateAvailable || next) ? html`<div class="machine-actions">
          <button class="btn sm" disabled=${!!busy} onClick=${() => install(cli)}>${t('settings.page.mettre_a_jour')}${next ? ' · ' + next : ''}</button>
        </div>` : cli.latestVersion && html`<span class="machine-agents-state ok"><${Icon} n="check" />${t('settings.machines.agents_uptodate')}</span>`}
      </div>`;
    })}
    ${!installed.length && html`<p class="machine-section-note muted">${t('settings.machines.no_agents')}</p>`}
    ${installable.length > 0 && html`<details class="machine-install">
      <summary>${t('settings.machines.install_agents')}</summary>
      ${installable.map(cli => html`<div class="machine-row" key=${cli.id}><${AgentName} cli=${cli} />
        <div class="machine-actions"><button class="btn sm" disabled=${!!busy} onClick=${() => install(cli)}>${t('agents.cfg.cli.install')}</button></div>
      </div>`)}
    </details>`}
  </${MachineSection}>`;
}

function MachineConnection({ machine, node, onChange }) {
  const [pairing, setPairing] = useState(false);
  const isLocal = machine.id === 'local';
  const linked = node?.linked || machine.node_id;
  const legacySSH = machine.user && !linked;
  const address = nodeAddress(machine, node);
  const ssh = machine.user && machine.host && machine.user + '@' + addressWithPort(machine.host, machine.port || 22);
  return html`<${MachineSection} title=${t('settings.machines.connection')}>
    ${isLocal ? html`<div class="machine-row">
      <div class="machine-row-text"><b>${t('settings.machines.interface')}</b><small class="mono">${location.host}</small></div>
      ${machine.user && html`<span class="mono">${[machine.user, home(machine.home)].filter(Boolean).join(' · ')}</span>`}
    </div>` : html`
      <div class="machine-row">
        <div class="machine-row-text"><b>${t('settings.machines.ssh')}</b><small class="mono">${ssh || t('settings.machines.ssh_unconfigured')}</small></div>
        ${legacySSH && html`<div class="machine-actions">
          <${MachineMigration} m=${machine} onChange=${onChange} />
        </div>`}
      </div>
      <div class="machine-row">
        <div class="machine-row-text"><b>${t('node.address')}</b><small class="mono">${node?.url || machine.node_url || (machine.node_id ? '—' : t('machines.node.not_paired'))}</small></div>
        ${!legacySSH && html`<div class="machine-actions"><button class="btn sm" onClick=${() => setPairing(true)}>${linked ? t('machines.node.repair') : t('machines.pair.ok')}</button></div>`}
      </div>
      ${linked ? html`<${NodeModuleSwitches} machine=${machine} modules=${machine.modules || node?.modules || []} onChange=${onChange} />`
        : html`<div class="machine-row">
        <div class="machine-row-text"><b>${t('settings.machines.node_modules')}</b></div>
        <${NodeModules} modules=${node?.modules || machine.modules} />
      </div>`}`}
    <div class="machine-row">
      <div class="machine-row-text"><b>${t('startup.title')}</b></div>
      <div class="machine-actions"><a class="btn sm" href="#/settings/startup"><${Icon} n="power" />${t('settings.machines.startup_settings')}</a></div>
    </div>
    ${pairing && html`<${PairDialog} start=${{ address, machine_id: machine.id }} onClose=${result => { setPairing(false); if (result) onChange(); }} />`}
  </${MachineSection}>`;
}

function WorkingFolders({ machine, onChange }) {
  const [saved, setSaved] = useState(null);
  const [pick, setPick] = useState(false);
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const folders = saved ?? (Array.isArray(machine.folders) ? machine.folders : []);
  const save = async next => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    try {
      const result = await post('/api/machines/folders', { machine: machine.id, folders: next });
      if (!result.ok) return toast(result.error || t('settings.machines.enregistrement_impossible'), 'err');
      setSaved(result.folders);
      onChange();
    } finally {
      pending.current = false;
      setBusy(false);
    }
  };
  const add = async () => {
    if (machine.id === 'local') return setPick(true);
    const value = await prompt(t('settings.machines.dossier_sur') + machine.name, {
      placeholder: (machine.home || '') + t('settings.machines.projet'),
      ok: t('settings.machines.ajouter'),
    });
    if (value) save([...folders, value]);
  };
  return html`<${MachineSection} title=${t('settings.machines.dossiers_de_travail')}>
    ${!folders.length && html`<p class="machine-section-note muted">${t('settings.machines.no_folders')}</p>`}
    ${folders.map(folder => html`<div class="machine-row machine-folder" key=${folder}>
      <${Icon} n="folder" />
      <div class="machine-row-text mono" title=${folder}>${home(folder)}</div>
      <div class="machine-actions">
        <${MachineTerminalButton} machine=${machine} dir=${folder} small />
        <button class="icon-btn" aria-label=${t('settings.machines.retirer_2') + ' ' + home(folder)} disabled=${busy} onClick=${() => save(folders.filter(path => path !== folder))}><${Icon} n="close" /></button>
      </div>
    </div>`)}
    <div class="machine-row"><button class="btn sm" disabled=${busy} onClick=${add}><${Icon} n="plus" />${t('settings.machines.ajouter_un_dossier')}</button></div>
    ${pick && html`<${FolderPicker} start="" onClose=${() => setPick(false)} onPick=${folder => { setPick(false); save([...folders, folder]); }} />`}
  </${MachineSection}>`;
}

export function MachineDetail({ machine, metric, roles, clis, codemanStatus, terminals, names, test, onTest, onEdit, onChange, revision, phone }) {
  const loom = useMachineLoom(machine, metric, revision, true);
  const engine = useStore(app, s => s.engineNode);
  const isLocal = machine.id === 'local';
  const address = isLocal ? location.host : loom.node?.url || machine.host;
  const remove = async () => {
    if (!await confirm(t('settings.machines.retirer') + machine.name, t('settings.machines.ses_harnesses_disparaissent_de_loom_les_discussions_deja_faites_r'), { ok: t('settings.machines.retirer_2'), danger: true })) return;
    const result = await post('/api/machines/delete', { id: machine.id });
    if (!result.ok) return toast(result.error || t('settings.machines.suppression_impossible'), 'err');
    onChange();
    refreshWorkspace();
    go('machines');
  };
  const items = isLocal ? [
    { icon: 'info', label: t('settings.page.mises_a_jour'), run: () => go('settings', 'about') },
    { icon: 'power', label: t('startup.title'), run: () => go('settings', 'startup') },
  ] : [
    { icon: 'edit', label: t('settings.machines.modifier'), run: onEdit },
    { icon: 'refresh', label: test?.busy ? t('settings.machines.verification') : t('settings.machines.test_connection'), run: onTest },
    '-',
    { icon: 'trash', label: t('settings.machines.retirer_2'), danger: true, run: remove },
  ];
  const resources = html`<${MachineSection} title=${t('settings.machines.resources')}><div class="machine-resource-body"><${ResourceMeters} metric=${metric} detailed /></div></${MachineSection}>`;
  const version = html`<${MachineVersion} machine=${machine} loom=${loom} />`;
  return html`
    ${!phone && html`<div class="machine-detail-top"><${MachinesBack} /></div>`}
    <header class="machine-detail-hero">
      <div class="machine-detail-head">
        <div class="machine-detail-identity">
          ${!phone && html`<span class="logo-mark machine-icon machine-icon-lg" aria-hidden="true"><${Icon} n="server" /></span>`}
          <div class="machine-row-text">
            <h1 class=${phone ? 'phone-management-title' : undefined}>${machine.name}</h1>
            <div class="machine-card-tags">
              ${isLocal && !phone && html`<span class="machine-interface">${t('settings.machines.interface')}</span>`}
              <${MachineStatus} machine=${machine} metric=${metric} loom=${loom} test=${test} />
              ${phone && version}
              ${!isLocal && html`<span class="tag" title=${machine.user ? t('machines.ssh.card_note') : t('machines.pair.card_note')}>${machine.user ? t('machines.link.ssh') : t('machines.link.paired')}</span>`}
            </div>
            <small class="mono machine-detail-addr">${[address, machine.os].filter(Boolean).join(' · ')}</small>
          </div>
        </div>
        <div class="machine-detail-controls">
          ${!phone && version}
          <div class="machine-actions">
            <${MachineTerminalButton} machine=${machine} />
            <${OverflowMenu} label=${t('settings.machines.more_actions')} items=${items} />
          </div>
        </div>
      </div>
    </header>
    ${resources}
    <div class="machine-detail-grid">
      <div class="machine-detail-col">
        <${Bricks} machine=${machine} roles=${roles} revision=${revision} onChange=${onChange} codemanStatus=${codemanStatus} phone=${phone} />
        ${roles?.engine === machine.id && (isLocal ? !engine : engine && !engine.direct) && html`<${ModelDirs} key=${engine?.url || 'local'} />`}
        <${WorkingFolders} machine=${machine} onChange=${onChange} />
      </div>
      <div class="machine-detail-col">
        <${MachineAgents} machine=${machine} clis=${clis} onChange=${onChange} />
        <${MachineConnection} machine=${machine} node=${loom.node} onChange=${onChange} />
        <${MachineTerminals} machine=${machine} terminals=${terminals} names=${names} />
      </div>
    </div>
  `;
}
