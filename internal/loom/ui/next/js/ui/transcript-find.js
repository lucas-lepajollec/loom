import { html, useState } from '../core/lib.js';
import { t } from '../core/i18n.js';
import { Modal } from './dialog.js';
import { Icon } from './icons.js';

export function TranscriptFind() {
  const [messages, setMessages] = useState(null);
  const [query, setQuery] = useState('');
  function open(event) {
    const pane = event.currentTarget.closest('.cm-main,.chat-col');
    const nodes = pane?.querySelectorAll('.thread .msg-user .bubble, .thread .msg-ai') || [];
    setMessages([...nodes].map(node => ({
      node, text: (node.querySelector('.md') || node).textContent.trim(),
      author: node.matches('.bubble') ? t('workspace.you') : t('workspace.assistant'),
    })).filter(item => item.text));
    setQuery('');
  }
  function reveal(node) {
    setMessages(null);
    requestAnimationFrame(() => {
      if (!node.isConnected) return;
      node.scrollIntoView({ block: 'center' });
      node.tabIndex = -1;
      node.focus({ preventScroll: true });
    });
  }
  const needle = query.trim().toLowerCase();
  const hits = needle ? messages?.filter(item => item.text.toLowerCase().includes(needle)) : [];
  return html`<button type="button" class="icon-btn transcript-search-trigger" title=${t('workspace.find_messages')} aria-label=${t('workspace.find_messages')} onClick=${open}><${Icon} n="search" /></button>
    ${messages && html`<${Modal} class="transcript-find" title=${t('workspace.find_messages')} onClose=${() => setMessages(null)}>
      <label class="search"><${Icon} n="search" /><input type="search" aria-label=${t('workspace.find_messages')} value=${query}
        placeholder=${t('workspace.find_messages')} onInput=${e => setQuery(e.target.value)} /></label>
      ${needle && html`<div role="status" class="find-count">${t('workspace.find_count', { n: hits.length })}</div>`}
      <div class="find-list">${hits.map(item => {
        const index = item.text.toLowerCase().indexOf(needle);
        return html`<button type="button" onClick=${() => reveal(item.node)}><b>${item.author}</b>
          <span>${index > 55 ? '…' : ''}${item.text.slice(Math.max(0, index - 55), index)}<mark>${item.text.slice(index, index + needle.length)}</mark>${item.text.slice(index + needle.length, index + needle.length + 120)}</span></button>`;
      })}</div>
    </${Modal}>`}`;
}
