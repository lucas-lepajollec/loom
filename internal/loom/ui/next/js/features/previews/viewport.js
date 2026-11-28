import { html, useState, useLayoutEffect, useRef } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { viewportPresets, viewportSize } from './navigation.js';

export function Viewport({ children }) {
  const [preset, setPreset] = useState('auto'), [rotate, setRotate] = useState(false);
  const [width, setWidth] = useState(0), stage = useRef();
  useLayoutEffect(() => {
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(stage.current);
    return () => observer.disconnect();
  }, []);
  const size = viewportSize(preset, rotate), scale = size && Math.min(1, width / size[0]);
  return html`<div class="up-toolbar"><select aria-label=${t('preview.viewport')} value=${preset} onChange=${e => setPreset(e.currentTarget.value)}>
    ${Object.entries(viewportPresets).map(([key, dims]) => html`<option value=${key} key=${key}>${t(`preview.${key}`)}${dims ? ' · ' + dims[0] + ' px' : ''}</option>`)}
    </select><button type="button" class="btn sm" disabled=${!size} aria-pressed=${rotate} onClick=${() => setRotate(!rotate)}>${t('preview.rotate')}</button></div>
    <div class="up-viewport" ref=${stage}><div class=${'up-device' + (size ? ' fixed' : '')}
      style=${size ? { width: size[0]*scale, height: size[1]*scale } : undefined}>
      <div style=${size ? { width: size[0], height: size[1], transform: 'scale(' + scale + ')' } : undefined}>${children}</div>
    </div></div>`;
}
