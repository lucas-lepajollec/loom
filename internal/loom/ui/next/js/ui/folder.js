// Sélecteur de dossier de travail (côté serveur : /api/fs/dirs ne liste que
// des dossiers, jamais le contenu des fichiers).
import { html, useState, useEffect, cls } from '../core/lib.js';
import { Icon } from './icons.js';
import { Modal } from './dialog.js';
import { get } from '../core/api.js';

export function FolderPicker({ start, onPick, onClose }) {
  const [at, setAt] = useState(null);
  const [err, setErr] = useState('');
  const [q, setQ] = useState('');
  const load = async path => {
    setErr('');
    try {
      const r = await get('/api/fs/dirs' + (path ? '?path=' + encodeURIComponent(path) : ''));
      if (r.ok === false) throw new Error(r.error || 'Dossier illisible');
      setAt(r); setQ('');
    } catch (e) { setErr(e.message); }
  };
  useEffect(() => { load(start || ''); }, []);
  const parts = at ? at.path.split('/').filter(Boolean) : [];
  const dirs = ((at && at.dirs) || []).filter(d => !q || d.name.toLowerCase().includes(q.toLowerCase()));
  return html`<${Modal} wide title="Dossier de travail" onClose=${onClose}
      foot=${html`<span class="grow muted mono trunc" style="font-size:12px">${at ? at.path : ''}</span><button class="btn ghost" onClick=${onClose}>Annuler</button><button class="btn primary" disabled=${!at} onClick=${() => onPick(at.path)}>Choisir ce dossier</button>`}>
    <div class="fp-crumbs">
      <button class="fp-crumb" onClick=${() => load('/')}>/</button>
      ${parts.map((p, i) => html`<button class="fp-crumb" onClick=${() => load('/' + parts.slice(0, i + 1).join('/'))}>${p}</button>`)}
    </div>
    <label class="search"><${Icon} n="search" /><input placeholder="Filtrer" aria-label="Filtrer les dossiers" value=${q} onInput=${e => setQ(e.target.value)} /></label>
    ${err && html`<p class="note err">${err}</p>`}
    <div class="fp-list">
      ${at && at.parent && html`<button class="fp-row" onClick=${() => load(at.parent)}><${Icon} n="left" /><span>Dossier parent</span></button>`}
      ${dirs.map(d => html`<button class="fp-row" key=${d.path} onClick=${() => load(d.path)}><${Icon} n="folder" /><span>${d.name}</span>${d.is_git && html`<span class="tag">git</span>`}</button>`)}
      ${at && !dirs.length && html`<p class="note" style="padding:10px 12px">Aucun sous-dossier.</p>`}
    </div>
  </${Modal}>`;
}
