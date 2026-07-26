import { describe, it, expect } from 'vitest'
import { classify, findErrorIndices, groupErrors, errorGroupMap, isNarration } from './logClassify'

describe('isNarration', () => {
  // App lines are the actual workload output; everything else is orbit-
  // emitted narration that should render dim. Adding a new narration
  // source must not require touching CSS selectors.
  it('returns false for app source', () => {
    expect(isNarration('app')).toBe(false)
  })

  it('returns true for every non-app source', () => {
    const sources = ['orbit', 'init', 'settings', 'devdb', 'poller', 'daemon', 'pre_start'] as const
    for (const s of sources) {
      expect(isNarration(s)).toBe(true)
    }
  })
})

describe('classify', () => {
  it('flags dotnet build error as error/app', () => {
    expect(classify('Worker.csproj : error NU1900: Unable to load')).toEqual({ level: 'error', source: 'app' })
  })

  it('classifies orbit narration as info/orbit', () => {
    expect(classify('[orbit] starting service name=worker')).toEqual({ level: 'info', source: 'orbit' })
  })

  it('classifies settings line as info/settings', () => {
    expect(classify('[settings] sql-server restart initiated')).toEqual({ level: 'info', source: 'settings' })
  })

  it('classifies poller error as error/poller', () => {
    expect(classify('[poller] error: Docker list error err="EOF"')).toEqual({ level: 'error', source: 'poller' })
  })

  it('classifies devdb info as info/devdb', () => {
    expect(classify('[devdb] rebuild succeeded — restarting sql-server')).toEqual({ level: 'info', source: 'devdb' })
  })

  it('classifies init line as info/init', () => {
    expect(classify('[init] Kafka topics created successfully')).toEqual({ level: 'info', source: 'init' })
  })

  it('classifies daemon line as info/daemon', () => {
    expect(classify('[daemon] ready')).toEqual({ level: 'info', source: 'daemon' })
  })

  it('classifies pre_start command echo as info/pre_start', () => {
    expect(classify('[pre_start] $ pnpm install --frozen-lockfile')).toEqual({ level: 'info', source: 'pre_start' })
  })

  it('classifies pre_start exit 0 as info/pre_start', () => {
    expect(classify('[pre_start] exit 0')).toEqual({ level: 'info', source: 'pre_start' })
  })

  it('classifies pre_start non-zero exit as error/pre_start', () => {
    expect(classify('[pre_start] exit 7')).toEqual({ level: 'error', source: 'pre_start' })
  })

  it('classifies pre_start multi-digit non-zero exit as error/pre_start', () => {
    expect(classify('[pre_start] exit 137')).toEqual({ level: 'error', source: 'pre_start' })
  })

  it('does NOT apply pre_start exit-code semantics to non-pre_start lines', () => {
    // The phrase "exit 7" by itself must not be treated as an error — only
    // "[pre_start] exit N" with N>0 is a failure signal. Locks the anchor
    // in PRE_START_EXIT so the rule can't be widened by accident.
    expect(classify('process said exit 7 and moved on')).toEqual({ level: 'info', source: 'app' })
  })

  it('does NOT flag "0 Error(s)" summary as error', () => {
    expect(classify('    0 Error(s)')).toEqual({ level: 'info', source: 'app' })
  })

  it('does NOT flag non-zero "Error(s)" summary as error', () => {
    expect(classify('    19 Error(s)')).toEqual({ level: 'info', source: 'app' })
  })

  it('does NOT flag "Build succeeded." as error', () => {
    expect(classify('Build succeeded.')).toEqual({ level: 'info', source: 'app' })
  })

  it('classifies bare app stdout as info/app', () => {
    expect(classify('Now listening on: http://localhost:5056')).toEqual({ level: 'info', source: 'app' })
  })

  it('classifies .NET exception as error/app', () => {
    expect(classify('System.InvalidOperationException: thing')).toEqual({ level: 'error', source: 'app' })
  })

  it('classifies "warning" line as warn/app', () => {
    expect(classify('warning CS1234: unused var')).toEqual({ level: 'warn', source: 'app' })
  })

  it('classifies empty string as info/app', () => {
    expect(classify('')).toEqual({ level: 'info', source: 'app' })
  })

  it('classifies "failed" line as error', () => {
    expect(classify('build failed')).toEqual({ level: 'error', source: 'app' })
  })

  it('orbit-prefixed warning still classifies as warn', () => {
    expect(classify('[orbit] warning: reconnect failed')).toEqual({ level: 'warn', source: 'orbit' })
  })

  it('orbit-prefixed error still classifies as error', () => {
    expect(classify('[orbit] error: orchestrator fatal')).toEqual({ level: 'error', source: 'orbit' })
  })

  it('Serilog [INF] line stays info even when message mentions Exception', () => {
    expect(classify('[22:47:59.488 INF] Exception caught in middleware, "..."')).toEqual({ level: 'info', source: 'app' })
  })

  it('Serilog [WAR] line is warn even when message mentions Exception', () => {
    expect(classify('[22:47:59.489 WAR] "Microsoft.AspNetCore.Http.BadHttpRequestException": "..."')).toEqual({ level: 'warn', source: 'app' })
  })

  it('Serilog [ERR] line is error', () => {
    expect(classify('[22:47:59.500 ERR] something blew up')).toEqual({ level: 'error', source: 'app' })
  })

  it('Serilog [FTL] line is error', () => {
    expect(classify('[22:47:59.500 FTL] fatal: process exiting')).toEqual({ level: 'error', source: 'app' })
  })

  it('Serilog [DBG] line is info', () => {
    expect(classify('[22:47:59.500 DBG] handler invoked')).toEqual({ level: 'info', source: 'app' })
  })

  it('full-word [INFO] prefix is info even when message says Exception', () => {
    expect(classify('[2026-05-06 INFO] Exception thrown but recovered')).toEqual({ level: 'info', source: 'app' })
  })

  it('full-word [WARNING] prefix is warn', () => {
    expect(classify('[2026-05-06 WARNING] connection retry')).toEqual({ level: 'warn', source: 'app' })
  })

  it('full-word [ERROR] prefix is error', () => {
    expect(classify('[2026-05-06 ERROR] db down')).toEqual({ level: 'error', source: 'app' })
  })

  it('full-word [FATAL] prefix is error', () => {
    expect(classify('[2026-05-06 FATAL] panic exiting')).toEqual({ level: 'error', source: 'app' })
  })

  it('level token must be inside leading brackets, not in message body', () => {
    // "ERROR" appears in the message, but the prefix is [orbit] (no level
    // token); falls through to keyword scan, which IS allowed to flag it.
    expect(classify('[orbit] hit ERROR while starting')).toEqual({ level: 'error', source: 'orbit' })
    // Plain text with ERROR mid-line, no leading bracket prefix at all —
    // also handled by keyword scan, not by the bracket-prefix rule.
    expect(classify('we hit a fatal ERROR somewhere')).toEqual({ level: 'error', source: 'app' })
  })
})

