import { toast } from '../../ui/dialog.js';
import { Icon } from '../../ui/icons.js';
import { shortVersion } from '../../core/version.js';
import { html, useState, useRef, useEffect } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { confirm } from '../../ui/dialog.js';
import { Line, Group } from './kit.js';
import { Seg } from '../../ui/controls.js';

export async function waitForUpdatedVersion(version, read = get, pause = ms => new Promise(resolve => setTimeout(resolve, ms)), alive = () => true, ping = '/api/ping') {
  for (let i = 0; i < 30 && alive(); i++) {
    await pause(2000);
    if (!alive()) return false;
    try { if ((await read(ping)).version === version) return true; } catch (_) {}
  }
  return false;
}

export function LoomUpdates({ node = false, endpoint = '', compact = false } = {}) {
 const base = endpoint || (node ? '/api/engine/node/update' : '/api/update');
  const [info, setInfo] = useState(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const running = useRef(false);
  const alive = useRef(true);
  useEffect(() => () => { alive.current = false; }, []);
  useEffect(() => { alive.current = true; check(); }, [base]);
  const check = async () => {
    if (running.current) return;
    running.current = true; setBusy(true); setError(''); setMessage('');
    try {
      const result = await get(base);
      if (result.error) { setInfo(result.channel ? { channel: result.channel } : null); throw new Error(result.error); }
      setInfo(result);
    } catch (err) { setInfo(old => old && !old.current ? old : null); setError(err.message); }
    finally { running.current = false; setBusy(false); }
  };
  const install = async () => {
    if (running.current || !info?.available || !info.can_apply) return;
    running.current = true;
    try {
      if (!await confirm(t('updates.install'), t(node ? 'node.update_confirm' : 'updates.confirm', { version: info.latest }), { ok: t('updates.install') })) return;
      setBusy(true); setError(''); setMessage(t('updates.installing'));
      const result = await post(base + '/apply', { version: info.latest }, { timeout: 8 * 60 * 1000 });
      if (!result.ok) throw new Error(result.error || t('updates.failed'));
      if (result.restarting) {
        setMessage(t('updates.reconnecting'));
        if (await waitForUpdatedVersion(result.version, get, undefined, () => alive.current, node ? base + '/ping' : '/api/ping')) {
          if (node) { setInfo(await get(base)); setMessage(t('node.updated')); } else location.reload();
        }
        else if (alive.current) setMessage(t('updates.restart_pending'));
      } else setMessage(t('updates.restart_manual') + ' ' + (result.restart || ''));
    } catch (err) { setMessage(''); setError(err.message); }
    finally { running.current = false; if (alive.current) setBusy(false); }
  };
  const setChannel = async channel => {
    if (running.current || channel === info?.channel) return;
    try {
      const result = await post(base + '/channel', { channel });
      if (!result.ok) throw new Error(result.error || t('updates.failed'));
      await check();
    } catch (err) { setError(err.message); }
  };
  // Compact (machine page): the state in one line, a small action only when
  // there is something to do; full versions in the tooltip.
  // Compact headers never grow: progress and results go to toasts.
  useEffect(() => { if (compact && message) toast(message); }, [compact, message]);
  useEffect(() => { if (compact && error) toast(error, 'err'); }, [compact, error]);
  if (compact) return html`<div class="upd">
    ${info?.available
      ? html`<button class="btn sm" disabled=${busy || !info.can_apply} title=${info.can_apply ? info.current + ' → ' + info.latest : info.apply_reason || ''} onClick=${install}><${Icon} n="download" />${busy ? t('updates.installing_short') : t('machines.node.update_to', { version: shortVersion(info.latest) })}</button>`
      : info?.current && html`<span class="state" title=${info.current}><i class="dot green"></i>${t('machines.node.up_to_date', { version: shortVersion(info.current) })}</span>`}</div>`;
  return html`<${Group} title=${t(node ? 'node.updates' : 'settings.page.mises_a_jour')}>
    ${!node && html`<${Line} label=${t('updates.channel')} tip=${t('updates.channel_tip')}><${Seg} size="sm" label=${t('updates.channel')} value=${info?.channel || 'stable'} onChange=${setChannel} options=${[{ value: 'stable', label: t('updates.stable'), disabled: busy }, { value: 'edge', label: t('updates.edge'), disabled: busy }]} /></${Line}>`}
    <${Line} label=${t('updates.source')}><a href="https://github.com/lucas-lepajollec/loom/releases" target="_blank" rel="noopener noreferrer">${t('updates.releases')}</a></${Line}>
    <${Line} label=${t('updates.version')}>
      ${info?.current && html`<span class="state" title=${info.available ? info.latest : info.current}>${info.available ? t('updates.available', { version: shortVersion(info.latest) }) : t('updates.current', { version: shortVersion(info.current) })}</span>`}
      <button class="btn sm" disabled=${busy} onClick=${check}>${t('settings.page.verifier')}</button>
      ${info?.available && html`<button class="btn sm primary" disabled=${busy || !info.can_apply} onClick=${install}>${t('updates.install')}</button>`}
    </${Line}>
    ${info?.available && !info.can_apply && html`<p class="set-note">${info.apply_reason || t('updates.setup')}</p>`}
    ${message && html`<p class="set-note" role="status">${message}</p>`}
    ${error && html`<p class="set-note" role="alert">${error}</p>`}
  </${Group}>`;
}
