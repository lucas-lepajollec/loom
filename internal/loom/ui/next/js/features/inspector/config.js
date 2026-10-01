// Configuration d'un modèle ou preset au format Loom (lignes CLÉ=valeur, les
// drapeaux libres dans EXTRA_ARGS). Même format que le serveur et l'ancien
// panneau : aucune traduction, le texte produit est envoyé tel quel à /api/apply.
const unquote = v => { v = String(v || '').trim(); return (v.length > 1 && (v[0] === '"' && v.at(-1) === '"' || v[0] === "'" && v.at(-1) === "'")) ? v.slice(1, -1) : v; };
const encode = (key, val) => {
  val = String(val == null ? '' : val);
  if (key === 'SYSPROMPT') val = val.replace(/\r\n/g, '\n').replace(/\n/g, '\\n');
  val = val.trim();
  if (val === '') return '';
  return /\s/.test(val) && !val.includes('"') ? '"' + val + '"' : val;
};
const lineRe = key => new RegExp('^[ \\t]*' + key + '[ \\t]*=(.*)$', 'm');

export class Config {
  constructor(text) { this.text = String(text || ''); }
  get(key) {
    const m = this.text.match(lineRe(key));
    if (!m) return '';
    const v = unquote(m[1]);
    return key === 'SYSPROMPT' ? v.replace(/\\n/g, '\n') : v;
  }
  set(key, val) {
    const enc = encode(key, val), re = lineRe(key);
    if (enc === '') this.text = this.text.replace(new RegExp('^[ \\t]*' + key + '[ \\t]*=.*\\n?', 'm'), '').replace(/\n{3,}/g, '\n\n');
    else if (re.test(this.text)) this.text = this.text.replace(re, key + '=' + enc);
    else this.text = this.text.replace(/\s*$/, '') + '\n' + key + '=' + enc + '\n';
    return this;
  }
  // EXTRA_ARGS : drapeaux libres de llama-server.
  tokens() { return this.get('EXTRA_ARGS').split(/\s+/).filter(Boolean); }
  setTokens(t) { return this.set('EXTRA_ARGS', t.join(' ')); }
  has(flag) { return this.tokens().includes(flag); }
  flag(flag, on) { const t = this.tokens().filter(x => x !== flag); if (on) t.push(flag); return this.setTokens(t); }
  arg(flag) { const t = this.tokens(), i = t.indexOf(flag); return i >= 0 && i + 1 < t.length && !t[i + 1].startsWith('-') ? t[i + 1] : ''; }
  setArg(flag, val) {
    const t = this.tokens(), i = t.indexOf(flag);
    if (i >= 0) { const had = i + 1 < t.length && !t[i + 1].startsWith('-'); t.splice(i, had ? 2 : 1); }
    val = String(val || '').trim();
    if (val !== '') t.push(flag, val);
    return this.setTokens(t);
  }
  // Valeur d'un drapeau du catalogue llama.cpp (clé Loom dédiée ou EXTRA_ARGS).
  flagValue(f) {
    if (!f) return '';
    if (f.key) { const v = this.get(f.key); if (v) return v; }
    if (f.kind === 'bool') return this.has(f.flag) ? 'on' : this.has('--no-' + f.id) ? 'off' : '';
    return this.arg(f.flag);
  }
  setFlagValue(f, val) {
    val = String(val == null ? '' : val).trim();
    if (f.key) { this.set(f.key, val); return this.setArg(f.flag, ''); }
    if (f.kind === 'bool') { this.flag('--no-' + f.id, false); return this.flag(f.flag, val === 'on' || val === 'true' || val === '1'); }
    return this.setArg(f.flag, val);
  }
  kv() { const b = this.get('KV_TYPE'), k = this.get('KV_TYPE_K') || b, v = this.get('KV_TYPE_V') || b; return k || v ? k + '|' + v : ''; }
  setKv(val) { const [k, v] = String(val || '').split('|'); this.set('KV_TYPE', ''); this.set('KV_TYPE_K', k || ''); return this.set('KV_TYPE_V', v || ''); }
  clone() { return new Config(this.text); }
}

// Configuration courante du moteur, sans les clés machine.
const MACHINE = { MEM_MODE: 1, CRAWL4AI_URL: 1, WEB_ENGINE: 1, CUDA_VISIBLE_DEVICES: 1, HOST: 1, MEM_ENCRYPTED: 1, BACKUP_AUTO: 1, COMPACT: 1, MACHINES: 1 };
export function fromMap(cfg) {
  const c = new Config('');
  Object.keys(cfg || {}).filter(k => cfg[k] && !MACHINE[k]).sort().forEach(k => c.set(k, cfg[k]));
  return c;
}
