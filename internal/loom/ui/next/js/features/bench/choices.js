const reasonKeys = {
  no_model_only_mode: 'bench.reason.no_model_only_mode',
  remote_windows_unsupported: 'bench.reason.remote_windows_unsupported',
  cli_unavailable: 'bench.reason.cli_unavailable',
  native_model_required: 'bench.reason.native_model_required',
  provider_disconnected: 'bench.reason.provider_disconnected',
};
// Bench visibility is independent from the discussion picker. Unsupported
// native modes remain visible with a reason, never silently run as an agent.
export function benchChoices(models, t) {
  return models.filter(m => ['cloud', 'harness'].includes(m.kind) && !m.via).map(m => ({
    key: m.id, name: m.name, logo: m.provider_name, provider: m.provider_name,
    kind: m.kind, external: true, supported: m.supported === true,
    sub: m.provider_name + ' · ' + (m.supported ? t(m.kind === 'cloud' ? 'bench.api' : 'bench.native') : t(reasonKeys[m.reason] || 'bench.reason.no_model_only_mode')),
    v: { choice_id: m.id, name: m.name },
  }));
}

export function benchMethod(row, t) {
  if (row.kind === 'account') return t('bench.native');
  if (row.kind === 'cloud') return t('bench.api');
  return t(row.result?.prompt_per_second != null ? 'bench.engine_timings' : 'bench.engine_request');
}
