import { useEffect, useRef, useState } from '../../core/lib.js';
import { t } from '../../core/i18n.js';
import { toast } from '../../ui/dialog.js';
import { WORKLET } from './audio.js';

export function useDictation(onText) {
  const [phase, setPhase] = useState('');
  const current = useRef(null);
  const release = run => {
    if (current.current !== run) return;
    current.current = null;
    clearTimeout(run.timer);
    run.socket?.close();
    run.stream?.getTracks().forEach(track => track.stop());
    run.audio?.close().catch(() => {});
    setPhase('');
  };
  useEffect(() => {
    const hide = () => { if (document.hidden && current.current) release(current.current); };
    document.addEventListener('visibilitychange', hide);
    return () => {
      document.removeEventListener('visibilitychange', hide);
      if (current.current) release(current.current);
    };
  }, []);
  const toggle = async () => {
    const active = current.current;
    if (active) {
      if (phase !== 'listening' || active.socket.readyState !== WebSocket.OPEN) { release(active); return; }
      active.finishing = true;
      active.stream.getTracks().forEach(track => track.stop());
      active.socket.send(JSON.stringify({ type: 'finish' }));
      active.timer = setTimeout(() => { release(active); toast(t('vm.lost'), 'err'); }, 8000);
      setPhase('finishing');
      return;
    }
    if (!window.isSecureContext || !navigator.mediaDevices) { toast(t('vm.insecure_note'), 'err'); return; }
    const run = { text: [] };
    current.current = run;
    setPhase('starting');
    const fail = message => {
      if (current.current === run) { release(run); toast(message, 'err'); }
    };
    try {
      const Audio = window.AudioContext || window.webkitAudioContext;
      const audio = run.audio = new Audio({ latencyHint: 'interactive' });
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1 } });
      if (current.current !== run) { stream.getTracks().forEach(track => track.stop()); return; }
      run.stream = stream;
      const blob = URL.createObjectURL(new Blob([WORKLET], { type: 'application/javascript' }));
      try { await audio.audioWorklet.addModule(blob); } finally { URL.revokeObjectURL(blob); }
      if (current.current !== run) return;
      const mic = new AudioWorkletNode(audio, 'loom-pcm16');
      const silence = audio.createGain();
      silence.gain.value = 0;
      mic.connect(silence);
      silence.connect(audio.destination);
      const socket = run.socket = new WebSocket(new URL('/api/voice/stream', location.href).href.replace(/^http/, 'ws'));
      socket.onmessage = e => {
        if (current.current !== run || typeof e.data !== 'string') return;
        let m;
        try { m = JSON.parse(e.data); } catch (_) { return; }
        if (m?.type === 'ready') {
          audio.createMediaStreamSource(stream).connect(mic);
          audio.resume().catch(() => {});
          setPhase('listening');
        } else if (m?.type === 'final' && typeof m.text === 'string' && m.text.trim()) {
          run.text.push(m.text.trim());
        } else if (m?.type === 'stt_done') {
          if (run.text.length) onText(run.text.join(' '));
          release(run);
        } else if (m?.type === 'error') fail(t('mobile.dictation_failed'));
      };
      socket.onclose = () => fail(t('mobile.dictation_failed'));
      mic.port.onmessage = e => {
        if (!run.finishing && socket.readyState === WebSocket.OPEN) socket.send(e.data.pcm);
      };
    } catch (err) {
      fail(err.name === 'NotAllowedError' ? t('vm.mic_denied') : t('mobile.dictation_failed'));
    }
  };
  return { phase, toggle };
}
