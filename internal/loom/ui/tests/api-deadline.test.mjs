import { validData, shapePatch } from '../next/js/core/shape.js';
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const source = fs.readFileSync(new URL('../next/js/core/api.js', import.meta.url), 'utf8');
const flush = () => new Promise(resolve => setImmediate(resolve));
function slowBody() {
  let timer, reads = 0;
  const env = { validData, AbortController, t: x => x, localStorage: { getItem:()=>null, removeItem(){} },
    setTimeout: cb => {timer=cb;return 1;}, clearTimeout(){},
    fetch: async (_url, opts) => {reads++;return { status:200, json:()=> new Promise((_resolve, reject) => {
      opts.signal.addEventListener('abort',()=>reject(Object.assign(new Error('aborted'),{name:'AbortError'})),{once:true});
    }) }; },
  };
  vm.runInNewContext(source.replace(/^import .*;\n/gm,'').replace(/^export /gm,'')+'\nglobalThis.read=get;',env);
  return {env,timeout:()=>timer(),reads:()=>reads};
}
test('GET deadline includes a stalled JSON body after successful headers',async()=>{
  const h=slowBody();const p=h.env.read('/slow',{timeout:6000});await flush();assert.equal(h.reads(),1);
  h.timeout();await assert.rejects(p,/core.api.le_serveur_ne_repond_pas/);
});
test('unmounted panel cancels its body read without turning cancellation into a timeout',async()=>{
  const h=slowBody(), controller=new AbortController();
  const p=h.env.read('/slow',{signal:controller.signal});await flush();controller.abort();
  await assert.rejects(p,e=>e.name==='AbortError');
});
