// Harnesses : agents qui exécutent (Codex, Antigravity…). Loom garde la
// discussion ; chaque harness garde son compte, ses permissions et sa mémoire.
import { Logo } from '../../ui/logo.js';
import { html, useState, useEffect, useStore, cls } from '../../core/lib.js';
import { Drawer, inspectTrigger } from '../../ui/drawer.js';
import { SelectionInfo } from '../inspector/selection.js';
import { Icon } from '../../ui/icons.js';
import { Switch, Tip } from '../../ui/controls.js';
import { confirm, toast } from '../../ui/dialog.js';
import { post } from '../../core/api.js';
import { app, go, refreshWorkspace } from '../../core/state.js';
import { groupVariants } from '../chat/picker.js';
import { setVisible } from '../cloud/page.js';

// Ce que Loom sait vraiment piloter aujourd'hui, par capacité déclarée.
const CAPS = [
  ['chat', 'Discussion dans le fil commun'], ['native-events', 'Outils natifs visibles'], ['usage', 'Tokens et quotas'],
  ['reasoning-summary', 'Résumé de réflexion'], ['approvals', 'Autorisations interactives'], ['skills', 'Skills partagés'], ['mcp', 'Serveurs MCP'],
];

function Detail({ rt, models, onInspect }) {
  const native = models.filter(m => m.runtime_id === rt.id);
  const groups = groupVariants(native);
  const connected = native.length > 0;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const connectable = supported && rt.capabilities.includes('connect');
  const [busy, setBusy] = useState(false);
  const connect = async () => {
    if (!connectable || !rt.cli || !rt.consent) return;
    if (!await confirm('Connecter ' + rt.name, rt.consent, { ok: 'Connecter' })) return;
    setBusy(true);
    let r;
    try { r = await post('/api/runtimes/' + encodeURIComponent(rt.id) + '/connect', { consent: true }); }
    catch (e) { toast(e.message, 'err'); return; }
    finally { setBusy(false); }
    if (!r.ok) return toast(r.error, 'err'); toast('Modèles de ' + rt.name + ' disponibles'); refreshWorkspace();
  };
  return html`<div class="h-detail anim-fade" key=${rt.id}>
    <div class="h-head"><div style="display:flex;gap:14px;align-items:center"><${Logo} name=${rt.id} size="lg" /><div><h2>${rt.name}</h2><p>${rt.description || ''}</p></div></div>
      ${connectable ? html`<button class="btn" disabled=${busy} onClick=${connect}>${connected ? html`<${Icon} n="refresh" />Actualiser` : 'Connecter'}</button>` : !supported && html`<span class="tag">bientôt</span>`}</div>
    ${!supported ? html`<div class="card pad soon"><${Icon} n="sparkle" /><div><b>Adaptateur en préparation</b><p>Loom ne lance pas encore ${rt.name}. Il apparaîtra dans le sélecteur quand son adaptateur saura gérer la session, les autorisations et les événements.</p></div></div>` : html`
      <div class="grid2">
        <div class="card pad"><div class="sec-h"><h2>Source du modèle<${Tip} text="Natif : le compte du harness choisit parmi ses modèles. Les modèles Loom et les API personnalisées viendront quand l’adaptateur pourra les brancher proprement." /></h2></div>
          <div class="source"><label class="choice on"><input type="radio" checked /><span class="grow"><b>Natif</b><small>Le compte ${rt.name} choisit ses modèles</small></span></label>
            <label class="choice off"><input type="radio" disabled /><span class="grow"><b>Modèle Loom</b><small>Local ou cloud · bientôt</small></span></label></div></div>
        <div class="card pad"><div class="sec-h"><h2>Ce que Loom pilote</h2></div>
          ${CAPS.map(([id, label]) => html`<div class="kv"><span>${label}</span>${(rt.capabilities || []).includes(id) ? html`<span class="state"><${Icon} n="check" />oui</span>` : html`<span class="muted">non</span>`}</div>`)}</div>
      </div>
      <div class="card"><div class="sec-h pad-h"><h2>Modèles <span class="count">${groups.length}</span></h2>${connected && html`<span class="muted">Visibles dans le sélecteur de discussion</span>`}</div>
        ${connected ? html`<div class="rows">${groups.map(g => { const on = g.variants.some(v => v.enabled); return html`<div class="row"><span class="grow" ...${inspectTrigger(() => onInspect(g.variants[0]), 'Inspecter ' + g.name)}><div class="t">${g.name}</div><div class="s">${g.variants.length > 1 ? g.variants.length + ' niveaux de réflexion' : g.variants[0].model}</div></span>
          <${Switch} checked=${on} label=${'Afficher ' + g.name} onChange=${v => g.variants.forEach(x => setVisible(x.id, v))} /></div>`; })}</div>`
          : html`<p class="note pad-b">Connecte ${rt.name} pour lire les modèles de ton compte.</p>`}</div>
      <p class="note">Chaque envoi ouvre une session native avec le texte commun de la discussion. Les autorisations, la mémoire et les réglages de ${rt.name} restent chez lui. <a href="#/usage">Voir les quotas</a></p>`}
  </div>`;
}

