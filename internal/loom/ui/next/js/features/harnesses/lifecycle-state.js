// A successful latest lookup cannot establish currency without an installed version.
export const lifecycleVersion = value => String(value || '').match(/\d+(?:\.\d+)+[\w.+-]*/)?.[0] || '';
export const lifecycleCurrent = state => !!(state?.installed && lifecycleVersion(state.version) && lifecycleVersion(state.latest) && !state.update_available);
export const lifecycleResult = (state, t) => state?.result === 'repaired' ? t('harnesses.lifecycle.repaired') : state?.result === 'unchanged'
  ? t('harnesses.lifecycle.unchanged')
  : state?.result === 'updated' ? t('harnesses.lifecycle.updated_versions', { from: lifecycleVersion(state.from_version), to: lifecycleVersion(state.version) })
    : t('harnesses.lifecycle.mis_a_jour');

const channelKeys = { npm: 'harnesses.lifecycle.channel.npm', native: 'harnesses.lifecycle.channel.native', homebrew: 'harnesses.lifecycle.channel.homebrew', unknown: 'harnesses.lifecycle.channel.unknown' };
export const lifecycleChannel = (state, t) => state?.channel ? t(channelKeys[state.channel] || channelKeys.unknown) : '';
export const lifecycleInstallAction = state => state?.can_repair ? 'repair' : 'install';
