// Liste déroulante tenue dans l'écran : un select natif ouvre une liste aussi
// large que son plus long libellé, qui peut déborder. La liste s'ouvre dans un
// Popover (au-dessus de tout, repositionné selon la place) : une carte ou un
// panneau qui coupe son contenu ne peut plus la masquer.
import { html, useState, useRef, cls } from '../core/lib.js';
import { Popover } from './controls.js';

export function ListPick({ value, options, onChange, label, disabled }) {
  const [open, setOpen] = useState(false);
  const btn = useRef();
  const cur = options.find(o => o.value === value);
  const groups = [...new Set(options.map(o => o.group || ''))];
  return html`<div class="lpick">
    <button type="button" ref=${btn} class="select lpick-b" disabled=${disabled} aria-haspopup="listbox" aria-expanded=${String(open)} aria-label=${label} title=${cur ? cur.label : ''} onClick=${() => setOpen(!open)}><span class="trunc">${cur ? cur.label : '—'}</span></button>
    ${open && btn.current && html`<${Popover} anchor=${btn.current} onClose=${() => setOpen(false)} width=${Math.max(btn.current.offsetWidth, 180)} heightLimit=${360} class="lpick-pop">
      <div class="lpick-list" role="listbox" aria-label=${label}>${groups.map(g => html`${g && html`<div class="lpick-g">${g}</div>`}
      ${options.filter(o => (o.group || '') === g).map(o => html`<button type="button" role="option" aria-selected=${String(o.value === value)} class=${cls('lpick-o', o.value === value && 'on')} disabled=${o.disabled} title=${o.label} onClick=${() => { setOpen(false); if (o.value !== value) onChange(o.value); }}><span class="trunc">${o.label}</span>${o.note && html`<small>${o.note}</small>`}</button>`)}`)}</div>
    </${Popover}>`}
  </div>`;
}
