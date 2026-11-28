import { html, useState } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { post } from '../../core/api.js';
import { Viewport } from './viewport.js';
import { appPath } from './navigation.js';

export function WebView({ item, grant }) {
  const initial = appPath(item.path || '/') || '/';
  const [history, setHistory] = useState([initial]), [index, setIndex] = useState(0);
  const [input, setInput] = useState(initial), [src, setSrc] = useState(grant.url), [revision, setRevision] = useState(0);
  const [error, setError] = useState('');
  function visit(path, i) {
    if (!appPath(path)) { setError(t('preview.bad_path')); return; }
    setInput(path); setError(''); setIndex(i);
    setSrc(grant.preview.origin + path); setRevision(r => r + 1);
  }
  async function external() {
    const tab = window.open('about:blank', '_blank');
    if (!tab) { setError(t('previews.popup_blocked')); return; }
    tab.opener = null;
    const r = await post('/api/previews', { action: 'ticket', id: grant.preview.id, path: history[index], consent: true });
    if (r.ok) tab.location.replace(r.url);
    else { tab.close(); setError(r.error); }
  }
  return html`<form class="up-toolbar" onSubmit=${e => {
    e.preventDefault();
    const path = appPath(e.currentTarget.elements.namedItem('path').value);
    if (!path) { setError(t('preview.bad_path')); return; }
    setHistory([...history.slice(0, index + 1), path]); visit(path, index + 1);
  }}>
    <button type="button" class="btn sm" aria-label=${t('preview.back')} disabled=${!index} onClick=${() => visit(history[index - 1], index - 1)}>←</button>
    <button type="button" class="btn sm" aria-label=${t('preview.forward')} disabled=${index + 1 >= history.length} onClick=${() => visit(history[index + 1], index + 1)}>→</button>
    <input name="path" class="input mono up-path" aria-label=${t('preview.path')} value=${input} onInput=${e => setInput(e.currentTarget.value)} />
    <button type="submit" class="btn sm">${t('preview.go')}</button>
    <button type="button" class="btn sm" onClick=${() => visit(history[index], index)}>${t('common.refresh')}</button>
    <button type="button" class="btn sm" onClick=${external}>${t('preview.external')}</button></form>
    ${error && html`<p role="alert">${error}</p>`}
    <${Viewport}><iframe key=${revision} title=${item.label || ':' + item.port} src=${src} referrerpolicy="no-referrer"
      sandbox="allow-scripts allow-same-origin allow-forms allow-downloads allow-modals allow-pointer-lock" /></${Viewport}>`;
}
