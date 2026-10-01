// Réglages : Général, Moteur, Internet, Sécurité, À propos. Une ligne par
// réglage, l'explication en ⓘ, jamais en paragraphe sous chaque option.
import { html, useState, useEffect, useRef, useStore, cls, fmtBytes } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip, Seg, Empty } from '../../ui/controls.js';
import { Modal, confirm, prompt, toast } from '../../ui/dialog.js';
import { get, post, download, setToken } from '../../core/api.js';
import { copyText } from '../../ui/clipboard.js';
import { app, go, setTheme, refreshStatus, refreshLibrary, refreshNav, refreshEngineNode } from '../../core/state.js';
import { liveSource } from '../inspector/params.js';
import { Config } from '../inspector/config.js';
import { Line, Group } from './kit.js';
import { MachinesSettings } from './machines.js';

const SECTIONS = [['general', 'Général', 'gear'], ['machines', 'Machines', 'server'], ['engine', 'Moteur llama.cpp', 'chip'], ['internet', 'Internet', 'globe'], ['security', 'Sécurité et données', 'lock'], ['about', 'À propos', 'info']];


function usePref() {
  const [p, setP] = useState(null);
  useEffect(() => { get('/api/prefs').then(r => setP(r.prefs || {})); }, []);
  const set = (k, v) => { const n = { ...p, [k]: v }; setP(n); post('/api/prefs', n); try { localStorage.setItem('loom-' + k.replace('_', '-'), v); } catch (_) {} };
  return [p, set];
}

function General() {
  const theme = useStore(app, s => s.theme);
  const [p, setPref] = usePref();
  const [sys, setSys] = useState(null);
  const [agent, setAgent] = useState(null);
  useEffect(() => { get('/api/sysprompt').then(r => setSys(r.text || '')); get('/api/agent').then(setAgent); }, []);
  const saveSys = async () => { await post('/api/sysprompt', { text: sys }); toast('Prompt système enregistré'); };
  return html`
    <${Group} title="Apparence">
      <${Line} label="Thème"><${Seg} size="sm" value=${theme} onChange=${t => { setTheme(t); setPref('theme', t); }} options=${[{ value: 'dark', label: 'Sombre' }, { value: 'light', label: 'Clair' }]} /></${Line}>
      ${p && html`
        <${Line} label="Masquer la réflexion" tip="Les blocs de réflexion des modèles ne s’affichent plus dans le fil."><${Switch} checked=${p.hide_reasoning === '1'} onChange=${v => setPref('hide_reasoning', v ? '1' : '0')} /></${Line}>
        <${Line} label="Masquer les appels d’outils"><${Switch} checked=${p.hide_tools === '1'} onChange=${v => setPref('hide_tools', v ? '1' : '0')} /></${Line}>
        <${Line} label="Entrée pour aller à la ligne" tip="Par défaut, Entrée envoie et Maj+Entrée va à la ligne."><${Switch} checked=${p.enter_newline === '1'} onChange=${v => setPref('enter_newline', v ? '1' : '0')} /></${Line}>`}
    </${Group}>
    <${Group} title="Discussions">
      <${PushNotifications} />
      ${agent && html`<${Line} label="Compactage automatique" tip="Vers 75 % du contexte, les anciens tours sont résumés pour laisser de la place. Le début et la fin restent intacts.">
        <${Switch} checked=${agent.compact} onChange=${async v => { await post('/api/agent/compact', { on: v }); setAgent({ ...agent, compact: v }); }} /></${Line}>`}
      <${Line} label="Prompt système global" tip="Envoyé à tous les modèles locaux, sauf si un modèle ou un preset a le sien." stack>
        ${sys !== null && html`<textarea class="textarea" rows="4" value=${sys} onInput=${e => setSys(e.target.value)} placeholder="ex. Réponds en français, de façon concise."></textarea>
          <div><button class="btn sm" onClick=${saveSys}>Enregistrer</button></div>`}</${Line}>
    </${Group}>`;
}

