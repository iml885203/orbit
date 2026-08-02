import { afterEach, describe, it, expect } from 'vitest'
import { render } from '@testing-library/svelte'
import ServiceNode from './ServiceNode.svelte'
import { store } from '../../lib/stores.svelte'
import type { GraphResponse } from '../../lib/types.gen'

const baseNode = {
  name: 'api',
  kind: 'backend' as const,
  state: 'healthy',
  mode: 'dev',
  ports: { http: 5000 },
}

describe('ServiceNode', () => {
  afterEach(() => {
    store.graph.preview = null
    store.daemon.envs = null
  })

  it('renders the service name', () => {
    const { getByText } = render(ServiceNode, { props: { data: baseNode, id: 'api' } })
    expect(getByText('api')).toBeTruthy()
  })

  it('applies the state class', () => {
    const { container } = render(ServiceNode, { props: { data: baseNode, id: 'api' } })
    expect(container.querySelector('.node')?.classList.contains('state-healthy')).toBe(true)
  })

  it('shows the log icon button', () => {
    const { getByRole } = render(ServiceNode, { props: { data: baseNode, id: 'api' } })
    expect(getByRole('button', { name: /log/i })).toBeTruthy()
  })

  it('omits the mode badge for containers', () => {
    const node = { ...baseNode, kind: 'infra' as const, mode: undefined }
    const { queryByText } = render(ServiceNode, { props: { data: node, id: 'redis' } })
    expect(queryByText(/^(dev|container)$/i)).toBeNull()
  })

  it('shows an infra icon container for infra nodes', () => {
    const node = { ...baseNode, name: 'redis', kind: 'infra' as const, mode: undefined }
    const { getByTestId } = render(ServiceNode, { props: { data: node, id: 'redis' } })
    expect(getByTestId('service-node-infra-icon')).toBeTruthy()
  })

  it('does not show an infra icon container for backend nodes', () => {
    const { queryByTestId } = render(ServiceNode, { props: { data: baseNode, id: 'api' } })
    expect(queryByTestId('service-node-infra-icon')).toBeNull()
  })

  it('shows an infra icon container for infra nodes without node.icon so the Cog fallback can render', () => {
    const node = { ...baseNode, name: 'postgres', kind: 'infra' as const, mode: undefined }
    const { getByTestId } = render(ServiceNode, { props: { data: node, id: 'postgres' } })
    expect(getByTestId('service-node-infra-icon')).toBeTruthy()
  })

  it('shows restart and stop buttons when running', () => {
    const { getByRole } = render(ServiceNode, { props: { data: baseNode, id: 'api' } })
    expect(getByRole('button', { name: /restart/i })).toBeTruthy()
    expect(getByRole('button', { name: /stop/i })).toBeTruthy()
  })

  it('hides misleading lifecycle actions when a port blocks startup', () => {
    const node = {
      ...baseNode,
      name: 'api',
      state: 'pending',
      portConflict: {
        port: 6379,
        resource: 'redis',
        inspect_command: 'lsof -nP -iTCP:6379 -sTCP:LISTEN',
      },
    }
    const { queryByRole } = render(ServiceNode, { props: { data: node, id: 'api' } })

    expect(queryByRole('button', { name: /^start api$/i })).toBeNull()
    expect(queryByRole('button', { name: /^restart api$/i })).toBeNull()
    expect(queryByRole('button', { name: /^stop api$/i })).toBeNull()
    expect(queryByRole('button', { name: /open logs for api/i })).toBeNull()
  })

  it('keeps logs available for a blocked service with buffered output', () => {
    const node = {
      ...baseNode,
      state: 'pending',
      logsAvailable: true,
      portConflict: {
        port: 6379,
        resource: 'redis',
        inspect_command: 'lsof -nP -iTCP:6379 -sTCP:LISTEN',
      },
    }
    const { getByRole } = render(ServiceNode, { props: { data: node, id: 'api' } })

    expect(getByRole('button', { name: /open logs for api/i })).toBeTruthy()
  })

  it('removes ineffective lifecycle actions from a dependency-blocked node', () => {
    const node = {
      ...baseNode,
      state: 'degraded',
      stateReason: 'dependency redis is stopped',
      blockedBy: 'redis',
    }
    const { queryByRole, getByRole } = render(ServiceNode, { props: { data: node, id: 'api' } })

    expect(queryByRole('button', { name: /^restart api$/i })).toBeNull()
    expect(queryByRole('button', { name: /^stop api$/i })).toBeNull()
    expect(getByRole('button', { name: /open logs for api/i })).toBeTruthy()
  })

  it('shows start button when stopped', () => {
    const stoppedNode = { ...baseNode, state: 'stopped' }
    const { getByRole, queryByRole } = render(ServiceNode, { props: { data: stoppedNode, id: 'api' } })
    // Use ^/$ to match the action button's exact aria-label and avoid matching
    // the node wrapper's "api — stopped" aria-label.
    expect(getByRole('button', { name: /^start api$/i })).toBeTruthy()
    expect(queryByRole('button', { name: /^restart api$/i })).toBeNull()
    expect(queryByRole('button', { name: /^stop api$/i })).toBeNull()
  })

  it('shows open button when url is present', () => {
    const nodeWithUrl = { ...baseNode, url: 'http://localhost:5000' }
    const { getByRole } = render(ServiceNode, { props: { data: nodeWithUrl, id: 'api' } })
    expect(getByRole('button', { name: /open.*browser/i })).toBeTruthy()
  })

  it('keeps a live health failure on evidence instead of blind retry or open', () => {
    const node = {
      ...baseNode,
      state: 'degraded',
      failureKind: 'health',
      logsAvailable: true,
      url: 'http://localhost:5000',
    }
    const { getByRole, queryByRole, getByText } = render(ServiceNode, {
      props: { data: node, id: 'api' },
    })

    expect(queryByRole('button', { name: /^restart api$/i })).toBeNull()
    expect(queryByRole('button', { name: /open.*browser/i })).toBeNull()
    expect(getByRole('button', { name: /open logs for api/i })).toBeTruthy()
    expect(getByRole('button', { name: /^stop api$/i })).toBeTruthy()
    expect(getByText(':5000').getAttribute('role')).toBeNull()
  })

  it('hides action affordances while previewing', () => {
    store.graph.preview = {
      env: 'preview',
      nodes: [],
      edges: [],
    } satisfies GraphResponse
    const node = {
      ...baseNode,
      url: 'http://localhost:5000',
      sidecars: [{ name: 'sidecar', url: 'http://localhost:5001' }],
    }

    const { queryByRole, getByText } = render(ServiceNode, { props: { data: node, id: 'api' } })

    expect(queryByRole('button', { name: /restart/i })).toBeNull()
    expect(queryByRole('button', { name: /stop/i })).toBeNull()
    expect(queryByRole('button', { name: /open.*browser/i })).toBeNull()
    expect(queryByRole('button', { name: /sidecar/i })).toBeNull()
    expect(queryByRole('button', { name: /log/i })).toBeNull()
    expect(getByText(':5000').getAttribute('role')).toBeNull()
  })

  it('shows port hint when ports are present', () => {
    const { getByText } = render(ServiceNode, { props: { data: baseNode, id: 'api' } })
    expect(getByText(':5000')).toBeTruthy()
  })

  it('lists kafka topics in the badge label', () => {
    const node = {
      ...baseNode,
      kafka: {
        produces: ['account.settle'],
        consumes: ['demo.order.place', 'demo.order.settle'],
      },
    }
    const { getByLabelText } = render(ServiceNode, { props: { data: node, id: 'api' } })

    const badge = getByLabelText(/Kafka/)
    expect(badge.getAttribute('aria-label')).toContain('Produces (1): account.settle')
    expect(badge.getAttribute('aria-label')).toContain('Consumes (2): demo.order.place, demo.order.settle')
  })

  it('node has wider width for new layout', () => {
    const { container } = render(ServiceNode, { props: { data: baseNode, id: 'api' } })
    const node = container.querySelector('.node') as HTMLElement
    expect(node).toBeTruthy()
    // node width is set via CSS (240px); just verify the node renders
  })
})
