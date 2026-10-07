// Liste déroulante tenue dans son conteneur : un select natif ouvre une liste
// aussi large que son plus long libellé, qui peut déborder de l'écran.
import { html, useState, useEffect, useRef, cls } from '../core/lib.js';

export function ListPick({ value, options, onChange, label, disabled }) {
  const [open, setOpen] = useState(false);
  const box = useRef();
  useEffect(() => {
    if (!open) return;
    const close = e => { if (box.current && !box.current.contains(e.target)) setOpen(false); };
    const esc = e => { if (e.key === 'Escape') setOpen(false); };
    addEventListener('mousedown', close); addEventListener('keydown', esc);
    return () => { removeEventListener('mousedown', close); removeEventListener('keydown', esc); };
  }, [open]);
  const cur = options.find(o => o.value === value);
  const groups = [...new Set(options.map(o => o.group || ''))];
  return html`<div class="lpick" ref=${box}>
    <button type="button" class="select lpick-b" disabled=${disabled} aria-haspopup="listbox" aria-expanded=${String(open)} aria-label=${label} title=${cur ? cur.label : ''} onClick=${() => setOpen(!open)}><span class="trunc">${cur ? cur.label : '—'}</span></button>
    ${open && html`<div class="lpick-list" role="listbox" aria-label=${label}>${groups.map(g => html`${g && html`<div class="lpick-g">${g}</div>`}
      ${options.filter(o => (o.group || '') === g).map(o => html`<button type="button" role="option" aria-selected=${String(o.value === value)} class=${cls('lpick-o', o.value === value && 'on')} disabled=${o.disabled} title=${o.label} onClick=${() => { setOpen(false); if (o.value !== value) onChange(o.value); }}><span class="trunc">${o.label}</span>${o.note && html`<small>${o.note}</small>`}</button>`)}`)}</div>`}
  </div>`;
}

