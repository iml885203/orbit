// Live ambient traffic for the Services graph. When enabled, recent traces
// (off the SSE 'trace' stream) mark their services as "recently active"; the
// graph pulses the flow-dot along edges whose endpoints are both recently
// active — an ambient "the system is moving" indicator, distinct from the
// per-trace playback replay.
//
// Membership is a sliding window of the last few traces' service lists
// (unioned), not a wall-clock TTL — that avoids depending on Date in a rune
// module and is deterministic to unit-test.

class LiveTraffic {
  enabled = $state(false)
  recentServices = $state<string[]>([])

  // Last N traces' service lists. Plain field (not $state); recentServices is
  // the reactive projection updated on each note().
  private window: string[][] = []
  private readonly cap = 6

  note(services: string[]) {
    if (!this.enabled) return
    this.window.push(services)
    if (this.window.length > this.cap) this.window.shift()
    const union: string[] = []
    for (const arr of this.window) {
      for (const s of arr) if (s && !union.includes(s)) union.push(s)
    }
    this.recentServices = union
  }

  toggle() {
    this.enabled = !this.enabled
    if (!this.enabled) this.clear()
  }

  clear() {
    this.window = []
    this.recentServices = []
  }

  isLive(service: string): boolean {
    return this.enabled && this.recentServices.includes(service)
  }

  isEdgeLive(from: string, to: string): boolean {
    return this.isLive(from) && this.isLive(to)
  }
}

export const liveTraffic = new LiveTraffic()
