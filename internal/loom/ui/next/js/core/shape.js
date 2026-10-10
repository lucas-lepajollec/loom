// Shared JSON/store boundary. Validate containers before publishing them;
// never coerce an error object into an empty successful observation.
export const isObject = value => value !== null && typeof value === 'object' && !Array.isArray(value);
export function validShape(value, shape) {
  if (typeof shape === 'function') return shape(value);
  if (Array.isArray(shape)) return Array.isArray(value) && value.every(x => x != null && (!shape.length || validShape(x, shape[0])));
  if (isObject(shape)) return isObject(value) && Object.entries(shape).every(([k, s]) => !(k in value) || validShape(value[k], s));
  return shape === null ? value === null || isObject(value) : typeof value === typeof shape;
}

// Partial store writes preserve each last valid field, including nested lists.
export function shapePatch(previous, patch, schema = previous) {
  if (!isObject(patch)) return {};
  const accepted = {};
  for (const [key, value] of Object.entries(patch)) {
    const shape = schema[key];
    if (!(key in schema) || (value?.ok !== false && validShape(value, shape))) accepted[key] = value;
  }
  return accepted;
}

// Explicit read contracts; opaque model/tool payloads and JSON schemas are not
// recursively interpreted as Loom collections. Null optional Go slices remain
// valid (consumers already use an empty list for those fields).
const list = shape => value => value == null || validShape(value, [shape]);
const rows = list({}), strings = list('');
const optional = shape => value => value === null || validShape(value, shape);
const option = value => validShape(value, {}) && (!('options' in value) || options(value.options));
const options = list(option);
const changedFiles = list(value => validShape(value, {}) && typeof value.path === 'string');
const features = { modes: strings, config_options: strings, permissions: strings, model_sources: strings, filesystem_policies: strings };
const harness = { modes: rows, config: options, files: changedFiles, commands: rows, usage: null, features: optional(features) };
const context = { items: rows, skills: rows, reference_ids: strings, brain_citations: strings };
const session = { messages: rows, turns: list({ events: rows }), available_modes: rows, available_config_options: options, changed_files: changedFiles, commands: rows, context: optional(context) };
const runtime = { capabilities: strings, degraded: rows, filesystem_policies: strings, features: optional(features), compatibility: null };
const project = { capability_ids: strings, mcp_servers: strings, brain_sources: strings };
const choice = { reasoning_efforts: strings };
const workspace = { models: list(choice), runtimes: list(runtime), providers: list({ models: strings }), projects: list(project), capabilities: rows, sessions: list(session), harness_profiles: rows };
export const chatShape = { harness, items: rows, frozen: rows, session: optional(session), context: optional(context), gen: null };
// Only the flat form fields rendered by RequestCard are interpreted. Arbitrary
// tool payloads and the rest of a harness's JSON schema stay opaque.
const form = { required: strings, properties: value => value == null || isObject(value) && Object.values(value).every(v => validShape(v, { title: '', description: '', enum: list(value => value === null || ['string', 'number', 'boolean'].includes(typeof value)), enumNames: strings })) };
const event = { type: '', text: '', error: '', seq: 0, session: optional(session), context: optional(context), state: null, modes: rows, options, files: list(value => typeof value === 'string' || validShape(value, {})), commands: rows, entries: rows, request: optional({ questions: list({ options: rows }), schema: optional(form) }), approval: optional({ options: rows }), tool: optional({ diffs: rows, locations: rows }), provenance: optional({ events: rows }) };
export const eventShape = value => validShape(value, event) && (value.type !== 'files' || changedFiles(value.files));
const shapes = {
  '/api/models': [value => validShape(value, { name: '' }) && typeof value.name === 'string'], '/api/presets': [{ id: '', name: '' }], '/api/vram': [{}], '/api/backends': [{}],
  '/api/models/download/status': [{}], '/api/agents/catalog': [{}], '/api/backends/custom': [{}],
  '/api/chat/upload': { id: '', path: '' },
  '/api/status': { active: false, health: false, model: '', model_name: '', preset_name: '' }, '/api/ping': { version: '', hostname: '' }, '/api/engine/node': { remote: false, degraded: rows, kind: '', models: strings },
  '/api/workspace': workspace, '/api/chat/history': { conversations: rows, projects: list(project) },
  '/api/runtime/sessions': { sessions: list(session), session: optional(session), context: optional(context) },
  '/api/agent': { pages: list(value => validShape(value, {}) && typeof value.name === 'string'), skills: list(value => validShape(value, {}) && typeof value.name === 'string') }, '/api/providers': { providers: list({ models: strings }) },
  '/api/providers/models': { models: [''] }, '/api/providers/save': { provider: optional({ models: strings }) },
  '/api/machines': { machines: [{ harnesses: strings, modules: strings, gpus: rows }], offers: value => value == null || isObject(value) && Object.values(value).every(rows) },
  '/api/machines/local': { gpus: rows, harnesses: strings }, '/api/machines/metrics': { metrics: null },
  '/api/machines/discover': { nodes: rows }, '/api/machines/folders': { folders: strings },
  '/api/agents/installations': { installations: rows }, '/api/terminals': { terminals: rows }, '/api/previews': { previews: rows },
  '/api/tasks': { tasks: list(value => validShape(value, { status: '', requests: rows }) && typeof value.status === 'string') },
  '/api/server': { slots: optional({ items: rows }) }, '/api/voice/download/status': { download: null },
  '/api/voice': { engine: null, config: null, service: null, download: null },
  '/api/voice/packs': { packs: rows }, '/api/voice/models': { models: list({ id: '', languages: strings }) },
  '/api/voice/node': { machines: rows }, '/api/voice/jarvis': { routes: rows, voice: null },
  '/api/usage': { models: rows, quotas: list({ windows: rows }) }, '/api/usage/native': { harnesses: list({ by_model: rows }) }, '/api/usage/providers': { providers: rows },
  '/api/brain/sources': { sources: list({ include: strings, exclude: strings }) }, '/api/brain/search': { hits: list({ heading: strings, highlights: rows, snippet: '' }) }, '/api/brain/read': { heading: strings, text: '' }, '/api/brain/agents': { agents: list(value => validShape(value, {}) && typeof value.id === 'string') },
  '/api/brain/memory': { items: list({ name: '', description: '', text: '' }) }, '/api/brain/memory/status': { last_operations: rows }, '/api/brain/semantic': { models: rows },
  '/api/skills/home': { detected: rows }, '/api/skills/home/adopt': { copied: strings, conflicts: strings }, '/api/skills/targets': { targets: list({ harnesses: [''] }) },
  '/api/skills/sources': { sources: rows, suggested: rows }, '/api/skills/apply': { targets: rows },
  '/api/mcp': { servers: list(value => validShape(value, { args: strings, tools: rows, disabled: strings }) && typeof value.name === 'string') },
  '/api/mcp/portable': { servers: rows }, '/api/mcp/gateway': { harnesses: rows }, '/api/mcp/sources': { sources: list({ servers: list({ env_names: strings, header_names: strings }) }), suggested: rows }, '/api/mcp/sources/adopt': { env_to_fill: strings },
  '/api/harness-history': { sources: rows, sessions: rows }, '/api/harness/custom': { agents: list({ args: strings }) },
  '/api/harness/lifecycle': { state: optional({ errors: strings, requires_missing: strings }) }, '/api/harness/bindings': { mcp: rows },
  '/api/harness/model-source': { models: 0 },
  '/api/engine/params': { params: list({ choices: list([]) }) }, '/api/model-caps': { mmproj: strings, effort: strings },
  '/api/hub/search': { models: list(value => validShape(value, {}) && typeof value.id === 'string') }, '/api/hub/model': { files: list(value => validShape(value, {}) && typeof value.name === 'string') }, '/api/models/dirs': { dirs: rows },
  '/api/env/services': { services: rows }, '/api/env/matrix': { results: rows }, '/api/env/docker': { containers: rows }, '/api/env/proxmox/resources': { resources: rows },
  '/api/bench/tests': { tests: rows }, '/api/bench/catalog': { models: list(choice) }, '/api/bench/queue': { job: optional({ rows }), rows }, '/api/bench/runs': { runs: list({ rows }) },
  '/api/engines/vllm': { job: '', download: null }, '/api/engines/vllm/download': { download: null }, '/api/engines/vllm/models': { models: rows }, '/api/engines/vllm/hub/search': { models: rows }, '/api/engines/vllm/params': { params: list({ choices: list([]) }), values: {} },
  '/api/policy': { rules: list(value => validShape(value, { conditions: optional({ command_prefixes: strings }) }) && typeof value.id === 'string') }, '/api/policy/audit': { entries: rows }, '/api/doctor/run': { checks: rows },
  '/api/startup': { services: rows, engines: optional({ vllm_models: strings }) }, '/api/https': { urls: strings }, '/api/fs/dirs': { dirs: list({ name: '' }), path: '' },
  '/api/mem/snapshots': { snapshots: rows }, '/api/llamacpp/job': { lines: strings }, '/api/prefs': { prefs: {} },
  '/api/discussion/events': eventShape,
  '/api/runtime/sessions/preview': { preview: optional({ messages: rows, context: optional(context) }) },
  '/api/notify': { config: optional({ rules: optional({ events: strings }) }) },
  '/api/backends/devices': { devices: rows },
  '/api/context/preferences': { selection: optional({ page: '', external: false }) },
};
export function validData(value, url = '') {
  if (value?.ok === false) return false;
  const path = url.split('?')[0];
  // POST probe on this same endpoint returns a direct engine catalog, while
  // GET and link/unlink return the selected node. Preserve both contracts.
  if (path === '/api/engine/node' && isObject(value) && !('remote' in value) && typeof value.kind === 'string') return validShape(value.models, ['']);
  let shape = shapes[path] || {};
  const required = { '/api/workspace': ['models', 'runtimes', 'projects', 'providers', 'capabilities'], '/api/status': ['active'], '/api/ping': ['version'], '/api/engine/node': ['remote'], '/api/chat/history': ['conversations', 'projects'], '/api/usage': ['models', 'quotas'], '/api/usage/native': ['harnesses'], '/api/usage/providers': ['providers'], '/api/engines/vllm/models': ['models', 'cache'], '/api/engines/vllm/download': ['download'], '/api/tasks': ['tasks'], '/api/fs/dirs': ['path', 'dirs'] }[path];
  if (required && (!isObject(value) || !required.every(k => k in value))) return false;
  if (path === '/api/runtime/sessions' && (!isObject(value) || !('session' in value || 'sessions' in value))) return false;
  if (/^\/api\/runtime\/sessions\/(create|select|local|configure|continue|compact|import)$/.test(path)) shape = shapes['/api/runtime/sessions'];
  if (/^\/api\/runtimes\/[^/]+\/inspect$/.test(path)) shape = { inspection: optional({ mcp: rows, skills: rows }) };
  if (/^\/api\/runtimes\/[^/]+\/probe$/.test(path)) shape = { probe: optional({ ...harness, degraded: rows }) };
  if (/^\/api\/runtimes\/[^/]+\/sessions$/.test(path)) shape = { sessions: rows };
  if (/^\/api\/runtimes\/[^/]+\/quota$/.test(path)) shape = { quota: optional({ windows: rows }) };
  if (/^\/api\/machines\/[^/]+\/(node\/)?startup$/.test(path)) shape = shapes['/api/startup'];
  if (/^\/api\/(machines\/[^/]+\/)?workspaces$/.test(path)) shape = { workspaces: rows };
  if (path === '/api/bench/queue/cancel') shape = shapes['/api/bench/queue'];
  if (path === '/api/bench/tests/delete') shape = shapes['/api/bench/tests'];
  return validShape(value, shape);
}
