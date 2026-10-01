import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const src = new URL('../src/', import.meta.url);
const read = name => fs.readFileSync(new URL(name, src), 'utf8');
function setup() {
  const nodes = new Map();
  const calls = [];
  const wsEl = id => {
    if (!nodes.has(id)) nodes.set(id, {value:'',hidden:false,disabled:false,innerHTML:'',textContent:'',classList:{toggle(){}},setAttribute(){},replaceChildren(){}});
    return nodes.get(id);
  };
  const context = vm.createContext({
    document:{addEventListener(){},documentElement:{dataset:{},removeAttribute(){}}},
    clearTimeout(){},setTimeout(){return 1;},console,crypto:{randomUUID:()=> 'test-request-id'},
    wsEl,wsEsc:s=>String(s||''),wsButton:()=>'',WS:{session:'',data:{models:[],runtimes:[]}},
    busy:false,ATTACH:[],renderSessionChrome:()=>calls.push('native-panel'),
    threadContextPanel:()=>calls.push('context'),sendHintText:()=> 'Entrée pour envoyer',
    toast:message=>calls.push(message),autoGrow(){},jpost:async(path,body)=>{calls.push({path,body});return {ok:true};},
  });
  vm.runInContext(read('js/24-conversations.js'),context);
  return {context,nodes,calls,run:code=>vm.runInContext(code,context),wsEl};
}

test('one original sidebar has local, cloud and preparatory harness states',()=>{
  const {run,wsEl,calls}=setup();
  assert.equal(run('threadPanelMode()'),'local');
  run('threadPanel()');
  assert.equal(wsEl('params-runtime').hidden,true);
  assert.ok(calls.includes('native-panel'));
  run('THREAD.current={runtime_id:"openai-compatible",provider_name:"Fixture",model:"model-a",endpoint:"http://127.0.0.1/v1"};threadPanel()');
  assert.equal(run('threadPanelMode()'),'cloud');
  assert.equal(wsEl('params-body').hidden,true);
  assert.equal(wsEl('params-runtime').hidden,false);
  assert.equal(wsEl('params-sub').textContent,'Cloud · Fixture');
  run('THREAD.harness={name:"Code",runtime_id:"codex",model_ids:[]};threadPanel();threadSyncSend()');
  assert.equal(run('threadPanelMode()'),'harness');
  assert.equal(wsEl('send').disabled,true);
  assert.match(wsEl('params-sub').textContent,/non connecté/);
});

test('the native stream yields only for cloud and harness views',()=>{
  const {run}=setup();
  assert.equal(run('threadOwnsChat()'),false);
  run('THREAD.current={runtime_id:"llama.cpp"};THREAD.native=true');
  assert.equal(run('threadOwnsChat()'),false);
  run('THREAD.native=false');
  assert.equal(run('threadOwnsChat()'),true);
  run('THREAD.native=true;THREAD.harness={name:"Preview"}');
  assert.equal(run('threadOwnsChat()'),true);
});

test('unsupported files and harnesses never send to the hidden native chat',async()=>{
  const {run,wsEl,calls,context}=setup();
  run('THREAD.current={id:"fixture",runtime_id:"openai-compatible",model:"model-a",status:"idle"};THREAD.context={revision:"revision"};ATTACH.push({name:"keep.txt"})');
  wsEl('input').value='Keep this draft';
  await run('threadSend()');
  assert.equal(wsEl('input').value,'Keep this draft');
  assert.equal(context.ATTACH.length,1);
  assert.equal(calls.filter(c=>c.path).length,0);
  run('ATTACH.length=0;THREAD.harness={name:"Preview"}');
  await run('threadSend()');
  assert.equal(calls.filter(c=>c.path).length,0);
  assert.equal(wsEl('input').value,'Keep this draft');
});

test('cloud uses the original input and idempotent runtime endpoint',async()=>{
  const {run,wsEl,calls}=setup();
  run('THREAD.current={id:"fixture",runtime_id:"openai-compatible",model:"model-a",status:"idle"};THREAD.context={revision:"revision"};threadPoll=async()=>{}');
  wsEl('input').value='Original composer';
  await run('threadSend()');
  const request=calls.find(c=>c.path);
  assert.equal(request.path,'/api/runtime/sessions/send');
  assert.equal(request.body.text,'Original composer');
  assert.equal(request.body.context_revision,'revision');
  assert.equal(request.body.request_id,'test-request-id');
  assert.equal(wsEl('input').value,'');
});

