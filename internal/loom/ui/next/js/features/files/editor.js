// Éditeur d'une note : TipTap (vendor/editor, chargé à la première note), qui
// lit et écrit du Markdown. Barre d'outils flottante au-dessus de la note,
// menu « / » pour insérer un bloc, enregistrement automatique. Les autres
// fichiers texte (et le Markdown brut sur demande) s'éditent en texte simple.
import { t } from '../../core/i18n.js';
import { html, useState, useEffect, useRef, cls } from '../../core/lib.js';
import { Icon } from '../../ui/icons.js';
import { DropdownButton, Menu } from '../../ui/controls.js';
import { prompt } from '../../ui/dialog.js';
import { post } from '../../core/api.js';
import { scopeQuery } from './page.js';

export const isNote = p => /\.(md|markdown)$/i.test(p);
let lib = null;
const loadLib = () => (lib = lib || import('/next/vendor/editor/editor.mjs'));

// Les images d'une note sont lues dans le second cerveau, relativement à la
// note ; les images externes ne sont pas chargées (rien ne sort de Loom).
function resolveSrc(src, notePath) {
  const s = String(src || '');
  if (!s || /^[a-z][a-z0-9+.-]*:/i.test(s) || s.startsWith('//')) return '';
  const base = notePath.includes('/') ? notePath.slice(0, notePath.lastIndexOf('/')).split('/') : [];
  const parts = s.startsWith('/') ? [] : base;
  for (const seg of s.replace(/^\/+/, '').split('/')) {
    if (seg === '..') parts.pop(); else if (seg && seg !== '.') parts.push(decodeURIComponent(seg));
  }
  return '/api/files/raw?path=' + encodeURIComponent(parts.join('/')) + scopeQuery('&');
}

// Blocs proposés par « / » et par le menu de style.
const BLOCKS = [
  { id: 'p', label: () => t('files.block_text'), mark: 'T', run: c => c.setParagraph() },
  { id: 'h1', label: () => t('files.block_h1'), mark: 'H1', hint: '#', run: c => c.setHeading({ level: 1 }) },
  { id: 'h2', label: () => t('files.block_h2'), mark: 'H2', hint: '##', run: c => c.setHeading({ level: 2 }) },
  { id: 'h3', label: () => t('files.block_h3'), mark: 'H3', hint: '###', run: c => c.setHeading({ level: 3 }) },
  { id: 'ul', label: () => t('files.block_bullets'), mark: '•', hint: '-', run: c => c.toggleBulletList() },
  { id: 'ol', label: () => t('files.block_numbers'), mark: '1.', hint: '1.', run: c => c.toggleOrderedList() },
  { id: 'task', label: () => t('files.block_tasks'), mark: '☐', hint: '[ ]', run: c => c.toggleTaskList() },
  { id: 'quote', label: () => t('files.block_quote'), mark: '“', hint: '>', run: c => c.toggleBlockquote() },
  { id: 'code', label: () => t('files.block_code'), mark: '{ }', hint: '```', run: c => c.toggleCodeBlock() },
  { id: 'hr', label: () => t('files.block_divider'), mark: '—', hint: '---', run: c => c.setHorizontalRule() },
  { id: 'table', label: () => t('files.block_table'), mark: '⊞', run: c => c.insertTable({ rows: 3, cols: 3, withHeaderRow: true }) },
  { id: 'image', label: () => t('files.image'), mark: '◫', action: ed => askImage(ed) },
];
// Le Markdown enregistré garde une seule ligne vide entre deux blocs (hors
// blocs de code) et finit par un retour à la ligne.
export function tidy(md) {
  const parts = String(md).split(/(^```[\s\S]*?^```[^\n]*$)/m);
  return parts.map((p, i) => (i % 2 ? p : p.replace(/\n{3,}/g, '\n\n'))).join('').replace(/\s*$/, '\n');
}
const norm = s => String(s).toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');

