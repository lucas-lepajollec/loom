// Champ vivant « Pixel Matrix » : trame fine tramée (Bayer 4×4 sur îlots
// organiques de bruit lent) + micro-croix « + » pixelisées, 100 % plat et net.
// Se met en pause hors écran / onglet caché et respecte prefers-reduced-motion.
import { html, useEffect, useRef, cls } from '../core/lib.js';

const BAYER4 = [
  0, 8, 2, 10,
  12, 4, 14, 6,
  3, 11, 1, 9,
  15, 7, 13, 5,
].map(v => (v + 0.5) / 16);

function hash2(ix, iy) {
  let n = Math.imul(ix, 374761393) + Math.imul(iy, 668265263);
  n = Math.imul(n ^ (n >>> 13), 1274126177);
  return ((n ^ (n >>> 16)) >>> 0) / 4294967295;
}

function vnoise(x, y) {
  const ix = Math.floor(x);
  const iy = Math.floor(y);
  const fx = x - ix;
  const fy = y - iy;
  const ux = fx * fx * fx * (fx * (fx * 6 - 15) + 10);
  const uy = fy * fy * fy * (fy * (fy * 6 - 15) + 10);
  const a = hash2(ix, iy);
  const b = hash2(ix + 1, iy);
  const c = hash2(ix, iy + 1);
  const d = hash2(ix + 1, iy + 1);
  return a + (b - a) * ux + (c - a) * uy + (a - b - c + d) * ux * uy;
}

// Champ basse fréquence sculptant de grands îlots tramés séparés par du vide calme.
function field(gx, gy, t) {
  const n1 = vnoise(gx * 0.028 + t * 0.075, gy * 0.028 - t * 0.038);
  const n2 = vnoise(gx * 0.068 - t * 0.05 + 19.3, gy * 0.068 + t * 0.045 + 7.1);
  const n3 = vnoise(gx * 0.015 - t * 0.02 + 41.7, gy * 0.015 + t * 0.018 + 13.4);
  return n1 * 0.56 + n2 * 0.24 + n3 * 0.2;
}