const SHORT = { chat: 'Discussion', 'native-events': 'Outils natifs', usage: 'Tokens et quotas', 'reasoning-summary': 'Résumé de réflexion', approvals: 'Autorisations', skills: 'Skills', mcp: 'MCP' };

function Card({ rt, models }) {
  const n = groupVariants(models.filter(m => m.runtime_id === rt.id)).length;
  const supported = rt.implemented && rt.capabilities && rt.capabilities.length > 0;
  const caps = CAPS.filter(([id]) => (rt.capabilities || []).includes(id)).map(([id]) => [id, SHORT[id]]);
  return html`<button type="button" class=${cls('hx', !supported && 'is-soon')} onClick=${() => go('harnesses', rt.id)}>
    <div class="hx-top"><${Logo} name=${rt.id} />
      <span class="grow"><b>${rt.name}</b>${rt.cli && html`<code>${rt.cli}</code>`}</span>
      ${!supported ? html`<span class="soon-pill">Bientôt</span>` : html`<span class="state"><i class=${cls('dot', n && 'green')}></i>${n ? 'Connecté' : 'Non connecté'}</span>`}</div>
    ${rt.description && html`<p>${rt.description}</p>`}
    ${caps.length > 0 && html`<ul>${caps.map(([id, label]) => html`<li key=${id}><${Icon} n="check" />${label}</li>`)}</ul>`}
    <div class="hx-foot">${!supported ? 'Adaptateur en préparation' : n ? n + ' modèle' + (n > 1 ? 's' : '') + ' dans le sélecteur' : 'Ouvre pour connecter ton compte'}</div>
  </button>`;
}

export function HarnessesPage({ route }) {
  const ws = useStore(app, s => s.workspace);
  const runtimes = ((ws && ws.runtimes) || []).filter(r => r.kind === 'harness');
  const models = (ws && ws.models) || [];
  const [selected, setSelected] = useState(null);
  useEffect(() => { setSelected(s => s && s.runtime !== route.sub ? null : s); }, [route.sub]);
  const selectedRuntime = runtimes.find(r => r.id === selected?.runtime);
  const selectedModel = models.find(m => m.id === selected?.model);
  const cur = runtimes.find(r => r.id === route.sub);
  const order = r => (r.implemented && r.capabilities && r.capabilities.length ? 0 : 1);
  return html`<div class="view page"><div class="page-in wide">
    ${cur ? html`<button class="btn ghost sm back" onClick=${() => go('harnesses')}><${Icon} n="left" />Harnesses</button>
      <div style="margin-top:14px"><${Detail} key=${cur.id} rt=${cur} models=${models} onInspect=${m => setSelected({ runtime: cur.id, model: m.id })} /></div>`
    : html`<div class="page-head"><div><h1>Harnesses</h1><p>Des agents qui gardent leurs outils, leur compte et leurs permissions. Loom leur passe la discussion.</p></div></div>
      ${!ws ? html`<div class="skeleton" style="height:220px"></div>` : html`<div class="hx-grid stagger">${[...runtimes].sort((a, b) => order(a) - order(b)).map(r => html`<${Card} key=${r.id} rt=${r} models=${models} />`)}</div>`}`}
    ${selectedRuntime && html`<${Drawer} title=${selectedModel?.name || selectedRuntime.name} onClose=${() => setSelected(null)}><${SelectionInfo} model=${selectedModel} runtime=${selectedRuntime} models=${models} /></${Drawer}>`}
  </div></div>`;
}
