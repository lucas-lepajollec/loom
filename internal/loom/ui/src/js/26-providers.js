// Connection -> discovered catalog -> selected models. Catalog reads send only
// credentials to the explicit destination, never a conversation or generation.
const CLOUD_PRESETS=[
  {name:'OpenRouter',endpoint:'https://openrouter.ai/api/v1',usage:true,description:'Un catalogue multi-provider'},
  {name:'OpenAI',endpoint:'https://api.openai.com/v1',usage:true,description:'API payante · distincte de Codex'},
  {name:'Mistral',endpoint:'https://api.mistral.ai/v1',usage:false,description:'Modèles Mistral via API'},
  {name:'Autre provider',endpoint:'',usage:true,description:'Compatible Chat Completions'}
];
function cloudSelectedModels(text){return [...new Set(text.split('\n').map(s=>s.trim()).filter(Boolean))];}
function renderCloudModelSelection(){
  const providers=WS.data.providers||[],models=WS.data.models||[];
  if(!providers.length)return wsEmpty('cloud','Vos modèles cloud','Connectez un provider, détectez ses modèles et choisissez ceux à proposer dans vos discussions.',wsButton('Connecter un provider','new-provider',true));
  return '<p class="ws-muted">Votre sélection, par provider. Aucun identifiant à saisir : le catalogue se détecte à la connexion.</p>'+providers.map(p=>'<section class="harness-config"><div class="ws-section-head"><div><h2>'+wsEsc(p.name)+'</h2><p class="ws-note">'+(p.ready?'Connecté':'Clé à reconnecter')+' · API cloud</p></div><button type="button" class="ws-button" data-provider-edit="'+wsEsc(p.id)+'">Choisir les modèles</button></div><div class="model-table">'+models.filter(m=>m.provider_id===p.id).map(m=>'<article class="model-line"><div class="model-line-icon">'+wsIcon('cloud')+'</div><div><h3>'+wsEsc(m.name)+'</h3></div><label class="model-visibility"><input type="checkbox" data-model-visible="'+wsEsc(m.id)+'" '+(m.enabled?'checked':'')+' aria-label="Afficher '+wsEsc(m.name)+' via '+wsEsc(p.name)+'"><span>Dans le sélecteur</span></label></article>').join('')+'</div></section>').join('')+'<p class="ws-note">Masquer un modèle conserve les discussions. Les catalogues peuvent inclure des modèles non compatibles avec le chat texte.</p>';
}
function cloudProviderEditor(id){
  const p=(WS.data.providers||[]).find(p=>p.id===id)||{},fields=threadEditor(id?'Choisir les modèles':'Connecter un provider','provider',id);
  wsEl('ws-editor-kicker').textContent='Cloud · API';wsEl('ws-editor-save').textContent='Ajouter au sélecteur';
  const name=wsField('Nom de la connexion','name',p.name||'',{required:true,maxLength:100});
  const endpoint=wsField('URL de base compatible Chat Completions','endpoint',p.endpoint||'',{required:true,placeholder:'https://api.exemple.com/v1'});
  endpoint.querySelector('input').readOnly=!!id;
  const choices=document.createElement('div');choices.className='provider-choices';choices.setAttribute('aria-label','Choisir un provider');fields.appendChild(choices);
  const connection=document.createElement('section');connection.className='provider-connect-step';connection.hidden=!id;
  const title=document.createElement('h3');title.textContent=p.name||'Connexion';connection.appendChild(title);
  const destination=document.createElement('p');destination.className='ws-note provider-destination';destination.textContent=p.endpoint||'';connection.appendChild(destination);
  const key=wsField('Clé API','key','',{maxLength:4096,placeholder:p.ready?'Déjà connectée · laisser vide pour conserver':'Votre clé API'});
  const password=key.querySelector('input');password.type='password';password.autocomplete='off';password.spellcheck=false;connection.appendChild(key);
  const action=document.createElement('button');action.type='button';action.className='ws-button primary';action.textContent=id?'Détecter les modèles disponibles':'Connecter et détecter les modèles';connection.appendChild(action);
  const status=document.createElement('p');status.className='ws-note';status.setAttribute('role','status');status.textContent='Clé en mémoire côté serveur. Ce clic lit le catalogue, sans génération ni conversation envoyée.';connection.appendChild(status);fields.appendChild(connection);
  const catalog=document.createElement('section');catalog.className='provider-catalog';catalog.hidden=true;fields.appendChild(catalog);
  const selected=document.createElement('input');selected.type='hidden';selected.name='models';selected.value=(p.models||[p.model]).filter(Boolean).join('\n');fields.appendChild(selected);
  const advanced=document.createElement('details');advanced.className='provider-advanced';advanced.innerHTML='<summary>Options avancées</summary>';advanced.append(name,endpoint);
  const usage=document.createElement('label');usage.className='thread-consent';const cb=document.createElement('input');cb.type='checkbox';cb.name='cloud_usage';cb.checked=p.usage_mode!=='none';usage.append(cb,document.createTextNode('Demander le décompte des tokens (stream_options).'));advanced.appendChild(usage);
  const fallback=wsField('Modèle absent du catalogue','manual_model','',{maxLength:200,placeholder:'Identifiant exact · si nécessaire'});advanced.appendChild(fallback);
  const add=document.createElement('button');add.type='button';add.className='ws-button';add.textContent='Ajouter cet identifiant';advanced.appendChild(add);fields.appendChild(advanced);
  let generation=0,catalogModels=(p.models||[p.model]).filter(Boolean);
  const sync=()=>{wsEl('ws-editor-save').disabled=cloudSelectedModels(selected.value).length===0;};
  function paint(){catalog.hidden=false;cloudRenderCatalog(catalog,catalogModels,selected,sync);sync();}
  function invalidate(){generation++;status.textContent='Connexion modifiée : détectez à nouveau les modèles disponibles.';}
  function presetPick(preset,button){
    for(const b of choices.querySelectorAll('button'))b.setAttribute('aria-pressed',String(b===button));
    name.querySelector('input').value=preset.endpoint?preset.name:'';endpoint.querySelector('input').value=preset.endpoint;cb.checked=preset.usage;
    title.textContent=preset.name;destination.textContent=preset.endpoint||'Renseignez la destination dans les options avancées.';connection.hidden=false;advanced.open=!preset.endpoint;
    selected.value='';catalogModels=[];catalog.hidden=true;password.value='';invalidate();sync();password.focus();
  }
  if(!id)CLOUD_PRESETS.forEach(preset=>{const button=document.createElement('button');button.type='button';button.className='provider-choice';button.setAttribute('aria-pressed','false');const label=document.createElement('strong');label.textContent=preset.name;const description=document.createElement('span');description.textContent=preset.description;button.append(label,description);button.addEventListener('click',()=>presetPick(preset,button));choices.appendChild(button);});else choices.hidden=true;
  endpoint.querySelector('input').addEventListener('input',()=>{destination.textContent=endpoint.querySelector('input').value;invalidate();});password.addEventListener('input',invalidate);
  add.addEventListener('click',()=>{const input=fallback.querySelector('input'),model=input.value.trim(),set=new Set(cloudSelectedModels(selected.value));if(!model||/[\r\n\x00]/.test(model))return;if(set.size>=32&&!set.has(model)){status.textContent='32 modèles maximum.';return;}set.add(model);selected.value=[...set].join('\n');catalogModels=[...new Set([...catalogModels,model])];input.value='';paint();});
  action.addEventListener('click',async()=>{
    const url=endpoint.querySelector('input').value.trim();
    if(!url||(!password.value&&!p.ready)){status.textContent='Renseignez votre clé'+(!url?' et la destination':'')+'.';password.focus();return;}
    const token=++generation;action.disabled=true;status.textContent='Détection des modèles…';
    try{
      const r=await jpost('/api/providers/models',{id:id||'',endpoint:url,key:password.value,consent:true});
      if(token!==generation||!catalog.isConnected||!wsEl('ws-editor').open)return;if(!r.ok)throw new Error(r.error||'Catalogue indisponible');
      catalogModels=[...new Set([...r.models,...cloudSelectedModels(selected.value)])];paint();
      status.textContent=r.models.length+' modèles détectés. Choisissez ceux à proposer dans vos discussions. Le catalogue ne garantit pas leur compatibilité chat ni votre accès.';
      wsEl('ws-editor-save').textContent=id?'Enregistrer la sélection':'Ajouter au sélecteur';catalog.querySelector('input[type="search"]').focus();
    }catch(e){if(token===generation)status.textContent=e.message;}finally{action.disabled=false;}
  });
  if(catalogModels.length){paint();status.textContent='Votre sélection enregistrée. Détectez les modèles pour lire le catalogue actuel.';}
  sync();wsEl('ws-editor').showModal();fields.querySelector('button,input')?.focus();
}
function cloudRenderCatalog(host,models,selected,onChange=()=>{}){
  host.replaceChildren();const head=document.createElement('h3');head.textContent='Modèles dans vos discussions';host.appendChild(head);
  const filter=document.createElement('input');filter.type='search';filter.placeholder='Rechercher un modèle…';filter.setAttribute('aria-label','Rechercher dans les modèles détectés');host.appendChild(filter);
  const tabs=document.createElement('div');tabs.className='provider-catalog-tabs';const all=document.createElement('button'),chosenTab=document.createElement('button');all.type=chosenTab.type='button';all.textContent='Tous les modèles';chosenTab.textContent='Ma sélection';tabs.append(all,chosenTab);host.appendChild(tabs);
  const list=document.createElement('div');list.className='provider-model-list';host.appendChild(list);const count=document.createElement('p');count.className='ws-note';count.setAttribute('role','status');host.appendChild(count);let onlySelected=false;
  function paint(){
    const chosen=new Set(cloudSelectedModels(selected.value)),matches=models.filter(m=>(!onlySelected||chosen.has(m))&&m.toLowerCase().includes(filter.value.toLowerCase())),shown=matches.slice(0,100);
    list.replaceChildren();count.textContent=chosen.size+'/32 sélectionnés · '+matches.length+' résultat(s)'+(matches.length>100?' · 100 affichés, affinez la recherche':'');all.setAttribute('aria-pressed',String(!onlySelected));chosenTab.setAttribute('aria-pressed',String(onlySelected));
    if(!shown.length){const empty=document.createElement('p');empty.className='ws-note';empty.textContent=onlySelected?'Aucun modèle sélectionné.':'Aucun modèle trouvé.';list.appendChild(empty);}
    shown.forEach(model=>{const label=document.createElement('label'),input=document.createElement('input'),text=document.createElement('span');input.type='checkbox';input.checked=chosen.has(model);input.dataset.cloudModel=model;input.disabled=!input.checked&&chosen.size>=32;text.textContent=model;
      input.addEventListener('change',()=>{const next=new Set(cloudSelectedModels(selected.value));input.checked?next.add(model):next.delete(model);selected.value=[...next].join('\n');paint();onChange();[...list.querySelectorAll('input')].find(el=>el.dataset.cloudModel===model)?.focus();});label.append(input,text);list.appendChild(label);});
  }
  all.addEventListener('click',()=>{onlySelected=false;paint();});chosenTab.addEventListener('click',()=>{onlySelected=true;paint();});filter.addEventListener('input',paint);paint();
}
document.addEventListener('DOMContentLoaded',()=>wsEl('ws-editor').addEventListener('close',()=>{
  wsEl('ws-editor-fields').querySelectorAll('input[type="password"]').forEach(input=>{input.value='';});wsEl('ws-editor-save').disabled=false;
}));
