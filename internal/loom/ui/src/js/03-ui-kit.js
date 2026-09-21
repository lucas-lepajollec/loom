// Pastille d'état d'une section du menu (mode agent, internet, MCP, distant).
// `state` : true = actif (pilule pleine), false = inactif (contour), 'warn'/'err'
// = état signalé, null = pastille masquée. Un seul endroit décide de leur allure.
function setBadge(id, state, text){
  const el = typeof id==='string' ? document.getElementById(id) : id;
  if(!el) return;
  el.className = 'sbadge' + (state===true ? ' on' : (state==='warn' || state==='err') ? ' '+state : '');
  el.textContent = state===null || state===undefined ? '' : (text||'');
}
// Ouverture/fermeture des modales. Le display seul apparaissait d'un bloc : on
// pose la classe .show une frame APRÈS l'affichage pour que la transition CSS
// parte de son état initial (sans ce reflow forcé, le navigateur applique tout
// d'un coup et l'animation ne joue pas). À la fermeture on attend la fin de la
// transition avant de repasser en display:none.
function showModal(id){
  const el = typeof id==='string' ? document.getElementById(id) : id;
  if(!el) return;
  el.style.display='flex';
  void el.offsetHeight;
  el.classList.add('show');
}
function hideModal(id){
  const el = typeof id==='string' ? document.getElementById(id) : id;
  if(!el) return;
  el.classList.remove('show');
  // Rouverte entre-temps ? On ne la cache surtout pas.
  setTimeout(()=>{ if(!el.classList.contains('show')) el.style.display='none'; }, 180);
}
function toast(m){ const t=document.getElementById('toast'); t.textContent=m; t.classList.add('show'); setTimeout(()=>t.classList.remove('show'),1800); }
// Modales natives (askConfirm/askPrompt/askAlert) — remplacent confirm()/prompt()/
// alert() par de vraies boîtes stylées. Chacune renvoie une Promise : askConfirm →
// bool, askPrompt → string|null (null si annulé), askAlert → void. Échap/clic dehors
// = annuler ; Entrée = valider (sur prompt aussi).
let _askResolver=null, _askKind='confirm', _askCheck=false;
// État de la case optionnelle de la DERNIÈRE confirmation (opts.check). Lu par
// l'appelant juste après le await — la Promise, elle, ne renvoie que oui/non.
function askChecked(){ return _askCheck; }
function askResolve(ok){
  if(!_askResolver) return;
  const r=_askResolver; _askResolver=null;
  _askCheck = ok && document.getElementById('ask-check-input').checked;
  hideModal('ask-modal');
  document.removeEventListener('keydown', _askKey, true);
  if(_askKind==='prompt') r(ok ? document.getElementById('ask-input').value : null);
  else r(ok);
}
function _askKey(e){
  if(e.key==='Escape'){ e.preventDefault(); e.stopPropagation(); askResolve(false); }
  else if(e.key==='Enter'){ e.preventDefault(); e.stopPropagation(); askResolve(true); }
}
function _openAsk(kind, message, opts){
  // Compat 2 conventions : (message, opts) [UI Loom] ET l'objet unique
  // {title,msg,yes,no,placeholder} passé par les adaptateurs distants (server.html,
  // où cette fonction remplace le window.askConfirm du bootstrap boîte noire).
  if(message && typeof message==='object'){ const o=message; opts={title:o.title, okText:o.yes, cancelText:o.no, placeholder:o.placeholder, default:o.default, danger:o.danger}; message=o.msg||''; }
  opts=opts||{};
  // Une modale déjà ouverte ne doit pas être écrasée en silence : son appelant
  // attend une réponse, et l'écraser laissait sa promesse pendante POUR
  // TOUJOURS (requête suspendue, bouton figé). On l'annule proprement d'abord.
  if(_askResolver){ const prev=_askResolver; _askResolver=null; prev(_askKind==='prompt' ? null : false); }
  _askKind=kind;
  document.getElementById('ask-title').textContent = opts.title || (kind==='alert'?'Info':kind==='prompt'?'Saisie':'Confirmation');
  document.getElementById('ask-msg').textContent = message||'';
  const inp=document.getElementById('ask-input');
  if(kind==='prompt'){ inp.style.display=''; inp.value=opts.default||''; inp.placeholder=opts.placeholder||''; }
  else inp.style.display='none';
  // Case facultative (ex. « supprimer aussi le fichier .gguf »).
  const chk=document.getElementById('ask-check');
  chk.style.display = opts.check ? 'inline-flex' : 'none';
  document.getElementById('ask-check-label').textContent = opts.check || '';
  document.getElementById('ask-check-input').checked = !!opts.checkOn;
  _askCheck=!!opts.checkOn;
  const cancel=document.getElementById('ask-cancel'), ok=document.getElementById('ask-ok');
  const extra=document.getElementById('ask-extra');
  cancel.style.display = kind==='alert' ? 'none' : '';
  cancel.textContent = opts.cancelText || 'Annuler';
  ok.textContent = opts.okText || (kind==='alert'?'OK':kind==='prompt'?'Valider':'Confirmer');
  ok.classList.toggle('danger', !!opts.danger);
  if(extra){
    extra.style.display = (kind==='confirm' && opts.extraText) ? '' : 'none';
    extra.textContent = opts.extraText || '';
  }
  showModal('ask-modal');
  document.addEventListener('keydown', _askKey, true);
  setTimeout(()=>{
    const f = kind==='prompt' ? inp : (extra && extra.style.display!=='none' ? extra : ok);
    f.focus();
    if(kind==='prompt') inp.select();
  }, 30);
  return new Promise(res=>{ _askResolver=res; });
}
function askConfirm(message,opts){ return _openAsk('confirm',message,opts); }
function askPrompt(message,opts){ return _openAsk('prompt',message,opts); }
function askAlert(message,opts){ return _openAsk('alert',message,opts); }

