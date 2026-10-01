// Clé de pilotage : envoyée en Authorization: Bearer sur chaque appel /api/*.
// Sur un 401 on la (re)demande et on rejoue la requête. Stockée en localStorage.
let TOKEN = localStorage.getItem('loom.key') || '';
function authHeaders(h){ h = Object.assign({}, h||{}); if(TOKEN) h['Authorization']='Bearer '+TOKEN; return h; }
// Base d'URL : "" en local (page à "/").
// L'ancienne interface est servie sous /classic : l'API reste à la racine.
const API_BASE = location.pathname.startsWith('/classic') ? '' : location.pathname.replace(/\/(index\.html)?$/, '');
// Délai maximal d'un appel /api/* ordinaire. Sans lui, une requête que le
// navigateur met en file d'attente (plafond de ~6 connexions par domaine, atteint
// dès qu'on laisse traîner plusieurs onglets Loom) reste suspendue POUR TOUJOURS :
// ni réponse, ni erreur, et une UI figée sur « chargement… » sans rien à afficher.
// Mieux vaut une erreur franche. Les flux longs (SSE) passent leur propre signal
// et ne sont donc jamais concernés.
const API_TIMEOUT_MS = 30000;
async function jfetch(u, opts){
  opts = opts || {};
  opts.headers = authHeaders(opts.headers);
  if(u.charAt(0) === '/') u = API_BASE + u;
  let timer=null;
  if(!opts.signal){
    const ac=new AbortController();
    opts.signal=ac.signal;
    timer=setTimeout(()=>ac.abort(), API_TIMEOUT_MS);
  }
  let r;
  try{ r = await fetch(u, opts); }
  catch(e){
    if(e && e.name==='AbortError'){ throw new Error('requête sans réponse après 30 s (trop d\'onglets Loom ouverts ?)'); }
    throw e;
  }
  finally{ if(timer) clearTimeout(timer); }
  if(r.status === 401){
    const k = await askKeyOnce();
    if(k){ opts.headers = authHeaders(opts.headers); r = await fetch(u, opts); }
  }
  return r;
}
// askKeyOnce : UNE seule demande de clé à la fois, partagée par tous les appels.
let _keyAsk = null;
function askKeyOnce(){
  if(!_keyAsk){
    _keyAsk = askPrompt('Clé de pilotage Loom requise :', {title:'Authentification', placeholder:'clé…'})
      .then(k => {
        _keyAsk = null;
        if(k){ TOKEN = k.trim(); localStorage.setItem('loom.key', TOKEN); }
        return k;
      }, e => { _keyAsk = null; throw e; });
  }
  return _keyAsk;
}
async function jget(u){ const r=await jfetch(u); return r.json(); }
async function jpost(u,b){ const r=await jfetch(u,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(b||{})}); return r.json(); }
