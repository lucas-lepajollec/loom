// 22-activity.js : Centre d'activité, tâches en direct, notifications et historique complet.

const LOOM_ACT = {
  tasks: [],      // requêtes API & Chat terminées (max 200)
  events: [],     // notifications & toasts du dashboard (max 200)
  downloads: [],  // téléchargements actifs depuis /api/models/download/status
  liveTask: null, // tâche active en cours (chat ou slot API)
  unreadCount: 0,
  filter: 'all',
  fold: {
    tasks: false,
    notifs: false
  },
  config: {
    retentionDays: 7
  },
  pollTimer: null
};

// Initialisation au chargement du DOM
function activityInit(){
  try {
    const savedTasks = localStorage.getItem('loom_act_tasks');
    if(savedTasks) LOOM_ACT.tasks = JSON.parse(savedTasks) || [];
  } catch(_) { LOOM_ACT.tasks = []; }

  try {
    const savedEvents = localStorage.getItem('loom_act_events');
    if(savedEvents) LOOM_ACT.events = JSON.parse(savedEvents) || [];
  } catch(_) { LOOM_ACT.events = []; }

  try {
    const savedRet = localStorage.getItem('loom_act_retention');
    if(savedRet !== null) LOOM_ACT.config.retentionDays = Number(savedRet);
  } catch(_) {}

  try {
    const savedFold = localStorage.getItem('loom_act_fold');
    if(savedFold) LOOM_ACT.fold = Object.assign(LOOM_ACT.fold, JSON.parse(savedFold));
  } catch(_) {}

  const retSel = document.getElementById('act-retention-sel');
  if(retSel) retSel.value = String(LOOM_ACT.config.retentionDays);

  activityPurgeOld();
  activitySyncBadge();

  // Interception universelle des toasts pour alimenter le journal
  activityHookToast();

  // Attache le bouton au header de la vue active
  let initialView = 'chat';
  if(document.documentElement.hasAttribute('data-server')) initialView = 'server';
  else if(document.documentElement.hasAttribute('data-bench')) initialView = 'bench';
  else if(document.documentElement.hasAttribute('data-hub')) initialView = 'hub';
  else if(document.documentElement.hasAttribute('data-settings')) initialView = 'settings';
  activityAttachToHeader(initialView);

  // Démarrage de la boucle de surveillance
  activityStartWatcher();

  // Clic extérieur pour fermer le popover
  document.addEventListener('click', e => {
    const pop = document.getElementById('activity-popover');
    const btn = document.getElementById('activity-btn');
    if(!pop || pop.hidden) return;
    if(pop.contains(e.target) || (btn && btn.contains(e.target))) return;
    toggleActivityPopover(false);
  });

  // Échap pour fermer le popover
  document.addEventListener('keydown', e => {
    if(e.key === 'Escape'){
      const pop = document.getElementById('activity-popover');
      if(pop && !pop.hidden){
        toggleActivityPopover(false);
      }
    }
  });

  window.addEventListener('resize', () => {
    const pop = document.getElementById('activity-popover');
    if(pop && !pop.hidden) positionActivityPopover();
  });
  window.addEventListener('scroll', () => {
    const pop = document.getElementById('activity-popover');
    if(pop && !pop.hidden) positionActivityPopover();
  }, true);
}

function activityHookToast(){
  if(typeof window.toast === 'function' && !window.toast._actHooked){
    const orig = window.toast;
    window.toast = function(msg){
      try { orig(msg); } catch(_) {}
      try { activityLogEvent(msg); } catch(_) {}
    };
    window.toast._actHooked = true;
  }
}

// Purge automatique selon la durée de rétention
function activityPurgeOld(){
  const days = Number(LOOM_ACT.config.retentionDays);
  if(!days || days <= 0) return; // 0 = conserver indéfiniment
  const cutoff = Date.now() - (days * 86400 * 1000);
  LOOM_ACT.tasks = (LOOM_ACT.tasks || []).filter(t => (t.time || 0) >= cutoff);
  LOOM_ACT.events = (LOOM_ACT.events || []).filter(e => (e.time || 0) >= cutoff);
  activitySaveStorage();
}

