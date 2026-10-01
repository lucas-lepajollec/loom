// Mémoire : pages Markdown que l'agent local relit entre les sessions.
// Liste filtrable à gauche, éditeur à droite.
import { html, useState, useEffect, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Empty, Tip } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';

const MODES = { off: 'désactivée', ondemand: 'à la demande' };

export function Memory() {
  const [pages, setPages] = useState(null);
  const [mode, setMode] = useState('');
  const [q, setQ] = useState('');
  const [cur, setCur] = useState(null); // { name, old, content, dirty }
  const load = async () => {
    const a = await get('/api/agent').catch(() => ({}));
    setPages((a.pages || a.skills || []).slice().sort((x, y) => x.name.localeCompare(y.name)));
    setMode(a.mem_mode || '');
  };
  useEffect(() => { load(); }, []);
  const pick = async name => {
    if (cur && cur.dirty && !await confirm('Quitter sans enregistrer', 'Les modifications de « ' + (cur.name || 'nouvelle page') + ' » seront perdues.', { ok: 'Quitter' })) return;
    const r = await get('/api/mem' + (name ? '?name=' + encodeURIComponent(name) : '')).catch(e => ({ error: e.message }));
    if (r.error) return toast(r.error, 'err');
    setCur({ name: r.name || '', old: r.name || '', content: r.content || '', dirty: false });
  };
  const save = async () => {
    const name = cur.name.trim();
    if (!name) return toast('Donne un nom à la page', 'err');
    const r = await post('/api/mem/save', { name, old: cur.old, content: cur.content });
    if (!r.ok) return toast(r.error || 'Enregistrement impossible', 'err');
    toast('Page enregistrée'); setCur({ ...cur, name, old: name, dirty: false }); load();
  };
  const del = async () => {
    if (!await confirm('Supprimer la page', '« ' + cur.old + ' » sera supprimée de la mémoire.', { ok: 'Supprimer', danger: true })) return;
    const r = await post('/api/mem/delete', { name: cur.old });
    if (!r.ok) return toast(r.error || 'Suppression impossible', 'err');
    setCur(null); load();
  };
  const shown = (pages || []).filter(p => !q || (p.name + ' ' + (p.desc || '')).toLowerCase().includes(q.toLowerCase()));
  return html`<div class="mem">
    <div class="toolbar">
      <label class="search"><${Icon} n="search" /><input placeholder="Filtrer les pages" aria-label="Filtrer les pages" value=${q} onInput=${e => setQ(e.target.value)} /></label>
      <span class="state">Mémoire ${MODES[mode] || mode || '…'}<${Tip} text="Le mode se règle dans Réglages › Général. Les pages restent sur cette machine, chiffrées si le coffre l’est." /></span>
      <span class="grow"></span>
      <button class="btn primary" onClick=${() => pick('')}><${Icon} n="plus" />Nouvelle page</button>
    </div>
    ${pages === null ? html`<div class="skeleton" style="height:220px;margin-top:14px"></div>`
      : !pages.length && !cur ? html`<div class="card" style="margin-top:14px"><${Empty} icon="brain" title="Aucune page" text="Écris ce que l’agent doit retenir d’une session à l’autre : préférences, contexte, conventions." >
          <button class="btn primary" onClick=${() => pick('')}>Créer une page</button></${Empty}></div>`
      : html`<div class="mem-grid">
        <div class="card mem-list">${shown.length ? shown.map(p => html`<button type="button" class=${cls('mem-item', cur && cur.old === p.name && 'on')} onClick=${() => pick(p.name)}>
            <b>${p.name}</b>${p.desc && html`<small>${p.desc}</small>`}</button>`) : html`<p class="note" style="padding:12px">Aucune page ne correspond.</p>`}</div>
        ${cur ? html`<div class="card mem-edit">
          <div class="mem-edit-h"><input class="input" aria-label="Nom de la page" placeholder="nom-de-la-page" value=${cur.name} onInput=${e => setCur({ ...cur, name: e.target.value, dirty: true })} />
            ${cur.old && html`<button class="icon-btn" aria-label="Supprimer la page" title="Supprimer" onClick=${del}><${Icon} n="trash" /></button>`}</div>
          <textarea class="textarea mono mem-text" aria-label="Contenu" spellcheck="false" value=${cur.content} onInput=${e => setCur({ ...cur, content: e.target.value, dirty: true })}></textarea>
          <div class="mem-edit-f"><span class="muted">${cur.dirty ? 'Modifications non enregistrées' : cur.old ? 'Enregistrée' : 'Nouvelle page'}</span><span class="grow"></span>
            <button class="btn ghost" onClick=${() => setCur(null)}>Fermer</button><button class="btn primary" disabled=${!cur.dirty} onClick=${save}>Enregistrer</button></div>
        </div>` : html`<div class="card mem-edit mem-none"><p class="muted">Choisis une page à gauche ou crée-en une.</p></div>`}
      </div>`}
  </div>`;
}