test('native chat, presets and all parameter tiers remain the primary DOM',()=>{
  const html=read('index.tmpl.html');
  for(const id of ['chat-view','input','plus-menu-btn','tool-menu-btn','ctxmeta','params','params-body','params-form','params-preset-btn','params-remember-btn','params-reset-btn']) {
    assert.equal((html.match(new RegExp('id="'+id+'"','g'))||[]).length,1,id);
  }
  assert.doesNotMatch(html,/ws-topbar|ws-breadcrumb|thread-composer|thread-messages/);
  assert.doesNotMatch(read('conversation.css'),/thread-shell|thread-local-controls|data-thread-local/);
  assert.match(read('js/18-nav.js'),/async function newChat\(projectId\)\{\s+THREAD.resumeID='';\s+closeSettings\(\)/);
  assert.doesNotMatch(read('js/08-chat-render.js'),/return threadImport\(id\)/);
});

test('cloud readiness and offline sends preserve the original draft',async()=>{
  const {run,wsEl,calls,context}=setup();
  run('THREAD.current={id:"fixture",runtime_id:"openai-compatible",provider_id:"p",model:"fixture",status:"idle"};WS.data.providers=[{id:"p",ready:false}];threadSyncSend()');
  assert.equal(wsEl('send').disabled,true);assert.match(wsEl('sendhint').textContent,/reconnectez/);
  run('WS.data.providers[0].ready=true;threadSyncSend()');assert.equal(wsEl('send').disabled,false);
  context.navigator={onLine:false};wsEl('input').value='Offline draft';await run('threadSend()');
  assert.equal(wsEl('input').value,'Offline draft');assert.equal(calls.filter(c=>c.path).length,0);
});

test('Antigravity shares the original composer, uses its native account and adapted panel',async()=>{
  const {run,wsEl,calls}=setup();
  run('THREAD.current={id:"agy",runtime_id:"antigravity",provider_name:"Antigravity",model:"native-model",messages:[],turns:[],status:"idle"};THREAD.context={revision:"revision"};threadHarnessEvents=()=>{};threadPanel();threadSyncSend();threadPoll=async()=>{}');
  assert.equal(run('threadPanelMode()'),'harness');
  assert.equal(wsEl('send').disabled,false);
  assert.match(wsEl('params-sub').textContent,/Antigravity/);
  wsEl('input').value='Same thread';await run('threadSend()');
  assert.equal(calls.find(c=>c.path).path,'/api/runtime/sessions/send');
  assert.equal(calls.find(c=>c.path).body.id,'agy');
});

test('quota UI keeps unknown separate from zero and has no automatic polling or reset action',()=>{
  const js=read('js/28-usage-harness.js'),html=read('index.tmpl.html');
  assert.equal((html.match(/id="usage-btn"/g)||[]).length,1);
  assert.match(js,/Non communiqué/);assert.match(js,/reported_turns/);assert.match(js,/Tarif non renseigné/);
  assert.doesNotMatch(js,/setInterval|rateLimitResetCredit\/consume|localStorage|dangerously-skip-permissions/);
  assert.match(js,/data-usage-close/);assert.match(js,/\.showModal\(\)/);
});

test('quota and cost rendering distinguish unknown, real zero and incomplete estimates',()=>{
  const context=vm.createContext({document:{addEventListener(){}},wsEsc:s=>String(s||'')});
  vm.runInContext(read('js/28-usage-harness.js'),context);
  context.box={innerHTML:''};context.data={scope:'Loom only',quotas:[{runtime_id:'codex',fetched_at:1,reset_credits:0,credits:null,windows:[{group:'Test',name:'Unknown',remaining_percent:null,reset_at:null}]}],models:[{choice_id:'cloud:test',name:'m',provider:'Fixture',reported_turns:1,turns:2,usage:{prompt_tokens:4,completion_tokens:2},price:{currency:'USD',input_per_million:0,output_per_million:0},estimated_cost:0}]};
  vm.runInContext('renderUsage(data,box)',context);
  assert.match(context.box.innerHTML,/Non communiqué/);assert.match(context.box.innerHTML,/Resets disponibles<br><strong>0/);
  assert.match(context.box.innerHTML,/1 \/ 2 tours/);assert.match(context.box.innerHTML,/facture provider/);
  assert.doesNotMatch(context.box.innerHTML,/<meter/);assert.match(context.box.innerHTML,/data-usage-price="cloud:test"/);
});

test('provider model choices are explicit and deduplicated, not a blanket import',()=>{
  const context=vm.createContext({document:{addEventListener(){}}});vm.runInContext(read('js/26-providers.js'),context);
  assert.deepEqual(Array.from(vm.runInContext('cloudSelectedModels(" a \\na\\nb\\n")',context)),['a','b']);
  assert.match(read('js/26-providers.js'),/\/api\/providers\/models/);
  assert.doesNotMatch(read('js/26-providers.js'),/localStorage|sessionStorage|jpost\(['"]\/chat\/completions/);
});
