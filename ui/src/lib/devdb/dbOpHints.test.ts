import { describe, it, expect } from 'vitest'
import { dbOpHint } from './dbOpHints'

describe('dbOpHint', () => {
  const hint = (lines: string[], errorCode?: string) => dbOpHint('publish', lines, errorCode)

  it.each([
    ['toolchain_missing', /dotnet tool install -g microsoft\.sqlpackage/],
    ['sql_project_not_found', /sqlserver\.projects/],
    ['dacpac_artifact_missing', /artifact root.*project directory.*expected leaf/],
    ['build_failed', /build errors/],
    ['publish_blocked_data_loss', /--force/],
    ['sql_server_unavailable', /configured SQL Server target/],
    ['database_busy', /active connections/],
    ['publish_failed', /operation log/],
    ['reference_unresolved', /referenced artifact.*beside the project dacpac/],
    ['reset_clean_state_missing', /Run Reset again/],
    ['reset_restore_failed', /connections.*disk space.*retry/],
    ['reset_prepare_failed', /connections.*disk space.*retry/],
  ])('maps %s to an actionable hint', (errorCode, expected) => {
    expect(hint(['unrecognized output'], errorCode)).toMatch(expected)
  })

  it('prefers an error code over output matching', () => {
    expect(hint(['might result in data loss'], 'toolchain_missing')).toMatch(/dotnet tool install/)
  })

  it('recognizes data-loss blocks', () => {
    const h = hint(['...', 'Error SQL72031: rows were detected. The schema update is terminating because data loss might occur.'])
    expect(h).toMatch(/data loss/)
  })
  it('recognizes unreachable sql-server', () => {
    expect(hint(['A network-related or instance-specific error occurred'])).toMatch(/orbit status/)
  })
  it('prefers the latest matching line', () => {
    const h = hint([
      'Login failed for user SA',
      'later: might result in data loss',
    ])
    expect(h).toMatch(/data loss/)
  })
  it('returns null on unknown output', () => {
    expect(hint(['everything is fine', 'done'])).toBeNull()
  })
})

it('maps reset_partial to a direct recovery action', () => {
  const hint = dbOpHint('reset', [], 'reset_partial')
  expect(hint).toContain('discarded local data')
  expect(hint).toContain('run Reset again')
})
