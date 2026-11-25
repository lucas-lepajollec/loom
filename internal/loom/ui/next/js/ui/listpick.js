import { html, useState } from '../core/lib.js';
import { DropdownButton, Menu } from './controls.js';

export function ListPick({ value, options, onChange, label, disabled, placeholder = '—', ...props }) {
  const [anchor, setAnchor] = useState(null);
  const current = options.find(option => option.value === value);
  return html`<${DropdownButton} ...${props} disabled=${disabled} popup="listbox" open=${anchor}
    aria-label=${label} title=${current?.label} onClick=${e => setAnchor(e.currentTarget)}>
    <span class="trunc">${current?.display ?? current?.label ?? placeholder}</span>
  </${DropdownButton}>
  ${anchor && html`<${Menu} anchor=${anchor} onClose=${() => setAnchor(null)} role="listbox" label=${label}
    width=${Math.min(Math.max(anchor.offsetWidth, 220), 360)} heightLimit=${360}
    items=${options.map(option => ({ ...option, key: option.value, selected: option.value === value,
      run: () => { if (option.value !== value) onChange(option.value); } }))} />`}`;
}
