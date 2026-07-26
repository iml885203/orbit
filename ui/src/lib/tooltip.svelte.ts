import { computePosition, autoUpdate, offset, flip, shift, arrow } from '@floating-ui/dom'

export type TooltipOpts = {
  content: string
  placement?: 'top' | 'bottom' | 'left' | 'right'
  delay?: number
}

const SHOW_DELAY = 50
const HIDE_DELAY = 0

// Module-level singleton — only one tooltip is visible at a time.
//
// The `owner` field is load-bearing. Svelte action `update()` fires on every
// reactive change to the bound props, and reactive store updates (e.g. SSE
// ticks) trigger update on *every* live `use:tooltip` on the page in the
// same microtask. Without `owner === node` gating, the hovered tip's text
// gets stomped on by whichever element happens to update last.
//
// Rule when extending: any DOM mutation against `activeTip` must verify
// `activeTip.owner === node` first.
let activeTip: { el: HTMLDivElement; arrow: HTMLDivElement; cleanup: () => void; owner: HTMLElement } | null = null
let showTimer: ReturnType<typeof setTimeout> | null = null

function makeTip(content: string) {
  const el = document.createElement('div')
  el.className = 'fui-tooltip'
  el.role = 'tooltip'
  el.textContent = content
  const arrowEl = document.createElement('div')
  arrowEl.className = 'fui-tooltip-arrow'
  el.appendChild(arrowEl)
  document.body.appendChild(el)
  return { el, arrowEl }
}

function destroyTip() {
  if (showTimer) { clearTimeout(showTimer); showTimer = null }
  if (activeTip) {
    activeTip.cleanup()
    activeTip.el.remove()
    activeTip = null
  }
}

export function tooltip(node: HTMLElement, opts: TooltipOpts) {
  let current = opts

  function show() {
    destroyTip()
    if (!current.content) return
    showTimer = setTimeout(() => {
      const { el, arrowEl } = makeTip(current.content)
      const cleanup = autoUpdate(node, el, () => {
        computePosition(node, el, {
          placement: current.placement ?? 'top',
          middleware: [
            offset(8),
            flip({ padding: 8 }),
            shift({ padding: 8 }),
            arrow({ element: arrowEl }),
          ],
        }).then(({ x, y, placement, middlewareData }) => {
          el.style.left = `${x}px`
          el.style.top = `${y}px`
          el.dataset.placement = placement
          const a = middlewareData.arrow
          if (a) {
            const side = placement.split('-')[0]
            const opp = { top: 'bottom', bottom: 'top', left: 'right', right: 'left' }[side]!
            Object.assign(arrowEl.style, {
              left: a.x != null ? `${a.x}px` : '',
              top:  a.y != null ? `${a.y}px` : '',
              [opp]: '-4px',
            })
          }
        })
      })
      requestAnimationFrame(() => el.classList.add('open'))
      activeTip = { el, arrow: arrowEl, cleanup, owner: node }
    }, current.delay ?? SHOW_DELAY)
  }

  function hide() {
    if (showTimer) { clearTimeout(showTimer); showTimer = null }
    if (HIDE_DELAY === 0) destroyTip()
    else setTimeout(destroyTip, HIDE_DELAY)
  }

  node.addEventListener('mouseenter', show)
  node.addEventListener('mouseleave', hide)
  node.addEventListener('focusin', show)
  node.addEventListener('focusout', hide)

  return {
    update(next: TooltipOpts) {
      current = next
      // Only mutate the DOM if the visible tip belongs to *this* trigger;
      // otherwise we'd be rewriting some other element's tooltip text on
      // every reactive update (which is exactly the bug — activeTip is a
      // shared singleton).
      if (activeTip && activeTip.owner === node) {
        activeTip.el.firstChild!.nodeValue = next.content
      }
    },
    destroy() {
      node.removeEventListener('mouseenter', show)
      node.removeEventListener('mouseleave', hide)
      node.removeEventListener('focusin', show)
      node.removeEventListener('focusout', hide)
      // Only tear down the active tip if it belongs to this trigger.
      // If element A unmounts while element B's tip is visible, A's destroy
      // must not tear down B's tooltip.
      if (activeTip?.owner === node) destroyTip()
    },
  }
}
