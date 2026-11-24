// Track each side of a unified diff. Deleted lines refer to the base revision.
export function reviewLines(diff) {
  let before, after;
  return diff.split('\n').map(text => {
    const hunk = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(text);
    if (hunk) {
      before = Number(hunk[1]);
      after = Number(hunk[2]);
      return { text, kind: 'hunk' };
    }
    if (before !== undefined) {
      if (text.startsWith('+')) return { text, kind: 'add', number: after++ };
      if (text.startsWith('-')) return { text, kind: 'del', number: before++ };
      if (text.startsWith(' ')) {
        before++;
        return { text, kind: '', number: after++ };
      }
      if (!text.startsWith('\\')) before = undefined;
    }
    return { text, kind: '' };
  });
}