function activitySaveStorage(){
  try {
    localStorage.setItem('loom_act_tasks', JSON.stringify((LOOM_ACT.tasks || []).slice(0, 200)));
    localStorage.setItem('loom_act_events', JSON.stringify((LOOM_ACT.events || []).slice(0, 200)));
  } catch(_) {}
}

// Ajoute une notification au journal
function activityLogEvent(text){
  if(!text || typeof text !== 'string') return;
  const clean = text.trim();
  if(!clean) return;

  const ev = {
    id: 'ev_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6),
    type: 'event',
    text: clean,
    time: Date.now()
  };

  LOOM_ACT.events.unshift(ev);
  if(LOOM_ACT.events.length > 200) LOOM_ACT.events.pop();

  const pop = document.getElementById('activity-popover');
  if(!pop || pop.hidden){
    LOOM_ACT.unreadCount = (LOOM_ACT.unreadCount || 0) + 1;
  }

  activitySaveStorage();
  activitySyncBadge();
  activityPaintPopover();
  activityPaintSettings();
}

// Enregistre une tâche complétée (Chat, API, Download)
function activityLogTask(task){
  if(!task) return;
  const item = {
    id: task.id || ('tk_' + Date.now() + '_' + Math.random().toString(36).slice(2, 6)),
    type: task.type || 'api', // 'chat', 'api', 'download'
    label: task.label || 'Requête',
    slot: task.slot,
    prompt: task.prompt || 0,
    tokens: task.tokens || 0,
    toks: task.toks || 0,
    ms: task.ms || 0,
    status: task.status || 'ok', // 'ok', 'error', 'canceled'
    size: task.size || 0,
    time: task.time || Date.now()
  };

  // Évite les doublons stricts pour les requêtes /api/server
  const exists = LOOM_ACT.tasks.some(t => t.time === item.time && t.slot === item.slot && t.tokens === item.tokens);
  if(exists) return;

  LOOM_ACT.tasks.unshift(item);
  if(LOOM_ACT.tasks.length > 200) LOOM_ACT.tasks.pop();

  activitySaveStorage();
  activitySyncBadge();
  activityPaintPopover();
  activityPaintSettings();
}

// Réglage de rétention
function setActRetention(val){
  const days = Number(val) || 0;
  LOOM_ACT.config.retentionDays = days;
  try { localStorage.setItem('loom_act_retention', String(days)); } catch(_) {}
  activityPurgeOld();
  activityPaintSettings();
  if(typeof toast === 'function') toast('Rétention mise à jour : ' + (days ? days + ' jours' : 'indéfinie'));
}

// Effacer tout l'historique
async function clearActivityHistory(){
  if(typeof askConfirm === 'function'){
    const ok = await askConfirm('Effacer tout l\'historique d\'activité, de requêtes et de notifications ?');
    if(!ok) return;
  }
  LOOM_ACT.tasks = [];
  LOOM_ACT.events = [];
  LOOM_ACT.unreadCount = 0;
  activitySaveStorage();
  activitySyncBadge();
  activityPaintPopover();
  activityPaintSettings();
  if(typeof toast === 'function') toast('Historique effacé');
}

// Effacer seulement les notifications non lues depuis le popover
function clearActivityNotifs(){
  LOOM_ACT.events = [];
  LOOM_ACT.unreadCount = 0;
  activitySaveStorage();
  activitySyncBadge();
  activityPaintPopover();
  activityPaintSettings();
}

// Accordéons dépliants du popover
function toggleActFold(sec){
  if(sec === 'tasks'){
    LOOM_ACT.fold.tasks = !LOOM_ACT.fold.tasks;
  } else if(sec === 'notifs'){
    LOOM_ACT.fold.notifs = !LOOM_ACT.fold.notifs;
  }
  try { localStorage.setItem('loom_act_fold', JSON.stringify(LOOM_ACT.fold)); } catch(_) {}
  activityPaintPopover();
}

