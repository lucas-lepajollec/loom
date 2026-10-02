import { t } from '../core/i18n.js';
import { html, useState, useEffect, useRef } from '../core/lib.js';
import { authAction } from '../core/api.js';
import { Icon } from '../ui/icons.js';

// One password per Loom. No credential is written to browser storage.
export function PasswordForm({ mode = 'login', needsKey = false, onDone }) {
  const [password, setPassword] = useState('');
  const [current, setCurrent] = useState('');
  const [repeat, setRepeat] = useState('');
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const first = useRef(null);
  const running = useRef(false);
  useEffect(() => { first.current?.focus(); }, []);
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
  const field = (label, name, value, setter, autocomplete, ref, minLength) => html`<label class="set-line stack">
    <span class="set-l">${label}</span><input class="input" ref=${ref} type="password" name=${name} value=${value}
      required minlength=${minLength} maxlength="1024" autocomplete=${autocomplete} onInput=${e => setter(e.target.value)} /></label>`;
  return html`<form onSubmit=${submit}>
    ${needsKey && field(t('access.existing_key'), 'existing-key', key, setKey, 'off', first)}
    ${mode === 'change' && field(t('access.current'), 'current-password', current, setCurrent, 'current-password', first)}
    ${field(mode === 'login' ? t('access.password') : t('access.new'), 'password', password, setPassword,
      mode === 'login' ? 'current-password' : 'new-password', mode === 'change' || needsKey ? null : first, mode === 'login' ? undefined : 12)}
    ${mode !== 'login' && field(t('access.confirm'), 'confirm-password', repeat, setRepeat, 'new-password')}
    ${mode !== 'login' && html`<p class="set-note">${t('access.minimum')}</p>`}
    ${error && html`<p class="set-note" role="alert">${error}</p>`}
    <div class="set-actions"><button class="btn primary" type="submit" disabled=${busy}>${busy ? t('access.wait') : mode === 'login' ? t('access.sign_in') : t('access.save')}</button></div>
  </form>`;
}

export function AccessScreen({ status, onDone }) {
  return html`<div class="welcome" style="min-height:100dvh">
    <div class="welcome-mark"><${Icon} n="lock" /></div><h1>Loom</h1>
    <p>${status.password_set ? t('access.enter_password') : t('access.setup_once')}</p>
    <div class="card" style="width:min(420px,100%);text-align:left">
      <${PasswordForm} mode=${status.password_set ? 'login' : 'setup'} needsKey=${!status.password_set && status.required} onDone=${onDone} />
    </div>
  </div>`;
}