function PushNotifications() {
  const supported = window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
  const [checked, setChecked] = useState(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState('Une notification à chaque réponse terminée, même si Loom est fermé.');
  const running = useRef(false);
  const register = () => navigator.serviceWorker.register('/sw.js', { scope: '/' });
  useEffect(() => {
    if (!supported) return;
    let alive = true;
    register().then(r => r.pushManager.getSubscription()).then(s => { if (alive) setChecked(!!s); })
      .catch(() => { if (alive) setNote('État des notifications inconnu. Réessaie en rouvrant ces réglages.'); });
    return () => { alive = false; };
  }, []);
  const toggle = async want => {
    if (!supported || running.current) return;
    running.current = true; setBusy(true);
    let created = null;
    try {
      // La demande de permission reste directement liée au clic.
      if (want && await Notification.requestPermission() !== 'granted') {
        setNote('Notifications bloquées. Autorise-les dans les réglages du navigateur.');
        return;
      }
      const reg = await register();
      let sub = await reg.pushManager.getSubscription();
      if (want) {
        if (!sub) {
          const r = await get('/api/push/key');
          if (!r.ok || !r.key) throw new Error();
          const b64 = (r.key + '='.repeat((4 - r.key.length % 4) % 4)).replace(/-/g, '+').replace(/_/g, '/');
          sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: Uint8Array.from(atob(b64), c => c.charCodeAt(0)) });
          created = sub;
        }
        const r = await post('/api/push/subscribe', sub.toJSON());
        if (!r.ok) throw new Error();
      } else if (sub) {
        const r = await post('/api/push/unsubscribe', { endpoint: sub.endpoint });
        if (!r.ok) throw new Error();
        try {
          if (!await sub.unsubscribe()) throw new Error();
        } catch (e) {
          // Si le navigateur conserve l’abonnement, rétablit aussi le serveur.
          await post('/api/push/subscribe', sub.toJSON()).catch(() => {});
          throw e;
        }
      }
      setChecked(want);
      setNote('Une notification à chaque réponse terminée, même si Loom est fermé.');
      toast(want ? 'Notifications activées' : 'Notifications désactivées');
    } catch (_) {
      if (created) await created.unsubscribe().catch(() => {});
      setNote('La modification des notifications a échoué. Réessaie.');
      toast('Notifications : échec de la modification', 'err');
    } finally { running.current = false; setBusy(false); }
  };
  const tip = !window.isSecureContext ? 'Les notifications exigent HTTPS ou une adresse locale sécurisée.'
    : !supported ? (/iphone|ipad|ipod/i.test(navigator.userAgent) ? 'Sur iPhone ou iPad, ajoute Loom à l’écran d’accueil et ouvre-le depuis son icône.' : 'Ce navigateur ne prend pas en charge les notifications.')
    : Notification.permission === 'denied' ? 'Notifications bloquées. Autorise-les dans les réglages du navigateur.' : note;
  return html`<${Line} label="Notifications" tip=${tip}>
    ${supported && checked === null && html`<span class="state">État inconnu</span>`}
    <${Switch} label="Notifications" checked=${checked === true} disabled=${!supported || checked === null || busy} onChange=${toggle} />
  </${Line}>`;
}

function Job() {
  const [j, setJ] = useState(null);
  useEffect(() => { const t = setInterval(async () => { try { setJ(await get('/api/llamacpp/job')); } catch (_) {} }, 1200); get('/api/llamacpp/job').then(setJ); return () => clearInterval(t); }, []);
  if (!j || !j.exists) return null;
  const lines = (j.lines || []).slice(-14).join('\n');
  return html`<div class=${cls('job', j.running && 'run', j.error && 'err')}>
    <div class="job-h">${j.running ? html`<span class="spinner"></span>` : j.error ? html`<${Icon} n="alert" />` : html`<${Icon} n="check" />`}
      <b>${j.running ? (j.phase || 'Installation en cours…') : j.error ? 'Échec : ' + j.error : 'Terminé'}</b>
      ${!j.running && html`<button class="btn sm ghost" onClick=${async () => { await post('/api/llamacpp/job/dismiss', {}); setJ(null); }}>Masquer</button>`}</div>
    ${lines && html`<pre class="mono">${lines}</pre>`}</div>`;
}