// Positionne dynamiquement le popover sous le bouton d'activité
function positionActivityPopover(){
  const pop = document.getElementById('activity-popover');
  const btn = document.getElementById('activity-btn');
  if(!pop || !btn || pop.hidden) return;
  const rect = btn.getBoundingClientRect();
  pop.style.top = Math.round(rect.bottom + 8) + 'px';
  const popWidth = Math.min(390, window.innerWidth - 20);
  let right = window.innerWidth - rect.right;
  if(right < 10) right = 10;
  if(right + popWidth > window.innerWidth - 10){
    right = window.innerWidth - popWidth - 10;
  }
  pop.style.right = Math.round(right) + 'px';
  pop.style.left = 'auto';
}

// Attache le bouton d'activité au conteneur du header de la vue active
// (il est TOUJOURS inséré en premier enfant, donc le plus à gauche du groupe d'actions)
function activityAttachToHeader(viewName){
  const btn = document.getElementById('activity-btn');
  if(!btn) return;
  let target = null;
  if(viewName === 'chat'){
    target = document.getElementById('chat-head-acts');
  } else if(viewName === 'server'){
    target = document.getElementById('srv-head-acts');
  } else if(viewName === 'bench'){
    target = document.getElementById('bench-head-acts');
  } else if(viewName === 'hub'){
    target = document.getElementById('hub-head-acts');
  } else if(viewName === 'settings'){
    target = document.getElementById('set-head-acts');
  }
  if(target && target.firstChild !== btn){
    target.insertBefore(btn, target.firstChild);
  }
  const pop = document.getElementById('activity-popover');
  if(pop && !pop.hidden){
    positionActivityPopover();
  }
}

// Ouverture / fermeture du popover
function toggleActivityPopover(force){
  const pop = document.getElementById('activity-popover');
  const btn = document.getElementById('activity-btn');
  if(!pop) return;

  const show = (typeof force === 'boolean') ? force : pop.hidden;
  pop.hidden = !show;
  if(btn){
    btn.setAttribute('aria-expanded', show ? 'true' : 'false');
    btn.classList.toggle('active', show);
  }

  if(show){
    LOOM_ACT.unreadCount = 0;
    activitySyncBadge();
    positionActivityPopover();
    activityPaintPopover();
    activityPollNow();
  }
}

// Ouvre la page d'historique dans les réglages
function openActivitySettings(){
  toggleActivityPopover(false);
  if(typeof openSettings === 'function'){
    openSettings();
  }
  if(typeof showSettingsTab === 'function'){
    showSettingsTab('activite');
  }
}

// Boucle de surveillance
function activityStartWatcher(){
  if(LOOM_ACT.pollTimer) clearInterval(LOOM_ACT.pollTimer);
  LOOM_ACT.pollTimer = setInterval(activityWatcherTick, 1400);
  activityWatcherTick();
}

async function activityWatcherTick(){
  await activityPollNow();
}

