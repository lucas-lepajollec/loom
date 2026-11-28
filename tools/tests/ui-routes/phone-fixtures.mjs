// Synthetic Local/Cloud observations; never generate, install or sign in.
export function phoneFixtures(ws, remote) {
  const model = 'cloud-model-with-a-long-discovered-name';
  const projects = [{ id: 'fixture-project', name: 'A project with a long title for phone layout', directory: '/workspace/a-long-project-folder', machine: remote.id, default_choice: 'fixture-cloud' }];
  const providers = [{ id: 'fixture', name: 'Fixture cloud', ready: true, endpoint: 'https://api.example.test/v1', models: [model] }];
  const at = Date.UTC(2026, 0, 1, 12);
  const workingDir = '/work/a-folder-with-a-long-name-for-phone-layout';
  const clis = [
    { id: 'claude', label: 'Claude Code', kind: 'agent', installed: true },
    { id: 'codex', label: 'Codex', kind: 'agent', installed: true },
    { id: 'omp', label: 'Oh My Pi', kind: 'agent', installed: true },
    { id: 'gemini', label: 'Gemini', kind: 'agent', installed: false, stock: true, installCommand: 'fixture-install' },
  ];
  const sessions = Array.from({ length: 10 }, (_, i) => ({
    id: 'fixture-session-' + i, mode: clis[i % 3].id, name: 'A session with a long title for phone layout ' + i, workingDir,
    pid: i === 2 ? null : 100 + i, status: i === 1 ? 'busy' : i === 3 ? 'error' : 'idle',
    lastActivityAt: at - i * 60000, ...(i === 2 ? { autoResumeAt: 4102444800000 } : {}),
  }));
  const history = Array.from({ length: 10 }, (_, i) => ({
    sessionId: 'fixture-history-' + i, mode: i === 2 ? undefined : clis[i % 3].id,
    firstPrompt: 'A past session with a long first prompt ' + i, workingDir, lastActivityAt: at - i * 60000, sources: ['history'],
    ...(i % 3 === 1 ? { resumeId: 'fixture-thread-' + i } : {}),
  }));
  const job = {
    id: 'fixture-job', name: 'A scheduled dependency review with a long title', agentType: 'claude', workingDir, enabled: true,
    promptMode: 'inline_text', promptText: 'Summarize the dependency report', inputMode: 'paste',
    scheduleType: 'daily', dailyTime: '09:00', nextRunAt: at + 3600000, lastStatus: 'prompt_sent',
    concurrencyPolicy: 'skip_if_same_agent_running',
  };
  const runs = [{ id: 'fixture-run', cronJobId: job.id, sessionId: sessions[0].id, startedAt: at, status: 'prompt_sent', triggerType: 'scheduled' }];
  let approvals = [
    { id: 'fixture-approval', sessionId: sessions[0].id, kind: 'permission', createdAt: at, message: 'Review the generated report.' },
    { id: 'fixture-reply', sessionId: sessions[4].id, kind: 'idle', createdAt: at, message: 'Which report should I summarize?' },
  ];
  const codeman = {
    '/sessions': sessions,
    '/sessions/unified': { sessions: history, total: history.length },
    '/clis': clis,
    '/reboot-restore': { sessions: [{ id: 'fixture-interrupted', name: 'An interrupted session', mode: 'claude', workingDir }] },
    '/cases': [{ name: 'Fixture case', path: workingDir }],
    '/status': { version: '1.51.1' },
    '/settings': { runMode: 'claude', defaultWorkingDir: workingDir, cliManagementEnabled: true, voiceSettings: { provider: 'deepgram', apiKeySet: true } },
    '/cron/jobs': [job],
    '/cron/runs': runs,
    '/cron/jobs/fixture-job/runs': runs,
    '/system/update/check': { currentVersion: '1.51.1', updateAvailable: true, latestVersion: '1.52.0', selfUpdateEnabled: true, installKind: 'git' },
    '/sessions/fixture-session-0/last-response': { messages: [
      { kind: 'prompt', role: 'user', text: 'Summarize the dependency report' },
      { kind: 'response', role: 'assistant', text: 'The report is ready for review.' },
    ] },
    '/sessions/fixture-session-0/terminal': { terminalBuffer: 'Fixture terminal output\r\n', captureCols: 38, paneHistoryLines: 1 },
  };
  const localModel = { name: 'synthetic-model-with-a-long-name-Q4_K_M.gguf', path: '/synthetic-model-library/model.gguf', dir: '/synthetic-model-library', size: 1073741824 };
  const engines = [
    { id: 'llama.cpp', name: 'llama.cpp', category: 'managed', status: 'active', installed: true, default_port: 8080,
      formats: ['GGUF'], capabilities: ['router', 'presets', 'vram'], install_options: ['prebuilt', 'source'] },
    { id: 'sglang', name: 'SGLang', category: 'managed', status: 'installed', installed: true, default_port: 30000,
      formats: ['Safetensors', 'FP8', 'AWQ'], capabilities: ['radix', 'structured', 'multi_gpu'], install_options: ['python'] },
    { id: 'ollama', name: 'Ollama', category: 'runtime', status: 'detected', detected_url: 'http://127.0.0.1:11434', default_port: 11434,
      formats: ['GGUF', 'Modelfile'], capabilities: ['pull', 'unload'], install_options: ['guide'] },
    { id: 'mlx', name: 'MLX', category: 'server', status: 'available', requirement_note: 'apple_silicon', default_port: 8080,
      formats: ['MLX'], capabilities: ['unified'], install_options: [] },
    { id: 'strata', name: 'Strata', category: 'runtime', status: 'available', default_port: 8080,
      formats: ['GGUF', '.strata'], capabilities: ['expert_streaming', 'speculative', 'openai', 'anthropic'], install_options: ['guide'] },
    { id: 'basalt', name: 'Basalt', category: 'runtime', status: 'available', default_port: 8080,
      formats: ['.basalt'], capabilities: ['moe', 'nvfp4', 'vision_offload', 'openai', 'anthropic'], install_options: ['guide'],
      hardware_badge: 'blackwell', install_hint: 'basalt_cuda', install_guide: 'https://github.com/jesdga95/basalt/blob/main/docs/platforms.md' },
    { id: 'mistral.rs', name: 'mistral.rs', category: 'runtime', status: 'available', default_port: 1234,
      formats: ['GGUF', 'Safetensors', 'ISQ'], capabilities: ['paged', 'isq', 'vision_audio'], install_options: ['guide'] },
    { id: 'ktransformers', name: 'KTransformers', category: 'server', status: 'available', default_port: 10002,
      formats: ['GGUF', 'Safetensors'], capabilities: ['moe', 'heterogeneous'], install_options: ['guide'] },
    { id: 'exo', name: 'exo', category: 'server', status: 'detected', detected_url: 'http://127.0.0.1:52415', default_port: 52415,
      formats: ['MLX', 'Safetensors'], capabilities: ['lan_cluster', 'model_sharding'], install_options: ['guide'] },
  ].map(engine => ({ compatible: true, installed: false, link_options: ['url'], ...engine }));
  const savedEngines = [
    { id: 'saved-ollama', name: 'Ollama on a machine with a long name', kind: 'ollama', url: 'http://127.0.0.1:11434', model: 'qwen3:8b', active: false, key_set: false },
    { id: 'saved-tabby', name: 'TabbyAPI', kind: 'tabbyapi', url: 'http://192.0.2.12:5000', model, active: false, key_set: true },
  ];
  const harnessSnippets = Object.fromEntries(['opencode', 'codex', 'claude', 'pi', 'continue', 'openai'].map(id => [id, {
    file: id === 'codex' ? '~/.codex/config.toml' : '',
    content: 'Fixture model: ' + model + '\nAPI: http://127.0.0.1:8080/v1\nKey: <LOOM_API_KEY>',
  }]));
  const observations = {
    '/api/workspace': { ...ws, runtimes: ws.runtimes.filter(r => ['local', 'cloud'].includes(r.kind)), models: [
      { id: 'fixture-cloud', name: model, model, kind: 'cloud', runtime_id: 'openai-compatible', provider_id: 'fixture', provider_name: 'Fixture cloud', enabled: true, ready: true },
      { id: 'fixture-local', name: localModel.name, model: localModel.path, kind: 'local', runtime_id: 'llama.cpp', enabled: true, ready: true },
    ], chat: { default_choice: 'fixture-local' }, projects, providers },
    '/api/chat/settings': { ok: true, default_choice: 'fixture-local' },
    '/api/machines': { ok: true, machines: [remote] },
    '/api/machines/local': { ok: true, hostname: 'Fixture workstation', os: 'Linux' },
    '/api/machines/metrics': { ok: true, metrics: { [remote.id]: { cpu: 27, cores: 4, ram_total: 8 * 1024 ** 3, ram_used: 2 * 1024 ** 3 } } },
    '/api/engines/catalog': { ok: true, engines, connections: savedEngines, active_engine: { kind: 'llama.cpp' },
      host: { os: 'linux', arch: 'amd64', accelerators: ['CPU', 'CUDA'], gpu_count: 1 } },
    '/api/engines/harness-snippets': { ok: true, model, url_local: 'http://127.0.0.1:8080/v1', url_lan: '',
      key_required: true, key_placeholder: '<LOOM_API_KEY>', snippets: harnessSnippets },
    '/api/engines/vllm': { ok: true, installed: true, running: false, job: '' },
    '/api/engines/sglang': { ok: true, installed: true, running: false, job: '' },
    '/api/engines/vllm/models': { ok: true, models: [{ id: 'Qwen/Qwen3-8B', size: 4096, servable: true }], cache: '/fixture/huggingface/hub' },
    '/api/engines/vllm/download': { ok: true, download: null },
    '/api/models': [localModel],
    '/api/presets': [{ id: 'fixture-preset', name: 'A preset with a long title', model: localModel.path, ctx: 4096 }],
    '/api/chat/history': { ok: true, projects, conversations: [] },
    '/api/mcp': {
      ok: true, tool_mode: 'smart',
      token_budget: { code_mode: true, direct_tokens: 34200, injected_tokens: 380, saved_tokens: 33820, saved_percent: 98, on_demand_tools: 42, direct_tools: 1 },
      servers: [
        { name: 'Home-assistant-with-a-very-long-server-name-for-phone-layout', description: 'A synthetic home automation server', transport: 'http',
          url: 'https://mcp.example.test/v0/stream', header_names: ['Authorization'], latency_ms: 31, enabled: true, connected: true, exposure: 'on_demand',
          tools: ['inspect_a_device_with_a_very_long_tool_name_for_phone_layout', 'list_rooms'], disabled: ['list_rooms'] },
        { name: 'Local fixture', transport: 'stdio', command: 'fixture', enabled: false, connected: false, exposure: 'direct', tools: [], disabled: [] },
      ],
    },
    '/api/mcp/gateway': { url: 'http://fixture.test/mcp/loom', token_set: false },
    '/api/mcp/gateway/mode': { ok: true, tool_mode: 'smart' },
    '/api/mcp/registry/search': { ok: true, servers: [{
      id: 'org.example/fixture-registry', title: 'Registry fixture with a long title for phone layout', description: 'A synthetic registry entry',
      suggested_name: 'fixture-registry', version: '1.2.3',
      presets: [
        { transport: 'http', type: 'streamable-http', url: 'https://mcp.example.test/long-registry-path-for-the-fixture', headers: [{ name: 'Authorization', isRequired: true, isSecret: true }] },
        { transport: 'stdio', command: 'npx', args: ['-y', '@example/fixture@1.2.3'], env_vars: [{ name: 'API_KEY', isSecret: true }] },
      ],
    }] },
    '/api/mcp/sources': {
      ok: true, suggested: [{ path: '~/.cursor/mcp.json', label: 'Cursor' }],
      sources: [{ path: '~/.claude.json', label: 'Claude Code', servers: [
        { name: 'Fixture from Claude', source: '~/.claude.json', transport: 'stdio', env_names: ['API_KEY'], header_names: [] },
      ] }],
    },
    '/api/usage': { ok: true, quotas: [], models: [{ runtime_id: 'openai-compatible', name: model, provider: 'Fixture cloud', turns: 2, reported_turns: 2, usage: { prompt_tokens: 1000, completion_tokens: 200 }, price: { input: 1, output: 2 } }] },
    '/api/usage/providers': { ok: true, providers: [] },
    '/api/runtime/sessions': { ok: true, sessions: [{ message_count: 1, id: 'synthetic', title: 'Discussion with a long title for phone checks', runtime_id: 'openai-compatible', model, updated_at: 1767225600000 }] },
  };
  return async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/estimate') return route.fulfill({ json: { ok: true, gpu_mb: 1024, vram_total_mb: 8192, ram_offload_mb: 0 } });
    if (path === '/api/chat/reset') return route.fulfill({ json: { ok: true, initial_choice: { kind: 'local', model: localModel.path, name: localModel.name } } });
    const decision = /^\/api\/codeman\/v1\/approvals\/(fixture-approval|fixture-reply)\/(answer|dismiss)$/.exec(path);
    if (decision && route.request().method() === 'POST') {
      approvals = approvals.filter(a => a.id !== decision[1]);
      return route.fulfill({ json: { success: true } });
    }
    if (path === '/api/codeman/v1/sessions/fixture-session-4/input' && route.request().method() === 'POST') return route.fulfill({ json: { success: true, data: { delivered: true } } });
    if (route.request().method() !== 'GET' && path !== '/api/discussion/events') return route.fulfill({ status: 409, json: { ok: false, error: 'Read-only phone fixture' } });
    if (observations[path]) return route.fulfill({ json: observations[path] });
    if (path === '/api/codeman/config') return route.fulfill({ json: { ok: true, configured: true, url: 'http://codeman.test:3000', username: 'admin', password_set: true } });
    if (path === '/api/codeman/status') return route.fulfill({ json: { ok: true, reachable: true, auth: 'ok', role: 'admin', version: '1.51.1', ui_url: 'http://codeman.test:3000' } });
    if (path === '/api/codeman/v1/events') return route.fulfill({ contentType: 'text/event-stream', body: 'event: hook:stop\ndata: {"sessionId":"fixture-session-5"}\n\n' });
    if (path === '/api/codeman/v1/approvals') return route.fulfill({ json: { success: true, data: { approvals } } });
    if (path.startsWith('/api/codeman/v1/')) {
      const data = codeman[path.slice('/api/codeman/v1'.length)];
      return data === undefined ? route.fulfill({ status: 404, json: { success: false, errorCode: 'NOT_FOUND' } }) : route.fulfill({ json: { success: true, data } });
    }
    if (path === '/api/workspaces' || path.endsWith('/workspaces')) return route.fulfill({ json: { ok: true, workspaces: [{ id: 'synthetic', name: 'A workspace with a long name', path: '/workspace/a-long-project-folder' }] } });
    return route.continue();
  };
}