// Où tourne le moteur : sur cette machine, ou celui d'une machine connectée
// (choisi dans Réglages › Machines).
function EngineLocation() {
  const node = useStore(app, a => a.engineNode);
  const unlink = async () => {
    if (!await confirm('Revenir au moteur de cette machine', 'Loom n’utilisera plus le moteur de ' + node.hostname + '. Rien n’est modifié sur cette machine-là.', { ok: 'Revenir' })) return;
    await post('/api/engine/node', { unlink: true });
    await refreshEngineNode(); refreshStatus(); refreshLibrary();
  };
  return html`<${Group} title="Emplacement du moteur">
    <${Line} label="Le moteur tourne" tip="Une machine avec carte graphique où Loom est installé peut servir de moteur : elle se choisit dans Réglages › Machines. Tes discussions restent ici.">
      ${node ? html`<span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>sur <b>${node.hostname}</b></span><button class="btn sm ghost" onClick=${unlink}>Revenir à cette machine</button>`
        : html`<span class="state">sur cette machine</span><a class="btn sm" href="#/settings/machines">Utiliser une autre machine</a>`}</${Line}>
    ${node && html`<${Line} label="Adresse"><code class="mono">${node.url}</code>${!node.reachable && html`<span class="tag amber">injoignable</span>`}</${Line}>`}
  </${Group}>`;
}

function Engine() {
  const [lc, setLc] = useState(null);
  const load = async () => { setLc(await get('/api/llamacpp')); };
  useEffect(() => { load(); }, []);
  const run = async (url, body, ok) => { const r = await post(url, body || {}); if (r.ok === false) return toast(r.error || 'Échec', 'err'); if (ok) toast(ok); setTimeout(load, 800); };
  const link = async () => { const bin = await prompt('Lier un llama-server existant', { message: 'Chemin complet du binaire llama-server déjà installé sur cette machine.', placeholder: '/chemin/vers/llama-server', ok: 'Lier' }); if (bin) run('/api/llamacpp/use', { mode: 'exist', bin }, 'Moteur lié'); };
  if (!lc) return html`<div class="skeleton" style="height:220px"></div>`;
  return html`
    <${EngineLocation} />
    <${Group} title="Moteur actuel">
      <${Line} label="llama.cpp"><span class="mono">${lc.commit || lc.prebuilt && lc.prebuilt.tag || '—'}</span>${lc.behind > 0 && html`<span class="tag amber">${lc.behind} commits de retard</span>`}</${Line}>
      <${Line} label="Accélération"><span class="tag blue">${(lc.plan && lc.plan.backend || '—').toUpperCase()}</span></${Line}>
      <${GpuDevices} bin=${lc.config_bin || lc.bin || ''} />
      <${Line} label="Binaire" stack><code class="mono path">${lc.bin || 'aucun'}</code></${Line}>
      <${EngineAuto} />
      <div class="set-actions">
        ${lc.can_update && html`<button class="btn" onClick=${() => run('/api/llamacpp/update', { clean: false }, 'Mise à jour lancée')}><${Icon} n="refresh" />Mettre à jour</button>`}
        <button class="btn ghost" onClick=${() => run('/api/llamacpp/check', {}, 'Vérification…')}>Vérifier</button>
        <button class="btn ghost" onClick=${link}><${Icon} n="link" />Lier un binaire existant</button>
      </div>
      <${Job} />
    </${Group}>
    ${!lc.installed && html`<${Group} title="Installer llama.cpp">
      <div class="set-note">${lc.reco && lc.reco.why}</div>
      <div class="set-actions"><button class="btn primary" onClick=${() => run('/api/llamacpp/install', { dir: '' }, 'Compilation lancée')}>Compiler llama.cpp</button><button class="btn" onClick=${() => run('/api/llamacpp/prebuilt', {}, 'Téléchargement lancé')}>Binaire officiel</button></div>
    </${Group}>`}
    <${ModelDirs} />`;
}

// Dossiers de modèles du moteur (sur la machine qui le possède : les requêtes
// suivent le moteur distant quand il y en a un).
export function ModelDirs() {
  const [dirs, setDirs] = useState(null);
  const load = () => get('/api/models/dirs').then(setDirs).catch(() => setDirs(null));
  useEffect(() => { load(); }, []);
  const run = async (url, body, ok) => { const r = await post(url, body || {}); if (r.ok === false) return toast(r.error || 'Échec', 'err'); if (ok) toast(ok); setTimeout(load, 500); };
  const addDir = async () => { const p = await prompt('Ajouter un dossier de modèles', { placeholder: '/chemin/vers/mes/modeles', ok: 'Ajouter' }); if (p) run('/api/models/dirs', { path: p, action: 'add' }, 'Dossier ajouté'); };
  return html`
    <${Group} title="Dossiers de modèles">
      ${dirs && (dirs.dirs || []).map(d => html`<div class="set-line"><div class="set-l"><${Icon} n="folder" /><span class="mono path">${d.path}</span>${d.download && html`<span class="tag blue">téléchargements</span>`}</div>
        <div class="set-c"><span class="muted mono">${d.count} modèle${d.count > 1 ? 's' : ''}</span>
          ${!d.download && html`<button class="btn sm ghost" onClick=${() => run('/api/models/dirs', { path: d.path, action: 'download' }, 'Dossier de téléchargement changé')}>Télécharger ici</button>`}
          ${!d.home && html`<button class="icon-btn" aria-label="Retirer" onClick=${async () => { if (await confirm('Retirer le dossier', 'Loom ne listera plus les modèles de ce dossier. Les fichiers restent sur le disque.', { ok: 'Retirer' })) run('/api/models/dirs', { path: d.path, action: 'remove' }); }}><${Icon} n="close" /></button>`}</div></div>`)}
      <div class="set-actions"><button class="btn ghost" onClick=${addDir}><${Icon} n="plus" />Ajouter un dossier</button></div>
    </${Group}>`;
}

