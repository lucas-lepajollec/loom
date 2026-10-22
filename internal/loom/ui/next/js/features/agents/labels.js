// Libellés à valeur variable : chaque clé reste écrite en toutes lettres pour
// que les dictionnaires et leurs tests la voient.
import { t } from '../../core/i18n.js';

const KEYS = {
  state: { waiting: 'agents.state.waiting', error: 'agents.state.error', working: 'agents.state.working', starting: 'agents.state.starting', idle: 'agents.state.idle', stopped: 'agents.state.stopped', gone: 'agents.state.gone' },
  term: { connecting: 'agents.term.connecting', open: 'agents.term.open', exited: 'agents.term.exited', missing: 'agents.term.missing', disconnected: 'agents.term.disconnected', error: 'agents.term.error' },
  conn: { idle: 'agents.conn.idle', connecting: 'agents.conn.connecting', unreachable: 'agents.conn.unreachable', auth: 'agents.conn.auth', rejected: 'agents.conn.rejected', error: 'agents.conn.error' },
  connText: { idle: 'agents.conn.idle_text', connecting: 'agents.conn.connecting_text', unreachable: 'agents.conn.unreachable_text', auth: 'agents.conn.auth_text', rejected: 'agents.conn.rejected_text', error: 'agents.conn.error_text' },
  nav: { '': 'agents.nav.sessions', control: 'agents.nav.control', grid: 'agents.nav.grid', cron: 'agents.nav.cron', settings: 'agents.nav.settings' },
  insp: { activity: 'agents.insp.activity', git: 'agents.insp.git', files: 'agents.insp.files', subagents: 'agents.insp.subagents', info: 'agents.insp.info' },
  gitKind: { staged: 'agents.git.kind.staged', unstaged: 'agents.git.kind.unstaged', untracked: 'agents.git.kind.untracked', conflicted: 'agents.git.kind.conflicted' },
  cronType: { once: 'agents.cron.type.once', interval: 'agents.cron.type.interval', daily: 'agents.cron.type.daily', weekly: 'agents.cron.type.weekly' },
  runStatus: { created: 'agents.cron.status.created', session_started: 'agents.cron.status.session_started', prompt_sent: 'agents.cron.status.prompt_sent', failed: 'agents.cron.status.failed', skipped: 'agents.cron.status.skipped' },
  approvals: { answer: 'agents.caps.appr.answer', terminal: 'agents.caps.appr.terminal', none: 'agents.caps.appr.none' },
};
// Une valeur inconnue (nouvelle version de Codeman) s'affiche telle quelle.
export const lt = (group, value) => { const key = KEYS[group] && KEYS[group][value]; return key ? t(key) : String(value ?? ''); };
