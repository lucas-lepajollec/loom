// Runtime and model together identify an execution; a filename alone does not.
export const executionKey = c => JSON.stringify([c.mode, c.session?.runtime_id, c.session?.provider_id, c.session?.provider_name, c.session?.model]);
const exact = value => String(value || '').replace(/\\/g, '/');
export function localChoice(models, target) {
  const value = exact(target.path || target.model);
  if (!value) return null;
  const matches = (models || []).filter(m => m.kind === 'local' && m.enabled !== false &&
    (exact(m.model) === value || (!target.path && exact(m.engine_value) === value)));
  return matches.length === 1 ? matches[0] : null;
}