function GpuDevices({ bin }) {
  const [devices, setDevices] = useState(null);
  const [src, setSrc] = useState(null);
  const [busy, setBusy] = useState(false);
  const running = useRef(false);
  const status = useStore(app, s => s.status);
  useEffect(() => {
    let alive = true;
    setDevices(null); setSrc(null);
    liveSource().then(async source => {
      // Les identifiants sont propres au binaire du preset actif.
      const r = await post('/api/backends/devices', { bin: new Config(source.base).get('BIN') || bin });
      if (alive) { setSrc(source); setDevices(r.ok && Array.isArray(r.devices) ? r.devices : null); }
    }).catch(() => {});
    return () => { alive = false; };
  }, [bin, status?.model, status?.preset_id]);
  const cfg = new Config(src?.base);
  const raw = cfg.arg('--device');
  const known = (devices || []).map(d => d.id);
  const selected = raw ? raw.split(',').filter(Boolean) : known;
  const unknown = selected.some(id => !known.includes(id));
  const pick = async (id, on) => {
    if (running.current || !src?.model || unknown) return;
    const ids = known.filter(k => k === id ? on : selected.includes(k));
    if (!ids.length) return toast('Garde au moins un GPU', 'err');
    running.current = true; setBusy(true);
    try {
      if (!await confirm('Appliquer le choix GPU', 'Le modèle actif sera rechargé. Ce choix est enregistré dans ses paramètres.', { ok: 'Appliquer' })) return;
      const fresh = await liveSource();
      if (fresh.model !== src.model || fresh.presetId !== src.presetId || fresh.base !== src.base) {
        setSrc(fresh); setDevices(null); toast('Les paramètres ont changé. Rouvre ces réglages.', 'err'); return;
      }
      const c = new Config(fresh.base).setArg('--device', ids.length === known.length ? '' : ids.join(','));
      if (ids.length < 2) c.setArg('--tensor-split', '');
      const saved = fresh.presetId
        ? await post('/api/preset/save', { id: fresh.presetId, name: fresh.presetName, content: c.text })
        : await post('/api/naked/remember', { model: fresh.model, content: c.text });
      if (!saved.ok) throw new Error();
      setSrc({ ...fresh, base: c.text }); refreshLibrary();
      const r = await post('/api/apply', { content: c.text, preset_id: fresh.presetId });
      if (!r.ok) { toast('Choix GPU enregistré, mais application impossible', 'err'); return; }
      toast('Choix GPU enregistré'); refreshStatus();
    } catch (_) { toast('Choix GPU : échec de l’enregistrement', 'err'); }
    finally { running.current = false; setBusy(false); }
  };
  const tip = 'Choix du modèle actif, enregistré dans EXTRA_ARGS avec --device, comme dans l’interface précédente. Toutes les cartes : aucune contrainte. Le binaire fournit les identifiants. Aucun modèle actif : lecture seule.';
  if (!devices?.length) return html`<${Line} label="GPU" tip=${tip}><span class="state">Inconnu</span></${Line}>`;
  return html`${unknown && html`<${Line} label="Choix GPU" tip="La sélection enregistrée ne correspond pas aux GPU détectés. Elle est conservée."><span class="state">Inconnu</span></${Line}>`}
    ${devices.map(d => html`<${Line} key=${d.id} label=${d.name || d.id || 'GPU inconnu'} tip=${tip}>
      <span class="tag">${d.id}</span><span class="num">${d.total_mib > 0 ? fmtBytes(d.total_mib * 1048576) : 'Mémoire inconnue'}</span>
      ${devices.length > 1 && src?.model && !unknown ? html`<${Switch} label=${'Utiliser ' + (d.name || d.id)} checked=${selected.includes(d.id)} disabled=${busy} onChange=${on => pick(d.id, on)} />`
        : html`<span class="state">${unknown ? 'Choix inconnu' : selected.includes(d.id) ? 'Sélectionné' : 'Non sélectionné'}</span>`}
    </${Line}>`)}`;
}

