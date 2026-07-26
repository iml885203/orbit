type LogLevel = 'error' | 'warn' | 'info'
type LogSource = 'orbit' | 'init' | 'settings' | 'devdb' | 'poller' | 'daemon' | 'pre_start' | 'app'

interface Classified {
  level: LogLevel
  source: LogSource
}

const SOURCE_PREFIXES: { prefix: string; source: LogSource }[] = [
  { prefix: '[orbit]', source: 'orbit' },
  { prefix: '[init]', source: 'init' },
  { prefix: '[settings]', source: 'settings' },
  { prefix: '[devdb]', source: 'devdb' },
  { prefix: '[poller]', source: 'poller' },
  { prefix: '[daemon]', source: 'daemon' },
  { prefix: '[pre_start]', source: 'pre_start' },
]

// "[pre_start] exit N" lines: zero is success (info), non-zero is failure
// (error). Without this, "exit 0" would stay info but "exit 7" wouldn't be
// caught by ERROR_PATTERNS either — the word "exit" isn't in our keyword
// list, and intentionally so (it appears too often in non-error contexts).
const PRE_START_EXIT = /^\[pre_start\] exit (\d+)\b/

// Lines like "0 Error(s)" or "1 Error(s)" are dotnet build summaries —
// the digit-prefix proves they're a count, not an error report.
const SUMMARY_COUNT = /\b\d+\s+Error\(s\)/

// Bracket-level prefix: a line that opens with "[ ... LVL ... ]" where
// LVL is a known level token. Matches the common shape used by most
// structured loggers, regardless of where the timestamp / source /
// fields sit inside the brackets. Authoritative when present — overrides
// keyword scanning, so a line like "[22:47 INF] Exception caught in
// middleware" stays info instead of being upgraded to error by the
// keyword "Exception".
const BRACKET_LEVEL_PREFIX =
  /^\[[^\]]*\b(DBG|DEBUG|INF|INFO|WAR|WARN|WARNING|ERR|ERROR|FTL|FATAL)\b[^\]]*\]/

const ERROR_PATTERNS = [
  /\berror[: ]/i,
  /Exception\b/,
  /\bfailed\b/i,
  /\bERR\b/,
]

const WARN_PATTERNS = [
  /\bwarning\b/i,
  /\bWARN\b/,
]

export function classify(line: string): Classified {
  let source: LogSource = 'app'
  for (const { prefix, source: src } of SOURCE_PREFIXES) {
    if (line.startsWith(prefix)) {
      source = src
      break
    }
  }

  let level: LogLevel = 'info'
  if (source === 'pre_start') {
    const exitMatch = PRE_START_EXIT.exec(line)
    if (exitMatch) {
      return { level: exitMatch[1] === '0' ? 'info' : 'error', source }
    }
  }
  const structured = BRACKET_LEVEL_PREFIX.exec(line)
  if (structured) {
    const lvl = structured[1]
    if (lvl === 'ERR' || lvl === 'ERROR' || lvl === 'FTL' || lvl === 'FATAL') level = 'error'
    else if (lvl === 'WAR' || lvl === 'WARN' || lvl === 'WARNING') level = 'warn'
    // DBG/DEBUG/INF/INFO stay 'info'.
  } else if (!SUMMARY_COUNT.test(line)) {
    // warn first: lines like "[orbit] warning: reconnect failed" mention both
    // "warning" and "failed"; the explicit "warning" prefix wins.
    if (WARN_PATTERNS.some((re) => re.test(line))) {
      level = 'warn'
    } else if (ERROR_PATTERNS.some((re) => re.test(line))) {
      level = 'error'
    }
  }

  return { level, source }
}

// isNarration reports whether a line's source is orbit-emitted commentary
// (lifecycle, pre_start, settings, etc.) rather than the service's own
// stdout. Templates use this to apply a single dim style instead of
// listing every narration source in CSS, which would drift each time a
// new source is added.
export function isNarration(source: LogSource): boolean {
  return source !== 'app'
}

export function findErrorIndices(lines: string[]): number[] {
  const out: number[] = []
  for (let i = 0; i < lines.length; i++) {
    if (classify(lines[i]).level === 'error') out.push(i)
  }
  return out
}

// Plain-text continuation markers (non-indented lines that still belong
// to a previous stack trace). Indented lines are caught by /^\s/ above.
const CONTINUATION_PREFIXES = [
  'Caused by:',
  '--- End of',  // dotnet "--- End of stack trace from previous location ---"
]

function isContinuation(line: string): boolean {
  if (line.length === 0) return false
  // A new structured log line ends the previous group, no matter what.
  if (BRACKET_LEVEL_PREFIX.test(line)) return false
  // Any leading whitespace (spaces or tabs, any indent depth) is a
  // stack-frame line. Covers `   at ...` (.NET 3-space), `\tat ...`
  // (Java/Kotlin), 4-space, and everything in between.
  if (/^\s/.test(line)) return true
  return CONTINUATION_PREFIXES.some((p) => line.startsWith(p))
}

// LineMeta carries everything the renderer needs about a single line:
// classified level, source, and the index of the error-group head this
// line belongs to (-1 if not in a group). Computed in one pass so callers
// don't reclassify the same line three times across derived chains.
interface LineMeta {
  level: LogLevel
  source: LogSource
  head: number
}

// analyzeLines walks lines once, classifying each and tracking error-group
// continuation. Use this in components instead of calling classify +
// errorGroupMap + groupErrors separately.
export function analyzeLines(lines: string[]): LineMeta[] {
  const out: LineMeta[] = new Array(lines.length)
  let head = -1
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (line === '') {
      head = -1
      out[i] = { level: 'info', source: 'app', head: -1 }
      continue
    }
    if (head !== -1 && isContinuation(line)) {
      const c = classify(line)
      out[i] = { level: c.level, source: c.source, head }
      continue
    }
    head = -1
    const c = classify(line)
    if (c.level === 'error') {
      head = i
      out[i] = { level: c.level, source: c.source, head: i }
    } else {
      out[i] = { level: c.level, source: c.source, head: -1 }
    }
  }
  return out
}

// groupErrors returns the index of each error-group head, in order.
// Thin wrapper over analyzeLines for callers that only need head indices.
export function groupErrors(lines: string[]): number[] {
  const meta = analyzeLines(lines)
  const out: number[] = []
  for (let i = 0; i < meta.length; i++) {
    if (meta[i].head === i) out.push(i)
  }
  return out
}

// errorGroupMap returns, for each line, the index of its error-group head
// (or -1). Thin wrapper over analyzeLines.
export function errorGroupMap(lines: string[]): number[] {
  return analyzeLines(lines).map((m) => m.head)
}
