// Text attachments are portable user text, not server paths or harness tools.
// Lengths allow exact round trips even when the paste contains our delimiters.
export const PASTE_THRESHOLD = 2500;
export const MAX_MESSAGE_BYTES = 64 << 10;
const START = '--- Loom text attachments ---\n';
const END = '\n--- End Loom text attachments ---';

export function attachPaste(draft, start, end, content, number) {
  if (content.length <= PASTE_THRESHOLD) return null;
  return { text: draft.slice(0, start) + draft.slice(end), caret: start,
    file: { name: `pasted-text-${number}.txt`, content } };
}

export function pastedMessage(text, files) {
  const pasted = files.filter(f => typeof f.content === 'string');
  if (!pasted.length) return text.trim();
  return (text.trim() ? text.trim() + '\n\n' : '') + START + pasted.map(f =>
    `${f.name} (${f.content.length} UTF-16 units)\n${f.content}`).join('\n') + END;
}

// Files attached to a cloud or agent turn travel as <loom-file> blocks
// (workspace_attachments.go): shown as file pills, not as raw text.
const LOOM_FILE = /\n*<loom-file name="((?:[^"\\]|\\.)*)"([^>]*?)(?:\/>|>\n([\s\S]*?)\n<\/loom-file>)/g;
function splitLoomFiles(text) {
  const files = [];
  const rest = text.replace(LOOM_FILE, (_, name, attrs, content) => {
    files.push({ name: name.replace(/\\(.)/g, '$1'), content: content === undefined ? null : content, note: /omitted|truncated/.test(attrs) ? attrs.trim() : '' });
    return '';
  }).replace(/\n*Attached files \(read them from these paths\):\s*$/, '');
  return files.length ? { text: rest.trimEnd(), files } : null;
}

export function splitPastedMessage(text) {
  text = String(text || '');
  const attached = splitLoomFiles(text);
  if (attached) return attached;
  const fallback = { text, files: [] };
  const start = text.indexOf(START);
  if (start < 0 || !text.endsWith(END)) return fallback;
  let pos = start + START.length;
  const limit = text.length - END.length, files = [];
  while (pos < limit && files.length < 32) {
    const header = /^(pasted-text-\d+\.txt) \((\d+) UTF-16 units\)\n/.exec(text.slice(pos));
    if (!header) return fallback;
    pos += header[0].length;
    const size = Number(header[2]);
    if (!Number.isSafeInteger(size) || pos + size > limit) return fallback;
    files.push({ name: header[1], content: text.slice(pos, pos + size) });
    pos += size;
    if (pos < limit) { if (text[pos] !== '\n') return fallback; pos++; }
  }
  return pos === limit && files.length ? { text: text.slice(0, start).trimEnd(), files } : fallback;
}

export function downloadPaste(file) {
  const url = URL.createObjectURL(new Blob([file.content], { type: 'text/plain;charset=utf-8' }));
  const a = document.createElement('a');
  a.href = url; a.download = file.name; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
