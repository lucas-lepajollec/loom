// A family is only a UI projection. Requests always use a discovered exact ID.
function threadModelGroups(models){
  const counts=new Map();
  for(const m of models){
    const match=m.runtime_id==='antigravity'&&m.model.match(/^(.*)-(low|medium|high)$/);
    if(match)counts.set(match[1],(counts.get(match[1])||0)+1);
  }
  const groups=new Map();
  for(const m of models){
    const match=m.runtime_id==='antigravity'&&m.model.match(/^(.*)-(low|medium|high)$/);
    const grouped=match&&counts.get(match[1])>1;
    const key=grouped?m.runtime_id+':'+match[1]:m.id;
    if(!groups.has(key))groups.set(key,{id:key,name:grouped?match[1]:m.name,variants:[]});
    groups.get(key).variants.push({...m,effort:grouped?match[2]:''});
  }
  return [...groups.values()].map(g=>({...g,variants:g.variants.sort((a,b)=>(['low','medium','high'].indexOf(a.effort))-(['low','medium','high'].indexOf(b.effort)))}));
}
function threadGroupChoice(group){
  return group.variants.find(m=>m.enabled&&m.model===THREAD.current?.model&&m.runtime_id===THREAD.current?.runtime_id)
    ||group.variants.find(m=>m.enabled&&m.effort==='high')
    ||group.variants.find(m=>m.enabled)||group.variants[0];
}
function threadSyncReason(){
  const row=wsEl('composer-reason'),select=wsEl('composer-reason-select');if(!row||!select)return;
  const s=THREAD.current,local=!THREAD.harness&&(!s||s.runtime_id==='llama.cpp');
  let options=[],current='';
  if(s?.runtime_id==='antigravity'&&!THREAD.harness){
    const group=threadModelGroups(WS.data?.models||[]).find(g=>g.variants.some(m=>m.model===s.model&&m.runtime_id===s.runtime_id));
    if(group?.variants.length>1){options=group.variants.map(m=>({value:m.id,label:{low:'Faible',medium:'Moyenne',high:'Élevée'}[m.effort]}));current=group.variants.find(m=>m.model===s.model).id;}
  }else if(s?.runtime_id==='codex'&&!THREAD.harness){
    const model=WS.data?.models.find(m=>m.runtime_id==='codex'&&m.model===s.model);
    options=(model?.reasoning_efforts||[]).map(value=>({value,label:{none:'Sans',minimal:'Minimale',low:'Faible',medium:'Moyenne',high:'Élevée',xhigh:'Maximale'}[value]||value}));current=s.reasoning_effort||model?.default_effort;
  }else if(local&&typeof REASON_EFFORT!=='undefined'&&REASON_EFFORT){
    const levels=typeof PF_CAPS!=='undefined'&&PF_CAPS.effort?.length?PF_CAPS.effort:[REASON_EFFORT];
    options=[...new Set([...levels,REASON_EFFORT])].map(value=>({value,label:{low:'Faible',medium:'Moyenne',high:'Élevée',xhigh:'Maximale'}[value]||value}));current=REASON_EFFORT;
  }
  row.hidden=options.length<1;
  const key=JSON.stringify(options);
  if(select.dataset.options!==key){select.replaceChildren(...options.map(o=>{const option=document.createElement('option');option.value=o.value;option.textContent=o.label;return option;}));select.dataset.options=key;}
  select.value=current;select.disabled=!!busy||s?.status==='running'||s?.status==='unsaved'||options.length<2;
}
async function threadPickReason(value){
  if(busy||THREAD.current?.status==='running'||THREAD.current?.status==='unsaved'){threadSyncReason();return;}
  try{
    if(THREAD.current?.runtime_id==='antigravity'){
      const choice=WS.data.models.find(m=>m.id===value&&m.runtime_id==='antigravity');
      const group=threadModelGroups(WS.data.models).find(g=>g.variants.some(m=>m.model===THREAD.current.model));
      if(!choice||!group?.variants.some(m=>m.id===choice.id))throw new Error('Niveau indisponible.');
      const r=await jpost('/api/runtime/sessions/select',{id:THREAD.current.id,choice_id:choice.id,consent:true});
      if(!r.ok)throw new Error(r.error);
      THREAD.current=r.session;if(r.context)threadReceiveContext(r.context);threadSyncPickerLabel();threadPanel();
      toast('Niveau de réflexion modifié pour le prochain envoi.');
    }else if(THREAD.current?.runtime_id==='codex'){
      const choice=WS.data.models.find(m=>m.runtime_id==='codex'&&m.model===THREAD.current.model);
      if(!choice?.reasoning_efforts?.includes(value))throw new Error('Niveau indisponible.');
      const r=await jpost('/api/runtime/sessions/select',{id:THREAD.current.id,choice_id:choice.id,reasoning_effort:value,consent:true});
      if(!r.ok)throw new Error(r.error);THREAD.current=r.session;if(r.context)threadReceiveContext(r.context);threadPanel();
      toast('Niveau de réflexion modifié pour le prochain envoi.');
    }else await pickReason(value);
  }catch(e){toast(e.message);}finally{threadSyncReason();}
}
function threadProvenance(turn){
  if(!turn?.runtime_id)return 'Provenance non enregistrée';
  return (turn.runtime_id==='llama.cpp'?'llama.cpp · local':turn.provider_name||turn.runtime_id)+(turn.model?' · '+threadModelName(turn):'');
}
function threadTurnSummary(turn,running=false){
  const parts=[threadProvenance(turn)],u=turn?.usage,st=turn?.stats;
  if(running)parts.push('En cours'+(turn?.started_at?' · '+fmtElapsed((Date.now()-turn.started_at)/1000):''));
  if(u){
    parts.push(usageNumber(u.completion_tokens)+' tokens en sortie');
    if(u.thinking_tokens>0)parts.push('dont '+usageNumber(u.thinking_tokens)+' de réflexion');
  }else if(st?.gen_tokens){parts.push(usageNumber(st.gen_tokens)+' tokens'+(st.gen_per_second>0?' · '+Number(st.gen_per_second).toFixed(1)+' tok/s decode':''));}
  else parts.push(running?'Tokens pas encore communiqués':'Tokens non communiqués');
  if(turn?.duration_seconds>0){
    parts.push(fmtElapsed(turn.duration_seconds));
    if(u)parts.push((u.completion_tokens/turn.duration_seconds).toFixed(1)+' tok/s moyen');
  }
  if(turn?.runtime_id==='llama.cpp')parts.push('Sans coût API');
  else if(['antigravity','codex'].includes(turn?.runtime_id))parts.push('Abonnement natif');
  return parts.join(' · ');
}
function threadTurnDetails(turn){
  const events=turn?.events||[],u=turn?.usage;
  let result='';
  if(turn?.reasoning_summary)result+='<details><summary>Résumé de réflexion · Codex</summary><p class="native-reasoning-summary">'+wsEsc(turn.reasoning_summary)+'</p><p>Résumé communiqué par le harness, conservé pour l’affichage seulement.</p></details>';
  if(u||turn?.runtime_id==='antigravity')result+='<details><summary>Détails de l’usage</summary>'+(u?'<p>'+usageNumber(u.prompt_tokens)+' tokens en entrée'+(u.cache_read_tokens?' · '+usageNumber(u.cache_read_tokens)+' lus en cache':'')+'</p>':'')+(turn?.runtime_id==='antigravity'?'<p>Le CLI ne transmet pas le texte de réflexion. Le débit moyen couvre la durée totale du tour, pas le décodage seul.</p>':'')+'</details>';
  if(events.length)result+='<details><summary>'+events.length+' outil'+(events.length>1?'s':'')+' natif'+(events.length>1?'s':'')+'</summary>'+events.map(e=>'<p>'+wsEsc(e.name||'Outil')+' · '+(e.failed?'Échec / refus':e.state==='ACTIVE'?'En cours':e.state==='DONE'?'Terminé (CLI)':'État non communiqué')+(e.file_target?'<br>Cible d’écriture : <code>'+wsEsc(e.file_target)+'</code>':'')+'</p>').join('')+'<p>Événements natifs, sans rejeu, auto-approbation ou export des arguments/résultats. Une cible ne constitue pas un diff vérifié.</p></details>';
  return result;
}
function threadPaintMetadata(item,turn,running){
  let footer=item.querySelector(':scope > .turn-metadata');
  if(!footer){footer=document.createElement('div');footer.className='turn-metadata';item.appendChild(footer);}
  const content='<div>'+wsEsc(threadTurnSummary(turn,running))+'</div>'+threadTurnDetails(turn);
  if(footer._content!==content){const opened=[...footer.querySelectorAll('details')].filter(d=>d.open).map(d=>d.querySelector('summary')?.textContent);footer.innerHTML=content;footer.querySelectorAll('details').forEach(d=>d.open=opened.includes(d.querySelector('summary')?.textContent));footer._content=content;}
}
document.addEventListener('change',async e=>{
  const input=e.target.closest('[data-model-group]');if(!input)return;
  input.disabled=true;
  try{
    for(const id of input.dataset.modelGroup.split('|')){
      const r=await jpost('/api/workspace/models/visibility',{id,enabled:input.checked});
      if(!r.ok)throw new Error(r.error);
      const model=WS.data.models.find(m=>m.id===id);if(model)model.enabled=input.checked;
    }
  }catch(error){toast(error.message);await openWorkspace('agents');}finally{input.disabled=false;}
});