async function activityPollNow(){
  let activeDownloads = [];
  let dlsChanged = false;

  // 1. Surveillance des téléchargements de modèles
  try {
    const dls = await jget('/api/models/download/status');
    if(Array.isArray(dls)){
      const inFlight = dls.filter(d => !d.finished && !d.canceled && !d.error);
      activeDownloads = inFlight;

      // Détecte les téléchargements venant de se terminer
      for(const d of dls){
        if(d.finished && !d._loggedFinished){
          d._loggedFinished = true;
          const already = LOOM_ACT.tasks.some(t => t.type === 'download' && t.label.includes(d.filename));
          if(!already){
            activityLogTask({
              type: 'download',
              label: 'Téléchargement ' + d.filename,
              size: d.total || 0,
              toks: 0,
              ms: d.started_at ? (Date.now() - d.started_at*1000) : 0,
              status: 'ok',
              time: Date.now()
            });
            if(typeof libRender === 'function' && document.documentElement.getAttribute('data-hub-lib') === '1'){
              libRender();
            }
          }
        }
      }
    }
  } catch(_) {}

  // Synchronisation des téléchargements actifs
  if(JSON.stringify(activeDownloads) !== JSON.stringify(LOOM_ACT.downloads)){
    LOOM_ACT.downloads = activeDownloads;
    dlsChanged = true;
  }

  // 2. Surveillance du serveur (slots en direct et complétions récentes)
  try {
    const s = await jget('/api/server');
    if(s && s.running){
      // Slots live
      const slots = (s.slots && s.slots.slots) || [];
      const busySlot = slots.find(sl => sl.is_processing || (sl.n_decoded > 0));
      if(busySlot){
        LOOM_ACT.liveTask = {
          type: 'api',
          label: 'Requête API /v1 (Slot ' + busySlot.id + ')',
          tokens: busySlot.n_decoded || 0,
          speed: 0
        };
      } else if(typeof busy !== 'undefined' && busy){
        LOOM_ACT.liveTask = {
          type: 'chat',
          label: 'Génération du chat en cours…',
          tokens: (typeof T !== 'undefined' && T.serverStats && T.serverStats.gen_tokens) || 0,
          speed: 0
        };
      } else {
        LOOM_ACT.liveTask = null;
      }

      // Complétions récentes
      const recent = s.recent || [];
      for(const r of recent){
        const rTime = r.ended ? new Date(r.ended).getTime() : Date.now();
        const exists = LOOM_ACT.tasks.some(t => t.time === rTime && t.slot === r.slot);
        if(!exists){
          activityLogTask({
            type: 'api',
            label: 'Requête API /v1/chat/completions',
            slot: r.slot,
            prompt: r.prompt,
            tokens: r.tokens,
            toks: r.toks,
            ms: r.ms,
            status: 'ok',
            time: rTime
          });
        }
      }
    } else {
      if(typeof busy !== 'undefined' && busy){
        LOOM_ACT.liveTask = {
          type: 'chat',
          label: 'Génération du chat en cours…',
          tokens: 0,
          speed: 0
        };
      } else {
        LOOM_ACT.liveTask = null;
      }
    }
  } catch(_) {}

  activitySyncBadge();

  const pop = document.getElementById('activity-popover');
  if(pop && !pop.hidden){
    activityPaintPopover();
  }

  // Si on est sur la page des modèles locaux, rafraîchir la section de téléchargement
  if(dlsChanged && document.documentElement.getAttribute('data-hub-lib') === '1'){
    activityRefreshLibDownloads();
  }
}

// Mise à jour de l'icône / pastille de notification dans le bandeau
function activitySyncBadge(){
  const badge = document.getElementById('activity-badge');
  const ind = document.getElementById('act-live-indicator');
  if(!badge) return;

  const dlCount = (LOOM_ACT.downloads || []).length;

  badge.className = 'act-badge';
  if(dlCount > 0){
    badge.classList.add('downloading');
    badge.textContent = String(dlCount);
  } else if(LOOM_ACT.liveTask){
    badge.classList.add('running');
    badge.textContent = '';
  } else if(LOOM_ACT.unreadCount > 0){
    badge.classList.add('unread');
    badge.textContent = '';
  } else {
    badge.textContent = '';
  }

  if(ind){
    if(dlCount > 0){
      ind.className = 'act-pill dl';
      ind.textContent = dlCount + ' téléchargement' + (dlCount > 1 ? 's' : '');
    } else if(LOOM_ACT.liveTask){
      ind.className = 'act-pill running';
      ind.textContent = 'En cours';
    } else {
      ind.className = 'act-pill idle';
      ind.textContent = 'Inactif';
    }
  }
}

