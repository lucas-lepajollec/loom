// Rangée d'onglets de page avec une pastille qui glisse : la page est remontée
// à chaque onglet, donc la position précédente est gardée ici et la pastille
// part de là (FLIP) au lieu de sauter.
import { html, useRef, useLayoutEffect, useState, useEffect } from '../core/lib.js';
import { Icon } from './icons.js';

const last = new Map();

export function TabNav({ id, label, tabs }) {
  const nav = useRef(), pill = useRef();
  useLayoutEffect(() => {
    const n = nav.current, p = pill.current;
    const cur = n && n.querySelector('a[aria-current="page"]');
    if (!n || !p || !cur) { if (p) p.style.opacity = '0'; return; }
    const to = { left: cur.offsetLeft, width: cur.offsetWidth };
    const from = last.get(id);
    const place = r => { p.style.transform = `translateX(${r.left}px)`; p.style.width = r.width + 'px'; };
    p.style.transition = 'none'; p.style.opacity = '1';
    place(from || to);
    if (from && (from.left !== to.left || from.width !== to.width)) {
      requestAnimationFrame(() => requestAnimationFrame(() => { p.style.transition = ''; place(to); }));
    }
    last.set(id, to);
  }, [tabs.map(tab => tab.current ? tab.href : '').join('|')]);
  const cur = tabs.find(tab => tab.current) || tabs[0];
  return html`<${TabDrop} label=${label} tabs=${tabs} cur=${cur} /><nav class="sec-tabs" ref=${nav} aria-label=${label}><span class="sec-pill" ref=${pill} aria-hidden="true"></span>${tabs.map(tab => html`<a key=${tab.href} href=${tab.href} aria-current=${tab.current ? 'page' : undefined}>${tab.label}</a>`)}</nav>`;
}

// Sur téléphone, les onglets deviennent un menu déroulant : la page courante
// en titre, la liste des autres pages en dessous, de grandes cibles.
function TabDrop({ label, tabs, cur }) {
  const [open, setOpen] = useState(false);
  const box = useRef();
  useEffect(() => {
    if (!open) return;
    const away = e => { if (box.current && !box.current.contains(e.target)) setOpen(false); };
    const key = e => { if (e.key === 'Escape') setOpen(false); };
    addEventListener('pointerdown', away, true); addEventListener('keydown', key);
    return () => { removeEventListener('pointerdown', away, true); removeEventListener('keydown', key); };
  }, [open]);
  if (!cur) return null;
  return html`<div class=${'sec-drop' + (open ? ' open' : '')} ref=${box}>
    <button type="button" class="sec-drop-btn" aria-haspopup="true" aria-expanded=${String(open)} aria-label=${label} onClick=${() => setOpen(!open)}><span>${cur.label}</span><${Icon} n="chevron" /></button>
    ${open && html`<nav class="sec-drop-list anim-fade" aria-label=${label}>${tabs.map(tab => html`<a key=${tab.href} href=${tab.href} aria-current=${tab.current ? 'page' : undefined} onClick=${() => setOpen(false)}>${tab.label}${tab.current && html`<${Icon} n="check" />`}</a>`)}</nav>`}
  </div>`;
}
