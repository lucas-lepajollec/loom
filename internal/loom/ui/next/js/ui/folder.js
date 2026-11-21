import { t } from '../core/i18n.js';
// Sélecteur de dossier de travail sur cette machine : il parcourt les dossiers
// avec l'API Fichiers (portée « toute la machine »), sans lire les fichiers.
import { html, useState, useEffect } from '../core/lib.js';
import { Icon } from './icons.js';
import { Modal } from './dialog.js';
import { get } from '../core/api.js';

const join = (root, rel) => (rel && rel !== '.' ? root.replace(/\/+$/, '') + '/' + rel : root);

export function FolderPicker({ start, onPick, onClose }) {
  const [at, setAt] = useState(null);
  const [err, setErr] = useState('');
  const [q, setQ] = useState('');
  // path : chemin absolu ; l'API reçoit le chemin relatif à la racine de la machine.
  const load = async path => {
    setErr('');
    try {
      let abs = path;
      if (!abs) { const local = await get('/api/machines/local').catch(() => null); abs = (local && local.home) || '/'; }
      const probe = await get('/api/files/list?scope=machine&machine=local&path=.');
      if (probe.ok === false) throw new Error(probe.error || t("ui.folder.dossier_illisible"));
      const root = probe.root || '/';
      const rel = abs.startsWith(root) ? abs.slice(root.length).replace(/^\/+/, '') || '.' : '.';
      const r = rel === '.' ? probe : await get('/api/files/list?scope=machine&machine=local&path=' + encodeURIComponent(rel));
      if (r.ok === false) throw new Error(r.error || t("ui.folder.dossier_illisible"));
      const here = join(root, r.path);
      const parent = r.path && r.path !== '.' ? join(root, r.path.includes('/') ? r.path.slice(0, r.path.lastIndexOf('/')) : '.') : '';
      setAt({ path: here, parent, dirs: (r.entries || []).filter(e => e.dir).map(e => ({ name: e.name, path: join(root, e.path) })) }); setQ('');
    } catch (e) { setErr(e.message); }
  };
  useEffect(() => { load(start || ''); }, []);
  const parts = at ? at.path.split('/').filter(Boolean) : [];
  const dirs = ((at && at.dirs) || []).filter(d => !q || d.name.toLowerCase().includes(q.toLowerCase()));
  return html`<${Modal} wide title="${t("ui.folder.dossier_de_travail")}" onClose=${onClose}
      foot=${html`<span class="grow muted mono trunc" style="font-size:12px">${at ? at.path : ''}</span><button class="btn" onClick=${onClose}>${t("ui.folder.annuler")}</button><button class="btn primary" disabled=${!at} onClick=${() => onPick(at.path)}>${t("ui.folder.choisir_ce_dossier")}</button>`}>
    <div class="fp-crumbs">
      <button class="fp-crumb" onClick=${() => load('/')}>/</button>
      ${parts.map((p, i) => html`<button class="fp-crumb" onClick=${() => load('/' + parts.slice(0, i + 1).join('/'))}>${p}</button>`)}
    </div>
    <label class="search"><${Icon} n="search" /><input placeholder="${t("ui.folder.filtrer")}" aria-label="${t("ui.folder.filtrer_les_dossiers")}" value=${q} onInput=${e => setQ(e.target.value)} /></label>
    ${err && html`<p class="note err">${err}</p>`}
    <div class="fp-list">
      ${at && at.parent && html`<button class="item fp-row" onClick=${() => load(at.parent)}><${Icon} n="left" /><span>${t("ui.folder.dossier_parent")}</span></button>`}
      ${dirs.map(d => html`<button class="item fp-row" key=${d.path} onClick=${() => load(d.path)}><${Icon} n="folder" /><span>${d.name}</span></button>`)}
      ${at && !dirs.length && html`<p class="note" style="padding:10px 12px">${t("ui.folder.aucun_sous_dossier")}</p>`}
    </div>
  </${Modal}>`;
}