// Rendu du popover
function activityPaintPopover(){
  const pop = document.getElementById('activity-popover');
  if(!pop || pop.hidden) return;

  // 1. Téléchargements
  const dlsCount = document.getElementById('act-dls-count');
  const dlsList = document.getElementById('act-dls-list');
  const dls = LOOM_ACT.downloads || [];

  if(dlsCount) dlsCount.textContent = String(dls.length);
  if(dlsList){
    if(!dls.length){
      dlsList.innerHTML = '<div class="act-empty">Aucun téléchargement en cours</div>';
    } else {
      dlsList.innerHTML = dls.map(d => {
        const pct = (d.total > 0) ? Math.min(100, Math.round((d.done / d.total) * 100)) : 0;
        const spd = d.speed ? (activityFmtBytes(d.speed) + '/s') : '—';
        const prog = activityFmtBytes(d.done) + ' / ' + activityFmtBytes(d.total);
        return '<div class="act-dl-item">'
          + '<div class="act-dl-head">'
          + '  <span class="act-dl-name" title="'+escHtml(d.filename)+'">'+escHtml(d.filename)+'</span>'
          + '  <button type="button" class="act-dl-cancel" onclick="activityCancelDownload(\''+escHtml(d.filename)+'\')" title="Annuler">✕</button>'
          + '</div>'
          + '<div class="act-dl-bar"><div class="act-dl-fill" style="width:'+pct+'%"></div></div>'
          + '<div class="act-dl-meta"><span>'+pct+'% · '+spd+'</span><span>'+prog+'</span></div>'
          + '</div>';
      }).join('');
    }
  }

  // 2. Tâches en direct & Requêtes
  const liveBox = document.getElementById('act-live-task-box');
  if(liveBox){
    if(LOOM_ACT.liveTask){
      liveBox.innerHTML = '<div class="act-live-card">'
        + '<span class="act-live-dot"></span>'
        + '<div class="act-live-info">'
        + '  <div class="act-live-t">'+escHtml(LOOM_ACT.liveTask.label)+'</div>'
        + (LOOM_ACT.liveTask.tokens ? '<div class="act-live-sub">'+LOOM_ACT.liveTask.tokens+' tokens générés</div>' : '')
        + '</div>'
        + '</div>';
    } else {
      liveBox.innerHTML = '';
    }
  }

  const tasksWrap = document.getElementById('act-tasks-wrap');
  const arrowTasks = document.getElementById('act-arrow-tasks');
  const tasksFolded = !!LOOM_ACT.fold.tasks;
  if(tasksWrap) tasksWrap.classList.toggle('folded', tasksFolded);
  if(arrowTasks) arrowTasks.textContent = tasksFolded ? '▸' : '▾';

  const tasksList = document.getElementById('act-tasks-list');
  if(tasksList){
    const recentTasks = (LOOM_ACT.tasks || []).slice(0, 5);
    if(!recentTasks.length){
      tasksList.innerHTML = '<div class="act-empty">Aucune requête récente</div>';
    } else {
      tasksList.innerHTML = recentTasks.map(t => {
        const timeAgo = activityFmtAgo(t.time);
        const tag = (t.type === 'chat') ? 'Chat' : (t.type === 'download' ? 'Téléchargement' : 'API');
        const tagCls = (t.type === 'chat') ? 'chat' : (t.type === 'download' ? 'dl' : 'api');
        const bits = [];
        if(t.tokens) bits.push(t.tokens + ' tok');
        if(t.toks) bits.push(activityFmtSpeed(t.toks));
        if(t.ms) bits.push(activityFmtMs(t.ms));
        if(t.size) bits.push(activityFmtBytes(t.size));

        return '<div class="act-row">'
          + '<div class="act-row-top">'
          + '  <span class="act-tag '+tagCls+'">'+tag+'</span>'
          + '  <span class="act-row-title">'+escHtml(t.label)+'</span>'
          + '  <span class="act-row-time">'+escHtml(timeAgo)+'</span>'
          + '</div>'
          + (bits.length ? '<div class="act-row-meta">'+bits.join(' · ')+'</div>' : '')
          + '</div>';
      }).join('');
    }
  }

  // 3. Notifications du dash
  const notifsWrap = document.getElementById('act-notifs-wrap');
  const arrowNotifs = document.getElementById('act-arrow-notifs');
  const notifsFolded = !!LOOM_ACT.fold.notifs;
  if(notifsWrap) notifsWrap.classList.toggle('folded', notifsFolded);
  if(arrowNotifs) arrowNotifs.textContent = notifsFolded ? '▸' : '▾';

  const notifsList = document.getElementById('act-notifs-list');
  if(notifsList){
    const recentEvents = (LOOM_ACT.events || []).slice(0, 5);
    if(!recentEvents.length){
      notifsList.innerHTML = '<div class="act-empty">Aucune notification récente</div>';
    } else {
      notifsList.innerHTML = recentEvents.map(e => {
        const timeAgo = activityFmtAgo(e.time);
        return '<div class="act-notif-row">'
          + '<span class="act-notif-dot"></span>'
          + '<span class="act-notif-txt">'+escHtml(e.text)+'</span>'
          + '<span class="act-notif-time">'+escHtml(timeAgo)+'</span>'
          + '</div>';
      }).join('');
    }
  }
}

