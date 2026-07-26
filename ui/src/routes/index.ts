import type { RouteDefinition } from 'svelte-spa-router'
import MainPage from './MainPage.svelte'
import HealthCheckPage from './HealthCheckPage.svelte'
import TracingPage from './TracingPage.svelte'
import { navItems as extNavItems, routes as extRoutes } from '$ext'

export type NavItem = {
  path: string
  label: string
  // Evaluated per render; the tab hides while it returns true. Lives on
  // the nav item so gating knowledge stays with the item it gates.
  hidden?: () => boolean
}

// Core tabs first, extension tabs spliced where the feature pages have
// always sat (after Services) so the layout doesn't move underfoot.
export const navItems: NavItem[] = [
  { path: '/', label: 'Services' },
  ...extNavItems,
  { path: '/tracing', label: 'Tracing' },
]

export const routes: RouteDefinition = {
  '/': MainPage,
  ...extRoutes,
  '/tracing': TracingPage,
  // Same component as /tracing: the detail renders as a routed modal over the
  // list, so the URL stays shareable while the list keeps its state.
  '/tracing/:traceId': TracingPage,
  '/healthcheck': HealthCheckPage,
  '*': MainPage,
}