function Internet() {
  const [n, setN] = useState(null);
  const load = async () => setN(await get('/api/internet'));
  useEffect(() => { load(); }, []);
  if (!n) return html`<div class="skeleton" style="height:140px"></div>`;
  const save = async body => { const r = await post('/api/internet', body); if (r.ok === false) toast(r.error, 'err'); load(); };
  return html`<${Group} title="Recherche web">
    <${Line} label="Donner internet aux modèles locaux" tip="Outils de recherche et de lecture de pages. Ils s’activent ensuite par discussion, dans Outils."><${Switch} checked=${n.enabled} onChange=${v => save({ enabled: v, url: n.url })} /></${Line}>
    <${Line} label="Moteur" tip="Intégré : aucune installation. Crawl4AI : navigateur headless, pour les pages qui demandent du JavaScript."><${Seg} size="sm" value=${n.engine} onChange=${v => save({ engine: v })} options=${[{ value: 'go', label: 'Intégré' }, { value: 'crawl4ai', label: 'Crawl4AI' }]} /></${Line}>
    ${n.engine === 'crawl4ai' && html`<${Line} label="Adresse Crawl4AI"><input class="input sm" style="width:260px" value=${n.url} placeholder="http://localhost:11235" onChange=${e => save({ url: e.target.value })} /></${Line}>
      <${Line} label="Clé Crawl4AI">${n.key_set ? html`<span class="muted mono">••••${n.key_hint}</span><button class="btn sm ghost" onClick=${() => save({ key: '' })}>Retirer</button>`
        : html`<input class="input sm" type="password" style="width:220px" placeholder="facultative" onChange=${e => save({ key: e.target.value })} />`}</${Line}>`}
  </${Group}>`;
}

function SecretDialog({ title, tip, onClose, onSubmit }) {
  const [value, setValue] = useState('');
  const submit = () => { if (value.trim()) { const secret = value; setValue(''); onSubmit(secret); } };
  return html`<${Modal} title=${title} onClose=${onClose}
    foot=${html`<button class="btn ghost" onClick=${onClose}>Annuler</button><button class="btn primary" disabled=${!value.trim()} onClick=${submit}>Continuer</button>`}>
    <${Line} label="Secret" tip=${tip}><input class="input" type="password" autocomplete="off" aria-label="Secret" value=${value} onInput=${e => setValue(e.target.value)} onKeyDown=${e => { if (e.key === 'Enter') { e.preventDefault(); submit(); } }} /></${Line}>
  </${Modal}>`;
}

const snapshotDate = s => {
  const d = s.when ? new Date(s.when) : null;
  return d && !Number.isNaN(d.getTime()) && d.getFullYear() > 1 ? d.toLocaleString('fr-FR') : 'Date inconnue';
};

