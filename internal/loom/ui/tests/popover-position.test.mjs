import test from 'node:test';
import assert from 'node:assert/strict';
import { popoverPosition } from '../next/js/ui/popover-position.js';

test('anchored popovers fit narrow, resized and keyboard-offset viewports', () => {
  for (const viewport of [
    { left: 0, top: 0, width: 320, height: 568 },
    { left: 0, top: 0, width: 390, height: 320 },
    { left: 20, top: 90, width: 300, height: 260 },
    { left: 0, top: 0, width: 1280, height: 800 },
  ]) {
    const width = Math.min(420, viewport.width - 16), height = Math.min(580, viewport.height - 16);
    for (const anchor of [{ left: 250, top: 4, bottom: 36 }, { left: 1100, top: 740, bottom: 772 }]) {
      for (const place of [undefined, 'above']) {
        const result = popoverPosition(anchor, width, height, viewport, place);
        const left = parseFloat(result.left), top = parseFloat(result.top);
        assert.ok(left >= viewport.left + 8);
        assert.ok(top >= viewport.top + 8);
        assert.ok(left + width <= viewport.left + viewport.width - 8);
        assert.ok(top + height <= viewport.top + viewport.height - 8);
      }
    }
  }
});
test('desktop popover keeps its anchor and flips above when there is room', () => {
  const viewport = { left: 0, top: 0, width: 1280, height: 800 };
  assert.equal(popoverPosition({ left: 300, top: 50, bottom: 82 }, 420, 500, viewport).top, '88px');
  assert.equal(popoverPosition({ left: 300, top: 650, bottom: 682 }, 420, 500, viewport).top, '142px');
});