// Infobulles « ? » : on les sort vers document.body (position:fixed viewport).
// Un parent avec transform (panneau params) ou overflow:hidden (réglages)
// ferait déborder la bulle hors écran.
let _tipHost = null;
function _tipEl(help){ return help && (help._loomTip || help.querySelector(':scope > .tip')); }
function placeHelpTip(help){
  const tip = _tipEl(help);
  if(!help || !tip) return;
  if(tip.parentElement !== document.body){
    help._loomTip = tip;
    document.body.appendChild(tip);
  }
  _tipHost = help;
  tip.classList.add('placed');
  const pad = 8;
  const r = help.getBoundingClientRect();
  const vw = window.innerWidth, vh = window.innerHeight;
  tip.style.position = 'fixed';
  tip.style.right = 'auto';
  tip.style.width = 'auto';
  tip.style.maxWidth = Math.max(120, Math.min(320, vw - pad*2)) + 'px';
  tip.style.maxHeight = Math.max(72, Math.min(vh*0.45, vh - pad*2)) + 'px';
  tip.style.left = '0px';
  tip.style.top = '0px';
  const t = tip.getBoundingClientRect();
  let left = r.left;
  let top = r.bottom + 6;
  if(left + t.width > vw - pad) left = vw - pad - t.width;
  if(left < pad) left = pad;
  if(top + t.height > vh - pad) top = r.top - t.height - 6;
  if(top < pad) top = pad;
  tip.style.left = Math.round(left) + 'px';
  tip.style.top = Math.round(top) + 'px';
}
function restoreHelpTip(help){
  const tip = help && help._loomTip;
  if(!tip) return;
  help.appendChild(tip);
  help._loomTip = null;
  tip.classList.remove('placed');
  tip.style.position = tip.style.left = tip.style.top = tip.style.right = '';
  tip.style.width = tip.style.maxWidth = tip.style.maxHeight = '';
  if(_tipHost === help) _tipHost = null;
}
function clearHelpTip(help){
  if(!help || help.classList.contains('open')) return;
  restoreHelpTip(help);
}
function helpFromEvent(e){
  const t = e.target;
  if(t && t.classList && t.classList.contains('tip') && t.classList.contains('placed')){
    return _tipHost;
  }
  return t && t.closest ? t.closest('.help') : null;
}
(function bindHelpTips(){
  document.addEventListener('pointerover', e=>{
    const h = helpFromEvent(e);
    if(h) placeHelpTip(h);
  });
  document.addEventListener('pointerout', e=>{
    const h = helpFromEvent(e);
    if(!h) return;
    const next = e.relatedTarget;
    if(next && (h.contains(next) || (h._loomTip && h._loomTip.contains(next)))) return;
    clearHelpTip(h);
  });
  document.addEventListener('focusin', e=>{
    const h = helpFromEvent(e);
    if(h) placeHelpTip(h);
  });
  document.addEventListener('focusout', e=>{
    const h = helpFromEvent(e);
    if(h) clearHelpTip(h);
  });
  document.addEventListener('click', e=>{
    const h = helpFromEvent(e);
    if(h && h.classList.contains('open')) placeHelpTip(h);
  });
  const hide = ()=>{
    document.querySelectorAll('.help.open').forEach(h=>{ if(h.isConnected) placeHelpTip(h); });
    document.querySelectorAll('.help').forEach(h=>{
      if(!h.classList.contains('open') && h._loomTip) restoreHelpTip(h);
    });
    document.querySelectorAll('body > .tip.placed').forEach(t=>{
      const live = [...document.querySelectorAll('.help')].some(h => h._loomTip === t);
      if(!live) t.remove();
    });
    if(_tipHost && !_tipHost.isConnected) _tipHost = null;
  };
  addEventListener('resize', hide);
  document.addEventListener('scroll', hide, true);
})();
