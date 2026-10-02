import { t, locale, getLang, setLang, tSource } from '../../core/i18n.js';
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

const SECTIONS = () => ([['general', t("settings.page.general"), 'gear'], ['machines', t("settings.page.machines"), 'server'], ['engine', t("settings.page.moteurs"), 'chip'], ['internet', 'Internet', 'globe'], ['security', t("settings.page.securite_et_donnees"), 'lock'], ['about', t("settings.page.a_propos"), 'info']]);


function usePref() {
  const [p, setP] = useState(null);
  useEffect(() => { get('/api/prefs').then(r => setP(r.prefs || {})); }, []);
  const set = (k, v) => { const n = { ...p, [k]: v }; setP(n); post('/api/prefs', { [k]: v }); try { localStorage.setItem('loom-' + k.replace('_', '-'), v); } catch (_) {} };
  return [p, set];
}

function General() {
  const theme = useStore(app, s => s.theme);
  const [p, setPref] = usePref();
  const [sys, setSys] = useState(null);
  const [agent, setAgent] = useState(null);
  useEffect(() => { get('/api/sysprompt').then(r => setSys(r.text || '')); get('/api/agent').then(setAgent); }, []);
  const saveSys = async () => { await post('/api/sysprompt', { text: sys }); toast(t("settings.page.prompt_systeme_enregistre")); };
  return html`
    <${Group} title="${t("settings.page.apparence")}">
      <${Line} label=${t('settings.language.label')}><select class="select sm" value=${getLang()} onChange=${e => setLang(e.target.value).then(ok => { if (!ok) toast(t('settings.language.save_failed'), 'err'); }).catch(() => toast(t('settings.language.save_failed'), 'err'))}>
        <option value="fr">Français</option><option value="en">English</option>
      </select></${Line}>
      <${Line} label="${t("settings.page.theme")}"><${Seg} size="sm" value=${theme} onChange=${localT => { setTheme(localT); setPref('theme', localT); }} options=${[{ value: 'dark', label: t("settings.page.sombre") }, { value: 'light', label: t("settings.page.clair") }]} /></${Line}>
      ${p && html`
        <${Line} label="${t("settings.page.masquer_la_reflexion")}" tip="${t("settings.page.les_blocs_de_reflexion_des_modeles_ne_s_affichent_plus_dans_le_fi")}"><${Switch} checked=${p.hide_reasoning === '1'} onChange=${v => setPref('hide_reasoning', v ? '1' : '0')} /></${Line}>
        <${Line} label="${t("settings.page.masquer_les_appels_d_outils")}"><${Switch} checked=${p.hide_tools === '1'} onChange=${v => setPref('hide_tools', v ? '1' : '0')} /></${Line}>
        <${Line} label="${t("settings.page.entree_pour_aller_a_la_ligne")}" tip="${t("settings.page.par_defaut_entree_envoie_et_maj_entree_va_a_la_ligne")}"><${Switch} checked=${p.enter_newline === '1'} onChange=${v => setPref('enter_newline', v ? '1' : '0')} /></${Line}>`}
    </${Group}>
    <${Group} title="${t("settings.page.discussions")}">
      <${PushNotifications} />
      ${agent && html`<${Line} label="${t("settings.page.compactage_automatique")}" tip="${t("settings.page.vers_75_du_contexte_les_anciens_tours_sont_resumes_pour_laisser_d")}">
        <${Switch} checked=${agent.compact} onChange=${async v => { await post('/api/agent/compact', { on: v }); setAgent({ ...agent, compact: v }); }} /></${Line}>`}
      <${Line} label="${t("settings.page.prompt_systeme_global")}" tip="${t("settings.page.envoye_a_tous_les_modeles_locaux_sauf_si_un_modele_ou_un_preset_a")}" stack>
        ${sys !== null && html`<textarea class="textarea" rows="4" value=${sys} onInput=${e => setSys(e.target.value)} placeholder="${t("settings.page.ex_reponds_en_francais_de_facon_concise")}"></textarea>
          <div><button class="btn sm" onClick=${saveSys}>${t("settings.page.enregistrer")}</button></div>`}</${Line}>
    </${Group}>`;
}

