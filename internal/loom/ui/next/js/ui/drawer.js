// Panneau latéral partagé : même rendu que le panneau de la discussion
// (mêmes classes .insp), posé par-dessus la page.
import { html, useEffect, useRef } from '../core/lib.js';
import { Icon } from './icons.js';

export function Drawer({ title, onClose, children }) {
  const box = useRef(), close = useRef(onClose); close.current = onClose;
  useEffect(() => {
    const previous = document.activeElement;
    box.current?.querySelector('button')?.focus();
    const key = e => {
      if (document.querySelector('.scrim')) return; // a confirmation takes priority
      if (e.key === 'Escape') { e.preventDefault(); close.current(); }
      if (e.key !== 'Tab') return;
      const nodes = [...box.current.querySelectorAll('button,input,select,textarea,a[href],[tabindex="0"]')].filter(n => !n.disabled && n.getClientRects().length);
      const first = nodes[0], last = nodes.at(-1);
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
    };
    document.addEventListener('keydown', key);
    return () => { document.removeEventListener('keydown', key); if (previous?.isConnected) previous.focus(); };
  }, []);
  return html`<div class="drawer-scrim" onMouseDown=${e => e.target === e.currentTarget && onClose()}>
    <aside class="insp open drawer-panel" role="dialog" aria-modal="true" aria-label=${title} ref=${box}>
      <div class="insp-in">
        <header class="insp-head"><span class="insp-title">${title}</span><button class="icon-btn" aria-label="Fermer" onClick=${onClose}><${Icon} n="close" /></button></header>
        <div class="insp-scroll">${children}</div>
      </div>
    </aside></div>`;
}

// Keep row presentation while making the name operable with mouse/keyboard.
export const inspectTrigger = (open, label) => ({ role: 'button', tabIndex: 0, 'aria-label': label,
  style: 'cursor:pointer', onClick: open,
  onKeyDown: e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(); } } });
