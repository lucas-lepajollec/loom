import { html, useState, useEffect, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Switch } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { toast } from '../../ui/dialog.js';
import { Line, Group } from './kit.js';

// Démarrage, machine par machine, en deux questions :
// 1. qu'est-ce qui se lance quand la machine démarre (Loom, son nœud) ?
// 2. quel moteur démarre, et avec quel modèle ?
// Sur le Loom principal, « llama.cpp au démarrage » est le service moteur et
// « vLLM au démarrage » une politique enregistrée : l'utilisateur ne voit qu'un
// seul choix, et seulement les moteurs réellement installés sur la machine.
export function StartupSettings() {
  const [machines, setMachines] = useState([]), [machine, setMachine] = useState('');
  const [state, setState] = useState(null), [node, setNode] = useState(null);
  const [error, setError] = useState(''), [busy, setBusy] = useState(false);
  const generation = useRef(0);
  useEffect(() => { const ac = new AbortController(); get('/api/machines', { signal: ac.signal }).then(r => { if (!ac.signal.aborted && r.ok) setMachines(r.machines || []); }).catch(() => {}); return () => ac.abort(); }, []);
  const serviceURL = machine ? '/api/machines/' + encodeURIComponent(machine) + '/startup' : '/api/startup';
  const nodeURL = machine ? '/api/machines/' + encodeURIComponent(machine) + '/node/startup' : '/api/startup';
  useEffect(() => {
    const ac = new AbortController(), id = ++generation.current;
    setState(null); setNode(null); setError(''); setBusy(false);
    get(serviceURL, { signal: ac.signal, timeout: 20000 }).then(r => { if (id !== generation.current || ac.signal.aborted) return; if (r.ok) setState(r); else setError(r.error); }).catch(e => { if (!ac.signal.aborted) setError(e.message); });
    get(nodeURL, { signal: ac.signal, timeout: 20000 }).then(r => { if (id === generation.current && !ac.signal.aborted && r.ok) setNode(r); }).catch(() => {});
    return () => { ac.abort(); generation.current++; };
  }, [machine]);
  const call = async (url, payload) => {
    const r = await post(url, payload, { timeout: 20000 }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) throw new Error(r.error || t('startup.not_saved'));
    return r;
  };
  const run = async steps => {
    if (busy) return; const id = generation.current; setBusy(true); setError('');
    try { await steps(); if (id === generation.current) toast(t('startup.saved')); }
    catch (e) { if (id === generation.current) setError(e.message); }
    finally {
      if (id === generation.current) {
        setBusy(false);
        get(serviceURL, { timeout: 20000 }).then(r => r.ok && setState(r)).catch(() => {});
        get(nodeURL, { timeout: 20000 }).then(r => r.ok && setNode(r)).catch(() => {});
      }
    }
  };
  const services = (state && state.services) || [];
  const svc = id => services.find(s => s.id === id);
  const engineService = svc('engine');
  // Le moteur llama.cpp du Loom principal est un service ; ailleurs, une politique du nœud.
  const controlPlane = node && !node.node && !machine;
  const policy = (node && node.policy) || { engine: 'off', model: '' };
  const engines = (node && node.engines) || { llama: false, vllm: false, vllm_models: [] };
  const current = controlPlane && engineService && engineService.enabled ? 'llama.cpp' : policy.engine || 'off';
  const options = [{ value: 'off', label: t('startup.engine_off') },
    ...(engines.llama || current === 'llama.cpp' ? [{ value: 'llama.cpp', label: 'llama.cpp', note: t('startup.engine_llama_note') }] : []),
    ...(engines.vllm || current === 'vllm' ? [{ value: 'vllm', label: 'vLLM', note: t('startup.engine_vllm_note') }] : [])];
  const choose = (engine, model = '') => run(async () => {
    if (controlPlane) {
      if (engine !== 'llama.cpp' && engineService && engineService.enabled) await call(serviceURL, { service: 'engine', enabled: false });
      if (engine !== 'vllm' && policy.engine === 'vllm') await call(nodeURL, { policy: { engine: 'off', model: '' } });
      if (engine === 'llama.cpp') await call(serviceURL, { service: 'engine', enabled: true });
      if (engine === 'vllm') await call(nodeURL, { policy: { engine, model } });
    } else {
      await call(nodeURL, { policy: { engine, model: engine === 'vllm' ? model : '' } });
    }
  });
  const label = { ui: t('startup.service.ui'), node: t('startup.service.node') };
  const bootServices = services.filter(s => s.id !== 'engine' && s.installed);
  const machineName = machine ? (machines.find(m => m.id === machine) || {}).name : t('history.local');
  return html`
    <${Group} title=${t('startup.machine')}>
      <${Line} label=${t('startup.machine')} tip=${t('startup.machine_tip')}><div class="set-pick"><${ListPick} label=${t('startup.machine')} disabled=${busy} value=${machine} onChange=${setMachine} options=${[{ value: '', label: t('history.local') }, ...machines.map(m => ({ value: m.id, label: m.name }))]} /></div></${Line}>
      ${error && html`<div class="pad notice warn" role="alert">${error}</div>`}
    </${Group}>
    ${!state && !node ? html`<${Group} title=${t('startup.boot')}><div class="pad"><p class="note">${error ? t('startup.retry') : t('startup.loading')}</p></div></${Group}>`
      : state && !state.supported && !node ? html`<${Group} title=${t('startup.boot')}><div class="pad"><p class="note">${t('startup.unsupported')}</p></div></${Group}>` : html`
      <${Group} title=${t('startup.boot')}>
        ${bootServices.map(s => html`<${Line} key=${s.id} label=${label[s.id] || s.unit} tip=${t('startup.service_tip', { unit: s.unit })}>
          <${Switch} label=${label[s.id] || s.unit} checked=${s.enabled} disabled=${busy} onChange=${enabled => run(() => call(serviceURL, { service: s.id, enabled }))} /></${Line}>`)}
        ${!bootServices.length && html`<div class="pad"><p class="note">${t('startup.no_service', { machine: machineName })}</p></div>`}
        ${bootServices.some(s => s.user) && state && state.linger === false && html`<div class="pad"><p class="note">${t('startup.linger')}</p></div>`}
      </${Group}>
      <${Group} title=${t('startup.engine_group')}>
        ${node && node.supported !== false && node.policy ? html`
          <${Line} label=${t('startup.engine')} tip=${t('startup.engine_tip')}><div class="set-pick"><${ListPick} label=${t('startup.engine')} disabled=${busy} value=${current}
            onChange=${v => v === 'vllm' ? choose('vllm', policy.model || (engines.vllm_models || [])[0] || '') : choose(v)} options=${options} /></div></${Line}>
          ${current === 'vllm' && html`<${Line} label=${t('startup.model')}>${(engines.vllm_models || []).length
            ? html`<div class="set-pick"><${ListPick} label=${t('startup.model')} disabled=${busy} value=${policy.model} onChange=${m => choose('vllm', m)} options=${engines.vllm_models.map(m => ({ value: m, label: m }))} /></div>`
            : html`<span class="note">${t('startup.no_vllm_model')}</span>`}</${Line}>`}
          ${!engines.llama && !engines.vllm && current === 'off' && html`<div class="pad"><p class="note">${t('startup.no_engine', { machine: machineName })}</p></div>`}
          <div class="pad"><p class="note">${t('startup.engine_note')}</p></div>`
        : html`<div class="pad"><p class="note">${machine ? t('startup.link_node') : t('startup.loading')}</p></div>`}
      </${Group}>`}`;
}