describe('findErrorIndices', () => {
  it('returns indices of error lines only', () => {
    const lines = [
      '[orbit] starting',
      'error: boom',
      'Now listening',
      'warning: small',
      'System.Exception: blew up',
    ]
    expect(findErrorIndices(lines)).toEqual([1, 4])
  })

  it('returns empty array when no errors', () => {
    expect(findErrorIndices(['hello', '[orbit] info'])).toEqual([])
  })
})

describe('groupErrors', () => {
  it('returns empty for empty input', () => {
    expect(groupErrors([])).toEqual([])
  })

  it('returns empty when no errors', () => {
    expect(groupErrors(['[INF] foo', '[INF] bar'])).toEqual([])
  })

  it('absorbs indented stack frames into the previous error', () => {
    const lines = [
      '[INF] starting',
      '[ERR] Middleware default exception ... System.InvalidOperationException: boom',
      '   at Worker.EndPoints.Vip.GetSetting(...)',
      '   at lambda_method469(...)',
      '   at Microsoft.AspNetCore.Http.RequestDelegateFactory(...)',
      '[INF] AlertService',
    ]
    expect(groupErrors(lines)).toEqual([1])
  })

  it('absorbs Caused by: continuation', () => {
    const lines = [
      '[ERR] outer failure',
      'Caused by: java.lang.NullPointerException',
      '   at com.foo.Bar(Bar.java:10)',
    ]
    expect(groupErrors(lines)).toEqual([0])
  })

  it('absorbs tab-indented (Java/Kotlin) stack frames', () => {
    const lines = [
      '[ERR] head',
      '\tat com.example.Foo.bar(Foo.java:42)',
      '\tat com.example.Foo.baz(Foo.java:18)',
    ]
    expect(groupErrors(lines)).toEqual([0])
  })

  it('a new bracket-prefixed line ends the group', () => {
    const lines = [
      '[ERR] err 1',
      '   at Foo.Bar()',
      '[ERR] err 2',
      '   at Foo.Baz()',
    ]
    expect(groupErrors(lines)).toEqual([0, 2])
  })

  it('empty line ends the group', () => {
    const lines = [
      '[ERR] err 1',
      '   at Foo.Bar()',
      '',
      '[ERR] err 2',
    ]
    expect(groupErrors(lines)).toEqual([0, 3])
  })

  it('two adjacent error heads each count as a group', () => {
    const lines = [
      '[ERR] err 1',
      '[ERR] err 2',
    ]
    expect(groupErrors(lines)).toEqual([0, 1])
  })

  it('error head followed by an info bracket line is one group', () => {
    const lines = [
      '[ERR] err',
      '[INF] next thing',
    ]
    expect(groupErrors(lines)).toEqual([0])
  })

  it('user real-world example: error block plus stack frames is one group', () => {
    const lines = [
      '[23:52:48.381 INF] "MongoDatabaseService" is initializing... {...}',
      '[23:52:48.461 INF] Exception caught in middleware, "intentional 500 test" {...}',
      '[23:52:48.462 ERR] Middleware default exception {...} System.InvalidOperationException: intentional 500 test',
      '   at Worker.EndPoints.Vip.VipEndPoint.GetSetting(...) in /Users/example/...:line 27',
      '   at lambda_method469(Closure, EndpointFilterInvocationContext)',
      '   at ExampleTeam.Core.Filters.ModelValidation.InvokeAsync(...)',
      '   at Microsoft.AspNetCore.Http.RequestDelegateFactory.<ExecuteValueTaskOfObject>g__ExecuteAwaited|128_0(...)',
      '   at Worker.Middlewares.ExceptionMiddleware.InvokeAsync(...) in /Users/example/...:line 18',
      '[23:52:48.465 INF] AlertService (Console): "🚨 Middleware default exception" {...}',
    ]
    expect(groupErrors(lines)).toEqual([2])
  })
})

