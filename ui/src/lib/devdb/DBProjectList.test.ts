import { fireEvent, render, screen } from '@testing-library/svelte'
import { describe, expect, it, vi } from 'vitest'
import DBProjectList from './DBProjectList.svelte'
import type { DevDBProject } from '$lib/types.gen'

const projects: DevDBProject[] = [
  { name: 'dbproject.development', path: '/repo/dbproject.development', databases: ['AppDB'] },
  { name: 'dbproject.game', path: '/repo/dbproject.game', databases: ['OrdersDB'] },
  { name: 'dbproject.common', path: '/repo/dbproject.common', databases: [] },
]

describe('DBProjectList', () => {
  it('summarizes the project count', () => {
    render(DBProjectList, { props: { projects, onSelect: vi.fn() } })
    expect(screen.getByText('3 SQL projects')).toBeInTheDocument()
  })

  it('singularizes a lone project', () => {
    render(DBProjectList, { props: { projects: [projects[0]], onSelect: vi.fn() } })
    expect(screen.getByText('1 SQL project')).toBeInTheDocument()
  })

  it('marks the selected project row via aria-pressed', () => {
    render(DBProjectList, {
      props: { projects, selectedPath: '/repo/dbproject.development', onSelect: vi.fn() },
    })
    expect(screen.getByRole('button', { name: /select dbproject\.development/i })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: /select dbproject\.game/i })).toHaveAttribute('aria-pressed', 'false')
  })

  it('calls onSelect when a row is clicked', async () => {
    const onSelect = vi.fn()
    render(DBProjectList, {
      props: { projects, onSelect },
    })
    await fireEvent.click(screen.getByRole('button', { name: /select dbproject\.development/i }))
    expect(onSelect).toHaveBeenCalledWith(projects[0])
  })

  it('renders a project icon for each project', () => {
    render(DBProjectList, {
      props: { projects, onSelect: vi.fn() },
    })
    const icons = document.querySelectorAll('.project-icon')
    expect(icons.length).toBe(projects.length)
  })

  it('renders a chevron only for projects with databases', () => {
    render(DBProjectList, {
      props: { projects, onSelect: vi.fn() },
    })
    const chevrons = document.querySelectorAll('.row-chevron')
    expect(chevrons.length).toBe(2)
  })

  it('does not render any rebuild button', () => {
    render(DBProjectList, {
      props: { projects, onSelect: vi.fn() },
    })
    expect(screen.queryByRole('button', { name: /rebuild/i })).not.toBeInTheDocument()
  })

  it('keeps projects without databases selectable', async () => {
    const onSelect = vi.fn()
    render(DBProjectList, {
      props: {
        projects,
        onSelect,
      },
    })
    await fireEvent.click(screen.getByRole('button', { name: /select dbproject\.common/i }))
    expect(onSelect).toHaveBeenCalledWith(projects[2])
  })
})
