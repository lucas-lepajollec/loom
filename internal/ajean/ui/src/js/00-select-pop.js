// Listes déroulantes : le popup natif (surtout GTK/Chromium) s’étire à la
// largeur du plus long libellé — chemins de GGUF, optgroup, etc. On ouvre
// à la place un menu calé sur le champ, avec ellipse. Téléphone : native.

function selPopSkip(sel){
  if(!sel || sel.disabled || sel.multiple || sel.size > 1) return true;
  if(sel.hidden || sel.getAttribute('hidden')!==null) return true;
  const st = getComputedStyle(sel);
  if(st.display==='none' || st.visibility==='hidden') return true;
  const r = sel.getBoundingClientRect();
  return r.width < 8 || r.height < 8;
}
function selPopCoarse(){
  return window.matchMedia('(pointer: coarse)').matches && window.matchMedia('(max-width:720px)').matches;
}

let SELPOP = {el:null, sel:null};

function selPopClose(){
  if(SELPOP.el){ SELPOP.el.remove(); SELPOP.el=null; }
  SELPOP.sel=null;
}
function selPopOpen(sel){
  if(selPopSkip(sel) || selPopCoarse()) return false;
  selPopClose();
  const r = sel.getBoundingClientRect();
  const pop = document.createElement('div');
  pop.className = 'sel-pop';
  pop.setAttribute('role','listbox');
  const maxW = Math.max(120, Math.min(r.width, window.innerWidth - 16));
  let left = r.left;
  if(left + maxW > window.innerWidth - 8) left = Math.max(8, window.innerWidth - 8 - maxW);
  pop.style.left = left+'px';
  pop.style.width = maxW+'px';
  pop.style.top = (r.bottom+4)+'px';

  const addOpt = (opt)=>{
    const b = document.createElement('button');
    b.type = 'button';
    b.setAttribute('role','option');
    b.className = 'sel-pop-opt'+(opt.selected?' on':'')+(opt.disabled?' dim':'');
    const lab = (opt.label || opt.textContent || '').replace(/\s+/g,' ').trim();
    b.textContent = lab;
    b.title = lab;
    if(opt.disabled) b.disabled = true;
    else b.onclick = ()=>{
      sel.value = opt.value;
      sel.dispatchEvent(new Event('input', {bubbles:true}));
      sel.dispatchEvent(new Event('change', {bubbles:true}));
      selPopClose();
    };
    pop.appendChild(b);
    return b;
  };

  let onBtn = null;
  [...sel.children].forEach(ch=>{
    if(ch.tagName==='OPTGROUP'){
      const h = document.createElement('div');
      h.className = 'sel-pop-g';
      h.textContent = ch.label || '';
      h.title = ch.label || '';
      pop.appendChild(h);
      [...ch.children].forEach(o=>{ const b=addOpt(o); if(o.selected) onBtn=b; });
    } else if(ch.tagName==='OPTION'){
      const b=addOpt(ch); if(ch.selected) onBtn=b;
    }
  });
  document.body.appendChild(pop);
  const pr = pop.getBoundingClientRect();
  if(pr.bottom > window.innerHeight - 8){
    pop.style.top = Math.max(8, r.top - pr.height - 4)+'px';
  }
  if(onBtn) onBtn.scrollIntoView({block:'nearest'});
  SELPOP.el = pop;
  SELPOP.sel = sel;
  return true;
}

document.addEventListener('mousedown', e=>{
  if(SELPOP.el && SELPOP.el.contains(e.target)) return;
  const sel = e.target.closest && e.target.closest('select');
  if(sel && !selPopSkip(sel) && !selPopCoarse()){
    e.preventDefault();
    if(SELPOP.sel===sel){ selPopClose(); return; }
    sel.focus();
    selPopOpen(sel);
    return;
  }
  if(SELPOP.el && !SELPOP.el.contains(e.target)) selPopClose();
}, true);

document.addEventListener('keydown', e=>{
  if(e.key==='Escape'){ selPopClose(); return; }
  const sel = e.target && e.target.tagName==='SELECT' ? e.target : null;
  if(!sel || selPopSkip(sel) || selPopCoarse()) return;
  if(e.key==='ArrowDown' || e.key==='ArrowUp' || e.key==='Enter' || e.key===' '){
    e.preventDefault();
    if(SELPOP.sel===sel) selPopClose();
    else selPopOpen(sel);
  }
}, true);

addEventListener('scroll', e=>{
  if(SELPOP.el && (e.target===SELPOP.el || SELPOP.el.contains(e.target))) return;
  selPopClose();
}, true);
addEventListener('resize', ()=>selPopClose());
document.addEventListener('click', e=>{
  const sel = e.target.closest && e.target.closest('select');
  if(sel && !selPopSkip(sel) && !selPopCoarse()) e.preventDefault();
  if(e.target.closest && e.target.closest('.modal-x')) selPopClose();
}, true);
