import { render, screen } from '@testing-library/svelte'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DBTankScene from './DBTankScene.svelte'

const { gsapTo } = vi.hoisted(() => ({ gsapTo: vi.fn() }))

vi.mock('gsap', () => ({ gsap: { to: gsapTo } }))

describe('DBTankScene', () => {
  beforeEach(() => gsapTo.mockClear())

  it('renders the selected project', () => {
    render(DBTankScene, { props: { state: 'ready', projectName: 'dbproject.development' } })

    expect(screen.getByRole('img', { name: /publish visualization for dbproject\.development/i })).toHaveClass('ready')
  })

  it('does not render the removed project pipe layer', () => {
    render(DBTankScene, { props: { state: 'ready' } })

    expect(screen.getByTestId('db-tank-energy-column')).toBeTruthy()
    expect(screen.getByTestId('db-tank-shell')).toBeTruthy()
    expect(screen.queryByTestId('db-tank-multi-pipe-layer')).not.toBeInTheDocument()
    expect(screen.getByTestId('db-tank-energy-rings')).toBeTruthy()
    expect(screen.getAllByTestId('db-tank-db-ring')).toHaveLength(7)
  })

  it('maps building progress to charged DB rings from bottom to top', () => {
    render(DBTankScene, { props: { state: 'building', progressPercent: 62 } })

    const rings = screen.getAllByTestId('db-tank-db-ring')
    expect(rings.filter((ring) => ring.dataset.charge === 'charged')).toHaveLength(4)
    expect(rings.filter((ring) => ring.dataset.charge === 'active')).toHaveLength(1)
    expect(rings.filter((ring) => ring.dataset.charge === 'pending')).toHaveLength(2)
  })

  it.each(['building', 'complete', 'failed'] as const)('applies the %s state class', (state) => {
    render(DBTankScene, { props: { state } })

    expect(screen.getByRole('img', { name: /local db publish visualization/i })).toHaveClass(state)
  })
})
