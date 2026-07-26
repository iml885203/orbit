/**
 * floatingArrow.ts
 *
 * Shared helper for positioning a floating-ui arrow element after
 * computePosition resolves. Used by tooltip.svelte.ts; ready for
 * EdgeInfoPopover and any future floating panel that needs an arrow.
 *
 * Usage:
 *   computePosition(...).then(({ placement, middlewareData }) => {
 *     applyArrowPosition(arrowEl, middlewareData, placement)
 *   })
 */

/**
 * applyArrowPosition sets the inline styles on `arrowEl` so it points
 * toward the reference element from the given `placement`.
 *
 * @param arrowEl      The arrow DOM element (must be inside the floating el).
 * @param middlewareData  The `middlewareData` object from computePosition.
 * @param placement    The resolved placement string (e.g. "top", "bottom-start").
 */
export function applyArrowPosition(
  arrowEl: HTMLElement,
  middlewareData: Record<string, unknown>,
  placement: string
): void {
  const a = middlewareData.arrow as { x?: number; y?: number } | undefined
  if (!a) return

  const side = placement.split('-')[0] as 'top' | 'bottom' | 'left' | 'right'
  const opposite: Record<string, string> = {
    top: 'bottom',
    bottom: 'top',
    left: 'right',
    right: 'left',
  }
  const opp = opposite[side]

  Object.assign(arrowEl.style, {
    left: a.x != null ? `${a.x}px` : '',
    top: a.y != null ? `${a.y}px` : '',
    [opp]: '-4px',
  })
}
