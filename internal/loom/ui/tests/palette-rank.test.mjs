import test from 'node:test';
import assert from 'node:assert/strict';
import { scoreText, matchRanges } from '../next/js/ui/search-rank.js';

test('an exact title outranks a prefix and a localized settings keyword', () => {
  const search = title => scoreText({ title, keywords: ['démarrage du moteur'] }, 'demarrage');
  assert.ok(search('Démarrage') > search('Démarrage du service'));
  assert.ok(search('Démarrage du service') > search('Réglages système'));
  assert.equal(scoreText({ title: 'Toggle theme' }, 'tg'), 0);
  assert.ok(scoreText({ title: 'Internet', keywords: ['Recherche web'] }, 'int web') > 0);
});

test('match ranges point into the displayed string and merge repeated prefixes', () => {
  assert.deepEqual(matchRanges('Open a session', 'session open'), [[0, 4], [7, 14]]);
  assert.deepEqual(matchRanges('Start a service', 'st sta'), [[0, 3]]);
  assert.deepEqual(matchRanges('İstanbul', 'stan'), []);
});
