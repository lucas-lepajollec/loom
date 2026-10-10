// All controls use the backend contract for the selected transport.
export const modeOptions = (features, modes) => (modes || []).filter(m => (features?.modes || []).includes(m.id));
export const configOptions = (features, options) => (options || []).filter(o => (features?.config_options || []).includes(o.id));
export const permissionOptions = (features, options) => (options || []).filter(o => (features?.permissions || []).includes(o.value));

export const protocolLabel = features => ({ 'app-server': 'App Server', 'pi-rpc': 'RPC', 'opencode-http': 'HTTP', 'agy-stream-json': 'Stream JSON', acp: 'ACP' })[features?.protocol] || '';

// Accepted versions and implementation flags never imply installation success.
export const capabilityEvidenceLevel = evidence => evidence?.observed_working ? 'observed_working' : evidence?.protocol_verified ? 'protocol_verified' : evidence?.discovered ? 'discovered' : 'unknown';
export const capabilityEvidenceLabel = (evidence, translate) => (evidence?.health === 'degraded' ? translate('agents.evidence.degraded') + ' · ' : '') + translate('agents.evidence.' + capabilityEvidenceLevel(evidence));
