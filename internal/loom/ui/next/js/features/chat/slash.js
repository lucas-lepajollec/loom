// Menu « / » à deux niveaux. Premier niveau : les commandes annoncées par le
// harness, plus ses réglages (modèle, effort, mode…) et ses sessions. Second
// niveau : les choix possibles, construits à partir de ce que le harness
// annonce réellement (valeurs de ses réglages, choix listés dans l'indice d'une
// commande, liste de ses sessions). Rien n'est inventé.
import { get, post } from '../../core/api.js';
import { app, go, refreshNav, refreshWorkspace } from '../../core/state.js';
import { toast } from '../../ui/dialog.js';
import { open } from './engine.js';

const CAT_NAME = { model: 'model', thought_level: 'effort', mode: 'mode' };

const optionValues = o => { const out = []; const walk = l => (l || []).forEach(x => x.options ? walk(x.options) : out.push(x)); walk(o && o.options); return out; };

// "on|off|toggle", "(no args to show) all | one-at-a-time", "[low|high] [--fix]",
// "[reconnect|enable|disable [<server>|all]]" : seuls les choix du premier
// groupe comptent, les arguments imbriqués se tapent ensuite.
function hintChoices(hint) {
  let h = String(hint || '').replace(/\(.*?\)/g, '').replace(/<[^>]*>/g, '').trim();
  if (h.startsWith('[')) {
    let depth = 0, end = h.length;
    for (let i = 0; i < h.length; i++) { if (h[i] === '[') depth++; else if (h[i] === ']' && --depth === 0) { end = i; break; } }
    h = h.slice(1, end);
  }
  while (/\[[^[\]]*\]/.test(h)) h = h.replace(/\[[^[\]]*\]/g, '');
  if (!h.includes('|')) return [];
  const words = h.split('|').map(s => s.trim().split(/\s+/)[0]).filter(s => /^[\w.-]+$/.test(s));
  return words.length >= 2 ? [...new Set(words)] : [];
}

// Entrées du premier niveau. Une entrée a soit `children` (liste), soit
// `load` (liste chargée à l'ouverture), soit rien (insérée telle quelle).
export function slashEntries({ commands, session, harness, rtId }) {
  const entries = (commands || []).map(c => {
    const hint = (c.input && c.input.hint) || '';
    const choices = hintChoices(hint);
    // Un seul choix attendu : il part tel quel. Sinon (autre argument, <valeur>,
    // --option), il est inséré et le reste se tape à la main.
    const complete = !/<|--/.test(hint) && (hint.match(/\[/g) || []).length <= 1;
    return { name: c.name, description: c.description || '', hint,
      children: choices.length ? choices.map(v => ({ label: v, insert: '/' + c.name + ' ' + v, complete })) : null };
  });
  const byName = n => entries.find(e => e.name === n);
  const add = (name, description, extra) => {
    const e = byName(name);
    if (e) Object.assign(e, extra, { description: e.description || description });
    else entries.push({ name, description, ...extra });
  };
  if (!session || !rtId) return entries;
  const cfg = (harness && harness.config) || [];
  const modes = (harness && harness.modes) || [];
  for (const o of cfg) {
    const name = CAT_NAME[o.category] || o.id;
    if (o.category === 'mode' && modes.length) continue;
    const values = optionValues(o);
    if (o.type === 'boolean') {
      add(name, o.name || o.id, { children: [true, false].map(v => ({ label: v ? 'Activé' : 'Désactivé', current: o.currentValue === v, run: { kind: 'config', id: o.id, value: v } })) });
    } else if (values.length) {
      add(name, o.name || o.id, { children: values.map(v => ({ label: v.name || v.value, description: v.description, current: v.value === o.currentValue,
        run: o.category === 'model' ? { kind: 'model', value: v.value } : { kind: 'config', id: o.id, value: v.value } })) });
    }
  }
  if (modes.length > 1) add('mode', 'Mode de l’agent', { children: modes.map(m => ({ label: m.name || m.id, description: m.description, current: m.id === harness.mode, run: { kind: 'mode', id: m.id } })) });
  // Sessions : celles de Loom avec ce harness et celles du harness lui-même.
  const sessions = { description: 'Ouvrir une autre discussion avec ce harness', load: async () => {
    const loom = ((app.get().nav && app.get().nav.conversations) || []).filter(c => c.runtime_id === rtId && c.id !== session.id)
      .map(c => ({ label: c.title || 'Discussion', description: 'dans Loom', run: { kind: 'open', id: c.id } }));
    const r = await get('/api/runtimes/' + rtId + '/sessions').catch(() => null);
    const native = (r && r.ok ? r.sessions : []).filter(x => !x.imported || !loom.some(l => l.run.id === x.imported))
      .sort((a, b) => String(b.updatedAt || '').localeCompare(String(a.updatedAt || '')))
      .map(x => ({ label: x.title || 'Session sans titre', description: x.imported ? 'dans Loom' : 'dans le harness · ' + x.cwd.replace(/^\/home\/[^/]+/, '~'), run: x.imported ? { kind: 'open', id: x.imported } : { kind: 'import', x } }));
    return [...loom, ...native];
  } };
  for (const n of ['sessions', 'resume']) if (byName(n)) add(n, '', sessions);
  if (!byName('sessions') && !byName('resume')) add('sessions', sessions.description, sessions);
  return entries;
}

// Exécute un choix du second niveau. Renvoie true si la commande est traitée
// par Loom (rien à envoyer).
export async function runChoice(run, { session, rtId }) {
  const fail = r => { toast((r && r.error) || 'Action impossible', 'err'); return true; };
  if (run.kind === 'open') { open(run.id); go('chat'); return true; }
  if (run.kind === 'import') {
    const r = await post('/api/runtimes/' + rtId + '/sessions/import', { sessionId: run.x.sessionId, cwd: run.x.cwd, title: run.x.title || '' }).catch(e => ({ ok: false, error: e.message }));
    if (!r.ok) return fail(r);
    await refreshNav(); open(r.session.id); go('chat'); return true;
  }
  if (run.kind === 'model') {
    const r = await post('/api/runtime/sessions/select', { id: session.id, choice_id: rtId + ':' + run.value, consent: true });
    if (!r.ok) return fail(r);
    await refreshWorkspace(); open(session.id, true); toast('Modèle changé'); return true;
  }
  const body = run.kind === 'mode' ? { id: session.id, mode: run.id } : { id: session.id, config: { [run.id]: run.value } };
  const r = await post('/api/runtime/sessions/configure', body);
  if (!r.ok) return fail(r);
  toast('Réglage appliqué'); return true;
}