// Annule un téléchargement depuis le popover ou la page locale
async function activityCancelDownload(fname){
  try {
    await jpost('/api/models/download/cancel', {filename: fname});
    if(typeof toast === 'function') toast('Téléchargement annulé');
  } catch(_) {}
  activityPollNow();
}

// Rendu du bloc téléchargements dans la page des Modèles locaux (#hub-lib)
function libRenderDownloads(){
  const dls = LOOM_ACT.downloads || [];
  if(!dls.length) return null;

  const sec = document.createElement('div');
  sec.className = 'hub-sec lib-dl-sec';
  sec.id = 'lib-dl-section';

  const head = document.createElement('div');
  head.className = 'hub-sec-h';
  head.innerHTML = '<span>Téléchargements en cours</span><span class="act-count-badge">'+dls.length+'</span>';
  sec.appendChild(head);

  const cards = document.createElement('div');
  cards.className = 'lib-dl-list';

  dls.forEach(d => {
    const pct = (d.total > 0) ? Math.min(100, Math.round((d.done / d.total) * 100)) : 0;
    const spd = d.speed ? (activityFmtBytes(d.speed) + '/s') : '—';
    const prog = activityFmtBytes(d.done) + ' / ' + activityFmtBytes(d.total);

    const card = document.createElement('div');
    card.className = 'lib-dl-card';
    card.innerHTML = '<div class="lib-dl-info">'
      + '<div class="lib-dl-name" title="'+escHtml(d.filename)+'">'+escHtml(d.filename)+'</div>'
      + '<div class="lib-dl-meta">'+pct+'% · '+spd+' · '+prog+'</div>'
      + '</div>'
      + '<div class="lib-dl-actions">'
      + '  <div class="lib-dl-bar"><div class="lib-dl-fill" style="width:'+pct+'%"></div></div>'
      + '  <button type="button" class="lib-dl-btn-cancel" onclick="activityCancelDownload(\''+escHtml(d.filename)+'\')">Annuler</button>'
      + '</div>';
    cards.appendChild(card);
  });

  sec.appendChild(cards);
  return sec;
}

function activityRefreshLibDownloads(){
  const existing = document.getElementById('lib-dl-section');
  const box = document.getElementById('hub-lib');
  if(!box) return;

  const newSec = libRenderDownloads();
  if(existing){
    if(newSec) existing.replaceWith(newSec);
    else existing.remove();
  } else if(newSec){
    box.prepend(newSec);
  }
}

// Rendu de la page Réglages -> Activité
function filterActLog(filter){
  LOOM_ACT.filter = filter || 'all';
  document.querySelectorAll('#act-filter-pills .act-pill-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.filter === LOOM_ACT.filter);
  });
  activityPaintSettings();
}

