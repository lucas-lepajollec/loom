// Ce qu'un agent demande à l'utilisateur en cours de tour, quel que soit son
// protocole : questions à choix ou texte libre (Codex requestUserInput, dialogues
// Pi), formulaire ou lien (élicitation MCP/ACP). Les autorisations gardent leur
// propre carte (Approval). Réponse : POST /api/workspace/sessions/{id}/requests/{rid}.
import { t } from '../../core/i18n.js';
import { html, useState, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { post } from '../../core/api.js';
import { toast } from '../../ui/dialog.js';

async function resolve(sessionId, rid, body) {
  const r = await post('/api/workspace/sessions/' + encodeURIComponent(sessionId) + '/requests/' + encodeURIComponent(rid), body).catch(e => ({ ok: false, error: e.message }));
  if (!r || r.ok === false) { toast((r && r.error) || t('chat.messages.reponse_impossible'), 'err'); return false; }
  return true;
}

function Question({ q, value, onChange, single }) {
  const picked = value || [];
  const opts = q.options || [];
  const other = picked.find(v => !opts.some(o => o.id === v)) || '';
  const toggle = id => onChange(picked.includes(id) ? picked.filter(v => v !== id) : [id]);
  return html`<div class="rq-q">
    ${(q.header || !single) && q.header && html`<div class="rq-head">${q.header}</div>`}
    <div class="rq-text">${q.question}</div>
    ${opts.length > 0 && html`<div class="rq-opts">${opts.map(o => html`<button type="button" class=${cls('rq-opt', picked.includes(o.id) && 'on')} onClick=${() => toggle(o.id)}>
      <b>${o.label}</b>${o.description && html`<small>${o.description}</small>`}</button>`)}</div>`}
    ${(q.free_text || !opts.length) && html`<input class="input" type=${q.secret ? 'password' : 'text'} autocomplete="off" placeholder=${opts.length ? t('chat.request.other') : t('chat.request.answer')}
      value=${other} onInput=${e => onChange(e.target.value ? [e.target.value] : [])} />`}
  </div>`;
}

// Formulaire d'élicitation : les types simples d'un schéma JSON plat.
function SchemaForm({ schema, value, onChange }) {
  const props = (schema && schema.properties) || {};
  const req = new Set((schema && schema.required) || []);
  const set = (k, v) => onChange({ ...value, [k]: v });
  return html`<div class="rq-form">${Object.entries(props).map(([k, p]) => {
    const label = html`<span>${p.title || k}${req.has(k) ? ' *' : ''}</span>`;
    if (p.type === 'boolean') return html`<label class="rq-check" key=${k}><input type="checkbox" checked=${!!value[k]} onChange=${e => set(k, e.target.checked)} />${label}</label>`;
    if (Array.isArray(p.enum)) return html`<label class="field" key=${k}>${label}<select class="input" value=${value[k] ?? ''} onChange=${e => set(k, e.target.value)}>
      <option value=""></option>${p.enum.map((v, i) => html`<option value=${v}>${(p.enumNames || [])[i] || v}</option>`)}</select></label>`;
    const num = p.type === 'number' || p.type === 'integer';
    return html`<label class="field" key=${k}>${label}${p.description && html`<small>${p.description}</small>`}
      <input class="input" type=${num ? 'number' : p.format === 'email' ? 'email' : 'text'} value=${value[k] ?? ''} onInput=${e => set(k, num ? (e.target.value === '' ? undefined : Number(e.target.value)) : e.target.value)} /></label>`;
  })}</div>`;
}

const OUTCOME = () => ({ answered: t('chat.request.answered'), accepted: t('chat.request.answered'), declined: t('chat.request.declined'), cancelled: t('chat.tools.demande_annulee') });

export function RequestCard({ r, resolved, sessionId }) {
  const [answers, setAnswers] = useState({});
  const [content, setContent] = useState({});
  const [busy, setBusy] = useState(false);
  const qs = r.questions || [];
  const send = async body => { setBusy(true); await resolve(sessionId, r.id, body); setBusy(false); };
  const head = resolved ? OUTCOME()[resolved.outcome] || t('chat.request.answered') : r.kind === 'elicitation' && r.url ? t('chat.request.link') : t('chat.request.title');
  if (resolved) return html`<div class="approval done rq"><div class="ap-h"><${Icon} n="chat" /><span>${head}</span></div>
    ${qs.length > 0 && html`<div class="ap-tool"><span>${qs.map(q => q.question).join(' · ')}</span></div>`}${!qs.length && r.message && html`<div class="ap-tool"><span>${r.message}</span></div>`}</div>`;
  const required = (r.schema && r.schema.required) || [];
  const ready = r.kind === 'user_input' ? qs.every(q => (answers[q.id] || []).length)
    : r.url || required.every(k => content[k] !== undefined && content[k] !== '');
  return html`<div class="approval rq">
    <div class="ap-h"><${Icon} n="chat" /><span>${head}</span></div>
    ${r.message && html`<div class="rq-msg">${r.message}</div>`}
    ${r.kind === 'user_input' && qs.map(q => html`<${Question} key=${q.id} q=${q} single=${qs.length === 1} value=${answers[q.id]} onChange=${v => setAnswers({ ...answers, [q.id]: v })} />`)}
    ${r.kind === 'elicitation' && !r.url && html`<${SchemaForm} schema=${r.schema} value=${content} onChange=${setContent} />`}
    ${r.kind === 'elicitation' && r.url && html`<a class="btn sm" href=${r.url} target="_blank" rel="noopener noreferrer"><${Icon} n="link" />${t('chat.request.open_link')}</a>`}
    <div class="ap-acts">
      ${r.kind === 'user_input'
        ? html`<button class="btn sm primary" disabled=${busy || !ready} onClick=${() => send({ answers })}>${t('chat.request.send')}</button>`
        : html`<button class="btn sm primary" disabled=${busy || !ready} onClick=${() => send(r.url ? { decision: 'accept' } : { decision: 'accept', content })}>${r.url ? t('chat.request.done') : t('chat.request.send')}</button>`}
      <button class="btn sm ghost" disabled=${busy} onClick=${() => send({ decision: r.kind === 'user_input' ? 'cancel' : 'decline' })}>${t('chat.request.skip')}</button>
    </div>
  </div>`;
}
