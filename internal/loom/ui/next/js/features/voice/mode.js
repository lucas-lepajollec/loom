// Mode vocal : une page plein écran à la place de la discussion. Une sphère
// réagit à ta voix et à celle de Jarvis, la transcription s'affiche en
// direct dessous, une croix referme. À la fermeture, l'échange peut être
// ajouté à la discussion d'origine (sans déclencher de réponse).
//
// Audio : micro → AudioWorklet (16 kHz PCM16) → WebSocket /api/voice/stream
// (transcription en continu) ; phrase finale → /api/voice/jarvis/turn (texte
// en flux) → phrases envoyées une à une à la synthèse → lecture planifiée.
// Couper la parole à Jarvis (barge-in) arrête la voix, la file et la réponse.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { get, post } from '../../core/api.js';

// Rééchantillonne le micro en PCM16 16 kHz par blocs de ~100 ms et mesure le niveau.
const WORKLET = `
class Pcm16 extends AudioWorkletProcessor {
  constructor() { super(); this.ratio = sampleRate / 16000; this.acc = 0; this.buf = new Int16Array(1600); this.n = 0; }
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (!ch) return true;
    let peak = 0;
    for (let i = 0; i < ch.length; i++) {
      const s = ch[i]; const a = s < 0 ? -s : s; if (a > peak) peak = a;
      this.acc += 1;
      if (this.acc >= this.ratio) {
        this.acc -= this.ratio;
        this.buf[this.n++] = Math.max(-1, Math.min(1, s)) * 0x7fff;
        if (this.n === this.buf.length) { this.port.postMessage({ pcm: this.buf.buffer, peak }, [this.buf.buffer]); this.buf = new Int16Array(1600); this.n = 0; peak = 0; }
      }
    }
    return true;
  }
}
registerProcessor('loom-pcm16', Pcm16);`;

