import { html, useState, useEffect, useMemo, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { request, get } from '../../core/api.js';
import { api } from '../agents/client.js';
import { normalizeSessionPath } from '../agents/panel-helpers.js';
import { md } from '../chat/md.js';
import { Viewport } from './viewport.js';
import { fileQuery } from './navigation.js';

export function TextView({ item, renderer, session, select, fileURL }) {
  const [source, setSource] = useState(false), [data, setData] = useState(null), [error, setError] = useState('');
  useEffect(() => {
    let active = true;
    const codeman = session.agent && !item.explicitTarget && !item.path.startsWith('/');
    const read = codeman ? api.fileContent(session.id, item.path) : get('/api/files/read?' + fileQuery(item, session));
    read.then(data => { if (active) setData(data || {}); }).catch(e => { if (active) setError(e.message); });
    return () => { active = false; };
  }, [item.path, item.target, session.id]);
  const text = data?.content, markdown = renderer.id === 'markdown';
  const rendered = useMemo(() => markdown && !source && typeof text === 'string' ? md(text) : '', [markdown, source, text]);
  const [textURL, setTextURL] = useState('');
  useEffect(() => {
    if (typeof text !== 'string') return;
    if (renderer.id === 'html' && item.target) { setTextURL(fileURL(item.path)); return; }
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
    setTextURL(url);
    return () => URL.revokeObjectURL(url);
  }, [text, renderer.id, item.path, item.target]);
  if (error) return html`<p role="alert">${item.path.startsWith('/') ? t('agents.preview.outside') : error}</p>`;
  if (!data) return html`<p role="status">${t('connections.mcp.loading')}</p>`;
  if (typeof text !== 'string') return html`<p>${t(data.too_large ? 'agents.files.truncated' : 'agents.files.binary')}</p>`;
  return html`${textURL && html`<a class="btn sm" href=${textURL} target="_blank" rel="noopener noreferrer">${t('preview.external')}</a>`}
    ${renderer.id !== 'code' && html`<div class="cm-preview-mode" role="group" aria-label=${t('agents.insp.preview')}>
    <button class=${!source && 'on'} aria-pressed=${!source} onClick=${() => setSource(false)}>${t('agents.preview.rendered')}</button>
    <button class=${source && 'on'} aria-pressed=${source} onClick=${() => setSource(true)}>${t('agents.preview.raw')}</button></div>`}
    ${markdown && !source ? html`<div class="md cm-preview-md" onClick=${e => {
      const a = e.target.closest('a');
      const path = a && normalizeSessionPath(a.dataset.fileHref || a.getAttribute('href'), session.workingDir);
      if (path) { e.preventDefault(); select({ ...item, path, label: '' }); }
    }} dangerouslySetInnerHTML=${{ __html: rendered }}></div>`
    : renderer.id === 'html' && !source ? html`<${Viewport}><iframe title=${item.path} sandbox="allow-scripts" referrerpolicy="no-referrer"
      srcdoc=${'<meta http-equiv="Content-Security-Policy" content="default-src &apos;none&apos;; img-src data:; style-src &apos;unsafe-inline&apos;; form-action &apos;none&apos;; base-uri &apos;none&apos;">' + text} /></${Viewport}>`
    : html`<pre class="cm-preview-code">${text}</pre>`}
    ${data.truncated && html`<p>${t('agents.files.truncated')}</p>`}`;
}

function Download({ item, url }) {
  return html`<a class="btn sm" href=${url} download=${item.path.split('/').pop()}>${t('resources.sem.download')}</a>`;
}
export function PDFView({ item, fileURL }) {
  const url = fileURL(item.path);
  return html`<iframe class="up-pdf" src=${url} title=${item.path}></iframe><${Download} item=${item} url=${url} />`;
}
export function MediaView({ item, fileURL }) {
  const tag = /\.(?:mp4|m4v|webm|mov)$/i.test(item.path) ? 'video' : 'audio';
  const [failed, setFailed] = useState(false);
  return html`<${tag} controls playsinline preload="metadata" aria-label=${item.path} src=${fileURL(item.path)} onError=${() => setFailed(true)} />
    ${failed && html`<p role="alert">${t('preview.media_error')}</p>`}<${Download} item=${item} url=${fileURL(item.path)} />`;
}
export function ImageView({ item, fileURL }) {
  const [zoom, setZoom] = useState(0), [grid, setGrid] = useState(true);
  const [dims, setDims] = useState(null), [bytes, setBytes] = useState(null), [failed, setFailed] = useState(false);
  const url = fileURL(item.path), img = useRef();
  const currentScale = () => zoom || (dims && img.current?.clientWidth / dims[0]) || 1;
  useEffect(() => {
    const controller = new AbortController();
    request(url, { method: 'HEAD', signal: controller.signal }).then(r => {
      if (!controller.signal.aborted && r.ok && r.headers.has('Content-Length')) setBytes(Number(r.headers.get('Content-Length')));
    }).catch(() => {});
    return () => controller.abort();
  }, [url]);
  return html`<div class="up-toolbar">
    <button class="btn sm" aria-pressed=${!zoom} onClick=${() => setZoom(0)}>${t('preview.fit')}</button>
    <button class="btn sm" onClick=${() => setZoom(1)}>100%</button>
    <button class="btn sm" aria-label=${t('preview.zoom_out')} onClick=${() => setZoom(Math.max(.0625, currentScale()/1.5))}>−</button>
    <button class="btn sm" aria-label=${t('preview.zoom_in')} onClick=${() => setZoom(Math.min(16, currentScale()*1.5))}>+</button>
    <button class="btn sm" aria-pressed=${grid} onClick=${() => setGrid(!grid)}>${t('preview.grid')}</button>
    <${Download} item=${item} url=${url} /></div>
    <div class=${'up-image' + (grid ? ' checked' : '')}><img ref=${img} src=${url} alt=${item.label || item.path}
      style=${zoom && dims ? { width: zoom * dims[0], maxWidth: 'none', maxHeight: 'none' } : undefined}
      onLoad=${e => setDims([e.currentTarget.naturalWidth, e.currentTarget.naturalHeight])} onError=${() => setFailed(true)} /></div>
    ${failed && html`<p role="alert">${t('preview.media_error')}</p>`}
    <small>${dims && dims.join(' × ') + ' px'} ${bytes !== null && ' · ' + t('preview.bytes', { n: bytes.toLocaleString() })}</small>`;
}
