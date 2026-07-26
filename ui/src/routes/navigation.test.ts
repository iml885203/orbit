import { describe, expect, it } from 'vitest'
import { navItems, routes } from './index'

describe('primary navigation', () => {
  it('keeps diagnostics routable without making it a primary workspace', () => {
    expect(navItems.map((item) => item.label)).not.toContain('Health Check')
    expect(Object.prototype.hasOwnProperty.call(routes, '/healthcheck')).toBe(true)
  })
})
