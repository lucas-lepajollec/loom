import { t } from '../core/i18n.js';
import { html, useState, useEffect, useRef, cls } from '../core/lib.js';
import { authAction } from '../core/api.js';
import { Icon, LoomMark } from '../ui/icons.js';
import { Modal } from '../ui/dialog.js';

// One password per Loom. No credential is written to browser storage.
export function PasswordForm({ mode = 'login', needsKey = false, onDone, title, onClose }) {
  const [password, setPassword] = useState('');
  const [current, setCurrent] = useState('');
  const [repeat, setRepeat] = useState('');
  const [key, setKey] = useState('');
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const first = useRef(null);
  const running = useRef(false);
  useEffect(() => { if (!title) first.current?.focus(); }, []);
  const submit = async e => {
    e.preventDefault();
    if (running.current) return;
    if (mode !== 'login' && password !== repeat) { setError(t('access.mismatch')); return; }
    running.current = true; setBusy(true); setError('');
    try {
      await authAction(mode === 'login' ? 'login' : 'password',
        mode === 'login' ? { password } : { password, current_password: current }, key);
      setPassword(''); setCurrent(''); setRepeat(''); setKey('');
      onDone();
    } catch (err) { setError(err.message); }
    finally { running.current = false; setBusy(false); }
  };
  const field = (label, name, value, setter, autocomplete, ref, minLength, toggleable = false) => html`<label class="access-field">
    <span class="access-label">${label}</span>
    <span class="access-input-wrap">
      <input class="input" ref=${ref} type=${toggleable && show ? 'text' : 'password'} name=${name} value=${value}
        required minlength=${minLength} maxlength="1024" autocomplete=${autocomplete}
        onInput=${e => setter(e.target.value)} />
      ${toggleable && html`<button type="button" class=${cls('icon-btn access-eye', show && 'on')}
        aria-label=${show ? t('access.hide_password') : t('access.show_password')}
        aria-pressed=${String(show)} onClick=${() => setShow(s => !s)}><${Icon} n="eye" /></button>`}
    </span></label>`;
  const save = html`<button class=${cls('btn primary', !title && 'lg access-submit')} type="submit" form=${title ? 'access-password-form' : undefined} disabled=${busy}>
    ${busy && html`<span class="spinner"></span>`}<span>${busy ? t('access.wait') : mode === 'login' ? t('access.sign_in') : t('access.save')}</span></button>`;
  const form = html`<form id=${title ? 'access-password-form' : undefined} class="access-form" onSubmit=${submit}>
    ${needsKey && field(t('access.existing_key'), 'existing-key', key, setKey, 'off', first)}
    ${mode === 'change' && field(t('access.current'), 'current-password', current, setCurrent, 'current-password', first)}
    ${field(mode === 'login' ? t('access.password') : t('access.new'), 'password', password, setPassword,
      mode === 'login' ? 'current-password' : 'new-password', mode === 'change' || needsKey ? null : first, mode === 'login' ? undefined : 12, true)}
    ${mode !== 'login' && field(t('access.confirm'), 'confirm-password', repeat, setRepeat, 'new-password')}
    ${mode !== 'login' && html`<p class="access-note">${t('access.minimum')}</p>`}
    ${error && html`<div class="access-err" role="alert"><${Icon} n="alert" /><span>${error}</span></div>`}
    ${!title && html`<div class="access-actions">${save}</div>`}
  </form>`;
  return title ? html`<${Modal} title=${title} onClose=${busy ? undefined : onClose}
    foot=${html`<button type="button" class="btn" disabled=${busy} onClick=${onClose}>${t('ui.dialog.annuler')}</button>${save}`}>
    ${form}
  </${Modal}>` : form;
}

export function AccessScreen({ status, onDone }) {
  return html`<div class="access-screen">
    <div class="access-shell anim-rise">
      <header class="access-head">
        <span class="access-mark" aria-hidden="true"><${LoomMark} size=${30} /></span>
        <h1>Loom</h1>
        <p>${status.password_set ? t('access.enter_password') : t('access.setup_once')}</p>
      </header>
      <div class="card access-card">
        <${PasswordForm} mode=${status.password_set ? 'login' : 'setup'} needsKey=${!status.password_set && status.required} onDone=${onDone} />
      </div>
      <p class="access-foot">${t('access.local_note')}</p>
    </div>
  </div>`;
}