// Accès réseau : l'interface (mode serveur, par ex. sur une VM) et l'API /v1
// des modèles, chacune avec sa clé. Jamais ouvert sans clé.
function NetworkAccess() {
  const [web, setWeb] = useState(null);
  const [api, setApi] = useState(null);
  const [busy, setBusy] = useState(false);
  const load = () => Promise.all([
    get('/api/network/web').then(r => setWeb(r.status || null)).catch(() => setWeb(null)),
    get('/api/network').then(r => setApi(r.status || null)).catch(() => setApi(null)),
  ]);
  useEffect(() => { load(); }, []);
  const toggleWeb = async on => {
    if (on && !await confirm('Ouvrir l’interface au réseau', 'Loom sera accessible depuis les autres appareils de ton réseau, protégé par une clé de pilotage (créée maintenant si besoin). Ne l’expose pas directement sur internet : passe par un VPN ou un tunnel.', { ok: 'Ouvrir' })) return;
    setBusy(true);
    const r = await post('/api/network/web', { exposed: on });
    setBusy(false);
    if (!r.ok) return toast(r.error || 'Réglage impossible', 'err');
    if (r.key) {
      setToken(r.key);
      const copied = await copyText(r.key);
      await confirm('Clé de pilotage', 'Note-la : elle sera demandée à la première ouverture de Loom sur chaque appareil. ' + (copied ? 'Elle est copiée dans le presse-papiers. ' : '') + r.key, { ok: 'C’est noté' });
    }
    load(); // post() remplace r.status par le code HTTP : on relit l'état
  };
  const restart = async () => {
    const r = await post('/api/network/web', { restart: true });
    toast(r.message || 'Redémarrage demandé', r.restarting ? '' : 'err');
  };
  const toggleApi = async on => { const r = await post('/api/network', { exposed: on }); if (r.ok === false) return toast(r.error, 'err'); load(); };
  return html`<${Group} title="Accès réseau">
    <${Line} label="Interface sur le réseau" tip="Mode serveur : ouvre Loom depuis un autre appareil (Loom sur une VM, un serveur…). Toujours protégé par la clé de pilotage.">
      ${web ? html`<${Switch} checked=${web.exposed} disabled=${busy} label="Interface sur le réseau" onChange=${toggleWeb} />` : html`<span class="state">…</span>`}</${Line}>
    ${web && web.exposed && html`<${Line} label="Adresse">${web.url ? html`<code class="mono">${web.url}</code><button class="btn sm ghost" onClick=${async () => toast(await copyText(web.url) ? 'Adresse copiée' : 'Copie refusée')}>Copier</button>` : html`<span class="state">après redémarrage</span>`}</${Line}>`}
    ${web && web.restart && html`<${Line} label="À appliquer" tip="L’adresse d’écoute ne change qu’au redémarrage de l’interface. Le modèle chargé n’est pas touché."><span class="state">Redémarrage nécessaire</span><button class="btn sm" onClick=${restart}>Redémarrer l’interface</button></${Line}>`}
    ${web && web.exposed && web.firewall === 'ferme' && html`<${Line} label="Pare-feu"><span class="state">Port ${web.port} non autorisé : ouvre-le dans le pare-feu de la machine</span></${Line}>`}
    <${Line} label="API /v1 sur le réseau" tip="Les logiciels et harnesses d’autres machines peuvent utiliser tes modèles locaux. La clé API ci-dessous devient obligatoire.">
      ${api ? html`<${Switch} checked=${api.exposed} label="API /v1 sur le réseau" onChange=${toggleApi} />` : html`<span class="state">…</span>`}</${Line}>
    ${api && api.exposed && html`<${Line} label="Adresse de l’API"><code class="mono">${api.url}</code></${Line}>`}
  </${Group}>`;
}

