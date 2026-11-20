import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render } from '@testing-library/svelte'
import { tick } from 'svelte'
import { Position, type EdgeProps } from '@xyflow/svelte'
import DependencyEdge from './DependencyEdge.svelte'
import { store } from '$lib/stores.svelte'
import type { GraphResponse } from '$lib/types.gen'

const graph = (env: string, state: string): GraphResponse => ({ env,
  nodes: [{ name: 'web', kind: 'frontend', state }, { name: 'api', kind: 'backend', state }],
  edges: [{ from: 'web', to: 'api', kind: 'sync', detachable: true, detached: false }] })
afterEach(() => { cleanup(); store.graph.dispose(); store.graph.data = null })

describe('dependency edge active snapshot', () => {
  it('uses same-name preview endpoints instead of the retained live snapshot', async () => {
    store.graph.data = graph('live', 'healthy')
    store.graph.preview = graph('preview', 'stopped')
    const props: EdgeProps = { id: 'web-api', type: 'dependency', source: 'web', target: 'api',
      sourceX: 0, sourceY: 0, targetX: 100, targetY: 100,
      sourcePosition: Position.Bottom, targetPosition: Position.Top,
      selected: false, animated: false, selectable: true, deletable: false,
      data: { ...store.graph.preview.edges[0], sourceOffset: 0, targetOffset: 0 } }
    const view = render(DependencyEdge, { props })
    expect(view.container.querySelector('.flow-dot')).toBeNull()
    store.graph.exitPreview()
    await tick()
    expect(view.container.querySelector('.flow-dot')).not.toBeNull()
  })
})
