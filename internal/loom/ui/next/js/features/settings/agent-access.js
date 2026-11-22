// Accès des agents : la passerelle MCP de Loom (/mcp/loom). Un agent qui a le
// jeton voit les machines, y lance des commandes, utilise le second cerveau et,
// via Codeman, lance d'autres sessions d'agents. Contrat : docs/agent-access.md.
import { html, useState, useEffect } from '../../core/lib.js';
import { t, locale } from '../../core/i18n.js';
import { get, post } from '../../core/api.js';
import { Switch, Seg } from '../../ui/controls.js';
import { Modal, confirm, toast } from '../../ui/dialog.js';
import { copyText } from '../../ui/clipboard.js';
import { Line, Group } from './kit.js';

// Configurations prêtes à coller. Claude Code en portée utilisateur : toutes
// les sessions de la machine l'ont, y compris celles que Codeman lance.
export const agentSetups = (url, token) => ({
  claude: `claude mcp add --scope user --transport http loom ${url} --header "Authorization: Bearer ${token}"`,
  codex: `[mcp_servers.loom]\nurl = "${url}"\nhttp_headers = { "Authorization" = "Bearer ${token}" }`,
  json: JSON.stringify({ mcpServers: { loom: { type: 'http', url, headers: { Authorization: 'Bearer ' + token } } } }, null, 2),
});

// « Me demander avant d'agir » : deux règles Confirmer, visibles et modifiables
// dans Autorisations. Lire, chercher et suivre ses propres sessions restent libres.
const ASK_RULES = [
  { id: 'agent-access-ask-run', scope: 'global', subject: 'mcp.tool:loom/loom_machine_run', decision: 'confirm', conditions: {} },
  { id: 'agent-access-ask-session', scope: 'global', subject: 'mcp.tool:agents/start_session', decision: 'confirm', conditions: {} },
];
const ASK_IDS = ASK_RULES.map(r => r.id);

export const clientName = c => !c ? '' : /claude/i.test(c) ? 'Claude Code' : /codex/i.test(c) ? 'Codex' : c;
const since = sec => {
  const s = Math.max(0, Date.now() / 1000 - sec);
  const f = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' });
  return s < 60 ? f.format(0, 'second') : s < 3600 ? f.format(-Math.round(s / 60), 'minute')
    : s < 86400 ? f.format(-Math.round(s / 3600), 'hour') : f.format(-Math.round(s / 86400), 'day');
};

function SetupDialog({ url, token, onClose }) {
  const [client, setClient] = useState('claude');
  const text = agentSetups(url, token)[client];
  const copy = async () => { const ok = await copyText(text); toast(ok ? t('agent_access.copied') : t('settings.page.copie_refusee'), ok ? '' : 'err'); };
  return html`<${Modal} title=${t('agent_access.ready_title')} sub=${t('agent_access.ready_sub')} wide onClose=${onClose}
    foot=${html`<button class="btn primary" onClick=${onClose}>${t('agent_access.done')}</button>`}>
    <div class="agent-access-block">
      <${Seg} size="sm" label=${t('agent_access.client')} value=${client} onChange=${setClient}
        options=${[{ value: 'claude', label: 'Claude Code' }, { value: 'codex', label: 'Codex' }, { value: 'json', label: t('agent_access.other') }]} />
      <div class="agent-access-head"><p class="note">${{ claude: t('agent_access.how_claude'), codex: t('agent_access.how_codex'), json: t('agent_access.how_json') }[client]}</p><button class="btn sm" onClick=${copy}>${t('settings.page.copier')}</button></div>
      <pre class="mono">${text}</pre>
    </div>
  </${Modal}>`;
}

export function AgentAccess() {
  const [gw, setGw] = useState(null);
  const [pol, setPol] = useState(null);
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const url = location.origin + '/mcp/loom';
  const load = () => get('/api/mcp/gateway').then(r => setGw(r && typeof r.token_set === 'boolean' ? r : null)).catch(() => setGw(null));
  const loadPolicy = () => get('/api/policy').then(r => setPol(r && Array.isArray(r.rules) ? r : null)).catch(() => setPol(null));
  useEffect(() => {
    load(); loadPolicy();
    const timer = setInterval(load, 20000); // le premier agent connecté apparaît sans recharger
    return () => clearInterval(timer);
  }, []);
  const rotate = async () => {
    if (gw?.token_set && !await confirm(t('agent_access.replace_title'), t('agent_access.replace_text'), { ok: t('agent_access.replace'), danger: true })) return;
    setBusy(true);
    const r = await post('/api/mcp/gateway/token', { rotate: true });
    setBusy(false);
    if (!r.ok || !r.token) return toast(r.error || t('settings.page.reglage_impossible'), 'err');
    setToken(r.token); load();
  };
  const asking = !!pol && ASK_RULES.every(a => pol.rules.some(r => r.id === a.id && r.decision === 'confirm'));
  const setAsk = async on => {
    const rest = pol.rules.filter(r => !ASK_IDS.includes(r.id));
    setBusy(true);
    const r = await post('/api/policy', { ...pol, rules: on ? [...rest, ...ASK_RULES] : rest }).catch(e => ({ ok: false, error: e.message }));
    setBusy(false);
    if (r.ok === false || r.error || !Array.isArray(r.rules)) return toast(r.error || t('policy.save_failed'), 'err');
    setPol(r);
  };
  const agents = gw?.agents === 'available' ? t('agent_access.agents_ok') : gw?.agents === 'codeman_cli_missing' ? t('agent_access.agents_cli') : t('agent_access.agents_off');
  const last = gw?.last_used ? t('agent_access.last_value', { client: clientName(gw.last_client) || t('agent_access.an_agent'), when: since(gw.last_used) }) : t('agent_access.never');
  return html`<${Group} title=${t('agent_access.title')}>
    <${Line} label=${t('settings.page.adresse')} tip=${t('agent_access.tip')}><code class="mono">${url}</code></${Line}>
    <${Line} label=${t('agent_access.token')}>
      <span class="state">${gw ? gw.token_set ? t('access.configured') : t('access.not_set') : '…'}</span>
      ${gw && html`<button class="btn sm" disabled=${busy} onClick=${rotate}>${gw.token_set ? t('agent_access.replace') : t('agent_access.create')}</button>`}
    </${Line}>
    ${gw?.token_set && html`<${Line} label=${t('agent_access.last')}><span class="state">${last}</span></${Line}>`}
    ${pol && html`<${Line} label=${t('agent_access.ask')} tip=${t('agent_access.ask_tip')}><${Switch} checked=${asking} disabled=${busy} label=${t('agent_access.ask')} onChange=${setAsk} /></${Line}>`}
    <${Line} label=${t('agent_access.agents')}><span class="state">${gw ? agents : '…'}</span></${Line}>
    ${token && html`<${SetupDialog} url=${url} token=${token} onClose=${() => setToken('')} />`}
  </${Group}>`;
}
