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
  const codeman = {
    '/sessions': sessions,
    '/sessions/unified': { sessions: history, total: history.length },
    '/clis': clis,
    '/approvals': { approvals: [{ id: 'fixture-approval', sessionId: sessions[0].id, kind: 'permission', createdAt: at, message: 'Review the generated report.' }] },
    '/reboot-restore': { sessions: [] },
    '/cases': [{ name: 'Fixture case', path: workingDir }],
    '/status': { version: '1.51.1' },
    '/settings': { runMode: 'claude', defaultWorkingDir: workingDir, cliManagementEnabled: true, voiceSettings: { provider: 'deepgram', apiKeySet: true } },
    '/cron/jobs': [job],
    '/cron/runs': runs,
    '/cron/jobs/fixture-job/runs': runs,
    '/system/update/check': { currentVersion: '1.51.1', updateAvailable: true, latestVersion: '1.52.0', selfUpdateEnabled: true, installKind: 'git' },
  };
  const observations = {
    '/api/workspace': { ...ws, runtimes: ws.runtimes.filter(r => ['local', 'cloud'].includes(r.kind)), models: [{ id: 'fixture-cloud', name: model, model, kind: 'cloud', runtime_id: 'openai-compatible', provider_id: 'fixture', provider_name: 'Fixture cloud', enabled: true, ready: true }], projects, providers },
    '/api/machines': { ok: true, machines: [remote] },
    '/api/models': [{ name: 'synthetic-model-with-a-long-name-Q4_K_M.gguf', path: '/synthetic-model-library/model.gguf', dir: '/synthetic-model-library', size: 1073741824 }],
    '/api/chat/history': { ok: true, projects, conversations: [] },
    '/api/usage': { ok: true, quotas: [], models: [{ runtime_id: 'openai-compatible', name: model, provider: 'Fixture cloud', turns: 2, reported_turns: 2, usage: { prompt_tokens: 1000, completion_tokens: 200 }, price: { input: 1, output: 2 } }] },
    '/api/usage/providers': { ok: true, providers: [] },
    '/api/runtime/sessions': { ok: true, sessions: [{ message_count: 1, id: 'synthetic', title: 'Discussion with a long title for phone checks', runtime_id: 'openai-compatible', model, updated_at: 1767225600000 }] },
  };
  return async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/estimate') return route.fulfill({ json: { ok: true, gpu_mb: 1024, vram_total_mb: 8192, ram_offload_mb: 0 } });
    if (route.request().method() !== 'GET' && path !== '/api/discussion/events') return route.fulfill({ status: 409, json: { ok: false, error: 'Read-only phone fixture' } });
    if (observations[path]) return route.fulfill({ json: observations[path] });
    if (path === '/api/codeman/config') return route.fulfill({ json: { ok: true, configured: true, url: 'http://codeman.test:3000', username: 'admin', password_set: true } });
    if (path === '/api/codeman/status') return route.fulfill({ json: { ok: true, reachable: true, auth: 'ok', role: 'admin', version: '1.51.1', ui_url: 'http://codeman.test:3000' } });
    if (path === '/api/codeman/v1/events') return route.fulfill({ contentType: 'text/event-stream', body: ': synthetic Codeman stream\n\n' });
    if (path.startsWith('/api/codeman/v1/')) {
      const data = codeman[path.slice('/api/codeman/v1'.length)];
      return data === undefined ? route.fulfill({ status: 404, json: { success: false, errorCode: 'NOT_FOUND' } }) : route.fulfill({ json: { success: true, data } });
    }
    if (path === '/api/workspaces' || path.endsWith('/workspaces')) return route.fulfill({ json: { ok: true, workspaces: [{ id: 'synthetic', name: 'A workspace with a long name', path: '/workspace/a-long-project-folder' }] } });
    return route.continue();
  };
}