function activityPaintSettings(){
  const box = document.getElementById('act-full-log');
  if(!box) return;

  const filter = LOOM_ACT.filter || 'all';
  let rows = [];

  if(filter === 'all' || filter === 'requests'){
    (LOOM_ACT.tasks || []).filter(t => t.type === 'chat' || t.type === 'api').forEach(t => {
      rows.push({
        cat: 'request',
        type: t.type,
        label: t.label,
        time: t.time,
        meta: [
          t.tokens ? (t.tokens + ' tokens') : '',
          t.toks ? activityFmtSpeed(t.toks) : '',
          t.ms ? activityFmtMs(t.ms) : ''
        ].filter(Boolean).join(' · '),
        status: t.status || 'ok'
      });
    });
  }

  if(filter === 'all' || filter === 'downloads'){
    (LOOM_ACT.tasks || []).filter(t => t.type === 'download').forEach(t => {
      rows.push({
        cat: 'download',
        type: 'download',
        label: t.label,
        time: t.time,
        meta: t.size ? activityFmtBytes(t.size) : '',
        status: t.status || 'ok'
      });
    });
  }

  if(filter === 'all' || filter === 'events'){
    (LOOM_ACT.events || []).forEach(e => {
      rows.push({
        cat: 'event',
        type: 'event',
        label: e.text,
        time: e.time,
        meta: 'Notification',
        status: 'ok'
      });
    });
  }

  rows.sort((a, b) => (b.time || 0) - (a.time || 0));

  if(!rows.length){
    box.innerHTML = '<div class="act-empty" style="padding:28px 16px">Aucune entrée pour ce filtre.</div>';
    return;
  }

  box.innerHTML = rows.map(r => {
    const dateStr = r.time ? new Date(r.time).toLocaleString('fr-FR') : '—';
    let tag = 'Système', tagCls = 'event';
    if(r.cat === 'request'){
      tag = (r.type === 'chat') ? 'Chat' : 'API';
      tagCls = (r.type === 'chat') ? 'chat' : 'api';
    } else if(r.cat === 'download'){
      tag = 'Téléchargement';
      tagCls = 'dl';
    }

    return '<div class="act-log-row">'
      + '<div class="act-log-left">'
      + '  <span class="act-tag '+tagCls+'">'+tag+'</span>'
      + '  <div class="act-log-info">'
      + '    <div class="act-log-title">'+escHtml(r.label)+'</div>'
      + (r.meta ? '<div class="act-log-meta">'+escHtml(r.meta)+'</div>' : '')
      + '  </div>'
      + '</div>'
      + '<div class="act-log-right">'
      + '  <span class="act-log-date">'+escHtml(dateStr)+'</span>'
      + '</div>'
      + '</div>';
  }).join('');
}

// Fonctions utilitaires
function activityFmtBytes(n){
  n = Number(n) || 0;
  if(n <= 0) return '0 o';
  if(n >= 1<<30) return (n / (1<<30)).toFixed(1) + ' Go';
  if(n >= 1<<20) return (n / (1<<20)).toFixed(1) + ' Mo';
  if(n >= 1<<10) return Math.round(n / (1<<10)) + ' Ko';
  return n + ' o';
}

function activityFmtSpeed(v){
  v = Number(v) || 0;
  if(v <= 0) return '—';
  return (Math.round(v * 10) / 10).toFixed(1).replace('.', ',') + ' tok/s';
}

function activityFmtMs(ms){
  ms = Number(ms) || 0;
  if(ms <= 0) return '—';
  if(ms < 1000) return Math.round(ms) + ' ms';
  return (ms / 1000).toFixed(1) + ' s';
}

function activityFmtAgo(time){
  if(!time) return '';
  const diffSec = Math.max(0, Math.floor((Date.now() - time) / 1000));
  if(diffSec < 45) return 'à l\'instant';
  if(diffSec < 3600) return Math.floor(diffSec / 60) + ' min';
  if(diffSec < 86400) return Math.floor(diffSec / 3600) + ' h';
  return Math.floor(diffSec / 86400) + ' j';
}

document.addEventListener('DOMContentLoaded', activityInit);
