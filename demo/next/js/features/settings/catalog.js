import { t } from '../../core/i18n.js';

const catalog = [
  ['general', 'settings.page.general', 'gear', 'workspace.preferences', ['settings.page.theme', 'settings.language.label', 'settings.brain.folder']],
  ['notifications', 'notify.title', 'bell', 'workspace.preferences', ['notify.push', 'notify.webhook', 'notify.quiet', 'notify.ntfy']],
  ['internet', 'settings.page.internet', 'globe', 'workspace.system', ['settings.page.internet', 'workspace.search_tools']],
  ['startup', 'startup.title', 'power', 'workspace.system', ['startup.boot', 'startup.voice', 'startup.engine']],
  ['doctor', 'doctor.title', 'activity', 'workspace.system', ['doctor.title', 'workspace.system']],
  ['about', 'settings.page.a_propos', 'info', 'workspace.system', ['settings.page.version', 'settings.page.emplacements']],
  ['policy', 'policy.title', 'lock', 'workspace.access_data', ['policy.title', 'workspace.permissions_keywords']],
  ['security', 'settings.page.securite_et_donnees', 'lock', 'workspace.access_data', ['settings.page.chiffrement_au_repos', 'settings.page.snapshots_locaux', 'settings.page.sauvegardes', 'workspace.connections']],
];

export function settingsSections() {
  return catalog.map(([id, title, icon, group, keywords]) => ({
    id, title: t(title), icon, group: t(group), keywords: [...keywords.map(key => t(key)), t(group)],
  }));
}
