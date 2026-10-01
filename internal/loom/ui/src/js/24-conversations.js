// Loom owns the conversation. A model/provider is the next turn's destination,
// never a second history. Harnesses will attach native sessions to this record.
const THREAD={timer:null,epoch:0,current:null,resumeID:'',native:false,harness:null,drafts:new Map(),pending:new Map(),notice:'',context:null,sharedContext:'',modelsTab:'local'};
function threadOwnsChat(){return !!(THREAD.harness||(THREAD.current&&!THREAD.native));}
function threadPanelMode(){return THREAD.harness||['antigravity','codex'].includes(THREAD.current?.runtime_id)?'harness':THREAD.current?.runtime_id==='openai-compatible'?'cloud':'local';}
function threadLeaveView(){
  const owned=threadOwnsChat(),draft=wsEl('input');if(draft&&THREAD.current)THREAD.drafts.set(THREAD.current.id,draft.value);
  clearTimeout(THREAD.timer);++THREAD.epoch;THREAD.current=null;THREAD.context=null;THREAD.notice='';
  THREAD.harness=null;THREAD.native=false;WS.session='';
  if(wsEl('thread-preview')?.open)wsEl('thread-preview').close();
  document.documentElement.removeAttribute('data-runtime-panel');
  wsEl('params-runtime').hidden=true;
  wsEl('params-context').hidden=true;
  wsEl('plus-menu-btn').disabled=false;wsEl('tool-menu-btn').disabled=false;
  wsEl('ctxmeta').hidden=false;
  if(owned){streamAbort?.abort();lastSeq=0;REPLAYING=true;busy=false;chatEl().replaceChildren();draft.value='';autoGrow(draft);setChatLoading('chargement de la conversation…');}
  renderSessionChrome();syncSendBtn();
}
function threadStatus(s){return ({idle:'Prête',running:'Réponse en cours',complete:'Terminée',cancelled:'Arrêtée',error:'À vérifier',interrupted:'Interrompue',unsaved:'Non enregistrée'})[s.status]||s.status;}
function threadModelName(s){return s.model?String(s.model).split(/[\\/]/).pop():'Choisir un modèle';}
function threadRows(sessions){return '<div class="ws-row-list">'+sessions.map(s=>'<div class="thread-row"><button type="button" class="ws-discussion-row" data-thread-open="'+wsEsc(s.id)+'"><span><strong>'+wsEsc(s.title)+'</strong><small>'+wsEsc(s.provider_name||'Loom')+' · '+wsEsc(threadModelName(s))+'</small></span>'+wsIcon('arrow')+'</button><button type="button" class="ws-icon-button" data-thread-delete="'+wsEsc(s.id)+'" aria-label="Supprimer '+wsEsc(s.title)+'">×</button></div>').join('')+'</div>';}
function renderDiscussions(){
  if(WS.session){threadOpen(WS.session);return;}
  const sessions=WS.data.sessions||[];
  const imported=new Set(sessions.map(s=>s.source_archive).filter(Boolean));
  const archives=(NAV.conversations||[]).filter(s=>!s.workspace&&!imported.has(s.id));
  wsEl('ws-body').innerHTML=(sessions.length?threadRows(sessions):'<div class="thread-welcome"><span>'+wsIcon('connections')+'</span><h2>Une discussion. Le modèle de votre choix.</h2><p>Commencez un fil, puis changez de modèle quand vous voulez.<br>L’historique et le contexte du projet restent dans la discussion.</p>'+wsButton('Nouvelle discussion','new-thread',true)+'</div>')+(archives.length?wsSection('Discussions existantes','<p class="ws-note">Ouvrez une discussion pour reprendre son texte dans le fil commun. L’archive complète reste conservée.</p><div class="ws-row-list">'+archives.map(s=>'<button type="button" class="ws-discussion-row" data-thread-import="'+wsEsc(s.id)+'"><span>'+wsEsc(s.title||'Discussion')+'</span>'+wsIcon('arrow')+'</button>').join('')+'</div>'):'');
}
async function threadCreate(projectID){
  return newChat(projectID);
}
function renderProjectThreads(projectID){
  const sessions=(WS.data.sessions||[]).filter(s=>s.project_id===projectID);if(!sessions.length)return;
  const section=document.createElement('section');section.className='ws-section';section.innerHTML='<div class="ws-section-head"><h2>Fils de discussion</h2></div>'+threadRows(sessions);wsEl('ws-body').appendChild(section);
}
function modelTabs(){return '<div class="workspace-tabs" aria-label="Gestion des modèles">'+[['local','Locaux'],['cloud','Cloud'],['providers','Providers']].map(([id,label])=>'<button type="button" class="'+(THREAD.modelsTab===id?'selected':'')+'" data-model-tab="'+id+'" aria-pressed="'+(THREAD.modelsTab===id)+'">'+label+'</button>').join('')+'</div>';}
function wsRenderModels(){
  const kind=THREAD.modelsTab;
  wsEl('ws-actions').innerHTML=kind==='local'?wsButton('Bibliothèque & paramètres','library')+wsButton('Télécharger · Hub','download',true):wsButton('Ajouter un provider','new-provider',true);
  if(kind==='providers'){wsEl('ws-body').innerHTML=modelTabs()+renderProviderConnections();return;}
  if(kind==='cloud'){wsEl('ws-body').innerHTML=modelTabs()+renderCloudModelSelection();return;}
  const models=(WS.data.models||[]).filter(m=>m.kind===kind);
  wsEl('ws-body').innerHTML=modelTabs()+'<p class="ws-muted">Cochez les modèles à proposer dans le sélecteur des discussions.</p>'+(models.length?'<div class="model-table">'+models.map(m=>'<article class="model-line"><div class="model-line-icon">'+wsIcon(m.kind)+'</div><div><h2>'+wsEsc(m.name)+'</h2><p>'+wsEsc(m.provider_name)+(m.kind==='local'?' · GGUF':' · '+wsEsc(m.endpoint))+'</p></div><label class="model-visibility"><input type="checkbox" data-model-visible="'+wsEsc(m.id)+'" '+(m.enabled?'checked':'')+' aria-label="Afficher '+wsEsc(m.name)+' dans le sélecteur"><span>Dans le sélecteur</span></label></article>').join('')+'</div>':wsEmpty(kind,kind==='local'?'Aucun modèle local':'Aucun modèle cloud',kind==='local'?'Téléchargez un GGUF depuis le Hub ou ajoutez votre dossier de modèles.':'Ajoutez un provider et renseignez les identifiants des modèles que vous utilisez.',kind==='local'?wsButton('Ouvrir le Hub','download'):wsButton('Ajouter un provider','new-provider')))+'<p class="ws-note">Masquer un modèle ne supprime ni ses fichiers, ni les discussions qui l’utilisent.</p>';
}
function renderProviderConnections(){
  const providers=WS.data.providers||[];
  return (providers.length?'<div class="ws-row-list">'+providers.map(p=>'<div class="ws-runtime-row"><span class="ws-lettermark">'+wsIcon('cloud')+'</span><div><h3>'+wsEsc(p.name)+'</h3><p>'+wsEsc(p.endpoint)+'</p><p>'+(p.models||[p.model]).length+' modèle(s) · '+(p.ready?'Clé en mémoire':'Clé à renseigner')+'</p></div><button type="button" class="ws-button" data-provider-edit="'+wsEsc(p.id)+'">Configurer</button>'+(p.ready?'<button type="button" class="ws-icon-button" data-provider-disconnect="'+wsEsc(p.id)+'" aria-label="Oublier la clé de '+wsEsc(p.name)+'">×</button>':'')+'</div>').join('')+'</div>':wsEmpty('cloud','Connecter un provider','Une connexion fournit plusieurs modèles. Ils se gèrent ensuite dans l’onglet Cloud.',wsButton('Ajouter un provider','new-provider')))+'<p class="ws-note">API compatible Chat Completions · clé conservée uniquement en mémoire côté serveur. Aucun appel externe à l’enregistrement.</p>';
}
function wsRenderHarnesses(){
  const profiles=WS.data.harness_profiles||[],models=WS.data.models||[];
  wsEl('ws-body').innerHTML='<div class="workspace-notice"><strong>Un harness exécute, Loom garde le fil.</strong><p>Antigravity et Codex disposent d’un premier pont textuel. Les autres configurations restent préparatoires : une association de modèle n’est pas une preuve de compatibilité.</p></div>'+WS.data.runtimes.filter(r=>r.kind==='harness').map(r=>{
    if(r.id==='antigravity')return renderAgyConnection(models);
    if(r.id==='codex')return renderCodexConnection(models)+profiles.filter(p=>p.runtime_id==='codex').map(p=>'<button type="button" class="harness-profile" data-harness-edit="'+wsEsc(p.id)+'"><strong>'+wsEsc(p.name)+'</strong><span>Association préparatoire · non exécutable</span><span>Modifier →</span></button>').join('');
    const rows=profiles.filter(p=>p.runtime_id===r.id);
    return '<section class="harness-config"><div class="ws-section-head"><div><h2>'+wsEsc(r.name)+'</h2><p class="ws-note">Adapter non connecté</p></div><button type="button" class="ws-button" data-harness-new="'+wsEsc(r.id)+'">Configurer</button></div>'+rows.map(p=>'<button type="button" class="harness-profile" data-harness-edit="'+wsEsc(p.id)+'"><strong>'+wsEsc(p.name)+'</strong><span>'+wsEsc(p.model_ids.map(id=>models.find(m=>m.id===id)?.name||'Modèle indisponible').join(' · ')||'Aucun modèle associé')+'</span><span>Modifier →</span></button>').join('')+'</section>';
  }).join('')+'<p class="ws-note">La compatibilité de chaque modèle et la reprise des sessions devront être vérifiées par l’adapter. Aucun routage cloud ni export de skills n’est effectué ici.</p>';
}
function threadEditor(title,kind,id){
  WS.editor={kind,id:id||''};wsEl('ws-editor-title').textContent=title;wsEl('ws-editor-kicker').textContent='Configuration';wsEl('ws-editor-error').hidden=true;wsEl('ws-editor-fields').replaceChildren();wsEl('ws-editor-save').textContent='Enregistrer';return wsEl('ws-editor-fields');
}
function editProvider(id){
  cloudProviderEditor(id);
}
function harnessEditor(runtimeID,id){
  const p=(WS.data.harness_profiles||[]).find(p=>p.id===id)||{},runtime=WS.data.runtimes.find(r=>r.id===(p.runtime_id||runtimeID));
  const fields=threadEditor('Configurer '+runtime.name,'harness',id);WS.editor.runtimeID=runtime.id;
  fields.appendChild(wsField('Nom de la configuration','name',p.name||runtime.name,{required:true,maxLength:100}));
  const group=document.createElement('fieldset');group.className='ws-skill-picker';group.innerHTML='<legend>Modèles souhaités · compatibilité à vérifier</legend>';
  (WS.data.models||[]).forEach(m=>{const label=document.createElement('label'),input=document.createElement('input');input.type='checkbox';input.name='model_ids';input.value=m.id;input.checked=(p.model_ids||[]).includes(m.id);label.append(input,document.createTextNode(m.name+' · '+m.provider_name));group.appendChild(label);});
  fields.appendChild(group);const note=document.createElement('p');note.className='ws-note';note.textContent='Configuration préparatoire. L’adapter devra confirmer les modèles, les autorisations et les modes de reprise réellement supportés.';fields.appendChild(note);wsEl('ws-editor').showModal();fields.querySelector('input').focus();
}
async function saveRuntimeEditor(event){
  const editor={...WS.editor},form=new FormData(event.target),button=wsEl('ws-editor-save');button.disabled=true;wsEl('ws-editor-error').hidden=true;
  try{let r;if(editor.kind==='provider'){const models=cloudSelectedModels(String(form.get('models')));if(!models.length||models.length>32)throw new Error('Sélectionnez entre 1 et 32 modèles.');r=await jpost('/api/providers/save',{id:editor.id,name:form.get('name'),endpoint:form.get('endpoint'),model:models[0]||'',models,key:form.get('key'),usage_mode:form.get('cloud_usage')?'':'none'});}else{r=await jpost('/api/workspace/harnesses/save',{id:editor.id,runtime_id:editor.runtimeID,name:form.get('name'),model_ids:form.getAll('model_ids')});}
    if(!r.ok)throw new Error(r.error);closeWorkspaceEditor();if(editor.kind==='provider')THREAD.modelsTab='cloud';await openWorkspace(editor.kind==='provider'?'models':'agents');
  }catch(e){wsEl('ws-editor-error').textContent=e.message;wsEl('ws-editor-error').hidden=false;}finally{button.disabled=false;}
}
// Common cloud transcripts reuse Loom's original chat DOM and composer.
// Native local conversations keep their existing journal, tools and rendering.
async function threadOpen(id){
  showMainView('chat');
  const epoch=++THREAD.epoch;
  try{
    const [r,workspace]=await Promise.all([jget('/api/runtime/sessions?id='+encodeURIComponent(id)),jget('/api/workspace')]);
    if(epoch!==THREAD.epoch)return;
    if(!r.ok||!workspace.ok)throw new Error(r.error||workspace.error);
    THREAD.native=false;THREAD.harness=null;
    WS.data=workspace;WS.session=id;THREAD.resumeID=id;THREAD.current=r.session;threadReceiveContext(r.context);
    streamAbort?.abort();REPLAYING=false;busy=false;
    chatEl().replaceChildren();chatEl().style.opacity='1';setChatLoading(null);
    wsEl('input').value=THREAD.drafts.get(id)||'';autoGrow(wsEl('input'));
    history.replaceState(null,'','#discussions/'+encodeURIComponent(id));
    if(r.session.runtime_id==='llama.cpp'){
      const local=await jpost('/api/runtime/sessions/local',{id});
      if(epoch!==THREAD.epoch)return;
      if(!local.ok)throw new Error(local.error);
      THREAD.current=local.session;THREAD.native=true;threadReceiveContext(local.context);
      handleDelta({reset:true,replay:true});
      wsEl('plus-menu-btn').disabled=false;wsEl('tool-menu-btn').disabled=false;wsEl('ctxmeta').hidden=false;
      setChatLoading('chargement de la conversation…');syncSendBtn();
    }
    threadPaint(r.session,true);threadPanel();
    THREAD.timer=setTimeout(()=>threadPoll(id,epoch),800);
    if(window.matchMedia('(max-width:720px)').matches)closeSide();
  }catch(e){if(epoch===THREAD.epoch){THREAD.notice=e.message;toast(e.message);if(THREAD.current){threadSyncSend();threadPanel();}else showMainView('chat');}}
}
function threadPaint(s,initial){
  const changed=!THREAD.current||THREAD.current.runtime_id!==s.runtime_id||THREAD.current.model!==s.model||THREAD.current.provider_id!==s.provider_id;
  THREAD.current=s;
  const navEntry=NAV.conversations.find(c=>c.id===s.id&&c.workspace);
  if(!navEntry||navEntry.title!==s.title||navEntry.project_id!==s.project_id||NAV.active!==s.id){
    if(navEntry){navEntry.title=s.title;navEntry.project_id=s.project_id;}else NAV.conversations.unshift({...s,workspace:true});
    NAV.active=s.id;renderNav();
  }
  if(!THREAD.native){
    const log=chatEl(),atEnd=log.scrollHeight-log.scrollTop-log.clientHeight<80;
    if(log.children.length>s.messages.length)log.replaceChildren();
    s.messages.forEach((message,i)=>{
      let item=log.children[i];if(!item)item=addMsg(message.role,'');
      const turn=(s.turns||[]).find(t=>t.message_index===i);
      setLabel(item,message.role==='user'?'':turn?threadModelName(turn)+' · '+turn.provider_name:'Assistant');
      const text=String(message.content||'');if(bodyOf(item).textContent!==text)bodyOf(item).textContent=text;
      if(message.role==='assistant')threadPaintMetadata(item,turn,s.status==='running'&&i===s.messages.length-1);
    });
    if(initial||atEnd)jumpBottom();
    syncChatEmpty();threadSyncSend();
  }
  threadSyncPickerLabel();
  if(typeof threadSyncReason==='function')threadSyncReason();
  if(initial||changed)threadPanel();
  if(s.runtime_id==='openai-compatible'&&!THREAD.harness)threadCloudUsage(s);
  if(['antigravity','codex'].includes(s.runtime_id))threadHarnessEvents(s);
  if(wsEl('thread-context-count'))wsEl('thread-context-count').textContent=s.messages.filter(m=>m.content).length+' messages textuels';
  document.querySelectorAll('[data-thread-configure]').forEach(b=>b.disabled=s.status==='running'||s.status==='unsaved'||(THREAD.native&&busy));
}
function threadSyncPickerLabel(){
  if(THREAD.harness){wsEl('model-pick-label').textContent=THREAD.harness.name+' · non connecté';return true;}
  if(THREAD.current&&!THREAD.native){const group=threadModelGroups(WS.data?.models||[]).find(g=>g.variants.some(m=>m.runtime_id===THREAD.current.runtime_id&&m.model===THREAD.current.model));wsEl('model-pick-label').textContent=(group?.name||threadModelName(THREAD.current))+(threadPanelMode()==='cloud'?' · '+THREAD.current.provider_name:'');return true;}
  return false;
}
function threadSyncSend(){
  const s=THREAD.current,unsupported=!!THREAD.harness;
  const disconnected=s?.runtime_id==='openai-compatible'&&!!s?.provider_id&&!(WS.data?.providers||[]).some(p=>p.id===s.provider_id&&p.ready);
  const running=!unsupported&&s?.status==='running',unsaved=s?.status==='unsaved';
  wsEl('send').hidden=running||unsaved;wsEl('stop').hidden=!running&&!unsaved;
  wsEl('send').disabled=unsupported||disconnected||!s?.model||!!THREAD.context?.problem;
  const hint=THREAD.notice||s?.error||THREAD.context?.problem||(unsupported?'Harness non connecté : aucun agent ne sera lancé.':disconnected?'Clé absente : reconnectez ce provider dans Modèles → Providers.':running?'Réponse en cours…':unsaved?'Réponse non enregistrée : utilisez Arrêter pour réessayer la sauvegarde.':sendHintText());
  wsEl('sendhint').textContent=hint;wsEl('sendhint').classList.toggle('waiting',!!(THREAD.notice||s?.error||unsupported||disconnected||THREAD.context?.problem));
  wsEl('plus-menu-btn').disabled=true;wsEl('tool-menu-btn').disabled=true;
  wsEl('ctxmeta').hidden=true;
}
function threadPanel(){
  const mode=threadPanelMode(),box=wsEl('params-runtime'),s=THREAD.current;
  document.documentElement.dataset.runtimePanel=mode;
  if(mode==='local'){
    box.hidden=true;renderSessionChrome();
    wsEl('params-context').hidden=!s;
    if(s)threadContextPanel(wsEl('params-context-body'),s);
    return;
  }
  wsEl('params-body').hidden=true;wsEl('params-empty').hidden=true;wsEl('params-tools').hidden=true;box.hidden=false;
  if(mode==='harness'){
    if(!THREAD.harness&&['antigravity','codex'].includes(s?.runtime_id)){
      wsEl('params-kind').textContent=threadModelName(s);wsEl('params-sub').textContent=s.provider_name+' · pont textuel expérimental';
      box.innerHTML='<h3>Session '+wsEsc(s.provider_name)+'</h3><p class="ws-note">Nouveau tour natif à chaque envoi ; Loom transmet le texte commun, pas la mémoire privée. Le dossier de projet n’est pas ouvert. '+(s.runtime_id==='codex'?'Compte ChatGPT natif · lecture seule · demandes d’approbation refusées dans ce premier pont.':'Compte et permissions gérés par le CLI installé.')+'</p><p id="cloud-usage" class="ws-note"></p><div id="harness-events"></div>'+wsButton('Gérer les harnesses','harnesses')+'<div id="thread-context"></div>';
      threadCloudUsage(s);threadHarnessEvents(s);threadContextPanel(wsEl('thread-context'),s);return;
    }
    const profile=THREAD.harness,runtime=WS.data?.runtimes.find(r=>r.id===profile.runtime_id);
    wsEl('params-kind').textContent=runtime?.name||'Harness';wsEl('params-sub').textContent='Adapter non connecté';
    box.innerHTML='<h3>'+wsEsc(profile.name)+'</h3><p class="ws-note">Configuration enregistrée. L’exécution native n’est pas encore disponible.</p><div class="context-fact"><strong>Modèles associés</strong><span>'+wsEsc(profile.model_ids.map(id=>WS.data.models.find(m=>m.id===id)?.name||'Modèle indisponible').join(' · ')||'Aucun modèle')+'</span></div>'+wsButton('Configurer les harnesses','harnesses');
  }else{
    wsEl('params-kind').textContent=threadModelName(s);wsEl('params-sub').textContent='Cloud · '+s.provider_name;
    box.innerHTML='<div class="context-fact"><strong>Provider</strong><span>'+wsEsc(s.provider_name)+'</span></div><div class="context-fact"><strong>Destination</strong><code class="thread-endpoint">'+wsEsc(s.endpoint)+'</code></div><p class="ws-note">Streaming textuel. Les fichiers et outils ne sont pas pris en charge par cet adapter.</p><p id="cloud-usage" class="ws-note"></p>'+wsButton('Configurer les providers','providers')+'<div id="thread-context"></div>';
    threadCloudUsage(s);
    threadContextPanel(wsEl('thread-context'),s);
  }
}
function threadCloudUsage(s){const el=wsEl('cloud-usage');if(el)el.textContent=s.usage?s.usage.prompt_tokens+' tokens en entrée · '+s.usage.completion_tokens+' en sortie (dernier tour)':'Décompte des tokens : non communiqué par le provider.';}
function threadSyncLocalControls(){if(threadPanelMode()!=='local'&&wsEl('params-runtime').hidden)threadPanel();}
async function threadRefreshPicker(){
  const workspace=await jget('/api/workspace');if(!workspace.ok)return;WS.data=workspace;
  const cloud=wsEl('model-menu-cloud'),harness=wsEl('model-menu-harness');cloud.replaceChildren();harness.replaceChildren();
  const choices=workspace.models.filter(m=>m.enabled&&m.kind==='cloud');
  choices.forEach(m=>cloud.appendChild(pickerRow({label:m.name,sub:m.provider_name,active:THREAD.current?.model===m.model&&THREAD.current?.provider_id===m.provider_id&&!THREAD.harness,onClick:()=>threadChooseModel(m)})));
  threadModelGroups(workspace.models.filter(m=>m.kind==='harness')).filter(g=>g.variants.some(m=>m.enabled)).forEach(g=>{const m=threadGroupChoice(g);harness.appendChild(pickerRow({label:g.name,sub:m.provider_name+' · pont textuel'+(g.variants.length>1?' · réflexion réglable':''),active:THREAD.current?.runtime_id===m.runtime_id&&g.variants.some(v=>v.model===THREAD.current?.model),onClick:()=>threadChooseModel(m)}));});
  (workspace.harness_profiles||[]).forEach(p=>harness.appendChild(pickerRow({label:p.name,sub:'Adapter non connecté · aperçu de la configuration',onClick:()=>{
    if(busy||THREAD.current?.status==='running'){toast('Arrêtez la réponse avant de changer de runtime.');return;}
    THREAD.harness=p;streamAbort?.abort();threadSyncPickerLabel();threadPanel();threadSyncSend();setParamsOpen(true);
  }})));
  wsEl('model-menu-cloud-section').hidden=!choices.length;wsEl('model-menu-harness-section').hidden=!harness.children.length;
  // Keep the original model/preset menu, honouring catalog visibility.
  wsEl('model-menu-models').querySelectorAll('.mpick-item').forEach(row=>{
    const name=row.querySelector('.mpick-name')?.textContent;
    const entry=workspace.models.find(m=>m.kind==='local'&&m.name===name);
    if(entry)row.hidden=!entry.enabled&&!row.classList.contains('inuse');
  });
}
async function threadChooseModel(choice){
  if(busy||THREAD.current?.status==='running'||THREAD.current?.status==='unsaved'){toast('Arrêtez ou enregistrez la réponse avant de changer de modèle.');return false;}
  const external=choice.kind==='cloud'||choice.kind==='harness';
  const message=choice.kind==='harness'?'Continuer avec '+choice.name+' via '+choice.provider_name+' ? Le texte du fil et les instructions/skills seront transmis à son compte natif. Une session est créée par tour. '+(choice.runtime_id==='codex'?'Pont en lecture seule ; les demandes d’approbation sont refusées.':'Ses permissions restent actives ; les approbations ne sont pas interactives dans Loom.')+' Fichiers et mémoire privée ne sont pas transférés.':'Continuer avec '+choice.name+' chez '+choice.provider_name+' ? Le texte du fil et les instructions/skills seront envoyés à '+choice.endpoint+'. Fichiers, outils et mémoire privée ne sont pas transférés.';
  const consent=external?await askConfirm(message,{title:'Changer de modèle',okText:'Continuer'}):false;
  if(external&&!consent)return false;
  try{
    let id=THREAD.current?.id;
    if(!id){
      const historyData=await jget('/api/chat/history');
      const sessions=await jget('/api/runtime/sessions');
      const bound=sessions.sessions?.find(s=>s.native_archive===historyData.active);
      if(bound)id=bound.id;
      else{
        const archived=historyData.conversations?.some(c=>c.id===historyData.active);
        const created=archived?await jpost('/api/runtime/sessions/import',{id:historyData.active}):await jpost('/api/runtime/sessions/create',{project_id:historyData.project_id||''});
        if(!created.ok)throw new Error(created.error);id=created.session.id;
      }
    }
    const selected=await jpost('/api/runtime/sessions/select',{id,choice_id:choice.id,consent});
    if(!selected.ok)throw new Error(selected.error);
    const draft=wsEl('input').value;
    await threadOpen(id);
    wsEl('input').value=draft;autoGrow(wsEl('input'));
    return true;
  }catch(e){toast(e.message);return false;}
}
async function threadPoll(id,epoch){
  clearTimeout(THREAD.timer);if(epoch!==THREAD.epoch||!THREAD.current)return;
  try{const r=await jget('/api/runtime/sessions?id='+encodeURIComponent(id));if(epoch!==THREAD.epoch)return;if(!r.ok)throw new Error(r.error);threadReceiveContext(r.context);threadPaint(r.session,false);}
  catch(e){if(epoch!==THREAD.epoch)return;THREAD.notice='Connexion interrompue. Reconnexion automatique…';threadSyncSend();}
  if(epoch===THREAD.epoch)THREAD.timer=setTimeout(()=>threadPoll(id,epoch),THREAD.current?.status==='running'?650:3000);
}
async function threadSend(event){
  if(typeof navigator!=='undefined'&&navigator.onLine===false){toast('Appareil hors ligne : votre brouillon est conservé.');return;}
  event?.preventDefault();const s=THREAD.current;
  if(THREAD.harness){toast('Cet adapter de harness n’est pas connecté.');return;}
  if(s?.runtime_id==='llama.cpp'){toast('La discussion locale n’a pas pu être ouverte. Réessayez de l’ouvrir.');return;}
  if(!s||s.status==='running'||s.status==='unsaved'||!s.model||THREAD.context?.problem||wsEl('send').disabled)return;
  const epoch=THREAD.epoch,input=wsEl('input'),text=input.value.trim();if(!text)return;
  if(ATTACH.length){toast('Cet adapter accepte uniquement du texte. Vos pièces jointes restent dans le brouillon.');return;}
  let pending=THREAD.pending.get(s.id);
  if(!pending||pending.text!==text){pending={text,request_id:crypto.randomUUID?crypto.randomUUID():Date.now().toString(36)+'-'+Math.random().toString(36).slice(2)};THREAD.pending.set(s.id,pending);}
  wsEl('send').disabled=true;
  try{
    const r=await jpost('/api/runtime/sessions/send',{id:s.id,...pending,context_revision:THREAD.context?.revision||''});
    if(!r.ok)throw new Error(r.error);
    THREAD.pending.delete(s.id);THREAD.drafts.delete(s.id);THREAD.notice='';
    if(epoch!==THREAD.epoch)return;
    input.value='';autoGrow(input);await threadPoll(s.id,epoch);
  }catch(e){if(epoch===THREAD.epoch){THREAD.notice=e.message;threadSyncSend();}}
}
async function threadStop(){
  if(!THREAD.current)return;
  try{const r=await jpost('/api/runtime/sessions/stop',{id:THREAD.current.id});if(!r.ok)throw new Error(r.error);await threadPoll(THREAD.current.id,THREAD.epoch);}catch(e){toast(e.message);}
}
async function threadChooseLocal(model,name,presetIndex){
  if(busy||THREAD.current?.status==='running'||THREAD.current?.status==='unsaved'){toast('Arrêtez ou enregistrez la réponse avant de changer de modèle.');return;}
  if(THREAD.current){
    const workspace=await jget('/api/workspace');
    const choice=workspace.models?.find(m=>m.kind==='local'&&(samePath(m.model,model)||samePath(baseName(m.model),baseName(model))));
    if(!choice){toast('Activez ce modèle dans Modèles pour le proposer dans le sélecteur.');return;}
    if(!await threadChooseModel(choice))return;
  }else if(THREAD.harness){
    THREAD.harness=null;threadPanel();syncPickerLabel({});lastSeq=0;REPLAYING=true;handleDelta({reset:true,replay:true});syncSendBtn();
  }
  if(presetIndex)await loadPresetAt(presetIndex,name);else await loadNaked(model,name);
}
async function threadImport(id){
  try{const r=await jpost('/api/runtime/sessions/import',{id});if(!r.ok)throw new Error(r.error);await threadOpen(r.session.id);}catch(e){toast(e.message);}
}
document.addEventListener('DOMContentLoaded',()=>{
  wsEl('ws-editor').addEventListener('close',()=>{const key=wsEl('ws-editor').querySelector('[name="key"]');if(key)key.value='';});
  wsEl('nav-hub').onclick=()=>openWorkspace('models');wsEl('nav-hub').dataset.wsPage='models';
  const newButton=document.querySelector('.ws-new');if(newButton)newButton.onclick=()=>newChat('');
  document.addEventListener('click',async event=>{
    const target=event.target.closest('[data-thread-open],[data-thread-delete],[data-thread-import],[data-provider-edit],[data-provider-disconnect],[data-model-tab],[data-harness-new],[data-harness-edit]');if(!target)return;
    if(target.dataset.threadOpen){threadOpen(target.dataset.threadOpen);return;}
    if(target.dataset.threadImport){restoreHistory(target.dataset.threadImport);return;}
    if(target.dataset.providerEdit){editProvider(target.dataset.providerEdit);return;}
    if(target.dataset.modelTab){THREAD.modelsTab=target.dataset.modelTab;wsRenderModels();return;}
    if(target.dataset.harnessNew){harnessEditor(target.dataset.harnessNew);return;}
    if(target.dataset.harnessEdit){harnessEditor('',target.dataset.harnessEdit);return;}
    try{
      if(target.dataset.providerDisconnect){if(!await askConfirm('Oublier la clé en mémoire ? Les réponses en cours continuent.',{title:'Déconnecter le provider'}))return;const r=await jpost('/api/providers/disconnect',{id:target.dataset.providerDisconnect});if(!r.ok)throw new Error(r.error);openWorkspace('models');}
      if(target.dataset.threadDelete){if(!await askConfirm('Supprimer définitivement cette discussion ?',{title:'Supprimer',danger:true,okText:'Supprimer'}))return;const r=await jpost('/api/runtime/sessions/delete',{id:target.dataset.threadDelete});if(!r.ok)throw new Error(r.error);THREAD.resumeID='';await loadNav();showMainView('chat');}
    }catch(e){toast(e.message);}
  });
  document.addEventListener('change',async event=>{
    const box=event.target.closest('[data-model-visible]');if(!box)return;box.disabled=true;
    try{const r=await jpost('/api/workspace/models/visibility',{id:box.dataset.modelVisible,enabled:box.checked});if(!r.ok)throw new Error(r.error);WS.data.models.find(m=>m.id===box.dataset.modelVisible).enabled=box.checked;}
    catch(e){box.checked=!box.checked;toast(e.message);}finally{box.disabled=false;}
  });
});