function PushNotifications() {
  const supported = window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
  const [checked, setChecked] = useState(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("settings.page.une_notification_a_chaque_reponse_terminee_meme_si_loom_est_ferme");
  const running = useRef(false);
  const register = () => navigator.serviceWorker.register('/sw.js', { scope: '/' });
  useEffect(() => {
    if (!supported) return;
    let alive = true;
    register().then(r => r.pushManager.getSubscription()).then(s => { if (alive) setChecked(!!s); })
      .catch(() => { if (alive) setNote("settings.page.etat_des_notifications_inconnu_reessaie_en_rouvrant_ces_reglages"); });
    return () => { alive = false; };
  }, []);
  const toggle = async want => {
    if (!supported || running.current) return;
    running.current = true; setBusy(true);
    let created = null;
    try {
      // La demande de permission reste directement liée au clic.
      if (want && await Notification.requestPermission() !== 'granted') {
        setNote("settings.page.notifications_bloquees_autorise_les_dans_les_reglages_du_navigate");
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
      setNote("settings.page.une_notification_a_chaque_reponse_terminee_meme_si_loom_est_ferme");
      toast(want ? t("settings.page.notifications_activees") : t("settings.page.notifications_desactivees"));
    } catch (_) {
      if (created) await created.unsubscribe().catch(() => {});
      setNote("settings.page.la_modification_des_notifications_a_echoue_reessaie");
      toast(t("settings.page.notifications_echec_de_la_modification"), 'err');
    } finally { running.current = false; setBusy(false); }
  };
  const tip = !window.isSecureContext ? t("settings.page.les_notifications_exigent_https_ou_une_adresse_locale_securisee")
    : !supported ? (/iphone|ipad|ipod/i.test(navigator.userAgent) ? t("settings.page.sur_iphone_ou_ipad_ajoute_loom_a_l_ecran_d_accueil_et_ouvre_le_de") : t("settings.page.ce_navigateur_ne_prend_pas_en_charge_les_notifications"))
    : Notification.permission === 'denied' ? t("settings.page.notifications_bloquees_autorise_les_dans_les_reglages_du_navigate") : t(note);
  return html`<${Line} label="${t("settings.page.notifications")}" tip=${tip}>
    ${supported && checked === null && html`<span class="state">${t("settings.page.etat_inconnu")}</span>`}
    <${Switch} label="${t("settings.page.notifications")}" checked=${checked === true} disabled=${!supported || checked === null || busy} onChange=${toggle} />
  </${Line}>`;
}

function Job() {
  const [j, setJ] = useState(null);
  useEffect(() => { const localT = setInterval(async () => { try { setJ(await get('/api/llamacpp/job')); } catch (_) {} }, 1200); get('/api/llamacpp/job').then(setJ); return () => clearInterval(localT); }, []);
  if (!j || !j.exists) return null;
  const lines = (j.lines || []).slice(-14).join('\n');
  return html`<div class=${cls('job', j.running && 'run', j.error && 'err')}>
    <div class="job-h">${j.running ? html`<span class="spinner"></span>` : j.error ? html`<${Icon} n="alert" />` : html`<${Icon} n="check" />`}
      <b>${j.running ? (tSource(j.phase) || t("settings.page.installation_en_cours")) : j.error ? t("settings.page.echec") + j.error : t("settings.page.termine")}</b>
      ${!j.running && html`<button class="btn sm ghost" onClick=${async () => { await post('/api/llamacpp/job/dismiss', {}); setJ(null); }}>${t("settings.page.masquer")}</button>`}</div>
    ${lines && html`<pre class="mono">${lines}</pre>`}</div>`;
}

// Où tourne le moteur : sur cette machine, ou celui d'une machine connectée
// (choisi dans Réglages › Machines).
// Mise à jour automatique du moteur : appliquée seulement quand elle
// n'interrompt rien (moteur arrêté ou aucun modèle chargé).
function EngineAuto() {
  const [a, setA] = useState(null);
  useEffect(() => { get('/api/engine/auto-update').then(r => setA(r.ok ? r.state : null)).catch(() => setA(null)); }, []);
  if (!a) return null;
  const toggle = async on => { const r = await post('/api/engine/auto-update', { auto: on }); if (!r.ok) return toast(r.error || t("settings.page.reglage_impossible"), 'err'); setA(r.state); };
  const when = localT => localT ? new Date(localT).toLocaleString(locale(), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '';
  const info = a.pending ? 'Version ' + a.pending + t("settings.page.prete_installee_des_qu_aucun_modele_n_est_charge")
    : a.last_error ? t("settings.page.derniere_tentative") + a.last_error
    : a.last_at ? t("settings.page.mis_a_jour_le") + when(a.last_at) + (a.last_to ? ' (' + (a.last_from ? a.last_from + ' → ' : '') + a.last_to + ')' : '')
    : a.checked_at ? t("settings.page.verifie_le") + when(a.checked_at) + t("settings.page.a_jour") : '';
  return html`<${Line} label="${t("settings.page.mise_a_jour_automatique")}" tip="${t("settings.page.verifie_les_nouvelles_versions_de_llama_cpp_toutes_les_6_h_une_mi")}">
    ${info && html`<span class=${'state' + (a.last_error ? ' err' : '')}>${info}</span>`}<${Switch} checked=${a.auto} label="${t("settings.page.mise_a_jour_automatique_du_moteur")}" onChange=${toggle} /></${Line}>`;
}

// vLLM : second moteur, installé par Loom dans son propre environnement Python
// et lancé ici ; Loom l'utilise alors comme moteur (lien direct local).
function VLLMEngine() {
  const [x, setX] = useState(null);
  const [f, setF] = useState({ model: '', util: '0.85', len: '' });
  const [logOpen, setLogOpen] = useState(false);
  const load = () => get('/api/engines/vllm').then(setX).catch(() => setX(null));
  useEffect(() => { load(); }, []);
  useEffect(() => { if (!x || !x.job) return; const localT = setInterval(load, 2500); return () => clearInterval(localT); }, [x && x.job]);
  const act = async body => {
    const r = await post('/api/engines/vllm', body);
    if (!r.ok) return toast(r.error || t("settings.page.action_impossible"), 'err');
    setLogOpen(true); load();
  };
  const start = () => act({ action: 'start', model: f.model.trim(), gpu_memory_utilization: Number(f.util) || 0, max_model_len: Number(f.len) || 0 });
  useEffect(() => { if (x && !x.job && x.running) { refreshEngineNode(); refreshStatus(); } }, [x && x.running, x && x.job]);
  if (!x) return null;
  return html`<${Group} title="${t("settings.page.vllm")}">
    <${Line} label="${t("settings.page.etat")}" tip="${t("settings.page.vllm_sert_des_modeles_hugging_face_pas_les_gguf_avec_beaucoup_de")}">
      ${x.job === 'install' ? html`<span class="state"><span class="spinner"></span>${t("settings.page.installation_plusieurs_minutes")}</span>`
        : x.job === 'start' ? html`<span class="state"><span class="spinner"></span>${t("settings.page.chargement_de")} ${f.model || x.model}…</span>`
        : x.running ? html`<span class="state"><i class="dot green"></i>${t("settings.page.sert")} <b class="mono">${x.model}</b></span><button class="btn sm ghost" onClick=${() => act({ action: 'stop' })}>${t("settings.page.arreter")}</button>`
        : x.installed ? html`<span class="state">${t("settings.page.installe_arrete")}</span>`
        : x.missing ? html`<span class="state err">${x.missing}</span>`
        : html`<span class="state">${t("settings.page.non_installe")}</span><button class="btn sm" onClick=${async () => { if (await confirm('Installer vLLM', t("settings.page.loom_cree_un_environnement_python_dans") + x.dir + t("settings.page.et_y_installe_vllm_plusieurs_go_quelques_minutes_rien_n_est_insta"), { ok: t("settings.page.installer") })) act({ action: 'install' }); }}>${t("settings.page.installer_vllm")}</button>`}</${Line}>
    ${x.error && html`<${Line} label="${t("settings.page.derniere_erreur")}"><span class="state err">${x.error}</span></${Line}>`}
    ${x.installed && !x.running && !x.job && html`<div class="eng-link">
      <label class="field"><span>${t("settings.page.modele_hugging_face")}</span><input class="input mono" placeholder="${t("settings.page.ex_qwen_qwen3_8b")}" value=${f.model} onInput=${e => setF({ ...f, model: e.target.value })} /><small>${t("settings.page.telecharge_par_vllm_au_premier_lancement")}</small></label>
      <div class="mx-fields"><label class="field"><span>${t("settings.page.memoire_gpu_utilisee")}</span><input class="input mono" value=${f.util} onInput=${e => setF({ ...f, util: e.target.value })} /><small>${t("settings.page.part_de_la_vram_0_5_a_0_95")}</small></label>
        <label class="field"><span>${t("settings.page.contexte_max")}</span><input class="input mono" placeholder="${t("settings.page.auto")}" value=${f.len} onInput=${e => setF({ ...f, len: e.target.value.replace(/\D/g, '') })} /></label></div>
      <div class="form-foot"><span class="grow"></span><button class="btn primary" disabled=${!f.model.trim()} onClick=${start}>${t("settings.page.lancer_avec_vllm")}</button></div></div>`}
    ${x.log && html`<details class="lc-log" open=${logOpen || !!x.job}><summary>${t("settings.page.journal_vllm")}</summary><pre>${x.log.split('\n').slice(-80).join('\n')}</pre></details>`}
  </${Group}>`;
}

const KIND_LABEL = { 'llama.cpp': 'llama.cpp', vllm: 'vLLM', get openai() { return t("local.page.serveur_compatible_openai"); } };

// Lier un serveur d'inférence par son adresse (llama.cpp, vLLM…) : rien de
// Loom n'est installé sur sa machine.
export function DirectEngineForm({ start, onDone }) {
  const [f, setF] = useState({ url: start || '', key: '', model: '' });
  const [probe, setProbe] = useState(null);
  const [busy, setBusy] = useState(false);
  const identify = async () => {
    setBusy(true);
    const r = await post('/api/engine/node', { probe: true, url: f.url, key: f.key });
    setBusy(false);
    if (!r.ok) { setProbe(null); return toast(r.error || t("settings.page.moteur_injoignable"), 'err'); }
    setProbe(r); setF({ ...f, model: r.models[0] });
  };
  const link = async () => {
    setBusy(true);
    const r = await post('/api/engine/node', { direct: true, url: f.url, key: f.key, model: f.model });
    setBusy(false);
    if (!r.ok) return toast(r.error || t("settings.page.liaison_impossible"), 'err');
    toast(KIND_LABEL[r.kind] + t("settings.page.de") + r.hostname + t("settings.page.lie")); await refreshEngineNode(); refreshStatus(); refreshLibrary(); onDone && onDone();
  };
  return html`<div class="eng-link">
    <p class="note">${t("settings.page.un_serveur_llama_cpp_llama_server_vllm_ou_compatible_openai_qui_t")}</p>
    <label class="field"><span>${t("settings.page.adresse_du_moteur")}</span><input class="input mono" placeholder="http://192.168.1.20:8080" value=${f.url} onInput=${e => { setF({ ...f, url: e.target.value }); setProbe(null); }} /></label>
    <label class="field"><span>${t("settings.page.cle_api_si_le_moteur_en_exige_une")}</span><input class="input mono" type="password" value=${f.key} onInput=${e => { setF({ ...f, key: e.target.value }); setProbe(null); }} /></label>
    ${probe && html`<label class="field"><span>${KIND_LABEL[probe.kind]} · ${probe.models.length} ${t("settings.page.modele")}${probe.models.length > 1 ? 's' : ''}${probe.ctx ? t("settings.page.contexte") + probe.ctx : ''}</span>
      <select class="select" value=${f.model} onChange=${e => setF({ ...f, model: e.target.value })}>${probe.models.map(m => html`<option value=${m}>${m}</option>`)}</select></label>`}
    <div class="form-foot"><span class="grow"></span>${onDone && html`<button class="btn ghost" onClick=${onDone}>${t("settings.page.annuler")}</button>`}
      ${probe ? html`<button class="btn primary" disabled=${busy} onClick=${link}>${t("settings.page.utiliser_ce_moteur")}</button>` : html`<button class="btn primary" disabled=${busy || !f.url} onClick=${identify}>${busy ? t("environment.page.connexion") : t("settings.page.identifier_le_moteur")}</button>`}</div>
  </div>`;
}

// Où tourne le moteur : sur cette machine, celui d'un autre Loom (Réglages ›
// Machines) ou un serveur d'inférence lié directement par son adresse.
function EngineLocation() {
  const node = useStore(app, a => a.engineNode);
  const [direct, setDirect] = useState(false);
  const unlink = async () => {
    if (!await confirm(t("settings.page.revenir_au_moteur_de_cette_machine"), t("settings.page.loom_n_utilisera_plus_le_moteur_de") + node.hostname + t("settings.page.rien_n_est_modifie_sur_cette_machine_la"), { ok: t("settings.page.revenir") })) return;
    await post('/api/engine/node', { unlink: true });
    await refreshEngineNode(); refreshStatus(); refreshLibrary();
  };
  return html`<${Group} title="${t("settings.page.emplacement_du_moteur")}">
    <${Line} label="${t("settings.page.le_moteur_tourne")}" tip="${t("settings.page.sur_cette_machine_sur_une_machine_ou_loom_est_installe_reglages_m")}">
      ${node ? html`<span class="state"><i class=${'dot ' + (node.reachable ? 'green' : 'red')}></i>${node.direct ? KIND_LABEL[node.kind] + t("settings.page.sur") : t("settings.page.sur_2")}<b>${node.hostname}</b></span><button class="btn sm ghost" onClick=${unlink}>${t("settings.page.revenir_a_cette_machine")}</button>`
        : html`<span class="state">${t("settings.page.sur_cette_machine")}</span>${!direct && html`<button class="btn sm" onClick=${() => setDirect(true)}>${t("settings.page.lier_un_moteur_par_son_adresse")}</button><a class="btn sm ghost" href="#/settings/machines">${t("settings.page.loom_d_une_autre_machine")}</a>`}`}</${Line}>
    ${node && html`<${Line} label="${t("settings.page.adresse")}"><code class="mono">${node.url}</code>${!node.reachable && html`<span class="tag amber">${t("settings.page.injoignable")}</span>`}</${Line}>`}
    ${node && node.direct && html`<${Line} label="${t("settings.page.modele_utilise")}"><code class="mono">${node.model}</code></${Line}>`}
    ${direct && !node && html`<${DirectEngineForm} onDone=${() => setDirect(false)} />`}
  </${Group}>`;
}

function Engine() {
  const node = useStore(app, a => a.engineNode);
  const [lc, setLc] = useState(null);
  const load = async () => { setLc(await get('/api/llamacpp')); };
  useEffect(() => { load(); }, []);
  const run = async (url, body, ok) => { const r = await post(url, body || {}); if (r.ok === false) return toast(r.error || t("settings.page.echec_2"), 'err'); if (ok) toast(ok); setTimeout(load, 800); };
  const link = async () => { const bin = await prompt(t("settings.page.lier_un_llama_server_existant"), { message: t("settings.page.chemin_complet_du_binaire_llama_server_deja_installe_sur_cette_ma"), placeholder: t("settings.page.chemin_vers_llama_server"), ok: t("settings.page.lier") }); if (bin) run('/api/llamacpp/use', { mode: 'exist', bin }, t("settings.page.moteur_lie")); };
  if (!lc) return html`<div class="skeleton" style="height:220px"></div>`;
  return html`
    <${EngineLocation} />
    <${VLLMEngine} />
    <${Group} title=${node ? t("settings.page.llama_cpp_de_cette_machine") : t("settings.page.moteur_actuel")}>
      <${Line} label="${t("settings.page.llama_cpp")}"><span class="mono">${lc.commit || lc.prebuilt && lc.prebuilt.tag || '—'}</span>${lc.behind > 0 && html`<span class="tag amber">${lc.behind} ${t("settings.page.commits_de_retard")}</span>`}</${Line}>
      <${Line} label="${t("settings.page.acceleration")}"><span class="tag blue">${(lc.plan && lc.plan.backend || '—').toUpperCase()}</span></${Line}>
      <${GpuDevices} bin=${lc.config_bin || lc.bin || ''} />
      <${Line} label="${t("settings.page.binaire")}" stack><code class="mono path">${lc.bin || t('settings.page.none')}</code></${Line}>
      <${EngineAuto} />
      <div class="set-actions">
        ${lc.can_update && html`<button class="btn" onClick=${() => run('/api/llamacpp/update', { clean: false }, t("settings.page.mise_a_jour_lancee"))}><${Icon} n="refresh" />${t("settings.page.mettre_a_jour")}</button>`}
        <button class="btn ghost" onClick=${() => run('/api/llamacpp/check', {}, t("settings.page.verification"))}>${t("settings.page.verifier")}</button>
        <button class="btn ghost" onClick=${link}><${Icon} n="link" />${t("settings.page.lier_un_binaire_existant")}</button>
      </div>
      <${Job} />
    </${Group}>
    ${!lc.installed && html`<${Group} title="${t("settings.page.installer_llama_cpp")}">
      <div class="set-note">${lc.reco && lc.reco.why}</div>
      <div class="set-actions"><button class="btn primary" onClick=${() => run('/api/llamacpp/install', { dir: '' }, t("settings.page.compilation_lancee"))}>${t("settings.page.compiler_llama_cpp")}</button><button class="btn" onClick=${() => run('/api/llamacpp/prebuilt', {}, t("settings.page.telechargement_lance"))}>${t("settings.page.binaire_officiel")}</button></div>
    </${Group}>`}
    <${ModelDirs} />`;
}

// Dossiers de modèles du moteur (sur la machine qui le possède : les requêtes
// suivent le moteur distant quand il y en a un).
export function ModelDirs() {
  const [dirs, setDirs] = useState(null);
  const load = () => get('/api/models/dirs').then(setDirs).catch(() => setDirs(null));
  useEffect(() => { load(); }, []);
  const run = async (url, body, ok) => { const r = await post(url, body || {}); if (r.ok === false) return toast(r.error || t("settings.page.echec_2"), 'err'); if (ok) toast(ok); setTimeout(load, 500); };
  const addDir = async () => { const p = await prompt(t("settings.page.ajouter_un_dossier_de_modeles"), { placeholder: t("settings.page.chemin_vers_mes_modeles"), ok: t("settings.page.ajouter") }); if (p) run('/api/models/dirs', { path: p, action: 'add' }, t("settings.page.dossier_ajoute")); };
  return html`
    <${Group} title="${t("settings.page.dossiers_de_modeles")}">
      ${dirs && (dirs.dirs || []).map(d => html`<div class="set-line"><div class="set-l"><${Icon} n="folder" /><span class="mono path">${d.path}</span>${d.download && html`<span class="tag blue">${t("settings.page.telechargements")}</span>`}</div>
        <div class="set-c"><span class="muted mono">${d.count} ${t("settings.page.modele")}${d.count > 1 ? 's' : ''}</span>
          ${!d.download && html`<button class="btn sm ghost" onClick=${() => run('/api/models/dirs', { path: d.path, action: 'download' }, t("settings.page.dossier_de_telechargement_change"))}>${t("settings.page.telecharger_ici")}</button>`}
          ${!d.home && html`<button class="icon-btn" aria-label="${t("settings.page.retirer")}" onClick=${async () => { if (await confirm(t("settings.page.retirer_le_dossier"), t("settings.page.loom_ne_listera_plus_les_modeles_de_ce_dossier_les_fichiers_reste"), { ok: t("settings.page.retirer") })) run('/api/models/dirs', { path: d.path, action: 'remove' }); }}><${Icon} n="close" /></button>`}</div></div>`)}
      <div class="set-actions"><button class="btn ghost" onClick=${addDir}><${Icon} n="plus" />${t("settings.page.ajouter_un_dossier")}</button></div>
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
    if (!ids.length) return toast(t("settings.page.garde_au_moins_un_gpu"), 'err');
    running.current = true; setBusy(true);
    try {
      if (!await confirm(t("settings.page.appliquer_le_choix_gpu"), t("settings.page.le_modele_actif_sera_recharge_ce_choix_est_enregistre_dans_ses_pa"), { ok: t("settings.page.appliquer") })) return;
      const fresh = await liveSource();
      if (fresh.model !== src.model || fresh.presetId !== src.presetId || fresh.base !== src.base) {
        setSrc(fresh); setDevices(null); toast(t("settings.page.les_parametres_ont_change_rouvre_ces_reglages"), 'err'); return;
      }
      const c = new Config(fresh.base).setArg('--device', ids.length === known.length ? '' : ids.join(','));
      if (ids.length < 2) c.setArg('--tensor-split', '');
      const saved = fresh.presetId
        ? await post('/api/preset/save', { id: fresh.presetId, name: fresh.presetName, content: c.text })
        : await post('/api/naked/remember', { model: fresh.model, content: c.text });
      if (!saved.ok) throw new Error();
      setSrc({ ...fresh, base: c.text }); refreshLibrary();
      const r = await post('/api/apply', { content: c.text, preset_id: fresh.presetId });
      if (!r.ok) { toast(t("settings.page.choix_gpu_enregistre_mais_application_impossible"), 'err'); return; }
      toast(t("settings.page.choix_gpu_enregistre")); refreshStatus();
    } catch (_) { toast(t("settings.page.choix_gpu_echec_de_l_enregistrement"), 'err'); }
    finally { running.current = false; setBusy(false); }
  };
  const tip = t("settings.page.choix_du_modele_actif_enregistre_dans_extra_args_avec_device_comm");
  if (!devices?.length) return html`<${Line} label="GPU" tip=${tip}><span class="state">${t("settings.page.inconnu")}</span></${Line}>`;
  return html`${unknown && html`<${Line} label="${t("settings.page.choix_gpu")}" tip="${t("settings.page.la_selection_enregistree_ne_correspond_pas_aux_gpu_detectes_elle")}"><span class="state">${t("settings.page.inconnu")}</span></${Line}>`}
    ${devices.map(d => html`<${Line} key=${d.id} label=${d.name || d.id || t("settings.page.gpu_inconnu")} tip=${tip}>
      <span class="tag">${d.id}</span><span class="num">${d.total_mib > 0 ? fmtBytes(d.total_mib * 1048576) : t("settings.page.memoire_inconnue")}</span>
      ${devices.length > 1 && src?.model && !unknown ? html`<${Switch} label=${t("settings.page.utiliser") + (d.name || d.id)} checked=${selected.includes(d.id)} disabled=${busy} onChange=${on => pick(d.id, on)} />`
        : html`<span class="state">${unknown ? t("settings.page.choix_inconnu") : selected.includes(d.id) ? t("settings.page.selectionne") : t("settings.page.non_selectionne")}</span>`}
    </${Line}>`)}`;
}

function Internet() {
  const [n, setN] = useState(null);
  const load = async () => setN(await get('/api/internet'));
  useEffect(() => { load(); }, []);
  if (!n) return html`<div class="skeleton" style="height:140px"></div>`;
  const save = async body => { const r = await post('/api/internet', body); if (r.ok === false) toast(r.error, 'err'); load(); };
  return html`<${Group} title="${t("settings.page.recherche_web")}">
    <${Line} label="${t("settings.page.donner_internet_aux_modeles_locaux")}" tip="${t("settings.page.outils_de_recherche_et_de_lecture_de_pages_ils_s_activent_ensuite")}"><${Switch} checked=${n.enabled} onChange=${v => save({ enabled: v, url: n.url })} /></${Line}>
    <${Line} label="${t("settings.page.moteur")}" tip="${t("settings.page.integre_aucune_installation_crawl4ai_navigateur_headless_pour_les")}"><${Seg} size="sm" value=${n.engine} onChange=${v => save({ engine: v })} options=${[{ value: 'go', label: t("settings.page.integre") }, { value: 'crawl4ai', label: t("settings.page.crawl4ai") }]} /></${Line}>
    ${n.engine === 'crawl4ai' && html`<${Line} label="${t("settings.page.adresse_crawl4ai")}"><input class="input sm" style="width:260px" value=${n.url} placeholder="http://localhost:11235" onChange=${e => save({ url: e.target.value })} /></${Line}>
      <${Line} label="${t("settings.page.cle_crawl4ai")}">${n.key_set ? html`<span class="muted mono">••••${n.key_hint}</span><button class="btn sm ghost" onClick=${() => save({ key: '' })}>${t("settings.page.retirer")}</button>`
        : html`<input class="input sm" type="password" style="width:220px" placeholder="${t("settings.page.facultative")}" onChange=${e => save({ key: e.target.value })} />`}</${Line}>`}
  </${Group}>`;
}

function SecretDialog({ title, tip, onClose, onSubmit }) {
  const [value, setValue] = useState('');
  const submit = () => { if (value.trim()) { const secret = value; setValue(''); onSubmit(secret); } };
  return html`<${Modal} title=${title} onClose=${onClose}
    foot=${html`<button class="btn ghost" onClick=${onClose}>${t("settings.page.annuler")}</button><button class="btn primary" disabled=${!value.trim()} onClick=${submit}>${t("settings.page.continuer")}</button>`}>
    <${Line} label="${t("settings.page.secret")}" tip=${tip}><input class="input" type="password" autocomplete="off" aria-label="${t("settings.page.secret")}" value=${value} onInput=${e => setValue(e.target.value)} onKeyDown=${e => { if (e.key === 'Enter') { e.preventDefault(); submit(); } }} /></${Line}>
  </${Modal}>`;
}

const snapshotDate = s => {
  const d = s.when ? new Date(s.when) : null;
  return d && !Number.isNaN(d.getTime()) && d.getFullYear() > 1 ? d.toLocaleString(locale()) : t("settings.page.date_inconnue");
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
    if (on && !await confirm(t("settings.page.ouvrir_l_interface_au_reseau"), t("settings.page.loom_sera_accessible_depuis_les_autres_appareils_de_ton_reseau_pr"), { ok: t("settings.page.ouvrir") })) return;
    setBusy(true);
    const r = await post('/api/network/web', { exposed: on });
    setBusy(false);
    if (!r.ok) return toast(r.error || t("settings.page.reglage_impossible"), 'err');
    if (r.key) {
      setToken(r.key);
      const copied = await copyText(r.key);
      await confirm(t("settings.page.cle_de_pilotage"), t("settings.page.note_la_elle_sera_demandee_a_la_premiere_ouverture_de_loom_sur_ch") + (copied ? t("settings.page.elle_est_copiee_dans_le_presse_papiers") : '') + r.key, { ok: t("settings.page.c_est_note") });
    }
    load(); // post() remplace r.status par le code HTTP : on relit l'état
  };
  const restart = async () => {
    const r = await post('/api/network/web', { restart: true });
    toast(r.message || t("settings.page.redemarrage_demande"), r.restarting ? '' : 'err');
  };
  const toggleApi = async on => { const r = await post('/api/network', { exposed: on }); if (r.ok === false) return toast(r.error, 'err'); load(); };
  return html`<${Group} title="${t("settings.page.acces_reseau")}">
    <${Line} label="${t("settings.page.interface_sur_le_reseau")}" tip="${t("settings.page.mode_serveur_ouvre_loom_depuis_un_autre_appareil_loom_sur_une_vm")}">
      ${web ? html`<${Switch} checked=${web.exposed} disabled=${busy} label="${t("settings.page.interface_sur_le_reseau")}" onChange=${toggleWeb} />` : html`<span class="state">…</span>`}</${Line}>
    ${web && web.exposed && html`<${Line} label="${t("settings.page.adresse")}">${web.url ? html`<code class="mono">${web.url}</code><button class="btn sm ghost" onClick=${async () => toast(await copyText(web.url) ? t("settings.page.adresse_copiee") : t("settings.page.copie_refusee"))}>${t("settings.page.copier")}</button>` : html`<span class="state">${t("settings.page.apres_redemarrage")}</span>`}</${Line}>`}
    ${web && web.restart && html`<${Line} label="${t("settings.page.a_appliquer")}" tip="${t("settings.page.l_adresse_d_ecoute_ne_change_qu_au_redemarrage_de_l_interface_le")}"><span class="state">${t("settings.page.redemarrage_necessaire")}</span><button class="btn sm" onClick=${restart}>${t("settings.page.redemarrer_l_interface")}</button></${Line}>`}
    ${web && web.exposed && web.firewall === 'ferme' && html`<${Line} label="${t("settings.page.pare_feu")}"><span class="state">${t("settings.page.port")} ${web.port} ${t("settings.page.non_autorise_ouvre_le_dans_le_pare_feu_de_la_machine")}</span></${Line}>`}
    <${Line} label="${t("settings.page.api_v1_sur_le_reseau")}" tip="${t("settings.page.les_logiciels_et_harnesses_d_autres_machines_peuvent_utiliser_tes")}">
      ${api ? html`<${Switch} checked=${api.exposed} label="${t("settings.page.api_v1_sur_le_reseau")}" onChange=${toggleApi} />` : html`<span class="state">…</span>`}</${Line}>
    ${api && api.exposed && html`<${Line} label="${t("settings.page.adresse_de_l_api")}"><code class="mono">${api.url}</code></${Line}>`}
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
  const key = async body => { const r = await post('/api/apikey', body); if (r.ok === false) return toast(r.error, 'err'); setK(r); if (body.action === 'generate' && r.key) { navigator.clipboard && navigator.clipboard.writeText(r.key); toast(t("settings.page.nouvelle_cle_copiee")); } };
  const act = async fn => {
    if (running.current) return;
    running.current = true; setBusy(true);
    try { await fn(); }
    catch (_) { toast(t("settings.page.donnees_echec_de_l_operation"), 'err'); }
    finally { running.current = false; setBusy(false); }
  };
  const secret = action => setSecretForm({ action,
    title: action === 'encrypt' ? t("settings.page.chiffrer_les_donnees") : action === 'unlock' ? t("settings.page.deverrouiller_les_donnees") : t("settings.page.ajouter_une_cle"),
    tip: action === 'unlock' ? t("settings.page.mot_de_passe_ou_cle_de_recuperation_le_secret_reste_masque_et_n_e")
      : action === 'encrypt' ? t("settings.page.choisis_une_phrase_secrete_note_la_cle_de_recuperation_qui_sera_a")
      : t("settings.page.ajoute_un_secret_pour_ouvrir_ce_meme_coffre_par_exemple_ta_cle_de"),
  });
  const submitSecret = async value => {
    const action = secretForm.action; setSecretForm(null);
    await act(async () => {
      if (action === 'addkey' && !await confirm(t("settings.page.ajouter_une_cle"), t("settings.page.la_cle_supplementaire_precedente_sera_remplacee_si_elle_existe_le"), { ok: t("settings.page.ajouter") })) return;
      const r = await post('/api/mem/' + action, action === 'encrypt' ? { password: value } : { secret: value }, { retryAuth: action !== 'unlock' });
      if (!r.ok) { toast(action === 'unlock' && r.status === 401 ? t("settings.page.secret_incorrect_ou_acces_refuse") : t("settings.page.donnees_echec_de_l_operation"), 'err'); return; }
      // Le dialogue global reste visible même si la page change pendant l’appel.
      if (action === 'encrypt' && r.recovery) await confirm(t("settings.page.cle_de_recuperation"), t("settings.page.note_cette_cle_maintenant_et_garde_la_hors_ligne_elle_ne_sera_plu") + r.recovery, { ok: t("settings.page.c_est_note") });
      toast(action === 'encrypt' ? t("settings.page.chiffrement_active") : action === 'unlock' ? t("settings.page.donnees_deverrouillees") : t("settings.page.cle_ajoutee"));
      await load(); refreshNav();
    });
  };
  const lock = () => act(async () => {
    const r = await post('/api/mem/lock', {});
    if (!r.ok) throw new Error();
    await load(); refreshNav(); toast(t("settings.page.donnees_verrouillees"));
  });
  const decrypt = () => act(async () => {
    if (!await confirm(t("settings.page.dechiffrer_les_donnees"), t("settings.page.la_memoire_et_les_discussions_seront_reecrites_en_clair_sur_le_di"), { ok: t("settings.page.dechiffrer"), danger: true })) return;
    const r = await post('/api/mem/decrypt', {});
    if (!r.ok) throw new Error();
    await load(); refreshNav(); toast(t("settings.page.chiffrement_desactive"));
  });
  const restore = s => act(async () => {
    if (!await confirm(t("settings.page.restaurer_le_snapshot"), snapshotDate(s) + t("settings.page.les_fichiers_memoire_actuels_seront_remplaces_par_cette_copie_cof"), { ok: t("settings.page.restaurer"), danger: true })) return;
    const r = await post('/api/mem/snapshots', { id: s.id });
    if (!r.ok) throw new Error();
    await load(); refreshNav(); refreshLibrary(); toast(t("settings.page.snapshot_restaure"));
  });
  const vault = mem && (mem.encrypted || mem.vault_copies > 0);
  const blocked = busy || !!secretForm;
  return html`
    <${NetworkAccess} />
    <${Group} title="${t("settings.page.api_v1")}">
      <${Line} label="${t("settings.page.cle_api_exigee")}" tip="${t("settings.page.les_applications_qui_utilisent_le_serveur_devront_envoyer_cette_c")}"><${Switch} checked=${k && k.required} onChange=${v => key({ action: 'require', on: v })} /></${Line}>
      <${Line} label="${t("settings.page.cle")}">${k && k.set ? html`<code class="mono">${k.masked}</code><button class="btn sm ghost" onClick=${() => key({ action: 'generate' })}>${t("settings.page.regenerer")}</button><button class="btn sm ghost" onClick=${() => key({ action: 'clear' })}>${t("settings.page.supprimer")}</button>`
        : html`<button class="btn sm" onClick=${() => key({ action: 'generate' })}>${t("settings.page.creer_une_cle")}</button>`}</${Line}>
    </${Group}>
    <${Group} title="${t("settings.page.memoire_et_donnees")}">
      <${Line} label="${t("settings.page.memoire_des_modeles_locaux")}" tip="${t("settings.page.desactivee_rien_n_est_retenu_entre_les_discussions_sur_demande_le")}">
        ${mode && html`<${Seg} size="sm" value=${mode} onChange=${async v => { await post('/api/memory', { mode: v }); setMode(v); }} options=${[{ value: 'off', label: t("settings.page.desactivee") }, { value: 'ondemand', label: t("settings.page.sur_demande") }]} />`}</${Line}>
      <${Line} label="${t("settings.page.chiffrement_au_repos")}" tip="${t("settings.page.aes_256_sur_la_memoire_et_les_discussions_protege_par_ta_phrase_s")}">
        ${!mem ? html`<span class="state">${t("settings.page.etat_inconnu")}</span>` : vault ? html`
          <span class=${'tag ' + (mem.fully ? 'green' : 'amber')}>${!mem.encrypted ? t("settings.page.etat_a_verifier") : mem.fully ? t("settings.page.chiffre") : t("settings.page.chiffrement_partiel")}</span>
          <span class="state">${mem.locked ? t("settings.page.verrouille") : mem.encrypted ? t("settings.page.deverrouille") : t("settings.page.etat_inconnu")}</span>
          ${(mem.locked || !mem.encrypted || !mem.fully) && html`<button class="btn sm" disabled=${blocked} onClick=${() => secret('unlock')}>${t("settings.page.deverrouiller")}</button>`}
          ${mem.encrypted && !mem.locked && html`<button class="btn sm ghost" disabled=${blocked} onClick=${lock}>${t("settings.page.verrouiller")}</button><button class="btn sm ghost" disabled=${blocked} onClick=${decrypt}>${t("settings.page.dechiffrer")}</button>`}`
          : html`<span class="state">${t("settings.page.en_clair")}</span><button class="btn sm" disabled=${blocked} onClick=${() => secret('encrypt')}>${t("settings.page.activer")}</button>`}</${Line}>
      ${mem?.encrypted && !mem.locked && html`<${Line} label="${t("settings.page.cle_supplementaire")}" tip="${t("settings.page.ajoute_ou_remplace_le_secret_d_ouverture_supplementaire_du_coffre")}"><button class="btn sm ghost" disabled=${blocked} onClick=${() => secret('addkey')}>${t("settings.page.ajouter_une_cle")}</button></${Line}>`}
      <${Line} label="${t("settings.page.exporter_la_discussion")}"><button class="btn sm ghost" onClick=${() => download('/api/chat/export?format=md', 'discussion.md').catch(() => toast(t("settings.page.export_impossible"), 'err'))}>${t("settings.page.markdown")}</button><button class="btn sm ghost" onClick=${() => download('/api/chat/export?format=json', 'discussion.json').catch(() => toast(t("settings.page.export_impossible"), 'err'))}>JSON</button></${Line}>
    </${Group}>
    <${Group} title="${t("settings.page.snapshots_locaux")}">
      <${Line} label="${t("settings.page.sauvegardes")}" tip="${t("settings.page.copies_locales_des_fichiers_memoire_coffre_compris_la_taille_n_es")}"><button class="btn sm ghost" disabled=${blocked} onClick=${load}>${t("settings.page.actualiser")}</button></${Line}>
      ${snapshots === null ? html`<${Line} label="${t("settings.page.liste")}"><span class="state">${t("settings.page.inconnue")}</span></${Line}>`
        : snapshots.length === 0 ? html`<${Line} label="${t("settings.page.liste")}"><span class="state">${t("settings.page.aucun_snapshot")}</span></${Line}>`
        : snapshots.map(s => html`<${Line} key=${s.id} label=${snapshotDate(s)} tip=${s.reason || t("settings.page.snapshot_local")}>
          <span class="num">${typeof s.size === 'number' && s.size > 0 ? fmtBytes(s.size) : t("settings.page.taille_inconnue")}</span>
          <button class="btn sm ghost" disabled=${blocked} onClick=${() => restore(s)}>${t("settings.page.restaurer")}</button>
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
    <${Group} title="${t("settings.page.loom")}">
      <${Line} label="${t("settings.page.version")}"><span class="mono">${status ? status.version : '—'}</span></${Line}>
      <${Line} label="${t("settings.page.mises_a_jour")}">${upd ? html`<span class="muted">${upd.available ? 'Version ' + upd.latest + t("settings.page.disponible") : t("settings.page.a_jour_2")}</span>` : html`<button class="btn sm" onClick=${async () => setUpd(await get('/api/update'))}>${t("settings.page.verifier")}</button>`}</${Line}>
    </${Group}>
    ${paths && html`<${Group} title="${t("settings.page.emplacements")}">${[[t("settings.page.donnees"), paths.home], [t("settings.page.base"), paths.database], [t("settings.page.modeles"), paths.models], ['Presets', paths.presets], [t("settings.page.moteurs"), paths.backends]].map(([l, p]) => html`<${Line} label=${l}><code class="mono path">${p}</code></${Line}>`)}</${Group}>`}`;
}

export function SettingsPage({ route }) {
  const sec = SECTIONS().some(s => s[0] === route.sub) ? route.sub : 'general';
  const View = { general: General, machines: MachinesSettings, engine: Engine, internet: Internet, security: Security, about: About }[sec];
  return html`<div class="view page"><div class="page-in">
    <div class="page-head"><div><h1>${t("settings.page.reglages")}</h1></div></div>
    <div class="settings">
      <nav class="set-nav">${SECTIONS().map(([id, label, ico]) => html`<a href=${'#/settings/' + id} aria-current=${id === sec ? 'page' : undefined}><${Icon} n=${ico} />${label}</a>`)}</nav>
      <div class="set-body" key=${sec}><${View} route=${route} /></div>
    </div>
  </div></div>`;
}
