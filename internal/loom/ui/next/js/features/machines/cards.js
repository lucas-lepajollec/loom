import { html, cls, useState, useEffect, useStore } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { shortVersion } from '../../core/version.js';
import { get } from '../../core/api.js';
import { visibleRefresh } from '../../core/poll.js';
import { app } from '../../core/state.js';
import { Icon } from '../../ui/icons.js';
import { Modal } from '../../ui/dialog.js';
import { CliMark } from '../agents/new-session.js';
import { LoomUpdates } from '../settings/updates.js';
import { openTerminalWith } from '../terminals/page.js';

export const machineHref = id => '#/machines/' + encodeURIComponent(id);
export const machineAPI = id => '/api/machines/' + encodeURIComponent(id);
export const home = path => String(path || '').replace(/^\/home\/[^/]+/, '~');

export function machineBricks(machine, roles) {
  const modules = machine.modules || [];
  return [
    { id: 'engine', label: t('settings.machines.brick_engine'), icon: 'chip' },
    { id: 'voice', label: t('settings.machines.brick_voice'), icon: 'mic' },
    { id: 'codeman', label: t('settings.machines.brick_codeman'), icon: 'agent' },
    { id: 'files', label: t('settings.machines.brick_files'), icon: 'folder' },
  ].map(brick => {
    const provided = roles?.[brick.id] === machine.id;
    let reason = '';
    if (!provided && machine.id !== 'local') {
      if (brick.id === 'codeman') {
        if (!machine.host) reason = t('settings.machines.brick_no_address');
      } else if (!modules.includes(brick.id)) {
        reason = machine.node_id
          ? t('settings.machines.brick_no_module')
          : t('settings.machines.brick_needs_node');
      }
    }
    return { ...brick, provided, reason };
  });
}

const agentTools = new Map([
  ['claude', ['claude', 'Claude Code']],
  ['claude-code', ['claude', 'Claude Code']],
  ['codex', ['codex', 'Codex']],
  ['gemini', ['gemini', 'Gemini CLI']],
  ['agy', ['antigravity', 'Antigravity']],
  ['antigravity', ['antigravity', 'Antigravity']],
  ['copilot', ['copilot', 'Copilot']],
  ['opencode', ['opencode', 'OpenCode']],
  ['pi', ['pi', 'Pi']],
  ['hermes', ['hermes', 'Hermes']],
  ['omp', ['omp', 'Oh My Pi']],
  ['deepseek', ['deepseek', 'DeepSeek']],
  ['grok', ['grok', 'Grok']],
  ['cursor-agent', ['cursor-agent', 'Cursor Agent']],
]);

export function machineAgents(machine, clis) {
  const installed = new Map();
  const tools = Array.isArray(machine.tools) ? machine.tools : [];
  for (const tool of tools) {
    const entry = agentTools.get(tool?.id);
    if (!entry) continue;
    const [id, label] = entry;
    installed.set(id, { id, label, version: tool.version });
  }
  for (const cli of clis) {
    const id = agentTools.get(cli.id)?.[0] || cli.id;
    const tool = installed.get(id);
    if (cli.installed === false || (cli.installed !== true && !cli.version && !tool)) continue;
    installed.set(id, { ...cli, version: cli.version || tool?.version });
  }
  return [...installed.values()];
}

export function AgentName({ cli }) {
  return html`<span class="machine-agent"><${CliMark} mode=${cli.id} size="xs" /><span>${cli.label || cli.id}</span>
    ${cli.version && html`<span class="machine-agent-version mono">${cli.version}</span>`}</span>`;
}

const hasMetrics = metric => metric && !metric.error && metric.supported !== false;

export function useMachineLoom(machine, metric, revision, detail = false) {
  const isLocal = machine.id === 'local';
  const base = machineAPI(machine.id) + '/node';
  const serverInfo = useStore(app, s => s.serverInfo);
  const [node, setNode] = useState(null);
  const [update, setUpdate] = useState(undefined);
  const [ping, setPing] = useState(null);
  const [refresh, setRefresh] = useState(0);
  const readNode = !isLocal && (detail || !!machine.node_id);
  const linked = !!machine.node_id || !!node?.linked;
  const endpoint = isLocal ? '/api/update' : linked ? base + '/update' : '';

  useEffect(() => {
    if (!readNode) return;
    const controller = new AbortController();
    get(base, { signal: controller.signal, timeout: 8000 }).then(setNode).catch(() => {});
    return () => controller.abort();
  }, [readNode, base, machine.node_id, revision]);

  useEffect(() => {
    if (!endpoint) return;
    const controller = new AbortController();
    get(endpoint, { signal: controller.signal }).then(setUpdate).catch(() => {
      if (!controller.signal.aborted) setUpdate(null);
    });
    return () => controller.abort();
  }, [endpoint, refresh, revision]);

  // A node can be up with observation disabled. Use its existing maintenance
  // ping to check reachability when metrics could not be collected.
  const probe = !isLocal && linked && (update === null || (metric !== undefined && !hasMetrics(metric)));
  useEffect(() => {
    if (!probe) return;
    return visibleRefresh(async alive => {
      const result = await get(base + '/update/ping', { timeout: 5000 }).catch(() => null);
      if (alive()) setPing(result ? { version: result.version } : false);
    }, 10000);
  }, [base, probe]);

  return {
    node,
    endpoint,
    version: (probe && ping?.version) || update?.current || node?.version || machine.version || (isLocal ? serverInfo?.version : ''),
    available: update?.available && (!probe || update.latest !== ping?.version) ? update.latest : '',
    canApply: update?.can_apply === true,
    reachable: probe && ping !== null ? !!ping : null,
    refresh: () => setRefresh(n => n + 1),
  };
}

