// Logos des fournisseurs et harnesses (ui/next/logos). Version couleur quand
// la marque en a une ; sinon le logo monochrome prend la couleur du texte
// (OpenAI, Anthropic, xAI…). Sans logo connu : l'initiale.
import { html, cls } from '../core/lib.js';

const KNOWN = new Set(['openai', 'anthropic', 'gemini', 'google', 'xai', 'mistral', 'cohere', 'deepseek', 'qwen', 'kimi', 'zai', 'minimax', 'doubao',
  'hunyuan', 'openrouter', 'groq', 'together', 'fireworks', 'cerebras', 'deepinfra', 'ollama', 'lmstudio', 'vllm', 'antigravity', 'codex',
  'claudecode', 'pi', 'hermesagent', 'claude', 'meta', 'nvidia']);
const COLOR = new Set(['antigravity', 'cerebras', 'claudecode', 'claude', 'codex', 'cohere', 'deepinfra', 'deepseek', 'doubao', 'fireworks', 'gemini',
  'google', 'hunyuan', 'kimi', 'meta', 'minimax', 'mistral', 'nvidia', 'openrouter', 'qwen', 'together', 'vllm']);
const ALIAS = { 'claude-code': 'claudecode', hermes: 'hermesagent', 'lm studio': 'lmstudio', 'together ai': 'together', glm: 'zai', 'zhipu · glm': 'zai',
  'moonshot · kimi': 'kimi', 'x-ai': 'xai', 'z-ai': 'zai', 'moonshotai': 'kimi', 'meta-llama': 'meta' };

export function logoId(name) {
  const k = String(name || '').trim().toLowerCase();
  const id = ALIAS[k] || k.replace(/[^a-z0-9]/g, '');
  return KNOWN.has(id) ? id : '';
}

// size : '' (32px), 'sm' (20px) ou 'lg' (42px), comme .mono-tile.
export function Logo({ id, name, size }) {
  const l = id || logoId(name);
  return html`<span class=${cls('mono-tile', size)} aria-hidden="true">${!l ? String(name || '?').slice(0, 1).toUpperCase()
    : COLOR.has(l) ? html`<img class="logo-img" src=${`/next/logos/${l}-color.svg`} alt="" />`
    : html`<i class="logo" style=${`--logo:url(/next/logos/${l}.svg)`}></i>`}</span>`;
}
