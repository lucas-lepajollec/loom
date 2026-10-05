import { html, useState, useEffect, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Switch } from '../../ui/controls.js';
import { toast } from '../../ui/dialog.js';
import { Line, Group } from './kit.js';

export function StartupSettings() {
  const [machines, setMachines] = useState([]), [machine, setMachine] = useState('');
  const [state, setState] = useState(null), [node, setNode] = useState(null);
  const [error, setError] = useState(''), [busy, setBusy] = useState(false);
  const [engine, setEngine] = useState('off'), [model, setModel] = useState('');
  const generation = useRef(0);
  useEffect(() => { const ac = new AbortController(); get('/api/machines', { signal: ac.signal }).then(r => { if (!ac.signal.aborted && r.ok) setMachines(r.machines || []); }).catch(() => {}); return () => ac.abort(); }, []);
  const serviceURL = machine ? '/api/machines/' + encodeURIComponent(machine) + '/startup' : '/api/startup';
  const nodeURL = machine ? '/api/machines/' + encodeURIComponent(machine) + '/node/startup' : '/api/startup';
  useEffect(() => {
    const ac = new AbortController(), id = ++generation.current;
    setState(null); setNode(null); setError(''); setBusy(false);
    get(serviceURL, { signal: ac.signal, timeout: 20000 }).then(r => { if (id !== generation.current || ac.signal.aborted) return; if (r.ok) setState(r); else setError(r.error); }).catch(e => { if (!ac.signal.aborted) setError(e.message); });
    get(nodeURL, { signal: ac.signal, timeout: 20000 }).then(r => {
      if (id !== generation.current || ac.signal.aborted || !r.ok) return;
      setNode(r); setEngine(r.policy?.engine || 'off'); setModel(r.policy?.model || '');
    }).catch(() => {});
    return () => { ac.abort(); generation.current++; };
  }, [machine]);
  const save = async (url, payload, policy = false) => {
    if (busy) return; const id = generation.current; setBusy(true); setError('');
    try {
      const r = await post(url, payload, { timeout: 20000 });
      if (id !== generation.current) return;
      if (!r.ok) { setError(r.error); return; }
      if (policy) setNode(r); else setState(r);
      toast(t('startup.saved'));
    } finally { if (id === generation.current) setBusy(false); }
  };
  return html`<${Group} title=${t('startup.services')}>
    <${Line} label=${t('startup.machine')}><select class="select" disabled=${busy} value=${machine} onChange=${e => setMachine(e.target.value)}><option value="">${t('history.local')}</option>${machines.map(m => html`<option key=${m.id} value=${m.id}>${m.name}</option>`)}</select></${Line}>
    ${error && html`<div class="pad notice warn" role="alert">${error}</div>`}
    ${!state ? html`<div class="pad"><p class="note">${error ? t('startup.retry') : t('startup.loading')}</p></div>` : !state.supported ? html`<div class="pad"><p class="note">${t('startup.unsupported')}</p></div>` : html`
      ${(state.services || []).map(s => html`<${Line} key=${s.id} label=${({ ui: t('startup.service.ui'), engine: t('startup.service.engine'), node: t('startup.service.node') })[s.id] || s.unit} tip=${s.unit + ' · ' + (s.user ? t('startup.user_service') : t('startup.system_service'))}>
        ${s.installed ? html`<${Switch} label=${s.unit} checked=${s.enabled} disabled=${busy} onChange=${enabled => save(serviceURL, { service: s.id, enabled })} />` : html`<span class="note">${t('startup.not_installed')}</span>`}
      </${Line}>`)}
      <div class="pad"><p class="note">${t('startup.service_hint')}</p>${(state.services || []).some(s => s.user && s.installed) && state.linger !== true && html`<p class="note">${t('startup.linger')}</p>`}</div>
    `}
    ${node?.supported && node.policy ? html`
      <${Line} label=${t('startup.engine')} tip=${t('startup.engine_hint')}><select class="select" disabled=${busy} value=${engine} onChange=${e => setEngine(e.target.value)}><option value="off">${t('startup.off')}</option>${node.node && html`<option value="llama.cpp">llama.cpp</option>`}<option value="vllm">vLLM</option></select></${Line}>
      ${engine === 'vllm' && html`<${Line} label=${t('startup.model')} stack><input class="input" disabled=${busy} value=${model} onInput=${e => setModel(e.target.value)} placeholder="org/model" /></${Line}>`}
      <${Line} label=${t('startup.policy')} tip=${t('startup.no_generation')}><button class="btn sm" disabled=${busy} onClick=${() => save(nodeURL, { policy: { engine, model: engine === 'vllm' ? model : '' } }, true)}>${t('settings.page.enregistrer')}</button></${Line}>
    ` : machine && html`<div class="pad"><p class="note">${t('startup.link_node')}</p></div>`}
  </${Group}>`;
}
