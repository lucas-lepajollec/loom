// Barre à traits d'un fil : un trait par message, comme dans les apps de
// Codex ou Conductor. Les traits des demandes sont plus longs ; ceux des
// messages visibles s'éclairent. Survol : début du message ; clic : on y va.
import { html, useState, useEffect, useRef, cls } from '../../core/lib.js';

const MARKS = '.msg-user, .msg-ai';

function preview(el) {
  const text = (el.querySelector('.bubble, .md') || el).textContent || '';
  return text.replace(/\s+/g, ' ').trim().slice(0, 90);
}

export function Minimap({ scroller }) {
  const [marks, setMarks] = useState([]);
  const [seen, setSeen] = useState([0, -1]);
  const [hover, setHover] = useState(-1);
  const list = useRef([]);
  const offsets = useRef([]);
  useEffect(() => {
    const box = scroller.current; if (!box) return;
    let collectFrame = 0, scrollFrame = 0;
    const measureOffsets = () => {
      offsets.current = list.current.map(el => {
        const top = el.offsetTop;
        return { top, bottom: top + el.offsetHeight };
      });
    };
    const visible = () => {
      scrollFrame = 0;
      const top = box.scrollTop, bottom = top + box.clientHeight;
      const pos = offsets.current;
      let first = -1, last = -1;
      for (let i = 0; i < pos.length; i++) {
        const p = pos[i];
        if (p.bottom > top + 8 && p.top < bottom - 8) {
          if (first < 0) first = i;
          last = i;
        } else if (first >= 0 && p.top >= bottom - 8) {
          break;
        }
      }
      setSeen(s => s[0] === first && s[1] === last ? s : [first, last]);
    };
    const onScroll = () => { if (!scrollFrame) scrollFrame = requestAnimationFrame(visible); };
    const collect = () => {
      collectFrame = 0;
      if (!box.isConnected) return;
      const els = [...box.querySelectorAll(MARKS)].filter(el => !el.closest('.work-b, .fold-b'));
      const same = els.length === list.current.length && els.every((el, i) => el === list.current[i]);
      list.current = els;
      if (!same) setMarks(els.map(el => ({ user: el.classList.contains('msg-user'), text: preview(el) })));
      measureOffsets();
      visible();
    };
    const later = () => { if (!collectFrame) collectFrame = requestAnimationFrame(collect); };
    const watch = new MutationObserver(later);
    watch.observe(box, { childList: true, subtree: true });
    const size = new ResizeObserver(later);
    size.observe(box);
    box.addEventListener('scroll', onScroll, { passive: true });
    collect();
    return () => {
      watch.disconnect(); size.disconnect();
      box.removeEventListener('scroll', onScroll);
      if (collectFrame) cancelAnimationFrame(collectFrame);
      if (scrollFrame) cancelAnimationFrame(scrollFrame);
    };
  }, [scroller.current]);
  if (marks.length < 3) return null;
  // Un long fil garde une fenêtre de traits autour de ce qui est visible.
  const span = 36;
  const from = marks.length <= span ? 0 : Math.max(0, Math.min(marks.length - span, (seen[0] < 0 ? marks.length : seen[0]) - Math.floor(span / 2)));
  const shown = marks.slice(from, from + span);
  const go = i => {
    const box = scroller.current, el = list.current[i]; if (!box || !el) return;
    box.scrollTo({ top: Math.max(0, el.offsetTop - 24), behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  };
  return html`<nav class="minimap" aria-label="Messages" onMouseLeave=${() => setHover(-1)}>
    ${shown.map((m, n) => { const i = from + n; return html`<button key=${i} type="button" class=${cls('mm-tick', m.user && 'user', i >= seen[0] && i <= seen[1] && 'on')}
      aria-label=${m.text} onMouseEnter=${() => setHover(i)} onFocus=${() => setHover(i)} onClick=${() => go(i)}><i></i></button>`; })}
    ${hover >= from && hover < from + span && marks[hover]?.text && html`<span class="mm-tip" style=${`--i:${hover - from};--n:${shown.length}`}>${marks[hover].text}</span>`}
  </nav>`;
}
