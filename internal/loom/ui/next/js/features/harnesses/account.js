import { html, useState, useRef, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Modal } from '../../ui/dialog.js';
import { TermView } from '../terminals/page.js';

// Native account setup stays on this page. The CLI owns its OAuth credentials.
// Codex exposes a device-login protocol; other CLIs retain their own interactive
// flows, rendered by the existing terminal rather than an invented OAuth client.
export function HarnessAccount({ rt, onClose, onConnect, onChanged }) {
  const [login, setLogin] = useState(null);
  const [terminal, setTerminal] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState('');
  const resource = useRef(null);
  const alive = useRef(true);
  const changed = useRef(false);
  const pending = useRef(false);
  const base = '/api/runtimes/' + encodeURIComponent(rt.id);
  const dispose = async () => {
    const owned = resource.current; resource.current = null;
    if (owned?.job) await post(base + '/account', { job: owned.job, cancel: true }).catch(() => {});
    if (owned?.terminal) await post('/api/terminals/close', { id: owned.terminal }).catch(() => {});
  };
  useEffect(() => () => { alive.current = false; dispose(); }, []);
  const native = async () => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true); setError('');
    try {
      await dispose(); setLogin(null); setTerminal(null);
      const plan = await get(base + '/login');
      if (!plan.ok) throw new Error(plan.error);
      const bytes = new Uint8Array(16); crypto.getRandomValues(bytes);
      const r = await post('/api/terminals', { target: plan.target, command: plan.command, title: plan.title,
        request_id: Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('') });
      if (!r.ok) throw new Error(r.error);
      resource.current = { terminal: r.terminal.id };
      if (!alive.current) { await dispose(); return; }
      setTerminal(r.terminal); setNote(rt.id === 'gemini' || rt.logo === 'gemini' ? t('harnesses.account.google_note') : t('harnesses.account.native_note'));
    } catch (e) { if (alive.current) setError(e.message); }
    finally { pending.current = false; if (alive.current) setBusy(false); }
  };
  const device = async () => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true); setError('');
    try {
      await dispose(); setLogin(null); changed.current = false;
      const r = await post(base + '/account', { consent: true });
      if (!r.ok) throw new Error(r.error);
      resource.current = { job: r.login.id };
      if (!alive.current) { await dispose(); return; }
      setLogin(r.login);
    } catch (e) { if (alive.current) setError(e.message); }
    finally { pending.current = false; if (alive.current) setBusy(false); }
  };
  useEffect(() => {
    if (!login || !['starting', 'waiting'].includes(login.state)) return;
    let stopped = false, timer;
    const poll = async () => {
      try {
        const r = await get(base + '/account?job=' + encodeURIComponent(login.id));
        if (stopped || !alive.current) return;
        if (!r.ok) { setError(r.error); return; }
        setLogin(r.login);
        if (r.login.state === 'completed' && !changed.current) { changed.current = true; onChanged(); }
      } catch (e) { if (!stopped && alive.current) setError(e.message); }
      if (!stopped && alive.current) timer = setTimeout(poll, 1500);
    };
    timer = setTimeout(poll, 500);
    return () => { stopped = true; clearTimeout(timer); };
  }, [login?.id, login?.state]);
  const active = login && ['starting', 'waiting'].includes(login.state);
  const labels = { starting: t('harnesses.account.starting'), waiting: t('harnesses.account.waiting'), completed: t('harnesses.account.completed'), error: t('harnesses.account.error'), expired: t('harnesses.account.expired'), cancelled: t('harnesses.account.cancelled') };
  return html`<${Modal} title=${t('harnesses.connection.login') + ' · ' + rt.name} sub=${t('harnesses.account.private')} wide=${!!terminal} onClose=${onClose}
    foot=${html`<button class="btn ghost" onClick=${onClose}>${t('ui.dialog.fermer')}</button><button class="btn primary" disabled=${busy || active} onClick=${async () => { await dispose(); onChanged(); onClose(); onConnect(); }}>${t('harnesses.account.verify_connect')}</button>`}>
    ${!login && !terminal && html`<p class="note">${t('harnesses.account.choose')}</p>`}
    <div class="acts">${(rt.id === 'codex' || rt.logo === 'codex') && html`<button class="btn primary" disabled=${busy || active || !!terminal} onClick=${device}>${t('harnesses.account.chatgpt')}</button>`}
      <button class="btn" disabled=${busy || !!terminal} onClick=${native}>${t('harnesses.account.native')}</button></div>
    ${error && html`<p class="note err" role="alert">${error}</p>`}
    ${login && html`<div class="card pad" role="status"><p>${labels[login.state] || ''}</p>
      ${login.state === 'waiting' && html`<p><a class="btn primary" href=${login.url} target="_blank" rel="noopener noreferrer">${t('harnesses.account.open_browser')}</a></p><p><code class="mono">${login.code}</code></p><p class="note">${t('harnesses.account.device_note')}</p>`}
      ${login.error && html`<p class="note err">${login.error}</p>`}</div>`}
    ${terminal && html`<p class="note">${note}</p><div style="display:grid;height:55vh;min-height:260px"><${TermView} t=${terminal} onExit=${() => { if (alive.current) onChanged(); }} /></div>`}
    <p class="note">${t('harnesses.account.separate')}</p>
  </${Modal}>`;
}
