// Keep anchored menus inside the visible viewport, including the mobile keyboard.
export function popoverPosition(anchor, width, height, viewport, place) {
  const leftEdge = viewport.left + 8, topEdge = viewport.top + 8;
  const rightEdge = viewport.left + viewport.width - 8, bottomEdge = viewport.top + viewport.height - 8;
  const left = Math.max(leftEdge, Math.min(anchor.left, rightEdge - width));
  let top = place === 'above' ? anchor.top - height - 8 : anchor.bottom + 6;
  if (top + height > bottomEdge) top = anchor.top - height - 8;
  top = Math.max(topEdge, Math.min(top, bottomEdge - height));
  return { left: left + 'px', top: top + 'px', transformOrigin: place === 'above' ? 'bottom left' : 'top left' };
}
