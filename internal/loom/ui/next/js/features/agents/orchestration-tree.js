// Fonctions pures de calcul d'arbre d'orchestration (sans dépendance DOM/réseau).

export function nodeOf(id, nodes) {
  return (id && nodes && nodes[id]) || null;
}

export function rootIdOf(id, nodes) {
  const n = nodeOf(id, nodes);
  return (n && n.root_id) || id || '';
}

// Renvoie le parent si celui-ci existe dans l'ensemble des sessions affichées ;
// si le parent a été fermé, l'enfant remonte proprement au niveau racine au
// lieu de disparaître de la barre latérale.
export function visibleParentOf(id, visibleIds, nodes) {
  let cur = nodeOf(id, nodes);
  const guard = new Set([id]);
  while (cur && cur.parent_id) {
    if (guard.has(cur.parent_id)) return '';
    guard.add(cur.parent_id);
    if (visibleIds.has(cur.parent_id)) return cur.parent_id;
    cur = nodeOf(cur.parent_id, nodes);
  }
  return '';
}

export function childrenOf(parentId, nodes) {
  if (!parentId || !nodes) return [];
  return Object.values(nodes)
    .filter(n => n && n.parent_id === parentId)
    .sort((a, b) => (a.created_at || 0) - (b.created_at || 0));
}

export function descendantsOf(parentId, nodes) {
  if (!parentId || !nodes) return [];
  const out = [];
  const seen = new Set([parentId]);
  const queue = [parentId];
  while (queue.length) {
    const cur = queue.shift();
    for (const child of childrenOf(cur, nodes)) {
      if (seen.has(child.session_id)) continue;
      seen.add(child.session_id);
      out.push(child);
      queue.push(child.session_id);
    }
  }
  return out;
}
