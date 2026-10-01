import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const source=fs.readFileSync(new URL('../sw.js',import.meta.url),'utf8');
function worker(offline=false){
  const handlers={},calls=[];
  vm.runInNewContext(source,{
    URL,Promise,self:{location:{origin:'https://loom.test'},addEventListener:(name,fn)=>handlers[name]=fn,skipWaiting(){},clients:{claim(){}}},
    caches:{open:async()=>({addAll:async assets=>calls.push([...assets])}),keys:async()=>['other-app','loom-public-offline-old'],delete:async name=>calls.push(name),match:async request=>{calls.push(request);return 'public-offline';}},
    fetch:async request=>{calls.push({network:request.url});if(offline)throw new Error('offline');return 'network-response';}
  });return {handlers,calls};
}
test('worker caches only public fallback assets and cleans only its own namespace',async()=>{
  const {handlers,calls}=worker();let done;
  handlers.install({waitUntil:p=>done=p});await done;
  assert.deepEqual(calls[0],['/offline.html','/icons/loom-192.png','/icons/loom-512.png']);
  handlers.activate({waitUntil:p=>done=p});await done;
  assert.ok(calls.includes('loom-public-offline-old'));assert.ok(!calls.includes('other-app'));
});
test('API, streams, credentials, query strings and foreign origins are not intercepted',()=>{
  const {handlers}=worker();
  for(const [method,url,mode] of [
    ['POST','https://loom.test/api/chat','cors'],['GET','https://loom.test/api/runtime/sessions','cors'],
    ['GET','https://loom.test/?key=test','navigate'],['GET','https://loom.test/marked.min.js','cors'],
    ['GET','https://provider.test/','navigate'],['POST','https://loom.test/','navigate']
  ]){let intercepted=false;handlers.fetch({request:{method,url,mode},respondWith(){intercepted=true;}});assert.equal(intercepted,false,url);}
});
test('navigation stays network-first; offline does not replay messages or cache the app',async()=>{
  for(const offline of [false,true]){
    const {handlers,calls}=worker(offline);let response;
    handlers.fetch({request:{method:'GET',url:'https://loom.test/',mode:'navigate'},respondWith:p=>response=p});
    assert.equal(await response,offline?'public-offline':'network-response');
    assert.deepEqual(calls.filter(c=>c.network).map(c=>c.network),['https://loom.test/']);
  }
});
