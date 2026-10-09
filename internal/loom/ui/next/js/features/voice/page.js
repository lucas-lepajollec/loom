// Voix : le moteur vocal (sherpa-onnx) se gère comme le moteur des modèles
// locaux. On choisit la machine, on installe le moteur et un pack par défaut
// (reconnaissance + voix + détection de parole), puis on règle, on essaie et on
// mesure. Contrat : docs/voice.md. Le mode vocal des discussions vient ensuite.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip } from '../../ui/controls.js';
import { ListPick } from '../../ui/listpick.js';
import { toast, confirm } from '../../ui/dialog.js';
import { get, post } from '../../core/api.js';
import { GroupPage } from '../../app/sections.js';
import { Line, Group } from '../settings/kit.js';

const fmtSize = n => !n ? '0' : n >= 1e9 ? t('voice.gb', { n: (n / 1e9).toFixed(1) }) : t('voice.mb', { n: (n / 1e6).toFixed(0) });
const KIND = () => ({ stt: t('voice.kind.stt'), tts: t('voice.kind.tts'), vad: t('voice.kind.vad'), kws: t('voice.kind.kws') });
const PACK = () => ({ 'fr-cpu-small': t('voice.pack.fr-cpu-small'), 'en-cpu-small': t('voice.pack.en-cpu-small'), 'multi-cpu': t('voice.pack.multi-cpu'), 'fr-gpu': t('voice.pack.fr-gpu') });
const PACK_WHY = () => ({ 'fr-cpu-small': t('voice.why.fr-cpu-small'), 'en-cpu-small': t('voice.why.en-cpu-small'), 'multi-cpu': t('voice.why.multi-cpu'), 'fr-gpu': t('voice.why.fr-gpu') });
const PHASE = () => ({ queued: t('voice.phase.queued'), downloading: t('voice.phase.downloading'), extracting: t('voice.phase.extracting'), complete: t('voice.phase.complete'), failed: t('voice.phase.failed'), cancelled: t('voice.phase.cancelled') });

