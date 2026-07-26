import { subscribe } from './eventbus'

export type HistorySource = 'ui' | 'cli'
export type HistoryStatus = 'pending' | 'ok' | 'error'

export interface HistoryRecord {
  id: string
  timestamp: string
  source: HistorySource
  method?: string
  path?: string
  command?: string
  summary?: string
  hasCLI: boolean
  status: HistoryStatus
  durationMs?: number
  error?: string
}

export interface HistoryGap {
  method: string
  pathPattern: string
  summary: string
  firstSeen: string
  lastSeen: string
  count: number
}

export interface HistoryFilter {
  source?: HistorySource
  onlyNoCli?: boolean
  onlyErrors?: boolean
  limit?: number
}

class HistoryStore {
  records = $state<HistoryRecord[]>([])
  expanded = $state(false)
  connected = $state(false)

  get latest() {
    return this.records[0] ?? null
  }

  upsert(record: HistoryRecord) {
    const idx = this.records.findIndex((r) => r.id === record.id)
    if (idx >= 0) {
      this.records[idx] = { ...this.records[idx], ...record }
    } else {
      this.records = [record, ...this.records]
    }
    if (this.records.length > 500) this.records = this.records.slice(0, 500)
  }
}

export const history = new HistoryStore()

export function startHistoryStream(): () => void {
  history.connected = true
  return subscribe('history', (data) => {
    history.connected = true
    history.upsert(data as HistoryRecord)
  })
}
