// Runs in the browser. Use text ranges as well as element boxes: a pill may
// have a 24px box while its wrapped text paints over the description below.
export function phoneLayoutIssues() {
  const issues = [], width = document.documentElement.clientWidth, tolerance = 2;
  const visible = el => {
    const r = el.getBoundingClientRect(), s = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none' && !!el.getClientRects().length;
  };
  const name = el => el.tagName.toLowerCase() + (el.className && typeof el.className === 'string' ? '.' + el.className.trim().replace(/\s+/g, '.') : '') + ' ' + el.textContent.trim().slice(0, 70);
  const roots = [...document.querySelectorAll('.view, .dialog, .pop, .tipbox')].filter(visible);
  const elements = [...new Set(roots.flatMap(el => [el, ...el.querySelectorAll('*')]))].filter(visible);
  for (const el of elements) {
    const r = el.getBoundingClientRect();
    // Exclude intentionally hidden drawer/tab strip content. Check the strip's
    // own bounds, but not the tabs outside its clipped scroll area.
    let clipped = false;
    for (let p = el.parentElement; p && !roots.includes(el); p = p.parentElement) {
      if (p.matches('.tabs, .insp-scroll, .pick-list, .thread') && /auto|hidden|scroll/.test(getComputedStyle(p).overflowX)) { clipped = true; break; }
    }
    if (!clipped && (r.left < -tolerance || r.right > width + tolerance)) issues.push('outside viewport: ' + name(el));
    if (el.matches('button, .btn') && r.width > width + tolerance) issues.push('button wider than viewport: ' + name(el));
    if (el.matches('h1,h2,h3,h4,.pill,.state,.tag,.btn,button,.am-head span')) {
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      for (let n; (n = walker.nextNode());) {
        if (n.textContent.trim().length < 4 || !visible(n.parentElement)) continue;
        const range = document.createRange(); range.selectNodeContents(n);
        const rects = [...range.getClientRects()].filter(r => r.width > 0);
        const lines = new Set(rects.map(r => Math.round(r.top)));
        const box = n.parentElement.getBoundingClientRect();
        if (lines.size > 1 && box.width < 40) issues.push('text column below 40px: ' + name(n.parentElement));
        if (n.parentElement.matches('.pill,.state,.tag,.btn') && rects.some(t => t.bottom > box.bottom + tolerance)) issues.push('text paints outside control: ' + name(n.parentElement));
      }
    }
  }
  for (const head of elements.filter(el => el.matches('.page-head,.agent-h,.agent-t,.h-head,.sec-h,.prov-h,.dialog-head,.hx-top,.mc-title,.am-head'))) {
    const children = [...head.children].filter(visible).filter(el => getComputedStyle(el).position !== 'absolute');
    for (let i = 0; i < children.length; i++) for (let j = i + 1; j < children.length; j++) {
      const a = children[i].getBoundingClientRect(), b = children[j].getBoundingClientRect();
      if (Math.min(a.right, b.right) - Math.max(a.left, b.left) > tolerance && Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > tolerance) issues.push('overlapping header siblings: ' + name(head));
    }
  }
  for (const el of roots) if (el.scrollWidth > el.clientWidth + tolerance) issues.push('horizontal overflow: ' + name(el));
  return [...new Set(issues)];
}