export function MachineStatus({ machine, metric, loom, test }) {
  const recentTest = test?.at && (Date.now() - test.at < 10000 || (!hasMetrics(metric) && !metric?.error && loom.reachable === null));
  const online = machine.id === 'local' ? true
    : recentTest ? test.ok
    : hasMetrics(metric) ? true
    : loom.reachable ?? (metric?.error && !machine.node_id ? false : null);
  return html`<span class=${cls('machine-status', online === true && 'online', online === false && 'offline')} title=${recentTest ? test.error : undefined}>
    <i aria-hidden="true"></i>${online === true ? t('settings.machines.online') : online === false ? t('settings.machines.offline') : t('settings.machines.status_unknown')}
  </span>`;
}

export function MachineVersion({ machine, loom }) {
  const [show, setShow] = useState(false);
  const updateTitle = t('settings.page.mises_a_jour');
  return html`<div class="machine-version">
    <span title=${loom.version}>${t('settings.page.loom')} <span class="mono">${shortVersion(loom.version) || '—'}</span></span>
    ${loom.available && html`<span class="machine-update" title=${loom.available}>${t('settings.machines.update_available')}</span>
      <button type="button" class="btn sm" onClick=${() => setShow(true)}><${Icon} n="download" />${t('settings.page.mettre_a_jour')}</button>`}
    ${show && html`<${Modal} title=${updateTitle} sub=${machine.name} onClose=${() => { setShow(false); loom.refresh(); }}>
      <div class="machine-dialog"><${LoomUpdates} node=${machine.id !== 'local'} endpoint=${loom.endpoint} compact=${loom.canApply} /></div>
    </${Modal}>`}
  </div>`;
}

export function canOpenTerminal(machine) {
  if (machine.id === 'local') return machine.terminalsSupported !== false && machine.os !== 'Windows';
  if (Array.isArray(machine.capabilities)) return machine.capabilities.includes('terminals');
  return !!machine.user || !!machine.modules?.includes('terminal');
}

export function MachineTerminalButton({ machine, dir, small = false }) {
  return html`<button type="button" class=${cls('btn', small && 'sm')} disabled=${!canOpenTerminal(machine)}
    onClick=${() => openTerminalWith({ target: machine.id, dir: dir ?? (machine.id === 'local' ? '' : machine.home), title: dir ? '' : machine.name })}>
    <${Icon} n="terminal" />${t('settings.machines.terminal')}
  </button>`;
}

const GiB = 1024 ** 3;
const gb = value => (value / GiB).toFixed(value >= 100 * GiB ? 0 : 1);
const percent = value => Math.max(0, Math.min(100, Math.round(value)));

function machineSpecs(machine, metric) {
  const gpu = metric?.gpus?.[0];
  const graphics = typeof gpu?.name === 'string' && gpu.name.replace(/^NVIDIA (GeForce )?/, '');
  const memory = metric?.ram_total > 0 && t('settings.machines.ram_suffix', { n: Math.round(metric.ram_total / GiB) });
  const cores = metric?.cores > 0 && t('machines.metrics.cores', { n: metric.cores });
  return [machine.os || metric?.os, graphics || cores, memory].filter(Boolean).join(' · ');
}

function ResourceMeter({ label, value, text, detail }) {
  return html`<div class=${cls('machine-meter', value >= 90 ? 'hot' : value >= 70 && 'warm')}>
    <div><span>${label}</span><span class="mono">${text}</span></div>
    <div class="machine-meter-track" role="meter" aria-label=${label} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${value} aria-valuetext=${text}><i style=${{ width: value + '%' }}></i></div>
    ${detail && html`<small class="muted">${detail}</small>`}
  </div>`;
}

