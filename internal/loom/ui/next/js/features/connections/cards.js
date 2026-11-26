// Cartes de la page Connexions, sans dépendance au rendu (testées seules).

// Les skills d'un dossier de famille (skills/<famille>/<skill>/) forment une
// seule carte, comme un plugin qui regroupe plusieurs skills.
export function skillCards(skills) {
  const cards = [], families = new Map();
  for (const s of skills || []) {
    if (!s.family) { cards.push({ kind: 'skill', id: s.name, name: s.name, description: s.description, skill: s }); continue; }
    if (!families.has(s.family)) {
      const card = { kind: 'family', id: s.family, name: s.family, members: [] };
      families.set(s.family, card);
      cards.push(card);
    }
    families.get(s.family).members.push(s);
  }
  for (const card of families.values()) card.description = card.members.map(m => m.name.slice(card.name.length + 1)).join(', ');
  return cards.sort((a, b) => a.name.localeCompare(b.name));
}

export const matches = (card, q) => !q || [card.name, card.description, ...(card.members || []).map(m => m.name + ' ' + m.description)]
  .some(v => String(v || '').toLowerCase().includes(q));
