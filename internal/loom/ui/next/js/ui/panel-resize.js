// Adapted from Paradev PanelResizeHandle (Superset Inc., MIT).
// The preference is shared by the file, review and preview tools.
import { html, useRef, useState } from '../core/lib.js';
import { t } from '../core/i18n.js';

const key = 'loom.workspace.toolWidth';
const min = 260, max = 560;
const clamp = value => Math.min(max, Math.max(min, Math.round(value)));

export function ToolPane({ label, children }) {
  const start = useRef(null);
  const [width, setWidth] = useState(() => {
    try { const saved = Number(localStorage.getItem(key)); if (saved) return clamp(saved); } catch (_) {}
    return 340;
  });
  const save = value => {
    setWidth(clamp(value));
    try { localStorage.setItem(key, String(clamp(value))); } catch (_) {}
  };
  const reset = () => save(340);
  return html`<aside class="workspace-tool-pane" aria-label=${label} style=${{'--tool-width': width + 'px'}}>
    <div class="pane-resizer" role="separator" tabindex="0" aria-orientation="vertical" aria-label=${t('workspace.resize')}
      aria-valuemin=${min} aria-valuemax=${max} aria-valuenow=${width}
      onPointerDown=${e => {
        if (e.button !== 0) return;
        e.currentTarget.setPointerCapture(e.pointerId);
        start.current = { x: e.clientX, width, pointer: e.pointerId };
      }}
      onPointerMove=${e => { if (start.current?.pointer === e.pointerId) setWidth(clamp(start.current.width + start.current.x - e.clientX)); }}
      onPointerUp=${e => {
        if (start.current?.pointer !== e.pointerId) return;
        save(start.current.width + start.current.x - e.clientX);
        start.current = null;
        e.currentTarget.releasePointerCapture(e.pointerId);
      }}
      onLostPointerCapture=${() => { if (start.current) setWidth(start.current.width); start.current = null; }}
      onDblClick=${reset}
      onKeyDown=${e => {
        const next = { ArrowLeft: width + 16, ArrowRight: width - 16, Home: min, End: max }[e.key];
        if (next !== undefined) { e.preventDefault(); save(next); }
      }}></div>${children}</aside>`;
}