export function ResourceMeters({ metric, detailed = false }) {
  const rows = [];
  const memory = (label, used, total, detail) => {
    if (total > 0 && Number.isFinite(used)) {
      rows.push({ label, value: percent(used * 100 / total), text: gb(used) + ' / ' + gb(total) + t('common.units.gb'), detail });
    }
  };
  if (hasMetrics(metric)) {
    if (Number.isFinite(metric.cpu) && !(metric.partial && metric.cpu === 0)) {
      const cpu = percent(metric.cpu);
      rows.push({ label: t('machines.metrics.cpu'), value: cpu, text: cpu + ' %', detail: detailed && metric.cores ? t('machines.metrics.cores', { n: metric.cores }) : '' });
    }
    memory(t('machines.metrics.ram'), metric.ram_used, metric.ram_total);
    const gpus = Array.isArray(metric.gpus) ? metric.gpus.filter(gpu => gpu?.vram_total > 0) : [];
    for (const gpu of detailed ? gpus : gpus.slice(0, 1)) {
      const info = [gpu.name, Number.isFinite(gpu.util) && percent(gpu.util) + ' %'].filter(Boolean).join(' · ');
      memory(t('settings.machines.gpu_memory'), gpu.vram_used, gpu.vram_total, detailed ? info : '');
    }
    if (detailed) memory(t('machines.metrics.disk'), metric.disk?.used, metric.disk?.total, metric.disk?.path);
  }
  return rows.length
    ? html`<div class="machine-meters">${rows.map((row, index) => html`<${ResourceMeter} key=${index} ...${row} />`)}</div>`
    : html`<p class="machine-note muted" title=${metric?.error}>${t('machines.metrics.none')}</p>`;
}

export function MachineCard({ machine, metric, roles, clis, test, revision }) {
  const loom = useMachineLoom(machine, metric, revision);
  const specs = machineSpecs(machine, metric);
  const bricks = machineBricks(machine, roles).filter(brick => !brick.reason);
  const installed = machineAgents(machine, clis);
  const name = machine.name;
  return html`<article class="machine-card" aria-label=${name}>
    <div class="machine-card-head">
      <span class="machine-icon"><${Icon} n="server" /></span>
      <div>
        <h2><a href=${machineHref(machine.id)}>${name}</a></h2>
        <div class="machine-card-tags">
          ${machine.id === 'local' && html`<span class="machine-interface">${t('settings.machines.interface')}</span>`}
          <${MachineStatus} machine=${machine} metric=${metric} loom=${loom} test=${test} />
        </div>
      </div>
    </div>
    <p class="machine-specs" title=${specs}>${specs || '—'}</p>
    <div class="machine-card-section">
      <h3>${t('settings.machines.bricks')}</h3>
      <div class="machine-chips">${bricks.map(brick => {
        const state = brick.provided ? t('settings.machines.brick_provides') : t('settings.machines.brick_available');
        return html`<span class=${cls('machine-chip', brick.provided && 'provided')} title=${state} key=${brick.id}>
          <${Icon} n=${brick.icon} />${brick.label}<span class="sr">${' · ' + state}</span>
        </span>`;
      })}${!bricks.length && html`<span class="muted">—</span>`}</div>
    </div>
    <div class="machine-card-section">
      <h3>${t('agents.title')}</h3>
      ${installed.length ? html`<div class="machine-chips">${installed.map(cli => html`<${AgentName} key=${cli.id} cli=${cli} />`)}</div>`
        : html`<span class="muted">${t('settings.machines.no_agents')}</span>`}
    </div>
    <${ResourceMeters} metric=${metric} />
    <div class="machine-card-foot">
      <${MachineVersion} machine=${machine} loom=${loom} />
      <div class="machine-actions">
        <${MachineTerminalButton} machine=${machine} />
        <a class="btn ghost" href=${machineHref(machine.id)}>${t('settings.machines.details')}<${Icon} n="right" /></a>
      </div>
    </div>
  </article>`;
}

export function MachineTerminals({ terminals, names, machine }) {
  return html`<section class="machine-section">
    <h2>${t('settings.machines.open_terminals')} ${terminals.length > 0 && html`<span class="count">${terminals.length}</span>`}</h2>
    <div class="card">
      ${terminals.map(term => html`<a class="machine-row machine-terminal-link" key=${term.id} href=${'#/terminals/' + encodeURIComponent(term.id)}>
        <${Icon} n="terminal" />
        <span class="machine-row-text"><b>${term.title}</b><small>${[names.get(term.target) || term.target, term.dir && home(term.dir)].filter(Boolean).join(' · ')}</small></span>
        <span class=${cls('machine-status', term.running && 'online')}><i></i><span class="sr">${term.running ? t('terminals.status.open') : t('terminals.status.finished')}</span></span>
        <${Icon} n="right" />
      </a>`)}
      ${!terminals.length && html`<p class="machine-section-note muted">${t('settings.machines.no_open_terminals')}</p>`}
      ${machine && !canOpenTerminal(machine) && html`<p class="machine-section-note muted">${machine.id === 'local'
        ? t('terminals.page.pas_encore_disponible_sur_ce_systeme') : t('machines.node.terminals_ssh')}</p>`}
    </div>
  </section>`;
}
