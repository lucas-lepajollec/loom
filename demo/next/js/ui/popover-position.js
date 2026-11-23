// Keep anchored menus inside the visible viewport, including the mobile keyboard.
export function popoverPosition(anchor, width, height, viewport, place) {
  const leftEdge = viewport.left + 8, topEdge = viewport.top + 8;
  const rightEdge = viewport.left + viewport.width - 8, bottomEdge = viewport.top + viewport.height - 8;
  const left = Math.max(leftEdge, Math.min(anchor.left, rightEdge - width));
  const above = Math.min(anchor.top - 8, bottomEdge);
  const below = Math.max(anchor.bottom + 6, topEdge);
  const spaceAbove = Math.max(0, above - topEdge);
  const spaceBelow = Math.max(0, bottomEdge - below);
  const opensAbove = place === 'above'
    ? height <= spaceAbove || spaceAbove > spaceBelow
    : height > spaceBelow && spaceAbove > spaceBelow;
  const maxHeight = opensAbove ? spaceAbove : spaceBelow;
  const top = opensAbove ? above - Math.min(height, maxHeight) : below;
  return {
    left: left + 'px',
    top: top + 'px',
    maxHeight: maxHeight + 'px',
    transformOrigin: opensAbove ? 'bottom left' : 'top left',
  };
}