function Security() {
  const [k, setK] = useState(null);
  const [mem, setMem] = useState(null);
  const [mode, setMode] = useState(null);
  const [snapshots, setSnapshots] = useState(null);
  const [busy, setBusy] = useState(false);
  const [secretForm, setSecretForm] = useState(null);
  const running = useRef(false);
  const load = async () => {
    await Promise.all([
      get('/api/apikey').then(setK).catch(() => setK(null)),
      get('/api/mem/health').then(r => setMem(typeof r.encrypted === 'boolean' ? r : null)).catch(() => setMem(null)),
      get('/api/memory').then(r => setMode(r.mode)).catch(() => setMode(null)),
      get('/api/mem/snapshots').then(r => setSnapshots(r.ok ? r.snapshots || [] : null)).catch(() => setSnapshots(null)),
    ]);
  };
  useEffect(() => { load(); }, []);
  const key = async body => { const r = await post('/api/apikey', body); if (r.ok === false) return toast(r.error, 'err'); setK(r); if (body.action === 'generate' && r.key) { navigator.clipboard && navigator.clipboard.writeText(r.key); toast('Nouvelle clé copiée'); } };
  const act = async fn => {
    if (running.current) return;
    running.current = true; setBusy(true);
    try { await fn(); }
    catch (_) { toast('Données : échec de l’opération', 'err'); }
    finally { running.current = false; setBusy(false); }
  };
  const secret = action => setSecretForm({ action,
    title: action === 'encrypt' ? 'Chiffrer les données' : action === 'unlock' ? 'Déverrouiller les données' : 'Ajouter une clé',
    tip: action === 'unlock' ? 'Mot de passe ou clé de récupération. Le secret reste masqué et n’est pas mémorisé.'
      : action === 'encrypt' ? 'Choisis une phrase secrète. Note la clé de récupération qui sera affichée une seule fois. Un snapshot de sécurité est pris avant.'
      : 'Ajoute un secret pour ouvrir ce même coffre, par exemple ta clé de pilotage. Il remplace la clé supplémentaire précédente si elle existe. Aucun nouveau chiffrement des données.',
  });
  const submitSecret = async value => {
    const action = secretForm.action; setSecretForm(null);
    await act(async () => {
      if (action === 'addkey' && !await confirm('Ajouter une clé', 'La clé supplémentaire précédente sera remplacée si elle existe. Le mot de passe initial et la clé de récupération sont conservés.', { ok: 'Ajouter' })) return;
      const r = await post('/api/mem/' + action, action === 'encrypt' ? { password: value } : { secret: value }, { retryAuth: action !== 'unlock' });
      if (!r.ok) { toast(action === 'unlock' && r.status === 401 ? 'Secret incorrect ou accès refusé' : 'Données : échec de l’opération', 'err'); return; }
      // Le dialogue global reste visible même si la page change pendant l’appel.
      if (action === 'encrypt' && r.recovery) await confirm('Clé de récupération', 'Note cette clé maintenant et garde-la hors ligne. Elle ne sera plus affichée : ' + r.recovery, { ok: 'C’est noté' });
      toast(action === 'encrypt' ? 'Chiffrement activé' : action === 'unlock' ? 'Données déverrouillées' : 'Clé ajoutée');
      await load(); refreshNav();
    });
  };
  const lock = () => act(async () => {
    const r = await post('/api/mem/lock', {});
    if (!r.ok) throw new Error();
    await load(); refreshNav(); toast('Données verrouillées');
  });
  const decrypt = () => act(async () => {
    if (!await confirm('Déchiffrer les données', 'La mémoire et les discussions seront réécrites en clair sur le disque. Un snapshot de sécurité est pris avant.', { ok: 'Déchiffrer', danger: true })) return;
    const r = await post('/api/mem/decrypt', {});
    if (!r.ok) throw new Error();
    await load(); refreshNav(); toast('Chiffrement désactivé');
  });
  const restore = s => act(async () => {
    if (!await confirm('Restaurer le snapshot', snapshotDate(s) + ' : les fichiers mémoire actuels seront remplacés par cette copie, coffre compris. Un snapshot de sécurité est pris avant. Les discussions, presets et réglages ne sont pas restaurés.', { ok: 'Restaurer', danger: true })) return;
    const r = await post('/api/mem/snapshots', { id: s.id });
    if (!r.ok) throw new Error();
    await load(); refreshNav(); refreshLibrary(); toast('Snapshot restauré');
  });
  const vault = mem && (mem.encrypted || mem.vault_copies > 0);
  const blocked = busy || !!secretForm;
  return html`
    <${NetworkAccess} />
    <${Group} title="API /v1">
      <${Line} label="Clé API exigée" tip="Les applications qui utilisent le serveur devront envoyer cette clé. Obligatoire si le serveur est exposé sur le réseau."><${Switch} checked=${k && k.required} onChange=${v => key({ action: 'require', on: v })} /></${Line}>
      <${Line} label="Clé">${k && k.set ? html`<code class="mono">${k.masked}</code><button class="btn sm ghost" onClick=${() => key({ action: 'generate' })}>Régénérer</button><button class="btn sm ghost" onClick=${() => key({ action: 'clear' })}>Supprimer</button>`
        : html`<button class="btn sm" onClick=${() => key({ action: 'generate' })}>Créer une clé</button>`}</${Line}>
    </${Group}>
    <${Group} title="Mémoire et données">
      <${Line} label="Mémoire des modèles locaux" tip="Désactivée : rien n’est retenu entre les discussions. Sur demande : le modèle peut lire et écrire des notes quand tu le lui demandes.">
        ${mode && html`<${Seg} size="sm" value=${mode} onChange=${async v => { await post('/api/memory', { mode: v }); setMode(v); }} options=${[{ value: 'off', label: 'Désactivée' }, { value: 'ondemand', label: 'Sur demande' }]} />`}</${Line}>
      <${Line} label="Chiffrement au repos" tip="AES-256 sur la mémoire et les discussions, protégé par ta phrase secrète.">
        ${!mem ? html`<span class="state">État inconnu</span>` : vault ? html`
          <span class=${'tag ' + (mem.fully ? 'green' : 'amber')}>${!mem.encrypted ? 'État à vérifier' : mem.fully ? 'Chiffré' : 'Chiffrement partiel'}</span>
          <span class="state">${mem.locked ? 'Verrouillé' : mem.encrypted ? 'Déverrouillé' : 'État inconnu'}</span>
          ${(mem.locked || !mem.encrypted || !mem.fully) && html`<button class="btn sm" disabled=${blocked} onClick=${() => secret('unlock')}>Déverrouiller</button>`}
          ${mem.encrypted && !mem.locked && html`<button class="btn sm ghost" disabled=${blocked} onClick=${lock}>Verrouiller</button><button class="btn sm ghost" disabled=${blocked} onClick=${decrypt}>Déchiffrer</button>`}`
          : html`<span class="state">En clair</span><button class="btn sm" disabled=${blocked} onClick=${() => secret('encrypt')}>Activer</button>`}</${Line}>
      ${mem?.encrypted && !mem.locked && html`<${Line} label="Clé supplémentaire" tip="Ajoute ou remplace le secret d’ouverture supplémentaire du coffre déjà déverrouillé, sans réécrire les données."><button class="btn sm ghost" disabled=${blocked} onClick=${() => secret('addkey')}>Ajouter une clé</button></${Line}>`}
      <${Line} label="Exporter la discussion"><button class="btn sm ghost" onClick=${() => download('/api/chat/export?format=md', 'discussion.md').catch(() => toast('Export impossible', 'err'))}>Markdown</button><button class="btn sm ghost" onClick=${() => download('/api/chat/export?format=json', 'discussion.json').catch(() => toast('Export impossible', 'err'))}>JSON</button></${Line}>
    </${Group}>
    <${Group} title="Snapshots locaux">
      <${Line} label="Sauvegardes" tip="Copies locales des fichiers mémoire, coffre compris. La taille n’est pas fournie par le serveur. La restauration ne remplace pas les discussions, presets ou réglages."><button class="btn sm ghost" disabled=${blocked} onClick=${load}>Actualiser</button></${Line}>
      ${snapshots === null ? html`<${Line} label="Liste"><span class="state">Inconnue</span></${Line}>`
        : snapshots.length === 0 ? html`<${Line} label="Liste"><span class="state">Aucun snapshot</span></${Line}>`
        : snapshots.map(s => html`<${Line} key=${s.id} label=${snapshotDate(s)} tip=${s.reason || 'Snapshot local'}>
          <span class="num">${typeof s.size === 'number' && s.size > 0 ? fmtBytes(s.size) : 'Taille inconnue'}</span>
          <button class="btn sm ghost" disabled=${blocked} onClick=${() => restore(s)}>Restaurer</button>
        </${Line}>`)}
    </${Group}>
    ${secretForm && html`<${SecretDialog} key=${secretForm.action} title=${secretForm.title} tip=${secretForm.tip} onClose=${() => setSecretForm(null)} onSubmit=${submitSecret} />`}`;
}

