import { render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { devStore } from '$lib/devdb/stores.svelte'
import TunnelPage from './TunnelPage.svelte'

const { fetchTunnels } = vi.hoisted(() => ({ fetchTunnels: vi.fn() }))
vi.mock('./api', () => ({ fetchTunnels }))

describe('TunnelPage', () => {
  afterEach(() => {
    devStore.devMeta = null
    vi.clearAllMocks()
  })

  it('guards the direct route when the active environment has no tunnel workflow', () => {
    devStore.devMeta = {
      environment_path: '/workspace/orbit.yaml',
      environment_name: 'local',
      sql_server_image: '',
      workspace_root: '/workspace',
      db_configured: false,
      claim_configured: false,
    }

    render(TunnelPage)

    expect(screen.getByRole('status')).toHaveTextContent('Tunnel workflow is not available')
    expect(screen.queryByRole('button', { name: 'Create tunnel' })).not.toBeInTheDocument()
    expect(fetchTunnels).not.toHaveBeenCalled()
  })
})