async function askLink(editor) {
  const prev = editor.getAttributes('link').href || '';
  const href = await prompt(t('files.link'), { value: prev, placeholder: 'https://…' });
  if (href === null || href === undefined) return;
  if (!href) editor.chain().focus().extendMarkRange('link').unsetLink().run();
  else editor.chain().focus().extendMarkRange('link').setLink({ href }).run();
}
async function askImage(editor) {
  const src = await prompt(t('files.image'), { placeholder: t('files.image_hint') });
  if (src) editor.chain().focus().setImage({ src }).run();
}

// Bureau : bulle de formatage discrète au-dessus de la sélection (façon Notion /
// Linear), les blocs passant par « / ». Mobile : barre ancrée compacte en bas.
function Toolbar({ editor, narrow }) {
  const [, tick] = useState(0);
  const [style, setStyle] = useState(null);
  const [pos, setPos] = useState(null);
  useEffect(() => {
    const sync = () => {
      tick(n => n + 1);
      if (narrow) return;
      const { from, to, empty } = editor.state.selection;
      if (empty || editor.isActive('codeBlock')) { setPos(null); setStyle(null); return; }
      try {
        const a = editor.view.coordsAtPos(from);
        const b = editor.view.coordsAtPos(to);
        const midX = (a.left + b.right) / 2;
        const topY = Math.min(a.top, b.top);
        const x = Math.max(150, Math.min(innerWidth - 150, midX));
        const y = topY > 64 ? topY - 46 : Math.max(a.bottom, b.bottom) + 8;
        setPos({ x, y });
      } catch (_) { setPos(null); }
    };
    const hide = () => { if (!narrow) { setPos(null); setStyle(null); } };
    editor.on('transaction', sync);
    editor.on('selectionUpdate', sync);
    editor.on('blur', hide);
    return () => {
      editor.off('transaction', sync);
      editor.off('selectionUpdate', sync);
      editor.off('blur', hide);
    };
  }, [editor, narrow]);
  if (!narrow && !pos) return null;
  const on = (name, attrs) => editor.isActive(name, attrs);
  const level = [1, 2, 3].find(l => on('heading', { level: l }));
  const styleLabel = (BLOCKS.find(b => b.id === (level ? 'h' + level : 'p')) || BLOCKS[0]).label();
  const btn = (label, active, run, body) => html`<button type="button" class="icon-btn fl-tb-b" aria-label=${label} title=${label} aria-pressed=${String(!!active)}
    onMouseDown=${e => e.preventDefault()} onClick=${run}>${body}</button>`;
  const c = () => editor.chain().focus();
  return html`<div class=${cls('fl-toolbar', narrow ? 'docked' : 'bubble')} role="toolbar" aria-label=${t('files.format')}
      style=${!narrow && pos ? `left:${pos.x}px;top:${pos.y}px` : undefined}>
    <${DropdownButton} class="sm fit" open=${style} onMouseDown=${e => e.preventDefault()} onClick=${e => setStyle(e.currentTarget)}>${styleLabel}</${DropdownButton}>
    ${style && html`<${Menu} anchor=${style} onClose=${() => setStyle(null)} items=${BLOCKS.slice(0, 4).map(b => ({ label: b.label(), icon: (b.id === 'p' ? !level : level === +b.id.slice(1)) ? 'check' : '', run: () => b.run(c()).run() }))} />`}
    <i class="fl-tb-sep"></i>
    ${btn(t('files.bold'), on('bold'), () => c().toggleBold().run(), html`<b>B</b>`)}
    ${btn(t('files.italic'), on('italic'), () => c().toggleItalic().run(), html`<i class="fl-tb-i">I</i>`)}
    ${btn(t('files.strike'), on('strike'), () => c().toggleStrike().run(), html`<s>S</s>`)}
    ${btn(t('files.code'), on('code'), () => c().toggleCode().run(), html`<span class="mono">${'</>'}</span>`)}
    ${btn(t('files.link'), on('link'), () => askLink(editor), html`<${Icon} n="link" />`)}
    ${narrow && html`<i class="fl-tb-sep"></i>
    ${btn(t('files.block_bullets'), on('bulletList'), () => c().toggleBulletList().run(), html`<svg viewBox="0 0 20 20" aria-hidden="true"><path d="M8 5.5h9M8 10h9M8 14.5h9"/><circle cx="4" cy="5.5" r=".9"/><circle cx="4" cy="10" r=".9"/><circle cx="4" cy="14.5" r=".9"/></svg>`)}
    ${btn(t('files.block_tasks'), on('taskList'), () => c().toggleTaskList().run(), html`<svg viewBox="0 0 20 20" aria-hidden="true"><rect x="3" y="3" width="14" height="14" rx="3"/><path d="M6.5 10l2.5 2.5 4.5-5"/></svg>`)}`}
  </div>`;
}