export function VoicePage() {
  const [machine, setMachine] = useState('local'), [machines, setMachines] = useState([]);
  const [v, setV] = useState(null), [packs, setPacks] = useState([]), [models, setModels] = useState([]);
  const [busy, setBusy] = useState(''), [kind, setKind] = useState('');
  const q = (path, extra = '') => path + (path.includes('?') ? '&' : '?') + 'machine=' + encodeURIComponent(machine) + extra;
  const load = () => {
    get(q('/api/voice')).then(r => setV(r.ok === false ? { error: r.error } : r)).catch(e => setV({ error: e.message }));
    get(q('/api/voice/packs')).then(r => setPacks(r.packs || [])).catch(() => setPacks([]));
    get(q('/api/voice/models')).then(r => setModels(r.models || [])).catch(() => setModels([]));
  };
  useEffect(() => { get('/api/voice/node').then(r => { if (r.ok !== false) { setMachines(r.machines || []); setMachine(r.machine || 'local'); } }).catch(() => {}); }, []);
  useEffect(() => { setV(null); load(); }, [machine]);
  // Suivi d'un téléchargement en cours.
  useEffect(() => {
    if (!v || !v.download || !v.download.running) return;
    const id = setInterval(() => get(q('/api/voice/download/status')).then(r => {
      if (!r.download) return;
      setV(old => ({ ...old, download: r.download }));
      if (!r.download.running) { load(); if (r.download.error) toast(r.download.error, 'err'); }
    }).catch(() => {}), 1500);
    return () => clearInterval(id);
  }, [v && v.download && v.download.running, machine]);
  const act = async (key, path, body, ok) => {
    setBusy(key);
    const r = await post(q(path), body || {}, { timeout: 60000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy('');
    if (r.ok === false) { toast(r.error || t('voice.failed'), 'err'); return null; }
    if (ok) toast(ok);
    load(); return r;
  };
  const chooseMachine = async id => { setMachine(id); await post('/api/voice/node', { machine: id }).catch(() => {}); };
  if (!v) return html`<${GroupPage} title=${t('voice.title')} lead=${t('voice.lead')}><div class="skeleton" style="height:220px"></div></${GroupPage}>`;
  const e = v.engine || {}, cfg = v.config || {}, svc = v.service || {}, dl = v.download || {};
  const installed = models.filter(m => m.installed);
  const pick = k => installed.filter(m => m.kind === k).map(m => ({ value: m.id, label: m.id, note: (m.languages || []).join(', ') + ' · ' + fmtSize(m.disk_bytes || m.size) }));
  const saveCfg = patch => act('cfg', '/api/voice', { ...cfg, ...patch });
  const ready = e.installed && cfg.stt && cfg.tts && cfg.vad;
  return html`<${GroupPage} title=${t('voice.title')} lead=${t('voice.lead')}>
    <${Group} title=${t('voice.where')}>
      <${Line} label=${t('voice.machine')} tip=${t('voice.machine_tip')}><div class="set-pick"><${ListPick} label=${t('voice.machine')} value=${machine} onChange=${chooseMachine} options=${(machines.length ? machines : [{ id: 'local', name: 'Local' }]).map(m => ({ value: m.id, label: m.id === 'local' ? t('history.local') : m.name }))} /></div></${Line}>
      ${v.error && html`<div class="pad"><p class="note err">${v.error}</p></div>`}
    </${Group}>

    <${Group} title=${t('voice.engine')}>
      <${Line} label="sherpa-onnx" tip=${t('voice.engine_tip')}>
        ${e.installed ? html`<span class="state"><i class="dot green"></i>${e.version} · ${(e.selection && e.selection.provider || e.provider || 'cpu').toUpperCase()}</span>`
          : html`<span class="state">${t('settings.page.non_installe')}</span><button class="btn sm primary" disabled=${!!busy || dl.running || !e.cpu_installable} onClick=${() => act('install', '/api/voice/install', { cuda: false })}>${t('voice.install')}</button>
            ${e.nvidia && html`<button class="btn sm" disabled=${!!busy || dl.running} onClick=${() => act('install', '/api/voice/install', { cuda: true })}>${t('voice.install_gpu')}</button>`}`}</${Line}>
      ${v.runtime_requires && html`<div class="pad"><p class="note">${t('voice.requires', { what: v.runtime_requires })}</p></div>`}
      ${dl.running && html`<${Line} label=${PHASE()[dl.phase] || dl.phase}><span class="mono muted">${dl.artifact} ${dl.total ? Math.round(dl.received * 100 / dl.total) + ' %' : ''}</span><button class="btn sm ghost" onClick=${() => act('cancel', '/api/voice/download/cancel', {})}>${t('ui.dialog.annuler')}</button></${Line}>`}
    </${Group}>

    ${e.installed && html`<section class="set-group"><h3>${t('voice.packs')}<${Tip} text=${t('voice.packs_tip')} /></h3>
      <div class="mcards">${packs.map(p => { const pk = p.pack || {}; const have = [pk.stt, pk.tts, pk.vad].filter(Boolean).every(id => installed.some(m => m.id === id));
        return html`<div class="mcard" key=${pk.id}><div class="mcard-h"><span class="mx-ico"><${Icon} n="mic" /></span><span class="grow"><b>${PACK()[pk.id] || pk.id}</b><small>${[pk.language, pk.hardware, fmtSize(p.size)].filter(Boolean).join(' · ')}</small></span></div>
          <p class="mcard-d">${PACK_WHY()[pk.id] || pk.reason}</p>
          <p class="mcard-d vx-contents">${t('voice.pack_contains')} ${[pk.stt, pk.tts, pk.vad].filter(Boolean).map(id => html`<span class=${cls('tag', installed.some(m => m.id === id) && 'green')}>${id}</span>`)}</p>
          <div class="mcard-a">${have ? html`<span class="state"><i class="dot green"></i>${t('voice.pack_installed')}</span>`
            : html`<button class="btn sm" disabled=${!!busy || dl.running || !p.installable} title=${p.requires_nvidia ? t('voice.needs_gpu') : ''} onClick=${() => act('pack', '/api/voice/packs', { id: pk.id, cuda: !!p.requires_nvidia })}>${t('voice.install')}</button>`}</div></div>`; })}</div>
    </section>`}

    ${e.installed && html`<${Group} title=${t('voice.service')}>
      <${Line} label=${t('voice.state')}><span class="state"><i class=${'dot ' + (svc.running ? 'green' : '')}></i>${svc.running ? t('voice.running') : t('voice.stopped')}</span>
        ${svc.running ? html`<button class="btn sm" disabled=${!!busy} onClick=${() => act('svc', '/api/voice/service', { action: 'restart' })}><${Icon} n="refresh" />${t('local.page.redemarrer')}</button><button class="btn sm ghost" disabled=${!!busy} onClick=${() => act('svc', '/api/voice/service', { action: 'stop' })}>${t('local.page.arreter')}</button>`
          : html`<button class="btn sm primary" disabled=${!!busy || !ready} title=${ready ? '' : t('voice.choose_models')} onClick=${() => act('svc', '/api/voice/service', { action: 'start' })}><${Icon} n="play" />${t('local.page.demarrer')}</button>`}</${Line}>
      ${svc.error && html`<div class="pad"><p class="note err">${svc.error}</p></div>`}
      <${Line} label=${t('voice.kind.stt')}><div class="set-pick"><${ListPick} label=${t('voice.kind.stt')} disabled=${svc.running} value=${cfg.stt} onChange=${x => saveCfg({ stt: x })} options=${pick('stt')} /></div></${Line}>
      <${Line} label=${t('voice.kind.tts')}><div class="set-pick"><${ListPick} label=${t('voice.kind.tts')} disabled=${svc.running} value=${cfg.tts} onChange=${x => saveCfg({ tts: x })} options=${pick('tts')} /></div></${Line}>
      <${Line} label=${t('voice.kind.vad')}><div class="set-pick"><${ListPick} label=${t('voice.kind.vad')} disabled=${svc.running} value=${cfg.vad} onChange=${x => saveCfg({ vad: x })} options=${pick('vad')} /></div></${Line}>
      <${Line} label=${t('voice.speed')} tip=${t('voice.speed_tip')}><input class="input sm num" type="number" min="0.5" max="2" step="0.05" disabled=${svc.running} value=${cfg.speed} onChange=${ev => saveCfg({ speed: Number(ev.target.value) })} /></${Line}>
      <${Line} label=${t('voice.voice_id')} tip=${t('voice.voice_id_tip')}><input class="input sm num" type="number" min="0" step="1" disabled=${svc.running} value=${cfg.voice} onChange=${ev => saveCfg({ voice: Number(ev.target.value) })} /></${Line}>
      <${Line} label=${t('voice.silence')} tip=${t('voice.silence_tip')}><input class="input sm num" type="number" min="0.2" max="3" step="0.1" disabled=${svc.running} value=${cfg.endpoint_silence} onChange=${ev => saveCfg({ endpoint_silence: Number(ev.target.value) })} /></${Line}>
      <${Line} label=${t('voice.idle')} tip=${t('voice.idle_tip')}><input class="input sm num" type="number" min="0" step="5" disabled=${svc.running} value=${cfg.idle_unload_minutes} onChange=${ev => saveCfg({ idle_unload_minutes: Number(ev.target.value) })} /></${Line}>
      <${Line} label=${t('voice.boot')} tip=${t('voice.boot_tip')}><${Switch} label=${t('voice.boot')} disabled=${svc.running || !ready} checked=${cfg.boot} onChange=${on => saveCfg({ boot: on })} /></${Line}>
      ${svc.running && html`<div class="pad"><p class="note">${t('voice.stop_to_change')}</p></div>`}
    </${Group}>`}

    ${ready && html`<${VoiceTest} q=${q} />`}

    ${e.installed && html`<details class="set-group vx-lib" open=${!packs.length}><summary><h3>${t('voice.library')} <span class="count">${fmtSize(v.disk_bytes)}</span></h3></summary>
      <p class="note vx-lib-note">${t('voice.library_note')}</p>
      <div class="vx-filter"><${ListPick} label=${t('voice.kind_filter')} value=${kind} onChange=${setKind} options=${[{ value: '', label: t('voice.all') }, ...Object.entries(KIND()).map(([k, l]) => ({ value: k, label: l }))]} /></div>
      <div class="card">${models.filter(m => !kind || m.kind === kind).map(m => html`<div class="set-line" key=${m.id}>
        <div class="set-l vx-model"><span><b>${m.id}</b> <span class="tag">${KIND()[m.kind] || m.kind}</span>${[cfg.stt, cfg.tts, cfg.vad].includes(m.id) && html` <span class="tag green">${t('voice.in_use')}</span>`}${m.streaming && html` <span class="tag">${t('voice.streaming')}</span>`}</span><small class="muted">${[(m.languages || []).join(', '), m.family, fmtSize(m.installed ? m.disk_bytes : m.size)].filter(Boolean).join(' · ')}</small></div>
        <div class="set-c">${m.installed ? html`<button class="icon-btn" aria-label=${t('settings.page.supprimer')} disabled=${!!busy || svc.running} onClick=${async () => { if (await confirm(t('voice.delete_title', { id: m.id }), t('voice.delete_text'), { ok: t('settings.page.supprimer'), danger: true })) act('del', '/api/voice/models/delete', { id: m.id }); }}><${Icon} n="trash" /></button>`
          : html`<button class="btn sm ghost" disabled=${!!busy || dl.running || !m.installable} onClick=${() => act('dl', '/api/voice/models/download', { id: m.id })}><${Icon} n="download" />${t('voice.download')}</button>`}</div></div>`)}</div>
    </details>`}
  </${GroupPage}>`;
}

// Essai : une phrase lue à voix haute, et le banc (latence, temps réel).
function VoiceTest({ q }) {
  const [text, setText] = useState(t('voice.sample'));
  const [busy, setBusy] = useState(false), [bench, setBench] = useState(null);
  const audio = useRef(null);
  const speak = async () => {
    setBusy(true);
    try {
      const res = await fetch(q('/api/voice/test/tts'), { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text }), credentials: 'same-origin' });
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || t('voice.failed'));
      const url = URL.createObjectURL(await res.blob());
      if (audio.current) { audio.current.pause(); URL.revokeObjectURL(audio.current.src); }
      audio.current = new Audio(url); await audio.current.play();
    } catch (e) { toast(e.message, 'err'); }
    setBusy(false);
  };
  const runBench = async () => {
    setBusy(true);
    const r = await post(q('/api/voice/bench'), {}, { timeout: 120000 }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false) return toast(r.error || t('voice.failed'), 'err');
    setBench(r);
  };
  const ms = x => Math.round(x) + ' ms';
  return html`<${Group} title=${t('voice.try')}>
    <div class="pad vx-try"><textarea class="textarea" rows="2" value=${text} onInput=${e => setText(e.target.value)}></textarea>
      <div class="set-actions"><button class="btn primary" disabled=${busy || !text.trim()} onClick=${speak}><${Icon} n="play" />${t('voice.speak')}</button><button class="btn" disabled=${busy} onClick=${runBench}><${Icon} n="gauge" />${t('voice.bench')}</button></div></div>
    ${bench && html`<${Line} label=${t('voice.kind.tts')}><span class="mono">${t('voice.first_audio')} ${ms(bench.tts.first_audio_ms)} · ×${(1 / bench.tts.real_time_factor).toFixed(1)} ${t('voice.realtime')}</span></${Line}>
      <${Line} label=${t('voice.kind.stt')}><span class="mono">${ms(bench.stt.latency_ms)} · ×${(1 / bench.stt.real_time_factor).toFixed(1)} ${t('voice.realtime')}</span></${Line}>
      <${Line} label=${t('voice.heard')} stack><span class="note">« ${bench.stt.text} » — ${t('voice.expected')} « ${bench.sample_text} »</span></${Line}>`}
  </${Group}>`;
}