export function AmbientField({ variant = 'calm', class: className = '' }) {
  const ref = useRef(null);

  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const hero = variant === 'hero';
    const page = variant === 'page';
    const step = 5; // grain fin haute densité (5 px)
    const px = 1.6; // micro-point net
    let w = 0, h = 0, cols = 0, rows = 0, dpr = 1;
    let raf = 0, lastDraw = 0, visible = true, alive = true;
    const pointer = { gx: -999, gy: -999, active: false };
    let sparkles = [];

    const readInk = () => {
      const light = document.documentElement.dataset.theme === 'light';
      return light ? [16, 16, 20] : [232, 232, 238];
    };
    let ink = readInk();

    const buildSparkles = () => {
      const count = hero ? 11 : 8;
      sparkles = Array.from({ length: count }, (_, i) => ({
        gx: Math.floor(hash2(i * 17 + 3, 91) * Math.max(1, cols - 6)) + 3,
        gy: Math.floor(hash2(41, i * 29 + 7) * Math.max(1, rows - 6)) + 3,
        phase: hash2(i, 133) * Math.PI * 2,
        speed: 0.4 + hash2(i, 77) * 0.45,
      }));
    };

    const resize = () => {
      const rect = canvas.getBoundingClientRect();
      w = Math.max(1, Math.round(rect.width));
      h = Math.max(1, Math.round(rect.height));
      dpr = Math.min(2, window.devicePixelRatio || 1);
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      cols = Math.ceil(w / step);
      rows = Math.ceil(h / step);
      buildSparkles();
      drawFrame(performance.now());
    };

    const drawFrame = ms => {
      const t = reduced ? 14.2 : ms * 0.001;
      ctx.clearRect(0, 0, w, h);

      const [r, g, b] = ink;
      const levels = hero
        ? [
            `rgba(${r},${g},${b},0.075)`,
            `rgba(${r},${g},${b},0.16)`,
            `rgba(${r},${g},${b},0.28)`,
            `rgba(${r},${g},${b},0.44)`,
          ]
        : page
        ? [
            `rgba(${r},${g},${b},0.06)`,
            `rgba(${r},${g},${b},0.135)`,
            `rgba(${r},${g},${b},0.235)`,
            `rgba(${r},${g},${b},0.37)`,
          ]
        : [
            `rgba(${r},${g},${b},0.065)`,
            `rgba(${r},${g},${b},0.145)`,
            `rgba(${r},${g},${b},0.25)`,
            `rgba(${r},${g},${b},0.39)`,
          ];

      const b0 = [], b1 = [], b2 = [], b3 = [];

      for (let gy = 0; gy < rows; gy++) {
        const y = gy * step;
        const by = (gy & 3) << 2;
        for (let gx = 0; gx < cols; gx++) {
          let v = field(gx, gy, t);

          if (pointer.active) {
            const dx = gx - pointer.gx;
            const dy = gy - pointer.gy;
            const d2 = dx * dx + dy * dy;
            if (d2 < 420) v += (1 - Math.sqrt(d2) / 20.5) * 0.14;
          }

          // ~65 % d'espace sombre calme parsemé de rares micro-étoiles.
          if (v < 0.515) {
            const star = hash2(gx + 101, gy + 307);
            if (star > 0.9978) {
              const twinkle = 0.5 + 0.5 * Math.sin(t * 0.9 + star * 80);
              (twinkle > 0.6 ? b2 : b1).push(gx * step, y);
            }
            continue;
          }

          const d = Math.min(1, (v - 0.515) / 0.34);
          const threshold = BAYER4[by | (gx & 3)];
          if (d <= threshold * 0.94) continue;

          const x = gx * step;
          if (d > 0.82) b3.push(x, y);
          else if (d > 0.58) b2.push(x, y);
          else if (d > 0.32) b1.push(x, y);
          else b0.push(x, y);
        }
      }

      const buckets = [b0, b1, b2, b3];
      for (let lvl = 0; lvl < buckets.length; lvl++) {
        const pts = buckets[lvl];
        if (!pts.length) continue;
        ctx.fillStyle = levels[lvl];
        for (let i = 0; i < pts.length; i += 2) {
          ctx.fillRect(pts[i], pts[i + 1], px, px);
        }
      }

      // Micro-croix « + » nettes et discrètes.
      for (const s of sparkles) {
        const pulse = 0.25 + 0.75 * (0.5 + 0.5 * Math.sin(t * s.speed + s.phase));
        const cx = s.gx * step;
        const cy = s.gy * step;
        ctx.fillStyle = `rgba(${r},${g},${b},${(pulse * (hero ? 0.72 : 0.62)).toFixed(3)})`;
        ctx.fillRect(cx, cy, 2, 2);
        ctx.fillStyle = `rgba(${r},${g},${b},${(pulse * (hero ? 0.26 : 0.22)).toFixed(3)})`;
        ctx.fillRect(cx - 2.5, cy, 2, 2);
        ctx.fillRect(cx + 2.5, cy, 2, 2);
        ctx.fillRect(cx, cy - 2.5, 2, 2);
        ctx.fillRect(cx, cy + 2.5, 2, 2);
      }
    };

    const tick = ms => {
      raf = 0;
      if (!alive || !visible || document.hidden || reduced) return;
      if (ms - lastDraw >= 42) {
        lastDraw = ms;
        drawFrame(ms);
      }
      raf = requestAnimationFrame(tick);
    };
    const start = () => {
      if (!raf && alive && visible && !document.hidden && !reduced) raf = requestAnimationFrame(tick);
    };
    const stop = () => {
      if (raf) { cancelAnimationFrame(raf); raf = 0; }
    };

    const onMove = e => {
      const rect = canvas.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;
      pointer.active = x >= 0 && y >= 0 && x <= rect.width && y <= rect.height;
      if (pointer.active) {
        pointer.gx = x / step;
        pointer.gy = y / step;
      }
    };
    const onLeave = () => { pointer.active = false; };
    const onVis = () => { if (document.hidden) stop(); else start(); };

    const ro = new ResizeObserver(resize);
    ro.observe(canvas);
    const io = typeof IntersectionObserver !== 'undefined'
      ? new IntersectionObserver(entries => {
          visible = !!entries[0]?.isIntersecting;
          if (visible) start(); else stop();
        })
      : null;
    io?.observe(canvas);
    const mo = new MutationObserver(() => {
      ink = readInk();
      drawFrame(performance.now());
    });
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

    window.addEventListener('pointermove', onMove, { passive: true });
    window.addEventListener('pointerleave', onLeave, { passive: true });
    document.addEventListener('visibilitychange', onVis);

    resize();
    start();

    return () => {
      alive = false;
      stop();
      ro.disconnect();
      io?.disconnect();
      mo.disconnect();
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerleave', onLeave);
      document.removeEventListener('visibilitychange', onVis);
    };
  }, [variant]);

  return html`<canvas ref=${ref} class=${cls('ambient-field', 'ambient-' + variant, className)} aria-hidden="true"></canvas>`;
}