function SlashMenu({ editor, slash, pick, setIndex }) {
  if (!slash) return null;
  const items = slash.items;
  return html`<div class="fl-slash pop" role="listbox" aria-label=${t('files.insert')} style=${`left:${slash.x}px;top:${slash.y}px`}>
    <span class="item-group">${t('files.insert')}</span>
    ${items.length ? items.map((b, i) => html`<button type="button" role="option" aria-selected=${String(i === slash.index)} class="item fl-slash-i"
        onMouseDown=${e => e.preventDefault()} onMouseMove=${() => setIndex(i)} onClick=${() => pick(b)}>
      <i>${b.mark}</i><span>${b.label()}</span>${b.hint && html`<kbd>${b.hint}</kbd>`}</button>`)
      : html`<span class="fl-slash-none">${t('files.no_block')}</span>`}
  </div>`;
}

// Enregistrement : 0,9 s après la dernière frappe. Un conflit (la note a
// changé ailleurs) arrête l'enregistrement jusqu'au choix de l'utilisateur.
function useSaver({ path, file, onState, onSaved }) {
  const modified = useRef(file.modified);
  const timer = useRef(0), pending = useRef(null), blocked = useRef(false);
  const flush = async force => {
    clearTimeout(timer.current);
    if (pending.current === null) return;
    const content = pending.current;
    onState('saving');
    const r = await post('/api/files/write' + scopeQuery(), { path, content, modified: force ? 0 : modified.current });
    if (r.conflict) { blocked.current = true; onState('conflict'); return; }
    if (r.ok === false) { onState('dirty'); return; }
    if (pending.current === content) pending.current = null;
    modified.current = r.modified; blocked.current = false;
    onState(pending.current === null ? 'saved' : 'dirty'); onSaved(r.modified);
  };
  const change = content => {
    pending.current = content; onState(blocked.current ? 'conflict' : 'dirty');
    if (blocked.current) return;
    clearTimeout(timer.current); timer.current = setTimeout(() => flush(false), 900);
  };
  useEffect(() => () => { clearTimeout(timer.current); if (pending.current !== null && !blocked.current) flush(false); }, []);
  return { change, flush };
}

function Conflict({ onReload, onKeep }) {
  return html`<div class="fl-conflict" role="alert"><${Icon} n="alert" /><span>${t('files.conflict')}</span>
    <button class="btn sm" onClick=${onReload}>${t('files.reload')}</button><button class="btn sm" onClick=${onKeep}>${t('files.keep_mine')}</button></div>`;
}

