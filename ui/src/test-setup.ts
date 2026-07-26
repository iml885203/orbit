import '@testing-library/jest-dom/vitest'
import { vi } from 'vitest'

// jsdom doesn't implement matchMedia; svelte/reactivity's MediaQuery
// requires it. Return a permanent "doesn't match" so tests don't have
// to know which query is being asked.
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })
}

// jsdom doesn't implement the Web Animations API; Svelte 5's transitions
// call element.animate(). Return a finished no-op so components that render
// with a transition (modals, drawers) mount instead of throwing.
if (typeof Element !== 'undefined' && !Element.prototype.animate) {
  Element.prototype.animate = () =>
    ({
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      addEventListener() {},
      removeEventListener() {},
      finished: Promise.resolve(),
      onfinish: null,
      oncancel: null,
      currentTime: 0,
      startTime: 0,
      playState: 'finished',
      playbackRate: 1,
    }) as unknown as Animation
}

// jsdom doesn't implement ResizeObserver; the trace waterfall (and other
// size-aware components) construct one on mount. A no-op keeps them mountable.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}

vi.mock('./components/ServiceControls.svelte', () => ({ default: () => null }))

vi.mock('@xyflow/svelte', () => ({
  Handle: () => null,
  Position: { Top: 'top', Bottom: 'bottom' },
  SvelteFlow: () => null,
  SvelteFlowProvider: ({ children }: any) => children,
  Background: () => null,
  Controls: () => null,
  BaseEdge: () => null,
  getBezierPath: () => ['', 0, 0],
}))