describe('errorGroupMap', () => {
  it('returns -1 for empty input', () => {
    expect(errorGroupMap([])).toEqual([])
  })

  it('returns -1 for lines outside any error group', () => {
    expect(errorGroupMap(['[INF] one', '[INF] two'])).toEqual([-1, -1])
  })

  it('maps head and absorbed continuations to the head index', () => {
    const lines = [
      '[INF] noise',                                              // 0 → -1
      '[ERR] head',                                               // 1 → 1
      '   at Foo.Bar()',                                          // 2 → 1
      '   at Baz.Qux()',                                          // 3 → 1
      '[INF] AlertService',                                       // 4 → -1 (new bracket ends group)
    ]
    expect(errorGroupMap(lines)).toEqual([-1, 1, 1, 1, -1])
  })

  it('two adjacent error heads each map to themselves', () => {
    expect(errorGroupMap(['[ERR] one', '[ERR] two'])).toEqual([0, 1])
  })

  it('empty line ends a group', () => {
    const lines = ['[ERR] head', '   at A.B()', '', '   at C.D()']
    // After the blank line, "   at C.D()" has no preceding head, so -1.
    expect(errorGroupMap(lines)).toEqual([0, 0, -1, -1])
  })

  it('absorbs Caused by: continuation (symmetric to groupErrors)', () => {
    const lines = [
      '[ERR] outer',
      'Caused by: NullPointerException',
      '   at A.B()',
    ]
    expect(errorGroupMap(lines)).toEqual([0, 0, 0])
  })
})
