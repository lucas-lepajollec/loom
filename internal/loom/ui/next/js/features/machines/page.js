import { html, useState, useEffect, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { visibleRefresh } from '../../core/poll.js';
import { go } from '../../core/state.js';
import { Icon } from '../../ui/icons.js';
import { Empty } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { Discovered, PairDialog } from '../settings/machines.js';
import { MachineDialog } from './edit.js';
import { MachineCard, MachineTerminals } from './cards.js';
import { MachineDetail, MachinesBack } from './detail.js';

function useMachines() {
  const [remote, setRemote] = useState(null);
  const [local, setLocal] = useState(null);
  const [roles, setRoles] = useState(null);
  const [metrics, setMetrics] = useState(null);
  const [terminals, setTerminals] = useState(null);
  const [codeman, setCodeman] = useState(null);
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  const reload = () => setRevision(n => n + 1);
  const provider = roles?.codeman;

  useEffect(() => {
    const controller = new AbortController();
    const options = { signal: controller.signal };
    get('/api/machines', options).then(result => {
      setRemote(result.machines || []);
      setError('');
    }).catch(error => { if (!controller.signal.aborted) setError(error.message); });
    get('/api/machines/local', options).then(setLocal).catch(() => {});
    get('/api/machines/roles', options).then(result => {
      if (['engine', 'voice', 'codeman', 'files'].every(role => typeof result.roles?.[role] === 'string')) setRoles(result.roles);
    }).catch(() => {});
    return () => controller.abort();
  }, [revision]);

  useEffect(() => {
    if (!provider) {
      setCodeman(null);
      return;
    }
    const controller = new AbortController();
    const options = { signal: controller.signal, timeout: 10000 };
    get('/api/codeman/status', options).then(async status => {
      const result = status.auth === 'ok' ? await get('/api/codeman/v1/clis', options).catch(() => null) : null;
      if (controller.signal.aborted) return;
      const clis = Array.isArray(result?.data) ? result.data : Array.isArray(result) ? result : [];
      setCodeman({ provider, status, clis: clis.filter(cli => typeof cli?.id === 'string' && cli.kind !== 'shell' && cli.id !== 'shell') });
    }).catch(() => { if (!controller.signal.aborted) setCodeman(null); });
    return () => controller.abort();
  }, [provider, revision]);

  useEffect(() => visibleRefresh(async alive => {
    await Promise.all([
      get('/api/machines/metrics', { timeout: 9000 }).then(result => {
        if (alive()) setMetrics(result.metrics || {});
      }).catch(() => { if (alive()) setMetrics({}); }),
      get('/api/terminals', { timeout: 9000 }).then(result => {
        if (alive()) setTerminals(result);
      }).catch(() => {}),
    ]);
  }, 10000), []);

  return { remote, setRemote, local, roles, metrics, terminals, codeman, error, revision, reload };
}

export function MachinesPage({ route }) {
  const { remote, setRemote, local, roles, metrics, terminals, codeman, error, revision, reload } = useMachines();
  const [pair, setPair] = useState(null);
  const [edit, setEdit] = useState(null);
  const [tests, setTests] = useState({});
  const testing = useRef(new Set());
  const here = { ...local, id: 'local', name: local?.hostname || t('settings.machines.cette_machine_2'), terminalsSupported: terminals?.supported };
  const machines = [here, ...(remote || []).filter(m => typeof m.id === 'string' && m.id !== 'local').map(m => ({ ...m, name: m.name || m.hostname || m.host || m.id }))];
  const byId = new Map(machines.map(machine => [machine.id, machine]));
  const names = new Map(machines.map(machine => [machine.id, machine.name]));
  const target = route.sub;
  const machine = byId.get(target);
  const open = (terminals?.terminals || []).filter(term => typeof term.id === 'string');

  const saveMachine = saved => {
    if (!saved?.id) return;
    setRemote(old => {
      const list = old || [];
      return list.some(machine => machine.id === saved.id)
        ? list.map(machine => machine.id === saved.id ? { ...machine, ...saved } : machine)
        : [...list, saved];
    });
  };

  const testMachine = async machine => {
    const id = machine.id;
    if (testing.current.has(id)) return;
    testing.current.add(id);
    setTests(old => ({ ...old, [id]: { ...old[id], busy: true } }));
    try {
      const result = await post('/api/machines', { machine, check_only: true });
      const error = result.ok ? '' : result.error || t('machines.edit.connexion_impossible');
      if (result.ok) saveMachine(result.machine);
      setTests(old => ({ ...old, [id]: { ok: result.ok, error, at: Date.now() } }));
      toast(error || machine.name + ' · ' + t('settings.machines.connection_ok'), error ? 'err' : undefined);
    } finally {
      testing.current.delete(id);
    }
  };

  const observations = machine => ({
    machine,
    roles,
    metric: metrics ? metrics[machine.id] || null : undefined,
    clis: codeman?.provider === machine.id ? codeman.clis : [],
    test: tests[machine.id],
    revision,
  });

  return html`<div class="view page"><div class=${target ? 'page-in mc-page machine-detail' : 'page-in mc-page'}>
    ${target ? machine ? html`<${MachineDetail} key=${machine.id} ...${observations(machine)} names=${names}
        terminals=${open.filter(term => term.target === machine.id)}
        codemanStatus=${codeman?.provider === machine.id ? codeman.status : null}
        onTest=${() => testMachine(machine)} onEdit=${() => setEdit({ machine })} onChange=${reload} />`
      : html`<${MachinesBack} />
        ${remote === null && !error ? html`<div class="skeleton machine-loading"></div>`
          : html`<${Empty} icon="server" title=${error ? t('settings.machines.load_failed') : t('settings.machines.machine_introuvable')} text=${error} />`}`
    : html`<div class="page-head">
      <h1>${t('settings.machines.machines')}</h1>
      <div class="acts">
        <button class="btn primary" onClick=${() => setPair({})}><${Icon} n="plus" />${t('machines.add.title')}</button>
      </div>
    </div>
    <div class="machine-grid">
      ${machines.map(machine => html`<${MachineCard} key=${machine.id} ...${observations(machine)} />`)}
    </div>
    ${error && html`<div class="machine-row machine-load-error" role="status"><div class="machine-row-text">
      <b>${t('settings.machines.load_failed')}</b><small>${error}</small></div><button class="btn sm" onClick=${reload}>${t('agents.retry')}</button>
    </div>`}
    ${open.length > 0 && html`<${MachineTerminals} terminals=${open} names=${names} />`}
    ${!pair && html`<${Discovered} onPair=${setPair} extra=${html`<button class="btn sm ghost" title=${t('settings.machines.ssh_add_tip')} onClick=${() => setEdit({ machine: null })}><${Icon} n="terminal" />${t('settings.machines.ssh_add_plain')}</button>`} />`}`}
    ${pair && html`<${PairDialog} start=${pair} onClose=${result => {
      setPair(null);
      if (result?.id) { saveMachine(result); reload(); go('machines', result.id); }
    }} />`}
    ${edit && html`<${MachineDialog} machine=${edit.machine} onClose=${result => {
      setEdit(null);
      if (result?.id) { saveMachine(result); reload(); }
    }} />`}
  </div></div>`;
}
