// Scroll a LogPanel line (identified by its data-line-index) into view and
// flash it. Shared by LogModal and the trace detail page so the jump + flash
// routine and its 600ms timing live in one place. The `.flash` class drives a
// keyframe defined in each host's styles.
export function flashLogLine(host: HTMLElement | undefined | null, index: number): boolean {
  if (!host) return false
  const el = host.querySelector(`[data-line-index="${index}"]`) as HTMLElement | null
  if (!el) return false
  el.scrollIntoView({ block: 'center' })
  el.classList.add('flash')
  setTimeout(() => el.classList.remove('flash'), 600)
  return true
}