export function NoteEditor({ path, file, raw, onState, onSaved, onReload, narrow }) {
  const host = useRef(null);
  const [editor, setEditor] = useState(null);
  const [failed, setFailed] = useState('');
  const [conflict, setConflict] = useState(false);
  const [slash, setSlashState] = useState(null);
  const slashRef = useRef(null);
  const setSlash = s => { slashRef.current = s; setSlashState(s); };
  const state = s => { setConflict(s === 'conflict'); onState(s); };
  const saver = useSaver({ path, file, onState: state, onSaved });

  // Menu « / » : un paragraphe qui ne contient que « /mot » avant le curseur.
  const updateSlash = ed => {
    const { $from, empty } = ed.state.selection;
    const text = $from.parent.textContent.slice(0, $from.parentOffset);
    const m = empty && $from.parent.type.name === 'paragraph' && /^\/([^\s/]*)$/.exec(text);
    if (!m) { if (slashRef.current) setSlash(null); return; }
    const q = norm(m[1]);
    const items = BLOCKS.filter(b => norm(b.label()).includes(q) || b.id.startsWith(q));
    const at = ed.view.coordsAtPos($from.pos);
    const height = 40 + Math.max(1, items.length) * 35;
    const below = innerHeight - at.bottom > height + 12;
    setSlash({ from: $from.start(), to: $from.pos, items, index: Math.min(slashRef.current?.index || 0, Math.max(0, items.length - 1)),
      x: Math.min(at.left, innerWidth - 272), y: below ? at.bottom + 6 : Math.max(8, at.top - height - 6) });
  };
  const pickBlock = (ed, b) => {
    const s = slashRef.current;
    if (!s) return;
    setSlash(null);
    if (b.action) {
      ed.chain().focus().deleteRange({ from: s.from, to: s.to }).run();
      b.action(ed);
      return;
    }
    b.run(ed.chain().focus().deleteRange({ from: s.from, to: s.to })).run();
  };

  useEffect(() => {
    if (raw) return;
    let ed = null, alive = true;
    loadLib().then(m => {
      if (!alive || !host.current) return;
      const NoteImage = m.Image.extend({ renderHTML({ HTMLAttributes }) { return ['img', { ...HTMLAttributes, src: resolveSrc(HTMLAttributes.src, path) }]; } });
      ed = new m.Editor({
        element: host.current,
        extensions: [
          m.StarterKit.configure({ link: { openOnClick: false, autolink: true } }),
          m.Markdown,
          m.TaskList, m.TaskItem.configure({ nested: true }),
          m.TableKit.configure({ table: { resizable: false } }),
          NoteImage,
          m.Placeholder.configure({ placeholder: t('files.placeholder') }),
        ],
        content: file.content || '',
        contentType: 'markdown',
        editorProps: {
          attributes: { class: 'fl-doc', spellcheck: 'true' },
          handleKeyDown: (view, e) => {
            const s = slashRef.current;
            if (!s) return false;
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); const n = s.items.length || 1; setSlash({ ...s, index: (s.index + (e.key === 'ArrowDown' ? 1 : n - 1)) % n }); return true; }
            if (e.key === 'Enter' && s.items[s.index]) { e.preventDefault(); pickBlock(ed, s.items[s.index]); return true; }
            if (e.key === 'Escape') { setSlash(null); return true; }
            return false;
          },
        },
        onUpdate: ({ editor: e }) => { saver.change(tidy(e.getMarkdown())); updateSlash(e); },
        onSelectionUpdate: ({ editor: e }) => updateSlash(e),
      });
      setEditor(ed);
    }).catch(e => setFailed(e.message || String(e)));
    return () => { alive = false; if (ed) ed.destroy(); };
  }, [raw]);

  const conflictBar = conflict && html`<${Conflict} onReload=${onReload} onKeep=${() => saver.flush(true)} />`;
  if (raw) return html`<div class="fl-body fl-raw">${conflictBar}
    <textarea class="fl-raw-in mono" spellcheck="false" aria-label=${path} defaultValue=${file.content} onInput=${e => saver.change(e.target.value)}></textarea></div>`;
  return html`<div class="fl-body">
    ${conflictBar}
    ${editor && html`<${Toolbar} editor=${editor} narrow=${narrow} />`}
    ${failed && html`<p class="muted">${failed}</p>`}
    <div class="fl-doc-wrap">
      <div class="fl-doc-host" ref=${host}></div>
      ${editor && html`<${SlashMenu} editor=${editor} slash=${slash} pick=${b => pickBlock(editor, b)} setIndex=${i => setSlash({ ...slashRef.current, index: i })} />`}
    </div>
  </div>`;
}
