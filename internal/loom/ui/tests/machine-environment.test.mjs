import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import htm from '../next/vendor/htm.mjs';

test('Docker unsupported/offline response is visible before disabled state and offers no switch', () => {
 const html=htm.bind((type,props,...children)=>({type,props:props||{},children}));
 const flat=x=>Array.isArray(x)?x.flatMap(flat):x&&typeof x==='object'?[x,...flat(x.children)]:[];
 const text=x=>Array.isArray(x)?x.map(text).join(''):x&&typeof x==='object'?text(x.children):String(x??'');
 const source=fs.readFileSync(new URL('../next/js/features/environment/page.js',import.meta.url),'utf8');
 for (const response of [{ok:false,unsupported:true,error:'observe unavailable'},{ok:false,error:'offline'}]) {
 const env={html,t:k=>k,Switch:'Switch',Tip:'Tip',Icon:'Icon',useEffect:()=>{},useState:()=>[response,()=>{}]};
 vm.runInNewContext(source.replace(/^import .*;\n/gm,'').replace(/^export /gm,'')+'\nglobalThis.render=DockerMachine;',env);
 const tree=env.render({machine:{id:'node',name:'Node'}});
 if (response.unsupported) assert.equal(flat(tree).find(n=>n.type==='Tip')?.props.text,response.error);
 else assert.match(text(tree),new RegExp(response.error));
 assert.equal(flat(tree).filter(n=>n.type==='Switch').length,0);
 assert.doesNotMatch(text(tree),/lecture_des_conteneurs_desactivee/);
 }
});

test('native usage errors and unsupported transports show unknown metrics, not zero', () => {
 const html=htm.bind((type,props,...children)=>({type,props:props||{},children}));
 const flat=x=>Array.isArray(x)?x.flatMap(flat):x&&typeof x==='object'?[x,...flat(x.children)]:[];
 const text=x=>Array.isArray(x)?x.map(text).join(''):x&&typeof x==='object'?text(x.children):String(x??'');
 const source=fs.readFileSync(new URL('../next/js/features/usage/page.js',import.meta.url),'utf8');
 const env={html,t:k=>k,tSource:x=>x,Logo:'Logo',Tip:'Tip',Icon:'Icon',fmtTok:String,locale:()=> 'en-US',useState:()=>[false,()=>{}]};
 vm.runInNewContext(source.replace(/^import .*;\n/gm,'').replace(/^export /gm,'')+'\nglobalThis.render=NativeRow;',env);
 for(const unsupported of [false,true]) {
 const tree=env.render({h:{runtime_id:'fixture',error:'offline',unsupported,input_tokens:0,output_tokens:0,total_tokens:0,cost_usd:0},onRefresh:()=>{}});
 const metrics=flat(tree).filter(n=>n.props['data-label']);
 assert.ok(metrics.every(n=>!/^0/.test(text(n))), 'failed readings must not fabricate zero tokens/cost');
 if(unsupported) assert.equal(flat(tree).find(n=>n.type==='button')?.props.disabled,true);
 }
});

test('quota refresh uses the quota capability independently of native usage support', () => {
 const html=htm.bind((type,props,...children)=>({type,props:props||{},children}));
 const flat=x=>Array.isArray(x)?x.flatMap(flat):x&&typeof x==='object'?[x,...flat(x.children)]:[];
 const source=fs.readFileSync(new URL('../next/js/features/usage/page.js',import.meta.url),'utf8');
 const env={html,t:k=>k,Icon:'Icon',useState:()=>[false,()=>{}]};
 vm.runInNewContext(source.replace(/^import .*;\n/gm,'').replace(/^export /gm,'')+'\nglobalThis.render=Quota;',env);
 const tree=env.render({q:{runtime_id:'fixture',windows:[]},rt:{implemented:true,capabilities:['quota']},onRefresh:()=>{}});
 assert.equal(flat(tree).find(n=>n.type==='button').props.disabled,false);
});
