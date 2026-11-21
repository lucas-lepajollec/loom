// Rangée d'onglets de page avec une pastille qui glisse : la page est remontée
// à chaque onglet, donc la position précédente est gardée ici et la pastille
// part de là (FLIP) au lieu de sauter.
import { html, useRef, useLayoutEffect, useState } from '../core/lib.js';
import { DropdownButton, Menu } from './controls.js';

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
  const [anchor, setAnchor] = useState(null);
  if (!cur) return null;
  return html`<div class="sec-drop">
    <${DropdownButton} open=${anchor} aria-label=${label} onClick=${e => setAnchor(e.currentTarget)}><span class="trunc">${cur.label}</span></${DropdownButton}>
    ${anchor && html`<${Menu} anchor=${anchor} label=${label} onClose=${() => setAnchor(null)}
      width=${anchor.offsetWidth} items=${tabs} />`}
  </div>`;
}
