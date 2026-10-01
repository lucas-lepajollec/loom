// Contrôles réutilisables : segmenté animé, onglets, interrupteur, curseur,
// info-bulle ⓘ, popover ancré.
import { html, useRef, useLayoutEffect, useEffect, useState, cls } from '../core/lib.js';
import { Icon } from './icons.js';

export function Seg({ value, options, onChange, size, label }) {
  const box = useRef(), ind = useRef();
  useLayoutEffect(() => {
    const on = box.current && box.current.querySelector('[aria-selected="true"]');
    if (on && ind.current) { ind.current.style.width = on.offsetWidth + 'px'; ind.current.style.transform = `translateX(${on.offsetLeft}px)`; }
  });
  return html`<div class=${cls('seg', size)} role="tablist" aria-label=${label} ref=${box}>
    <span class="ind" ref=${ind} aria-hidden="true"></span>
    ${options.map(o => html`<button type="button" role="tab" aria-selected=${String(o.value === value)} onClick=${() => onChange(o.value)} disabled=${o.disabled}>
      ${o.label}${o.count != null && o.count !== '' ? html` <span class="n">${o.count}</span>` : ''}</button>`)}
  </div>`;
}

export function Tabs({ value, options, onChange, label }) {
  return html`<div class="tabs" role="tablist" aria-label=${label}>
    ${options.map(o => html`<button type="button" role="tab" aria-selected=${String(o.value === value)} onClick=${() => onChange(o.value)}>${o.label}${o.count != null ? html`<span class="n">${o.count}</span>` : ''}</button>`)}
  </div>`;
}

export const Switch = ({ checked, onChange, disabled, label }) =>
  html`<button type="button" class="switch" role="switch" aria-checked=${String(!!checked)} aria-label=${label} disabled=${disabled} onClick=${() => onChange(!checked)}></button>`;

// Curseur : piste + remplissage + poignée, piloté par un input range invisible
// (clavier et lecteurs d'écran restent natifs).
export function Slider({ value, min, max, step, onInput, onChange, label }) {
  const [drag, setDrag] = useState(false);
  const pct = max > min ? Math.max(0, Math.min(100, (Number(value) - min) * 100 / (max - min))) : 0;
  return html`<div class=${cls('slider', drag && 'drag')}>
    <span class="rail"></span><span class="fill" style=${`width:${pct}%`}></span><span class="knob" style=${`left:${pct}%`}></span>
    <input type="range" min=${min} max=${max} step=${step || 1} value=${value} aria-label=${label}
      onInput=${e => onInput && onInput(Number(e.target.value))} onChange=${e => onChange && onChange(Number(e.target.value))}
      onPointerDown=${() => setDrag(true)} onPointerUp=${() => setDrag(false)} />
  </div>`;
}

// ⓘ : l'explication vit dans une bulle au survol/focus, jamais en paragraphe.
export function Tip({ text }) {
  const [pos, setPos] = useState(null);
  const ref = useRef();
  const show = () => { const r = ref.current.getBoundingClientRect(); setPos({ x: Math.min(r.left, innerWidth - 296), y: r.bottom + 8 }); };
  return html`<span class="tip" tabindex="0" ref=${ref} onMouseEnter=${show} onMouseLeave=${() => setPos(null)} onFocus=${show} onBlur=${() => setPos(null)} aria-label=${text}>i
    ${pos && html`<span class="tipbox" role="tooltip" style=${`left:${pos.x}px;top:${pos.y}px`}>${text}</span>`}</span>`;
}

// Popover ancré sous (ou au-dessus de) son déclencheur, fermé par clic extérieur/Échap.
export function Popover({ anchor, onClose, children, width, place, class: c }) {
  const box = useRef();
  const [st, setSt] = useState({ visibility: 'hidden' });
  useLayoutEffect(() => {
    if (!anchor) return;
    const r = anchor.getBoundingClientRect(), el = box.current;
    const w = el.offsetWidth, hgt = el.offsetHeight;
    let left = Math.max(8, Math.min(r.left, innerWidth - w - 8));
    let top = place === 'above' ? r.top - hgt - 8 : r.bottom + 6;
    if (top + hgt > innerHeight - 8) top = Math.max(8, r.top - hgt - 8);
    if (top < 8) top = 8;
    setSt({ left: left + 'px', top: top + 'px', transformOrigin: place === 'above' ? 'bottom left' : 'top left' });
  }, [anchor]);
  useEffect(() => {
    const down = e => { if (box.current && !box.current.contains(e.target) && !(anchor && anchor.contains(e.target))) onClose(); };
    const key = e => { if (e.key === 'Escape') { onClose(); anchor && anchor.focus(); } };
    setTimeout(() => document.addEventListener('mousedown', down), 0);
    document.addEventListener('keydown', key);
    return () => { document.removeEventListener('mousedown', down); document.removeEventListener('keydown', key); };
  }, [anchor]);
  const style = Object.entries({ ...st, width: width ? width + 'px' : undefined }).filter(([, v]) => v).map(([k, v]) => k.replace(/[A-Z]/g, m => '-' + m.toLowerCase()) + ':' + v).join(';');
  return html`<div class=${cls('pop', c)} ref=${box} style=${style} role="dialog">${children}</div>`;
}

// Menu contextuel simple : liste d'actions dans un Popover.
export function Menu({ anchor, onClose, items }) {
  return html`<${Popover} anchor=${anchor} onClose=${onClose}>
    ${items.map(it => it === '-' ? html`<hr />` : html`<button class=${cls('item', it.danger && 'danger')} onClick=${() => { onClose(); it.run(); }}>${it.icon && html`<${Icon} n=${it.icon} />`}${it.label}</button>`)}
  </${Popover}>`;
}

export const Empty = ({ icon, title, text, children }) => html`<div class="empty anim-rise"><${Icon} n=${icon || 'info'} /><h3>${title}</h3>${text && html`<p>${text}</p>`}${children}</div>`;
