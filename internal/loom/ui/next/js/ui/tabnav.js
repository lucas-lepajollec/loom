// Rangée d'onglets de page avec une pastille qui glisse : la page est remontée
// à chaque onglet, donc la position précédente est gardée ici et la pastille
// part de là (FLIP) au lieu de sauter.
import { html, useRef, useLayoutEffect } from '../core/lib.js';

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
  return html`<nav class="sec-tabs" ref=${nav} aria-label=${label}><span class="sec-pill" ref=${pill} aria-hidden="true"></span>${tabs.map(tab => html`<a key=${tab.href} href=${tab.href} aria-current=${tab.current ? 'page' : undefined}>${tab.label}</a>`)}</nav>`;
}
