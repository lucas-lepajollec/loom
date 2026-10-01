// Read-only account snapshots and Loom-scoped usage. Opening the sheet never
// calls a model or polls an account. External reads require a deliberate click.
const USAGE={dialog:null,data:null,epoch:0};
function renderCodexConnection(models){
  const native=models.filter(m=>m.runtime_id==='codex');
  return '<section class="harness-config"><div class="ws-section-head"><div><h2>Codex</h2><p class="ws-note">'+(native.length?'Compte ChatGPT · '+native.length+' modèles détectés':'CLI Codex · compte ChatGPT natif')+'</p></div><button type="button" class="ws-button" data-codex-connect>'+(native.length?'Actualiser le catalogue':'Connecter Codex')+'</button></div><p class="ws-note">Pas de clé API : Loom utilise votre CLI déjà connecté. Premier pont textuel en lecture seule, sans dossier de projet. Les demandes d’approbation sont refusées. Chaque tour crée une session isolée avec le contexte commun ; la mémoire privée n’est pas transférée.</p>'+(native.length?'<details class="harness-models"><summary>Choisir les modèles du sélecteur</summary><div class="model-table">'+native.map(m=>'<article class="model-line"><div class="model-line-icon">'+wsIcon('connections')+'</div><div><h3>'+wsEsc(m.name)+'</h3><p>Réflexion réglable dans le composeur</p></div><label class="model-visibility"><input type="checkbox" data-model-visible="'+wsEsc(m.id)+'" '+(m.enabled?'checked':'')+' aria-label="Afficher '+wsEsc(m.name)+' via Codex"><span>Dans le sélecteur</span></label></article>').join('')+'</div></details>':'')+'</section>';
}
async function connectCodex(button){
  if(!await askConfirm('Lire les modèles du CLI Codex connecté avec votre compte ChatGPT ? Aucun message envoyé, aucune génération, aucune clé API copiée. Les futurs tours utiliseront un pont textuel en lecture seule, sans accès au dossier de projet.',{title:'Connecter Codex',okText:'Connecter'}))return;
  button.disabled=true;
  try{const r=await jpost('/api/workspace/codex/connect',{consent:true});if(!r.ok)throw new Error(r.error);await openWorkspace('agents');toast('Modèles Codex disponibles.');}catch(e){toast(e.message);}finally{button.disabled=false;}
}
function renderAgyConnection(models){
  const native=models.filter(m=>m.runtime_id==='antigravity');
  const groups=threadModelGroups(native);
  return '<section class="harness-config"><div class="ws-section-head"><div><h2>Antigravity</h2><p class="ws-note">'+(native.length?'Catalogue connecté · '+groups.length+' modèles · '+native.length+' variantes natives':'CLI agy · première intégration textuelle')+'</p></div><button type="button" class="ws-button" data-agy-connect>'+(native.length?'Actualiser le catalogue':'Connecter le CLI')+'</button></div><p class="ws-note">L’authentification reste dans Antigravity. Son catalogue n’est pas celui de vos providers API. Le pont transmet le contexte textuel et les instructions des skills sélectionnés ; il n’installe pas de skills et ne reprend pas la mémoire privée. Aucun dossier de projet n’est donné au CLI. Ses permissions/configurations natives restent actives : ce n’est pas un sandbox complet.</p>'+(native.length?'<details class="harness-models"><summary>Gérer les '+groups.length+' modèles du sélecteur</summary><p class="ws-note">Un modèle par famille. Le niveau de réflexion se choisit dans le composeur ; activer ou masquer une famille agit sur toutes ses variantes.</p><div class="model-table">'+groups.map(g=>'<article class="model-line"><div class="model-line-icon">'+wsIcon('connections')+'</div><div><h3>'+wsEsc(g.name)+'</h3><p>'+(g.variants.length>1?'Réflexion : '+g.variants.map(m=>({low:'faible',medium:'moyenne',high:'élevée'}[m.effort])).join(' · '):'Mode natif')+'</p></div><label class="model-visibility"><input type="checkbox" data-model-group="'+wsEsc(g.variants.map(m=>m.id).join('|'))+'" '+(g.variants.some(m=>m.enabled)?'checked':'')+' aria-label="Afficher '+wsEsc(g.name)+' via Antigravity"><span>Dans le sélecteur</span></label></article>').join('')+'</div></details>':'')+'</section>';
}
async function connectAgy(){
  if(!await askConfirm('Lire le catalogue du CLI agy installé et proposer ses modèles dans Loom ? Votre connexion Antigravity reste native. Les futurs envois utiliseront son compte et sa configuration : sans auto-approbation ajoutée par Loom, mais avec les permissions déjà accordées au CLI. Le dossier de projet ne sera pas ouvert. Aucun message ni clé de provider n’est transmis maintenant.',{title:'Connecter Antigravity',okText:'Connecter'}))return;
  try{const r=await jpost('/api/workspace/antigravity/connect',{consent:true});if(!r.ok)throw new Error(r.error);await openWorkspace('agents');toast('Catalogue Antigravity disponible dans le sélecteur.');}catch(e){toast(e.message);}
}
function threadHarnessEvents(s){
  threadCloudUsage(s);
  const box=wsEl('harness-events');if(!box)return;
  const turn=s.turns?.at(-1),events=turn?.events||[];
  box.innerHTML=(turn?.native_session_id?'<p class="ws-note">Session native : <code>'+wsEsc(turn.native_session_id)+'</code></p>':'')+'<div class="turn-metadata">'+threadTurnDetails(turn)+'</div>'+(events.length?'':'<p class="ws-note">Aucun événement d’outil communiqué pour ce tour.</p>');
}
function usageDate(seconds){return seconds?new Date(seconds*1000).toLocaleString(undefined,{dateStyle:'medium',timeStyle:'short'}):'Non communiqué';}
function usageNumber(n){return Number(n).toLocaleString();}
async function openUsage(){
  if(!USAGE.dialog){
    const d=document.createElement('dialog');d.id='usage-sheet';d.className='ws-dialog usage-sheet';d.setAttribute('aria-labelledby','usage-title');
    d.innerHTML='<div class="ws-dialog-head"><div><h2 id="usage-title">Usage et quotas</h2><p class="ws-note">Abonnements natifs et consommation dans Loom</p></div><button type="button" class="ws-icon-button" data-usage-close aria-label="Fermer usage et quotas">×</button></div><div class="usage-body" aria-live="polite"></div><div class="ws-dialog-foot"><span>Aucun reset consommé. Aucun appel IA pour lire cette page.</span><button type="button" class="ws-button" data-usage-close>Fermer</button></div>';
    d.addEventListener('close',()=>{++USAGE.epoch;wsEl('usage-btn')?.focus();});
    d.addEventListener('click',async e=>{
      if(e.target.closest('[data-usage-close]')){d.close();return;}
      const refresh=e.target.closest('[data-quota-refresh]');if(!refresh)return;
      refresh.disabled=true;refresh.textContent='Lecture…';
      try{const r=await jpost('/api/usage/refresh',{runtime_id:refresh.dataset.quotaRefresh});if(!r.ok)toast(r.error);await loadUsage();}catch(error){toast(error.message);refresh.textContent='Réessayer';refresh.disabled=false;}
    });
    d.addEventListener('submit',async e=>{
      const form=e.target.closest('[data-usage-price]');if(!form)return;e.preventDefault();const f=new FormData(form),button=form.querySelector('button');button.disabled=true;
      try{const r=await jpost('/api/usage/price',{choice_id:form.dataset.usagePrice,input_per_million:Number(f.get('input')),output_per_million:Number(f.get('output')),currency:f.get('currency')});if(!r.ok)throw new Error(r.error);await loadUsage();}catch(error){toast(error.message);button.disabled=false;}
    });
    document.body.appendChild(d);USAGE.dialog=d;
  }
  if(!USAGE.dialog.open){USAGE.dialog.showModal();USAGE.dialog.scrollTop=0;}
  await loadUsage();
}
async function loadUsage(){
  const epoch=++USAGE.epoch,box=USAGE.dialog.querySelector('.usage-body');
  if(!USAGE.data)box.textContent='Chargement de l’usage enregistré…';
  try{const r=await jget('/api/usage');if(epoch!==USAGE.epoch||!USAGE.dialog.open)return;if(!r.ok)throw new Error(r.error);USAGE.data=r;renderUsage(r,box);}
  catch(e){if(epoch===USAGE.epoch){box.textContent='Lecture indisponible : '+e.message;}}
}
function renderUsage(data,box){
  const names={'antigravity':'Antigravity','codex':'Codex','claude-code':'Claude Code','pi':'Pi','hermes':'Hermes'};
  const quota=(data.quotas||[]).map(q=>{
    if(!['antigravity','codex'].includes(q.runtime_id))return '';
    const windows=(q.windows||[]).map(w=>{
      const known=typeof w.remaining_percent==='number'&&Number.isFinite(w.remaining_percent),p=known?Math.max(0,Math.min(100,w.remaining_percent)):null;
      return '<div class="usage-window"><div><strong>'+wsEsc(w.group)+'</strong><span>'+wsEsc(w.name)+'</span><b>'+(known?p.toLocaleString(undefined,{maximumFractionDigits:2})+' % restants':'Non communiqué')+'</b></div>'+(known?'<meter min="0" max="100" value="'+p+'" aria-label="Quota restant '+wsEsc(w.group)+' '+wsEsc(w.name)+'">'+p+' %</meter>':'')+'<p class="ws-note">Réinitialisation : '+wsEsc(usageDate(w.reset_at))+'</p></div>';
    }).join('');
    const readable=['antigravity','codex'].includes(q.runtime_id);
    return '<article class="usage-card"><div class="ws-section-head"><h3>'+wsEsc(names[q.runtime_id]||q.name)+'</h3>'+(readable?'<button type="button" class="ws-button" data-quota-refresh="'+wsEsc(q.runtime_id)+'">'+(q.fetched_at?'Actualiser':'Lire le compte local')+'</button>':'<span class="ws-note">Lecture non intégrée</span>')+'</div>'+(q.error?'<p class="usage-error">'+wsEsc(q.error)+(q.fetched_at?' · Ancienne lecture conservée, potentiellement obsolète.':'')+'</p>':'')+(windows||'<p class="ws-note">Quotas et resets non communiqués à Loom. Cela ne signifie pas que le quota est nul ou illimité.</p>')+(q.fetched_at?'<div class="usage-credit-grid"><p>Resets disponibles<br><strong>'+(q.reset_credits===null||q.reset_credits===undefined?'Non communiqué':usageNumber(q.reset_credits))+'</strong></p><p>Crédits IA<br><strong>'+(q.credits===null||q.credits===undefined?'Non communiqué':usageNumber(q.credits))+'</strong></p></div>'+(q.reset_details||[]).map(c=>'<p class="ws-note">'+wsEsc(c.title||'Crédit de reset')+' · '+wsEsc(c.status)+' · expiration : '+wsEsc(usageDate(c.expiresAt))+'</p>').join('')+'<p class="ws-note">'+wsEsc(q.source)+' · lu le '+wsEsc(usageDate(q.fetched_at))+'</p>':'')+'</article>';
  }).join('');
  const models=(data.models||[]).map(m=>{
    const price=m.price;
    return '<article class="usage-card"><h3>'+wsEsc(m.name)+'</h3><p class="ws-note">'+wsEsc(m.provider)+' · '+m.reported_turns+' / '+m.turns+' tours avec décompte</p><div class="usage-credit-grid"><p>Tokens en entrée<br><strong>'+(m.reported_turns?usageNumber(m.usage.prompt_tokens):'—')+'</strong></p><p>Tokens en sortie<br><strong>'+(m.reported_turns?usageNumber(m.usage.completion_tokens):'—')+'</strong></p><p>Coût API estimé<br><strong>'+(m.estimated_cost!==null&&m.estimated_cost!==undefined?Number(m.estimated_cost).toLocaleString(undefined,{style:'currency',currency:price.currency,maximumFractionDigits:6}):m.runtime_id==='antigravity'?'Abonnement natif':'Tarif non renseigné')+'</strong></p></div>'+(m.choice_id.startsWith('cloud:')?'<details class="usage-pricing"><summary>'+(price?'Modifier le tarif indicatif':'Renseigner un tarif indicatif')+'</summary><form data-usage-price="'+wsEsc(m.choice_id)+'"><label class="ws-field">Entrée / million de tokens<input name="input" type="number" min="0" max="10000" step="any" required value="'+(price?.input_per_million??'')+'"></label><label class="ws-field">Sortie / million de tokens<input name="output" type="number" min="0" max="10000" step="any" required value="'+(price?.output_per_million??'')+'"></label><label class="ws-field">Devise<select name="currency"><option '+(price?.currency==='USD'?'selected':'')+'>USD</option><option '+(price?.currency==='EUR'?'selected':'')+'>EUR</option></select></label><button class="ws-button" type="submit">Enregistrer le tarif</button></form><p class="ws-note">Tarif saisi manuellement, appliqué aux tours conservés. Estimation simple entrée/sortie, hors remises/cache, paliers, taxes et autres frais. Ce n’est pas votre facture provider.</p></details>':'')+'</article>';
  }).join('');
  box.innerHTML='<section><h3>Quotas d’abonnement</h3><p class="ws-note">« Lire le compte local » utilise uniquement le protocole du CLI connecté, sans génération. Les limites peuvent être partagées avec les autres applications du compte. Les crédits IA et les crédits de reset sont distincts.</p>'+quota+'<details class="usage-pricing"><summary>Autres harnesses</summary><p class="ws-note">Claude Code, Pi et Hermes : lecture de quotas non intégrée. Aucun pourcentage, crédit ou reset ne peut être déduit par Loom.</p></details></section><section><h3>Tokens et coûts API</h3><p class="ws-note">'+wsEsc(data.scope)+'. Toutes les discussions encore conservées, pas toute l’activité de vos comptes. Les tours sans usage restent inconnus ; supprimer un fil retire son décompte. Une estimation peut donc être partielle.</p>'+(models||'<p class="ws-note">Aucune consommation enregistrée. Connectez vos providers dans Modèles ou utilisez un harness connecté.</p>')+'<p class="ws-note">llama.cpp reste local : aucune facturation API cloud dans Loom. Les métriques de contexte et vitesse restent dans son panneau natif.</p></section>';
}
document.addEventListener('click',e=>{if(e.target.closest('[data-agy-connect]'))connectAgy();const codex=e.target.closest('[data-codex-connect]');if(codex)connectCodex(codex);});
