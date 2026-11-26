// Tours d'une conversation d'agent, à partir des blocs que Codeman
// reconstitue (prompt, réponses, statuts, outils) et de son historique des
// modifications de fichiers. Sans dépendance au rendu : testé seul.
//
// Un tour = la demande, puis ce que l'agent a dit et fait en route, puis sa
// réponse finale. Pendant le tour, tout est visible ; une fois fini, le
// travail se replie sous « A travaillé … » au-dessus de la réponse.

const ms = v => (typeof v === 'number' ? v : Date.parse(v || '')) || 0;

// Première ligne utile d'un bloc d'outil ou de statut (Codex, écran).
export function stepLabel(text) {
  const line = String(text || '').split('\n').map(l => l.trim()).find(Boolean) || '';
  return line.length > 160 ? line.slice(0, 159) + '…' : line;
}

/**
 * @param blocks  messages de `last-response?context=full`
 * @param edits   entrées de `/edits` ({ id, ts, relPath, added, removed, … })
 * @param live    la session travaille encore sur le dernier tour
 */
export function buildTurns(blocks, edits = [], live = false) {
  const turns = [];
  for (const b of blocks || []) {
    if (!b || typeof b.text !== 'string') continue;
    if (b.role === 'user' || b.kind === 'prompt') {
      turns.push({ key: 't' + turns.length, prompt: b.text, start: ms(b.timestamp), items: [], end: ms(b.timestamp), edits: [] });
      continue;
    }
    const turn = turns[turns.length - 1];
    if (!turn) continue; // bannière du CLI avant la première demande
    const at = ms(b.timestamp);
    if (at) turn.end = Math.max(turn.end, at);
    if (b.kind === 'tool' || b.kind === 'status') {
      const label = stepLabel(b.text);
      if (label) turn.items.push({ k: 'step', kind: b.kind, text: label, full: b.text });
    } else if (b.text.trim()) turn.items.push({ k: 'say', text: b.text, at });
  }
  // Les modifications de fichiers se rangent dans le tour où elles ont eu lieu.
  const sorted = [...(edits || [])].filter(e => e && e.ts).sort((a, b) => a.ts - b.ts);
  turns.forEach((turn, i) => {
    const next = turns[i + 1]?.start || Infinity;
    turn.edits = sorted.filter(e => e.ts >= turn.start && e.ts < next);
  });
  turns.forEach((turn, i) => {
    turn.live = live && i === turns.length - 1;
    // La réponse finale est la dernière prise de parole d'un tour fini.
    const lastSay = turn.items.map(x => x.k).lastIndexOf('say');
    turn.answer = !turn.live && lastSay >= 0 ? turn.items[lastSay].text : '';
    turn.work = !turn.live && lastSay >= 0 ? turn.items.filter((_, n) => n !== lastSay) : turn.items;
    turn.ms = turn.end > turn.start ? turn.end - turn.start : 0;
  });
  return turns;
}

// Fichiers modifiés d'un tour : un fichier par chemin, totaux cumulés.
export function changedFiles(edits) {
  const files = new Map();
  for (const e of edits || []) {
    const key = e.relPath || e.path;
    const f = files.get(key) || { path: key, added: 0, removed: 0, created: false, ids: [] };
    f.added += e.added || 0; f.removed += e.removed || 0; f.created ||= !!e.created; f.ids.push(e.id);
    files.set(key, f);
  }
  const list = [...files.values()];
  return { files: list, added: list.reduce((n, f) => n + f.added, 0), removed: list.reduce((n, f) => n + f.removed, 0) };
}

// « 2 min 14 s », « 48 s ».
export function duration(msValue) {
  const s = Math.max(0, Math.round(msValue / 1000));
  if (s < 60) return s + ' s';
  const m = Math.floor(s / 60), r = s % 60;
  if (m < 60) return r ? `${m} min ${r} s` : `${m} min`;
  const h = Math.floor(m / 60), rm = m % 60;
  return rm ? `${h} h ${rm} min` : `${h} h`;
}

// Choix d'une fenêtre d'accord lus à l'écran (Codex et les CLI sans réponse
// par Codeman) : « 1. Yes, proceed (y) ». La touche entre parenthèses est
// celle que le CLI attend ; à défaut, le numéro.
export function screenChoices(context) {
  const out = [];
  for (const raw of String(context || '').split('\n')) {
    const m = raw.match(/^[\s│|›>❯]*(\d)[.)]\s+(.+?)\s*(?:\((esc|enter|[a-z])\))?\s*[│|]?\s*$/i);
    if (!m) continue;
    const label = m[2].replace(/\s{2,}/g, ' ').trim();
    if (!label || out.some(c => c.n === +m[1])) continue;
    const key = (m[3] || '').toLowerCase();
    out.push({ n: +m[1], label, key: key === 'esc' ? '\u001b' : key === 'enter' ? '\r' : key || m[1], hint: key ? (key === 'esc' ? 'Esc' : key.toUpperCase()) : m[1], deny: key === 'esc' || /^no\b|^non\b|deny|refus/i.test(label) });
  }
  return out;
}