function About() {
  const status = useStore(app, s => s.status);
  const [paths, setPaths] = useState(null);
  const [upd, setUpd] = useState(null);
  useEffect(() => { get('/api/paths').then(setPaths); }, []);
  return html`
    <${Group} title="Loom">
      <${Line} label="Version"><span class="mono">${status ? status.version : '—'}</span></${Line}>
      <${Line} label="Mises à jour">${upd ? html`<span class="muted">${upd.available ? 'Version ' + upd.latest + ' disponible' : 'À jour'}</span>` : html`<button class="btn sm" onClick=${async () => setUpd(await get('/api/update'))}>Vérifier</button>`}</${Line}>
    </${Group}>
    ${paths && html`<${Group} title="Emplacements">${[['Données', paths.home], ['Base', paths.database], ['Modèles', paths.models], ['Presets', paths.presets], ['Moteurs', paths.backends]].map(([l, p]) => html`<${Line} label=${l}><code class="mono path">${p}</code></${Line}>`)}</${Group}>`}`;
}

export function SettingsPage({ route }) {
  const sec = SECTIONS.some(s => s[0] === route.sub) ? route.sub : 'general';
  const View = { general: General, machines: MachinesSettings, engine: Engine, internet: Internet, security: Security, about: About }[sec];
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>Réglages</h1></div></div>
    <div class="settings">
      <nav class="set-nav">${SECTIONS.map(([id, label, ico]) => html`<a href=${'#/settings/' + id} aria-current=${id === sec ? 'page' : undefined}><${Icon} n=${ico} />${label}</a>`)}</nav>
      <div class="set-body" key=${sec}><${View} route=${route} /></div>
    </div>
  </div></div>`;
}
