import test from 'node:test';
import assert from 'node:assert/strict';
import { skillCards, matches } from '../next/js/features/connections/cards.js';

test('family skills become one card listing its members, sorted with the others', () => {
  const cards = skillCards([
    { name: 'review', description: 'Use when reviewing.' },
    { name: 'stitch-design-md', family: 'stitch', description: 'Design system.' },
    { name: 'stitch-remotion', family: 'stitch', description: 'Videos.' },
    { name: 'create-capability', description: 'Use when creating.' },
  ]);
  assert.deepEqual(cards.map(c => [c.kind, c.name]), [['skill', 'create-capability'], ['skill', 'review'], ['family', 'stitch']]);
  assert.equal(cards[2].members.length, 2);
  assert.equal(cards[2].description, 'design-md, remotion');
});

test('search matches a family through its members', () => {
  const [family] = skillCards([{ name: 'stitch-remotion', family: 'stitch', description: 'Walkthrough videos.' }]);
  assert.ok(matches(family, 'videos'));
  assert.ok(matches(family, ''));
  assert.ok(!matches(family, 'kavita'));
});