// Découpe un texte en phrases prononçables dès qu'elles sont complètes.
export function takeSentences(buffer, final) {
  const out = [];
  let rest = buffer;
  const re = /^(.+?[.!?…:;](?:["»)\]]*)?)(\s+|$)/s;
  for (;;) {
    const m = rest.match(re);
    if (!m || (!m[2] && !final)) break;
    out.push(m[1].trim()); rest = rest.slice(m[0].length);
  }
  if (rest.length > 220) { const cut = rest.lastIndexOf(', ', 200); if (cut > 40) { out.push(rest.slice(0, cut + 1).trim()); rest = rest.slice(cut + 2); } }
  if (final && rest.trim()) { out.push(rest.trim()); rest = ''; }
  return { sentences: out.filter(Boolean), rest };
}

// Sphère : un seul canvas, ~30 i/s sur mobile, arrêt quand l'onglet est caché.
function Orb({ levels, state }) {
  const ref = useRef();
  useEffect(() => {
    const c = ref.current, ctx = c.getContext('2d');
    const mobile = matchMedia('(max-width:720px)').matches;
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const dpr = Math.min(devicePixelRatio || 1, mobile ? 1.5 : 2);
    let raf = 0, last = 0, tm = 0, me = 0, them = 0;
    const size = () => { const r = c.getBoundingClientRect(); c.width = r.width * dpr; c.height = r.height * dpr; };
    size(); const ro = new ResizeObserver(size); ro.observe(c);
    const frame = now => {
      raf = requestAnimationFrame(frame);
      if (document.hidden || (mobile && now - last < 33)) return;
      const dt = Math.min(0.1, (now - (last || now)) / 1000); last = now; tm += dt;
      const L = levels.current;
      me += (L.me - me) * 0.25; them += (L.them - them) * 0.2;
      const w = c.width, h = c.height, cx = w / 2, cy = h / 2, base = Math.min(w, h) * 0.22;
      const css = getComputedStyle(c);
      const fg = css.getPropertyValue('--orb-fg').trim() || '#f2f2f0', glow = css.getPropertyValue('--orb-glow').trim() || 'rgba(255,255,255,.18)';
      ctx.clearRect(0, 0, w, h);
      const st = L.state;
      const breathe = reduced ? 0 : Math.sin(tm * (st === 'thinking' ? 3 : 1.2)) * 0.03;
      const energy = Math.min(1, me * 2.2 + them * 2.2);
      const r = base * (1 + breathe + energy * 0.35);
      // halo
      const g = ctx.createRadialGradient(cx, cy, r * 0.4, cx, cy, r * 2.1);
      g.addColorStop(0, glow); g.addColorStop(1, 'rgba(0,0,0,0)');
      ctx.fillStyle = g; ctx.beginPath(); ctx.arc(cx, cy, r * 2.1, 0, Math.PI * 2); ctx.fill();
      // anneaux de Jarvis (sa voix pousse des ondes vers l'extérieur)
      if (!reduced) for (let k = 0; k < 3; k++) {
        const ph = (tm * (0.35 + them) + k / 3) % 1;
        const a = (1 - ph) * (0.12 + them * 0.7);
        if (a < 0.01) continue;
        ctx.strokeStyle = fg; ctx.globalAlpha = a; ctx.lineWidth = dpr * (1 + them * 2);
        ctx.beginPath(); ctx.arc(cx, cy, r * (1.05 + ph * 0.9), 0, Math.PI * 2); ctx.stroke();
      }
      ctx.globalAlpha = 1;
      // corps : contour déformé par ta voix
      const pts = 96, amp = reduced ? 0 : r * (0.04 + me * 0.45 + (st === 'thinking' ? 0.05 : 0));
      ctx.beginPath();
      for (let i = 0; i <= pts; i++) {
        const a = (i / pts) * Math.PI * 2;
        const n = Math.sin(a * 3 + tm * 2.1) * 0.5 + Math.sin(a * 5 - tm * 1.7) * 0.3 + Math.sin(a * 7 + tm * 3.3) * 0.2;
        const rr = r + n * amp;
        const x = cx + Math.cos(a) * rr, y = cy + Math.sin(a) * rr;
        i ? ctx.lineTo(x, y) : ctx.moveTo(x, y);
      }
      const body = ctx.createRadialGradient(cx - r * 0.3, cy - r * 0.35, r * 0.1, cx, cy, r * 1.3);
      body.addColorStop(0, fg); body.addColorStop(1, css.getPropertyValue('--orb-edge').trim() || '#5c5c58');
      ctx.fillStyle = body; ctx.globalAlpha = st === 'idle' ? 0.55 : 0.92; ctx.fill(); ctx.globalAlpha = 1;
    };
    raf = requestAnimationFrame(frame);
    return () => { cancelAnimationFrame(raf); ro.disconnect(); };
  }, []);
  return html`<canvas ref=${ref} class=${cls('vm-orb', state)} aria-hidden="true"></canvas>`;
}

export function VoiceMode({ discussionId, internet = false, onClose }) {
  const [phase, setPhase] = useState('starting'); // starting | listening | thinking | speaking | error | insecure | unavailable
  const [error, setErrorRaw] = useState('');
  // Erreurs codées du serveur (« jarvis_xxx: détail ») : une phrase claire par cas.
  const setError = m => { const c = /^(jarvis_\w+):\s*(?:Load (.+?) in the engine)?/.exec(m || ''); setErrorRaw(!c ? m : ({ jarvis_model_not_loaded: c[2] ? t('vm.err_not_loaded', { model: c[2].split('/').pop() }) : t('vm.err_no_local'), jarvis_no_fallback: t('vm.err_no_fallback'), jarvis_model_unavailable: t('vm.err_unavailable') })[c[1]] || m); };
  const [lines, setLines] = useState([]); // {who:'me'|'jarvis', text, live?}
  const [ending, setEnding] = useState(false);
  const levels = useRef({ me: 0, them: 0, state: 'idle' });
  const rt = useRef({});
  const setState = s => { levels.current.state = s; setPhase(s); };
  const upsert = (who, text, live) => setLines(old => {
    const next = old.slice();
    const lastIdx = next.length - 1;
    if (lastIdx >= 0 && next[lastIdx].who === who && next[lastIdx].live) next[lastIdx] = { who, text, live };
    else next.push({ who, text, live });
    return next.slice(-40);
  });

  useEffect(() => {
    if (!window.isSecureContext || !navigator.mediaDevices) { setState('insecure'); return; }
    let closed = false;
    const r = rt.current;
    r.queue = []; r.speaking = false; r.turnAbort = null; r.speechId = 0;
    const start = async () => {
      // Session Jarvis + vérifs du moteur vocal.
      const v = await get('/api/voice').catch(() => null);
      if (!v || !v.engine || !v.engine.installed || !(v.config && v.config.stt && v.config.tts)) { setState('unavailable'); return; }
      if (!v.service || !v.service.running) await post('/api/voice/service', { action: 'start' }).catch(() => {});
      const s = await post('/api/voice/jarvis/session', { discussion_id: discussionId || '', internet }).catch(e => ({ ok: false, error: e.message }));
      if (s.ok === false) throw new Error(s.error || t('vm.failed'));
      r.session = s.session_id;
      // Micro.
      r.stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true, channelCount: 1 } });
      r.ac = new (window.AudioContext || window.webkitAudioContext)({ latencyHint: 'interactive' });
      const url = URL.createObjectURL(new Blob([WORKLET], { type: 'application/javascript' }));
      await r.ac.audioWorklet.addModule(url); URL.revokeObjectURL(url);
      const src = r.ac.createMediaStreamSource(r.stream);
      r.node = new AudioWorkletNode(r.ac, 'loom-pcm16');
      src.connect(r.node);
      // Sortie : un analyseur mesure la voix de Jarvis.
      r.out = r.ac.createAnalyser(); r.out.fftSize = 512; r.out.connect(r.ac.destination);
      r.playAt = 0; r.sources = new Set();
      const outBuf = new Uint8Array(r.out.fftSize);
      r.meter = setInterval(() => {
        r.out.getByteTimeDomainData(outBuf);
        let p = 0; for (const b of outBuf) { const a = Math.abs(b - 128) / 128; if (a > p) p = a; }
        levels.current.them = r.sources.size ? p : 0;
      }, 50);
      // Flux voix.
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      r.ws = new WebSocket(proto + '//' + location.host + '/api/voice/stream');
      r.ws.binaryType = 'arraybuffer';
      r.node.port.onmessage = ev => {
        levels.current.me = r.speaking ? ev.data.peak * 0.4 : ev.data.peak;
        if (r.ws.readyState === 1) r.ws.send(ev.data.pcm);
      };
      r.ws.onmessage = ev => typeof ev.data === 'string' ? onMessage(JSON.parse(ev.data)) : play(ev.data);
      r.ws.onclose = () => { if (!closed) { setError(t('vm.lost')); setState('error'); } };
      r.ws.onerror = () => {};
      setState('listening');
    };
    const play = buf => {
      if (!r.rate || !r.ac) return;
      const pcm = new Int16Array(buf), f = new Float32Array(pcm.length);
      for (let i = 0; i < pcm.length; i++) f[i] = pcm[i] / 0x8000;
      const ab = r.ac.createBuffer(1, f.length, r.rate); ab.copyToChannel(f, 0);
      const s = r.ac.createBufferSource(); s.buffer = ab; s.connect(r.out);
      const at = Math.max(r.ac.currentTime + 0.02, r.playAt); s.start(at); r.playAt = at + ab.duration;
      r.sources.add(s); s.onended = () => { r.sources.delete(s); if (!r.sources.size && !r.speaking && !r.queue.length && !r.thinking) setState('listening'); };
    };
    const stopVoice = () => {
      for (const s of r.sources) { try { s.stop(); } catch (_) {} }
      r.sources.clear(); r.playAt = 0; r.queue = [];
      if (r.speaking && r.ws && r.ws.readyState === 1) r.ws.send(JSON.stringify({ type: 'cancel', id: 'jv-' + r.speechId }));
      r.speaking = false;
      if (r.turnAbort) { r.turnAbort.abort(); r.turnAbort = null; }
      r.thinking = false;
    };
    const speakNext = () => {
      if (r.speaking || !r.queue.length || !r.ws || r.ws.readyState !== 1) return;
      r.speaking = true; r.speechId++;
      r.ws.send(JSON.stringify({ type: 'speak', id: 'jv-' + r.speechId, text: r.queue.shift() }));
      setState('speaking');
    };
    const onMessage = m => {
      if (m.type === 'partial' && m.text && m.text.trim()) {
        // Couper la parole : quelques mots pendant que Jarvis parle.
        if ((r.speaking || r.sources.size || r.thinking) && m.text.trim().split(/\s+/).length >= 2) { stopVoice(); r.bargeIn = true; }
        upsert('me', m.text, true);
      } else if (m.type === 'final' && m.text && m.text.trim()) {
        // Sa propre voix captée par le micro pendant qu'il parle : ignorée.
        if ((r.speaking || r.sources.size) && !r.bargeIn) return;
        r.bargeIn = false;
        upsert('me', m.text.trim(), false);
        ask(m.text.trim());
      } else if (m.type === 'audio_start') { r.rate = m.sample_rate; }
      else if (m.type === 'audio_end' || m.type === 'cancelled') { r.speaking = false; speakNext(); }
      else if (m.type === 'error') { setError(m.error || t('vm.failed')); }
    };
    const ask = async text => {
      stopVoice();
      r.thinking = true; setState('thinking');
      const ac = new AbortController(); r.turnAbort = ac;
      let said = '', buffer = '';
      try {
        const res = await fetch('/api/voice/jarvis/turn', { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin', body: JSON.stringify({ session_id: r.session, text }), signal: ac.signal });
        if (!res.ok || !res.body) throw new Error((await res.json().catch(() => ({}))).error || t('vm.failed'));
        const reader = res.body.getReader(), dec = new TextDecoder();
        let pending = '';
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          pending += dec.decode(value, { stream: true });
          let i;
          while ((i = pending.indexOf('\n\n')) >= 0) {
            const chunk = pending.slice(0, i); pending = pending.slice(i + 2);
            const data = chunk.split('\n').filter(l => l.startsWith('data:')).map(l => l.slice(5).trim()).join('');
            if (!data) continue;
            const ev = JSON.parse(data);
            if (ev.type === 'status') {
              setState(ev.text === 'searching' ? 'searching' : 'thinking');
            } else if (ev.type === 'delta') {
              said += ev.text; buffer += ev.text;
              upsert('jarvis', said, true);
              const { sentences, rest } = takeSentences(buffer, false); buffer = rest;
              if (sentences.length) { r.queue.push(...sentences); speakNext(); }
            } else if (ev.type === 'error') throw new Error(ev.error);
          }
        }
        const { sentences } = takeSentences(buffer, true);
        if (sentences.length) { r.queue.push(...sentences); speakNext(); }
        upsert('jarvis', said, false);
      } catch (e) {
        if (e.name !== 'AbortError') { setError(e.message); }
        if (said) upsert('jarvis', said, false);
      } finally {
        r.thinking = false; if (r.turnAbort === ac) r.turnAbort = null;
        if (!r.speaking && !r.sources.size && !r.queue.length) setState('listening');
      }
    };
    start().catch(e => { if (!closed) { setError(e.name === 'NotAllowedError' ? t('vm.mic_denied') : e.message); setState('error'); } });
    const decay = setInterval(() => { levels.current.me *= 0.85; }, 60);
    return () => {
      closed = true; clearInterval(decay); clearInterval(r.meter);
      try { stopVoice(); } catch (_) {}
      try { r.ws && r.ws.close(); } catch (_) {}
      try { r.stream && r.stream.getTracks().forEach(tr => tr.stop()); } catch (_) {}
      try { r.ac && r.ac.close(); } catch (_) {}
    };
  }, []);

  const finish = async inject => {
    const id = rt.current.session;
    if (id) await post('/api/voice/jarvis/end', { session_id: id, inject }).catch(() => {});
    onClose(inject);
  };
  const exchanged = lines.some(l => l.who === 'jarvis' && !l.live);
  const close = () => (discussionId && exchanged ? setEnding(true) : finish(false));
  const label = { starting: t('vm.starting'), listening: t('vm.listening'), thinking: t('vm.thinking'), searching: t('vm.searching'), speaking: t('vm.speaking'), error: t('vm.error') }[phase] || '';
  return html`<div class="voice-mode" role="dialog" aria-label=${t('vm.title')}>
    <button class="icon-btn vm-close" aria-label=${t('vm.close')} onClick=${close}><${Icon} n="close" /></button>
    ${phase === 'insecure' ? html`<${Insecure} onClose=${() => onClose(false)} />`
      : phase === 'unavailable' ? html`<div class="vm-msg"><h2>${t('vm.unavailable')}</h2><p>${t('vm.unavailable_note')}</p><a class="btn primary" href="#/voice" onClick=${() => onClose(false)}>${t('vm.open_voice')}</a></div>`
      : html`<div class="vm-stage"><${Orb} levels=${levels} state=${phase} /><div class="vm-state" aria-live="polite">${label}</div></div>
        <div class="vm-lines" aria-live="polite">${lines.slice(-6).map((l, i) => html`<p key=${i} class=${cls('vm-line', l.who, l.live && 'live')}>${l.text}</p>`)}
          ${error && html`<p class="vm-line err">${error}</p>`}</div>`}
    ${ending && html`<div class="vm-end"><div class="card pad"><b>${t('vm.keep_title')}</b><p class="note">${t('vm.keep_note')}</p>
      <div class="set-actions"><button class="btn primary" onClick=${() => finish(true)}>${t('vm.keep')}</button><button class="btn ghost" onClick=${() => finish(false)}>${t('vm.discard')}</button></div></div></div>`}
  </div>`;
}

// Le micro n'est accessible qu'en HTTPS (ou sur cette machine) : on propose l'adresse sûre.
function Insecure({ onClose }) {
  const [https, setHttps] = useState(null);
  useEffect(() => { get('/api/https').then(r => setHttps(r.ok === false ? null : r)).catch(() => {}); }, []);
  const url = https && https.enabled && (https.urls || [])[0];
  return html`<div class="vm-msg"><h2>${t('vm.insecure')}</h2><p>${t('vm.insecure_note')}</p>
    ${url ? html`<a class="btn primary" href=${url + location.pathname + location.hash}>${t('vm.open_https')}</a><p class="note">${t('vm.cert_note')}</p>`
      : html`<a class="btn primary" href="#/settings/security" onClick=${onClose}>${t('vm.enable_https')}</a>`}</div>`;
}
