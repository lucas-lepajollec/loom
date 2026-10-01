import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const read=name=>fs.readFileSync(new URL('../src/'+name,import.meta.url),'utf8');
function fixture(){
  const nodes=new Map(),calls=[];
  const wsEl=id=>{
    if(!nodes.has(id))nodes.set(id,{hidden:false,disabled:false,value:'',dataset:{},replaceChildren(...children){this.children=children;}});
    return nodes.get(id);
  };
  const context=vm.createContext({document:{addEventListener(){},createElement(){return {};}},wsEl,wsEsc:String,usageNumber:String,fmtElapsed:s=>Math.round(s)+'s',threadModelName:s=>s.model,
    THREAD:{current:null,harness:null},WS:{data:{models:[]}},busy:false,REASON_EFFORT:'',PF_CAPS:{effort:[]},threadSyncPickerLabel(){},threadPanel(){},toast(){},pickReason:async v=>calls.push(v),jpost:async(path,body)=>{calls.push({path,body});return {ok:true,session:{...context.THREAD.current,model:body.choice_id.split(':').slice(1).join(':'),reasoning_effort:body.reasoning_effort}};}});
  vm.runInContext(read('js/29-runtime-display.js'),context);
  const model=(id,enabled=true)=>({id:'antigravity:'+id,model:id,name:id,runtime_id:'antigravity',enabled});
  return {context,calls,wsEl,model,run:source=>vm.runInContext(source,context)};
}
test('catalog groups only discovered reasoning siblings; no cross-provider or thinking aliases',()=>{
  const {context,model,run}=fixture();
  context.models=[model('gemini-3.8-flash-high'),model('gemini-3.8-flash-low'),model('claude-opus-4-6-thinking'),model('gpt-oss-120b-medium'),{id:'cloud:other',model:'gemini-3.8-flash-high',name:'Custom high',runtime_id:'openai-compatible',enabled:true}];
  const groups=run('threadModelGroups(models)');
  assert.equal(groups.length,4);assert.equal(groups[0].name,'gemini-3.8-flash');assert.equal(groups[0].variants.length,2);
  assert.equal(groups[1].name,'claude-opus-4-6-thinking');assert.equal(groups[2].name,'gpt-oss-120b-medium');
});
test('effort switches the exact native variant in the same fil without sending or rewriting messages',async()=>{
  const {context,model,run,wsEl,calls}=fixture();
  context.WS.data.models=[model('gemini-high'),model('gemini-low')];
  context.THREAD.current={id:'same-thread',runtime_id:'antigravity',model:'gemini-high',messages:[{role:'user',content:'retained'}]};
  run('threadSyncReason()');assert.equal(wsEl('composer-reason').hidden,false);assert.equal(wsEl('composer-reason-select').value,'antigravity:gemini-high');
  await run('threadPickReason("antigravity:gemini-low")');
  assert.equal(calls.length,1);assert.equal(calls[0].path,'/api/runtime/sessions/select');assert.equal(calls[0].body.id,'same-thread');
  assert.equal(context.THREAD.current.messages[0].content,'retained');
  context.THREAD.current.status='running';run('threadSyncReason()');assert.equal(wsEl('composer-reason-select').disabled,true);
  await run('threadPickReason("antigravity:gemini-high")');assert.equal(calls.length,1);
});
test('local effort uses existing hot setting; unsupported cloud never offers native controls',async()=>{
  const {context,run,wsEl,calls}=fixture();context.REASON_EFFORT='high';context.PF_CAPS.effort=['low','high'];
  run('threadSyncReason()');assert.equal(wsEl('composer-reason-select').children.length,2);
  await run('threadPickReason("low")');assert.deepEqual(calls,['low']);
  context.THREAD.current={runtime_id:'openai-compatible'};run('threadSyncReason()');assert.equal(wsEl('composer-reason').hidden,true);
});
test('past-response provenance never comes from the current model; costs and missing metrics stay honest',()=>{
  const {context,run}=fixture();context.THREAD.current={model:'new-model',runtime_id:'llama.cpp'};
  context.turn={runtime_id:'antigravity',provider_name:'Antigravity',model:'old-model',usage:{prompt_tokens:10,completion_tokens:40,thinking_tokens:30},duration_seconds:2};
  const summary=run('threadTurnSummary(turn)');assert.match(summary,/old-model/);assert.doesNotMatch(summary,/new-model/);assert.match(summary,/20.0 tok\/s moyen/);assert.match(summary,/30 de réflexion/);
  assert.match(run('threadTurnDetails(turn)'),/ne transmet pas le texte/);
  assert.match(run('threadTurnSummary({runtime_id:"llama.cpp",model:"local"})'),/Sans coût API/);
  assert.match(run('threadTurnSummary(null)'),/Provenance non enregistrée.*Tokens non communiqués/);
});
test('harness models are collapsed and confirmations use viewport width, bounded scrolling and keyboard semantics',()=>{
  const css=read('workspace-polish.css'),html=read('index.tmpl.html');
  assert.match(css,/#ask-modal\{padding:16px;align-items:center;justify-content:center\}/);assert.match(css,/max-height:calc\(100dvh - 32px\);overflow:auto/);
  assert.match(html,/class="ask-box" role="dialog" aria-modal="true"/);
  assert.match(read('js/28-usage-harness.js'),/<details class="harness-models">/);assert.match(read('js/03-ui-kit.js'),/e.key==='Tab'/);
});
test('Codex uses account-discovered efforts with one model entry, in the same conversation',async()=>{
  const {context,run,wsEl,calls}=fixture();
  context.WS.data.models=[{id:'codex:native-model',model:'native-model',name:'Native model',runtime_id:'codex',reasoning_efforts:['low','max','ultra'],default_effort:'low'}];
  context.THREAD.current={id:'same',model:'native-model',runtime_id:'codex',reasoning_effort:'low',messages:[{role:'user',content:'keep'}]};
  assert.equal(run('threadModelGroups(WS.data.models)').length,1);
  run('threadSyncReason()');assert.equal(wsEl('composer-reason-select').children.length,3);
  await run('threadPickReason("ultra")');assert.equal(calls[0].body.reasoning_effort,'ultra');assert.equal(calls[0].body.id,'same');assert.equal(context.THREAD.current.messages[0].content,'keep');
  await run('threadPickReason("invented")');assert.equal(calls.length,1);
  assert.match(run('threadTurnSummary({runtime_id:"codex",provider_name:"Codex",model:"native-model"})'),/Abonnement natif/);
  assert.match(run('threadTurnDetails({runtime_id:"codex",reasoning_summary:"Reported summary"})'),/Reported summary/);
});
test('cloud selection is catalog-first; manual IDs and destination options stay advanced',()=>{
  const js=read('js/26-providers.js');
  assert.match(js,/provider-choices/);assert.match(js,/Connecter et détecter/);assert.match(js,/Ma sélection/);
  assert.match(js,/selected.type='hidden'/);assert.doesNotMatch(js,/Un identifiant par ligne|multiline:true|cloud_preset/);
  assert.match(js,/advanced.append\(name,endpoint\)/);assert.match(js,/catalog.isConnected/);
});
