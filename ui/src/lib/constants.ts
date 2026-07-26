export const ICONS: Record<string, string> = {
  healthy: '●',
  building: '◐',
  starting: '◐',
  degraded: '◑',
  stopped: '○',
  pending: '○',
  stopping: '◔',
}

export const COLORS: Record<string, string> = {
  healthy: 'var(--green)',
  building: 'var(--blue)',
  starting: 'var(--yellow)',
  degraded: 'var(--red)',
  stopped: 'var(--dim)',
  pending: 'var(--dim)',
  stopping: 'var(--yellow)',
}

export const MAX_LINES = 500
