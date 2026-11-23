// Development builds carry a timestamp (0.2.11-dev.20261009215302): show the
// short form and keep the full one for tooltips.
export const shortVersion = v => String(v || '').replace(/^v/, '').replace(/^(\d+\.\d+\.\d+-[a-z]+)\.\d{8,}$/i, '$1');
