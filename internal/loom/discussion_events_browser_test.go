package loom

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise the shared reducer without a browser or a model call.
func TestDiscussionEventRenderer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable")
	}
	engine, err := os.ReadFile("ui/next/js/features/chat/engine.js")
	if err != nil {
		t.Fatal(err)
	}
	script := `
const vm = require('node:vm'), assert = require('node:assert/strict');
const source = process.argv[2].replace(/^import .*;$/gm, '').replace(/^export /gm, '');
const calls = [];
const ctx = vm.createContext({
 createStore: initial => { let state = initial; return {get:()=>state,set:p=>state={...state,...p}}; },
 document:{addEventListener(){}}, refreshNav(){}, refreshWorkspace(){}, go(){}, app:{get:()=>({})},
 toast(){}, confirm:async()=>true, crypto:{randomUUID:()=> 'request-id'}, Date,
 post:async()=> { vm.runInContext("onEvent({type:'turn_done',session:{id:'s',status:'complete'}})",ctx); return {ok:true}; },
});
vm.runInContext(source,ctx);
const emit = e => { ctx.event = e; vm.runInContext('onEvent(event)',ctx); };
const state = () => vm.runInContext('chat.get()',ctx);
const rt = {runtime_id:'openai-compatible',model:'cloud'};
emit({reset:true,replay:false});
emit({type:'turn_start',text:'question',provenance:rt});
emit({type:'text_delta',text:'<script>model</script>'});
assert.equal(state().gen.tok,null);
emit({type:'reasoning_delta',text:'reported',summary:true});
emit({type:'usage',provenance:{...rt,usage:{completion_tokens:7}}});
assert.equal(state().gen.tok,7);
emit({type:'tool_end',native_tool:{name:'write',state:'DONE'},provenance:{...rt,events:[{name:'write'}]}});
assert.equal(state().items.filter(i=>i.k==='tool').length,0);
emit({type:'turn_done',provenance:rt});
assert.deepEqual(Array.from(state().items,i=>i.k),['user','reasoning','assistant','foot']);
assert.equal(state().items[2].text,'<script>model</script>');
assert.equal(state().items[3].running,false);
assert.equal(state().busy,false);
// Returning to the native journal uses the same reducer. Imported assistant text
// stays Markdown (md.js escapes raw HTML), so code blocks render for every runtime.
emit({type:'turn_start',text:'local',seq:1});
emit({type:'text_delta',text:'first',seq:2,toks:2,portable_text:true});
emit({type:'text_delta',text:'first complete',seq:3,toks:4,replace:true,portable_text:true});
assert.equal(state().items.at(-1).text,'first complete');
assert.ok(!state().items.at(-1).plain);
emit({type:'usage',metrics:{gen_per_second:12,prompt_tokens_total:10,gen_tokens:4}});
emit({type:'turn_done',provenance:{runtime_id:'llama.cpp',model:'local'},seq:4});
assert.equal(state().items.at(-1).tok,4);
assert.equal(state().items.at(-1).rate,12);
assert.equal(state().ctxUsed,14);
assert.equal(state().items[3].rt.model,'cloud');
// A quick completion before the send HTTP response must leave the composer free.
vm.runInContext("chat.set({mode:'thread',session:{id:'s',status:'idle'}})",ctx);
(async()=> { assert.equal(await vm.runInContext("send('quick')",ctx),true); assert.equal(state().busy,false); })().catch(e=>{throw e});
`
	path := filepath.Join(t.TempDir(), "renderer.cjs")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path, string(engine)).CombinedOutput(); err != nil {
		t.Fatalf("renderer: %v\n%s", err, out)
	}
}
