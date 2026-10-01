// Catalogue des fournisseurs connus. Tous exposent une API compatible Chat
// Completions (adresses vérifiées le 2026-10-01) ; ajouter un fournisseur =
// ajouter une ligne ici. logo : fichier de ui/next/logos.
// usage : le fournisseur accepte stream_options.include_usage (décompte des tokens).
// nokey : serveur local qui n'exige pas de clé.
export const GROUPS = [
  { name: 'Grands laboratoires', items: [
    { name: 'OpenAI', logo: 'openai', hint: 'GPT, séries o', endpoint: 'https://api.openai.com/v1', usage: true },
    { name: 'Anthropic', logo: 'anthropic', hint: 'Claude', endpoint: 'https://api.anthropic.com/v1' },
    { name: 'Google', logo: 'gemini', hint: 'Gemini', endpoint: 'https://generativelanguage.googleapis.com/v1beta/openai' },
    { name: 'xAI', logo: 'xai', hint: 'Grok', endpoint: 'https://api.x.ai/v1', usage: true },
    { name: 'Mistral', logo: 'mistral', hint: 'Mistral, Codestral', endpoint: 'https://api.mistral.ai/v1' },
    { name: 'Cohere', logo: 'cohere', hint: 'Command', endpoint: 'https://api.cohere.ai/compatibility/v1' },
  ] },
  { name: 'Chine', items: [
    { name: 'DeepSeek', logo: 'deepseek', hint: 'DeepSeek V et R', endpoint: 'https://api.deepseek.com/v1', usage: true },
    { name: 'Qwen', logo: 'qwen', hint: 'Alibaba Cloud', endpoint: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', usage: true },
    { name: 'Kimi', logo: 'kimi', hint: 'Moonshot AI', endpoint: 'https://api.moonshot.ai/v1' },
    { name: 'GLM', logo: 'zai', hint: 'Zhipu · Z.ai', endpoint: 'https://api.z.ai/api/paas/v4' },
    { name: 'MiniMax', logo: 'minimax', hint: 'MiniMax', endpoint: 'https://api.minimax.io/v1' },
    { name: 'Doubao', logo: 'doubao', hint: 'ByteDance · BytePlus', endpoint: 'https://ark.ap-southeast.bytepluses.com/api/v3' },
    { name: 'Hunyuan', logo: 'hunyuan', hint: 'Tencent', endpoint: 'https://api.hunyuan.cloud.tencent.com/v1' },
  ] },
  { name: 'Agrégateurs et inférence rapide', items: [
    { name: 'OpenRouter', logo: 'openrouter', hint: 'Des centaines de modèles', endpoint: 'https://openrouter.ai/api/v1', usage: true },
    { name: 'Groq', logo: 'groq', hint: 'Inférence très rapide', endpoint: 'https://api.groq.com/openai/v1', usage: true },
    { name: 'Together AI', logo: 'together', hint: 'Modèles ouverts', endpoint: 'https://api.together.xyz/v1', usage: true },
    { name: 'Fireworks', logo: 'fireworks', hint: 'Modèles ouverts', endpoint: 'https://api.fireworks.ai/inference/v1', usage: true },
    { name: 'Cerebras', logo: 'cerebras', hint: 'Inférence rapide', endpoint: 'https://api.cerebras.ai/v1', usage: true },
    { name: 'DeepInfra', logo: 'deepinfra', hint: 'Modèles ouverts', endpoint: 'https://api.deepinfra.com/v1/openai', usage: true },
  ] },
  { name: 'Local et auto-hébergé', items: [
    { name: 'Ollama', logo: 'ollama', hint: '127.0.0.1:11434', endpoint: 'http://127.0.0.1:11434/v1', usage: true, nokey: true },
    { name: 'LM Studio', logo: 'lmstudio', hint: '127.0.0.1:1234', endpoint: 'http://127.0.0.1:1234/v1', nokey: true },
    { name: 'vLLM', logo: 'vllm', hint: '127.0.0.1:8000', endpoint: 'http://127.0.0.1:8000/v1', usage: true, nokey: true },
    { name: 'Compatible OpenAI', hint: 'N’importe quelle URL', endpoint: '', usage: true, custom: true },
  ] },
];

export const ALL = GROUPS.flatMap(g => g.items);
const host = u => { try { return new URL(u).host.toLowerCase(); } catch (_) { return ''; } };
// Associe un fournisseur enregistré à son entrée de catalogue (par hôte).
export const entryFor = p => ALL.find(e => e.endpoint && host(e.endpoint) === host(p.endpoint));
