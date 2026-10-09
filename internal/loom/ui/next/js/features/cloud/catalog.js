import { t } from '../../core/i18n.js';
// Catalogue des fournisseurs connus. Tous exposent une API compatible Chat
// Completions (adresses vérifiées le 2026-10-01) ; ajouter un fournisseur =
// ajouter une ligne ici. logo : fichier de ui/next/logos.
// usage : le fournisseur accepte stream_options.include_usage (décompte des tokens).
// nokey : serveur local qui n'exige pas de clé.
export const GROUPS = [
  { get name() { return t("cloud.catalog.grands_laboratoires"); }, items: [
    { name: 'OpenAI', logo: 'openai', get hint() { return t("cloud.catalog.gpt_series_o"); }, endpoint: 'https://api.openai.com/v1', usage: true },
    { name: 'Anthropic', logo: 'anthropic', get hint() { return t("cloud.catalog.claude"); }, endpoint: 'https://api.anthropic.com/v1' },
    { name: 'Google', logo: 'google', get hint() { return t("cloud.catalog.google_ai"); }, endpoint: 'https://generativelanguage.googleapis.com/v1beta/openai' },
    { name: 'xAI', logo: 'xai', get hint() { return t("cloud.catalog.grok"); }, endpoint: 'https://api.x.ai/v1', usage: true },
    { name: 'Mistral', logo: 'mistral', get hint() { return t("cloud.catalog.mistral_codestral"); }, endpoint: 'https://api.mistral.ai/v1' },
    { name: 'Cohere', logo: 'cohere', get hint() { return t("cloud.catalog.command"); }, endpoint: 'https://api.cohere.ai/compatibility/v1' },
  ] },
  { get name() { return t("cloud.catalog.chine"); }, items: [
    { name: 'DeepSeek', logo: 'deepseek', get hint() { return t("cloud.catalog.deepseek_v_et_r"); }, endpoint: 'https://api.deepseek.com/v1', usage: true },
    { name: 'Qwen', logo: 'qwen', get hint() { return t("cloud.catalog.alibaba_cloud"); }, endpoint: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', usage: true },
    { name: 'Kimi', logo: 'kimi', get hint() { return t("cloud.catalog.moonshot_ai"); }, endpoint: 'https://api.moonshot.ai/v1' },
    { name: 'GLM', logo: 'zai', get hint() { return t("cloud.catalog.zhipu_z_ai"); }, endpoint: 'https://api.z.ai/api/paas/v4' },
    { name: 'MiniMax', logo: 'minimax', get hint() { return t("cloud.catalog.minimax"); }, endpoint: 'https://api.minimax.io/v1' },
    { name: 'Doubao', logo: 'doubao', get hint() { return t("cloud.catalog.bytedance_byteplus"); }, endpoint: 'https://ark.ap-southeast.bytepluses.com/api/v3' },
    { name: 'Hunyuan', logo: 'hunyuan', get hint() { return t("cloud.catalog.tencent"); }, endpoint: 'https://api.hunyuan.cloud.tencent.com/v1' },
  ] },
  { get name() { return t("cloud.catalog.agregateurs_et_inference_rapide"); }, items: [
    { name: 'OpenRouter', logo: 'openrouter', get hint() { return t("cloud.catalog.des_centaines_de_modeles"); }, endpoint: 'https://openrouter.ai/api/v1', usage: true },
    { name: 'Groq', logo: 'groq', get hint() { return t("cloud.catalog.inference_tres_rapide"); }, endpoint: 'https://api.groq.com/openai/v1', usage: true },
    { name: 'Together AI', logo: 'together', get hint() { return t("cloud.catalog.modeles_ouverts"); }, endpoint: 'https://api.together.xyz/v1', usage: true },
    { name: 'Fireworks', logo: 'fireworks', get hint() { return t("cloud.catalog.modeles_ouverts"); }, endpoint: 'https://api.fireworks.ai/inference/v1', usage: true },
    { name: 'Cerebras', logo: 'cerebras', get hint() { return t("cloud.catalog.inference_rapide"); }, endpoint: 'https://api.cerebras.ai/v1', usage: true },
    { name: 'DeepInfra', logo: 'deepinfra', get hint() { return t("cloud.catalog.modeles_ouverts"); }, endpoint: 'https://api.deepinfra.com/v1/openai', usage: true },
  ] },
  { get name() { return t("cloud.catalog.local_et_auto_heberge"); }, items: [
    { name: 'Ollama', logo: 'ollama', hint: '127.0.0.1:11434', endpoint: 'http://127.0.0.1:11434/v1', usage: true, nokey: true },
    { name: 'LM Studio', logo: 'lmstudio', hint: '127.0.0.1:1234', endpoint: 'http://127.0.0.1:1234/v1', nokey: true },
    { name: 'vLLM', logo: 'vllm', hint: '127.0.0.1:8000', endpoint: 'http://127.0.0.1:8000/v1', usage: true, nokey: true },
    { get name() { return t("cloud.catalog.compatible_openai"); }, get hint() { return t("cloud.catalog.n_importe_quelle_url"); }, endpoint: '', usage: true, custom: true },
  ] },
];

export const ALL = GROUPS.flatMap(g => g.items);
const host = u => { try { return new URL(u).host.toLowerCase(); } catch (_) { return ''; } };
// Associe un fournisseur enregistré à son entrée de catalogue (par hôte).
export const entryFor = p => ALL.find(e => e.endpoint && host(e.endpoint) === host(p.endpoint));
