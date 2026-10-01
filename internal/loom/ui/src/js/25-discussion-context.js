// A view of the next turn, not an independent memory engine. Rendering uses
// textContent for all user/provider content; previewing never invokes a model.
function threadReceiveContext(context){
  if(!context)return;
  const previous=THREAD.context;
  THREAD.context=context;THREAD.sharedContext=context.system||'';
  if(!previous||previous.system!==context.system||previous.project_name!==context.project_name||previous.problem!==context.problem||previous.warning!==context.warning)threadPanel();
}
function threadContextPanel(box,s){
  const c=THREAD.context||{},skills=c.skills||[];
  box.innerHTML='<div class="thread-context-head"><h3>Contexte du fil</h3><button type="button" class="ws-button" data-thread-configure '+(s.status==='running'||s.status==='unsaved'?'disabled':'')+'>Modifier</button></div>'+
    '<div class="context-fact"><strong>Projet</strong><span>'+wsEsc(c.project_name||(s.project_id?'Projet indisponible':'Espace personnel'))+'</span></div>'+
    '<div class="context-fact"><strong>Historique partagé</strong><span id="thread-context-count">'+s.messages.filter(m=>m.content).length+' messages textuels</span></div>'+
    (c.problem?'<p class="ws-error thread-context-alert">'+wsEsc(c.problem)+'</p>':'')+
    (c.warning?'<p class="workspace-notice">'+wsEsc(c.warning)+'</p>':'')+
    '<details'+(c.project_instructions?' open':'')+'><summary>Instructions du projet</summary><pre>'+wsEsc(c.project_instructions||'Aucune instruction de projet.')+'</pre></details>'+
    '<details'+(c.discussion_instructions?' open':'')+'><summary>Consigne de cette discussion</summary><pre>'+wsEsc(c.discussion_instructions||'Aucune consigne supplémentaire.')+'</pre></details>'+
    '<details><summary>Skills du projet · '+skills.length+'</summary>'+skills.map(skill=>'<h3>'+wsEsc(skill.name)+'</h3><pre>'+wsEsc(skill.instructions)+'</pre>').join('')+(!skills.length?'<p class="ws-note">Aucun skill sélectionné.</p>':'')+'</details>'+
    '<button type="button" class="ws-button thread-preview-button" data-thread-preview>Voir le texte préparé</button>'+
    '<p class="ws-note">Historique, instructions, skills et brouillon éventuel. Vérification locale, sans appel au modèle.</p>'+
    '<h3>Ce qui reste au runtime</h3><p class="ws-note">Cache, raisonnement interne, mémoire privée, autorisations et état des outils ne sont pas transférés. Aucun dossier n’est lu automatiquement.</p>'+
    (s.source_archive?'<p class="workspace-notice">Reprise textuelle d’une archive. Ses pièces jointes et événements d’outils restent dans l’archive originale.</p>':'');
}
async function threadConfigure(){
  const id=THREAD.current?.id,epoch=THREAD.epoch;if(!id)return;
  try{
    const [r,workspace]=await Promise.all([jget('/api/runtime/sessions?id='+encodeURIComponent(id)),jget('/api/workspace')]);
    if(epoch!==THREAD.epoch)return;
    if(!r.ok||!workspace.ok)throw new Error(r.error||workspace.error);
    if(r.session.status==='running'||r.session.status==='unsaved'||(THREAD.native&&busy))throw new Error('Arrêtez ou enregistrez la réponse avant de modifier le contexte.');
    threadReceiveContext(r.context);threadPaint(r.session,false);
    const s=r.session,fields=threadEditor('Configurer la discussion','discussion',id);
    WS.editor.revision=r.context.revision;WS.editor.epoch=epoch;
    fields.appendChild(wsField('Titre','title',s.title,{required:true,maxLength:100}));
    const project=document.createElement('label');project.className='ws-field';
    const label=document.createElement('span');label.textContent='Projet et contexte commun';
    const select=document.createElement('select');select.name='project_id';
    const none=document.createElement('option');none.value='';none.textContent='Espace personnel · aucun projet';select.appendChild(none);
    workspace.projects.forEach(p=>{const option=document.createElement('option');option.value=p.id;option.textContent=p.name;select.appendChild(option);});
    if(s.project_id&&!workspace.projects.some(p=>p.id===s.project_id)){const option=document.createElement('option');option.value=s.project_id;option.textContent='Projet indisponible · choisissez une autre option';option.disabled=true;select.appendChild(option);}
    select.value=s.project_id||'';
    const note=document.createElement('small');note.textContent='Changer de projet remplace ses instructions et skills au prochain envoi. Les messages déjà échangés restent dans le fil.';
    project.append(label,select,note);fields.appendChild(project);
    fields.appendChild(wsField('Consigne de cette discussion','instructions',s.instructions,{multiline:true,rows:5,maxLength:12000,placeholder:'Ex. Réponds en français, en privilégiant les exemples courts.',note:'Facultative. Ajoutée après les instructions et skills du projet. Elle suit ce fil quel que soit le modèle.'}));
    if(s.runtime_id!=='llama.cpp'){
      const consent=document.createElement('label');consent.className='thread-consent';
      const check=document.createElement('input');check.type='checkbox';check.name='consent';check.required=true;
      const text=document.createElement('span');text.textContent='J’autorise le partage de ce contexte et du fil avec '+s.provider_name+' ('+s.endpoint+') lors des prochains envois. Aucun message n’est envoyé à l’enregistrement.';
      consent.append(check,text);fields.appendChild(consent);
    }
    wsEl('ws-editor').showModal();fields.querySelector('input').focus();
  }catch(e){toast(e.message);}
}
async function saveDiscussionEditor(event){
  const editor={...WS.editor},form=new FormData(event.target),button=wsEl('ws-editor-save');
  button.disabled=true;wsEl('ws-editor-error').hidden=true;
  try{
    const r=await jpost('/api/runtime/sessions/configure',{id:editor.id,title:form.get('title'),project_id:form.get('project_id'),instructions:form.get('instructions'),context_revision:editor.revision,consent:form.has('consent')});
    if(!r.ok)throw new Error(r.error);
    closeWorkspaceEditor();
    if(editor.epoch!==THREAD.epoch)return;
    threadReceiveContext(r.context);threadPaint(r.session,false);threadPanel();await loadNav();
    if(THREAD.native){const local=await jpost('/api/runtime/sessions/local',{id:editor.id});if(!local.ok)throw new Error(local.error);}
    if(editor.epoch===THREAD.epoch)wsEl('ws-actions').querySelector('[data-thread-configure]')?.focus();
    toast('Discussion enregistrée. Le nouveau contexte s’applique au prochain message.');
  }catch(e){wsEl('ws-editor-error').textContent=e.message;wsEl('ws-editor-error').hidden=false;}
  finally{button.disabled=false;}
}
async function threadPreview(){
  const s=THREAD.current,epoch=THREAD.epoch;if(!s)return;
  const text=wsEl('input')?.value||'',dialog=wsEl('thread-preview');
  try{
    const r=text.trim()?await jpost('/api/runtime/sessions/preview',{id:s.id,text}):await jget('/api/runtime/sessions/preview?id='+encodeURIComponent(s.id));
    if(epoch!==THREAD.epoch)return;
    if(!r.ok)throw new Error(r.error);
    const p=r.preview,box=wsEl('thread-preview-content');box.replaceChildren();
    const description=document.createElement('p');description.className='ws-note';description.textContent=(r.runtime_id==='llama.cpp'?'Local · llama.cpp':r.provider_name+' · '+r.endpoint)+' · '+threadModelName(r);box.appendChild(description);
    const summary=document.createElement('p');summary.className='thread-preview-summary';summary.textContent=p.messages.length+' messages · '+p.text_bytes.toLocaleString('fr-FR')+' octets de texte / '+p.max_bytes.toLocaleString('fr-FR')+' · '+(p.draft_added?'brouillon inclus':'sans nouveau message');box.appendChild(summary);
    const note=document.createElement('p');note.className='ws-note';note.textContent=r.runtime_id==='llama.cpp'?'Projection textuelle portable, pas la requête locale complète : le prompt du modèle, les outils et la compaction restent gérés par le chat natif. Ce n’est pas un comptage de tokens.':'Requête textuelle initiale préparée à cet instant. Ce n’est pas un comptage de tokens ni la fenêtre maximale du modèle. Le gabarit et les traitements internes restent propres au runtime.';box.appendChild(note);
    if(r.running||p.problem||p.context.warning){const warning=document.createElement('p');warning.className='workspace-notice';warning.textContent=p.problem||p.context.warning||'Une réponse est en cours : cet aperçu évoluera à sa fin.';box.appendChild(warning);}
    p.messages.forEach((message,i)=>{
      const item=document.createElement('details');item.className='thread-preview-message';item.open=message.role==='system'||(p.draft_added&&i===p.messages.length-1);
      const title=document.createElement('summary');title.textContent=(i+1)+'. '+({system:'Instructions partagées',user:'Vous',assistant:'Assistant'}[message.role]||message.role)+(p.draft_added&&i===p.messages.length-1?' · brouillon':'');
      const content=document.createElement('pre');content.textContent=message.content;item.append(title,content);box.appendChild(item);
    });
    if(!p.messages.length){const empty=document.createElement('p');empty.textContent='Aucun texte pour le moment.';box.appendChild(empty);}
    dialog.showModal();
  }catch(e){toast(e.message);}
}
document.addEventListener('DOMContentLoaded',()=>{
  document.addEventListener('click',event=>{if(event.target.closest('[data-thread-configure]'))threadConfigure();if(event.target.closest('[data-thread-preview]'))threadPreview();});
  wsEl('thread-preview-close').onclick=()=>wsEl('thread-preview').close();
  wsEl('thread-preview').addEventListener('close',()=>wsEl('thread-preview-content').replaceChildren());
});
