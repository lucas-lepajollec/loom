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
  useEffect(() => {
    const box = scroller.current; if (!box) return;
    let frame = 0;
    const visible = () => {
      const top = box.scrollTop, bottom = top + box.clientHeight;
      let first = -1, last = -1;
      list.current.forEach((el, i) => {
        const a = el.offsetTop, b = a + el.offsetHeight;
        if (b > top + 8 && a < bottom - 8) { if (first < 0) first = i; last = i; }
      });
      setSeen(s => s[0] === first && s[1] === last ? s : [first, last]);
    };
    const collect = () => {
      frame = 0;
      const els = [...box.querySelectorAll(MARKS)].filter(el => !el.closest('.work-b, .fold-b'));
      const same = els.length === list.current.length && els.every((el, i) => el === list.current[i]);
      list.current = els;
      if (!same) setMarks(els.map(el => ({ user: el.classList.contains('msg-user'), text: preview(el) })));
      visible();
    };
    const later = () => { if (!frame) frame = requestAnimationFrame(collect); };
    const watch = new MutationObserver(later);
    watch.observe(box, { childList: true, subtree: true });
    const size = new ResizeObserver(later);
    size.observe(box);
    box.addEventListener('scroll', visible, { passive: true });
    collect();
    return () => { watch.disconnect(); size.disconnect(); box.removeEventListener('scroll', visible); if (frame) cancelAnimationFrame(frame); };
  }, [scroller.current]);
  if (marks.length < 3) return null;
  const go = i => {
    const box = scroller.current, el = list.current[i]; if (!box || !el) return;
    box.scrollTo({ top: Math.max(0, el.offsetTop - 24), behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  };
  return html`<nav class="minimap" aria-label="Messages" onMouseLeave=${() => setHover(-1)}>
    ${marks.map((m, i) => html`<button key=${i} type="button" class=${cls('mm-tick', m.user && 'user', i >= seen[0] && i <= seen[1] && 'on')}
      aria-label=${m.text} onMouseEnter=${() => setHover(i)} onFocus=${() => setHover(i)} onClick=${() => go(i)}><i></i></button>`)}
    ${hover >= 0 && marks[hover] && marks[hover].text && html`<span class="mm-tip" style=${`--i:${hover};--n:${marks.length}`}>${marks[hover].text}</span>`}
  </nav>`;
}
