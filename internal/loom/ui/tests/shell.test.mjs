import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

const read=p=>fs.readFileSync(new URL(p,import.meta.url),'utf8');
const shell=read('../src/js/30-shell.js'),css=read('../src/shell.css'),tmpl=read('../src/index.tmpl.html');

// Charge le module avec les seules dépendances dont ses fonctions pures ont besoin.
function load(extra={}){
  const ctx={WS_PAGES:{},THREAD:{modelsTab:'local'},HUB:{place:'lib'},document:{addEventListener(){}},window:{addEventListener(){}},...extra};
  vm.runInNewContext(shell,ctx);return ctx;
}

test('navigation is Local, Cloud, Harnesses, Ressources, Usage in that order',()=>{
  const ctx=load();
  assert.deepEqual(JSON.parse(vm.runInNewContext('JSON.stringify(SHELL_NAV.map(n=>n[1]))',ctx)),['Local','Cloud','Harnesses','Ressources','Usage']);
  assert.ok(ctx.WS_PAGES.resources&&ctx.WS_PAGES.usage,'Ressources et Usage sont des pages du workspace');
});

test('every legacy view maps to one sidebar section',()=>{
  const ctx=load();
  const section=v=>vm.runInNewContext('shellSection('+JSON.stringify(v)+')',ctx);
  for(const v of ['hub','server','bench'])assert.equal(section(v),'local');
  assert.equal(section('agents'),'harnesses');
  for(const v of ['resources','capabilities','connections'])assert.equal(section(v),'resources');
  assert.equal(section('usage'),'usage');
  assert.equal(section('chat'),'');
  vm.runInNewContext('SHELL.cloudOnly=true',ctx);assert.equal(section('models'),'cloud');
});

test('local tabs follow the real view, including remote Hub vs library',()=>{
  const ctx=load();
  const tab=v=>vm.runInNewContext('shellLocalTab('+JSON.stringify(v)+')',ctx);
  assert.equal(tab('server'),'engine');assert.equal(tab('bench'),'bench');assert.equal(tab('hub'),'library');
  ctx.HUB.place='remote';assert.equal(tab('hub'),'hub');
});

test('dark theme is the default before and after first paint',()=>{
  assert.match(tmpl,/localStorage\.getItem\('loom-theme'\)\|\|'dark'/);
  assert.match(read('../src/js/01-theme.js'),/localStorage\.getItem\('loom-theme'\)\|\|'dark'/);
});

test('fonts are embedded, motion respects reduced-motion preferences',()=>{
  assert.match(css,/url\(\/fonts\/geist\.woff2\)/);
  assert.doesNotMatch(css+tmpl,/fonts\.googleapis\.com/,'aucun CDN de polices');
  assert.match(css,/prefers-reduced-motion:reduce/);
});
